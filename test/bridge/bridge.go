package bridge

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"porttunnel/types"

	"github.com/google/uuid"
	"github.com/quic-go/quic-go"
)

// Bridge QUIC客户端，连接到Proxy服务器
type Bridge struct {
	ID               string
	connection       quic.Connection
	controlStream    quic.Stream
	dataStreams      sync.Map // streamID -> dataStream
	targets          sync.Map // "tcp:8080" -> *net.TCPConn / *net.UDPConn
	udpClients       sync.Map // "udpClient:127.0.0.1:12345" -> *net.UDPConn
	closed           bool
	closeOnce        sync.Once
	closeCh          chan struct{}
	mu               sync.RWMutex
	lastHeartbeatAck int64 // 上次收到心跳应答的时间戳
}

// NewBridge 创建一个新的Bridge实例
func NewBridge() *Bridge {
	return &Bridge{
		ID:               uuid.New().String(),
		closeCh:          make(chan struct{}),
		lastHeartbeatAck: time.Now().UnixNano(),
	}
}

// Start 启动Bridge，连接到Proxy并发送端口映射
func (b *Bridge) Start(proxyAddr string, portMappings []types.PortMapping) error {
	// TLS配置
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"quic-tunnel"},
	}

	// QUIC配置
	quicConfig := &quic.Config{}

	// 连接到Proxy
	conn, err := quic.DialAddr(context.Background(), proxyAddr, tlsConfig, quicConfig)
	if err != nil {
		return fmt.Errorf("连接Proxy失败: %w", err)
	}

	b.connection = conn
	log.Printf("已连接到Proxy: %s", proxyAddr)

	// 创建控制流
	stream, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		conn.CloseWithError(1, "创建控制流失败")
		return fmt.Errorf("创建控制流失败: %w", err)
	}

	b.controlStream = stream
	log.Printf("已创建控制流: %d", stream.StreamID())

	// 为每个端口映射添加bridgeID
	mappings := make([]types.PortMapping, 0, len(portMappings))
	for _, mapping := range portMappings {
		mapping.BridgeID = b.ID
		mappings = append(mappings, mapping)
	}

	// 发送注册消息
	msg := types.ControlMessage{
		Type: types.MessageTypeRegisterMappings,
		Payload: types.RegisterMappingsPayload{
			BridgeID: b.ID,
			Mappings: mappings,
		},
	}

	encoder := json.NewEncoder(b.controlStream)
	if err := encoder.Encode(msg); err != nil {
		conn.CloseWithError(1, "发送注册消息失败")
		return fmt.Errorf("发送注册消息失败: %w", err)
	}

	log.Printf("已发送端口映射注册消息，映射数量: %d", len(mappings))

	// 在后台处理控制流消息
	go b.handleControlMessages()

	// 在后台接受数据流
	go b.acceptDataStreams()

	// 启动心跳发送
	go b.sendHeartbeats()

	// 启动心跳超时检测
	go b.checkHeartbeatTimeout()

	return nil
}

// 发送心跳
func (b *Bridge) sendHeartbeats() {
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-b.closeCh:
			return
		case <-ticker.C:
			// 创建心跳消息
			heartbeat := types.ControlMessage{
				Type: "heartbeat",
				Payload: map[string]interface{}{
					"timestamp": time.Now().UnixNano(),
					"id":        b.ID,
				},
			}

			// 发送心跳
			b.mu.RLock()
			if b.controlStream != nil {
				encoder := json.NewEncoder(b.controlStream)
				err := encoder.Encode(heartbeat)
				if err != nil {
					log.Printf("发送心跳失败: %v", err)
					b.mu.RUnlock()
					b.Close()
					b.exitProgram()
					return
				}
			}
			b.mu.RUnlock()
		}
	}
}

// 检查心跳超时
func (b *Bridge) checkHeartbeatTimeout() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-b.closeCh:
			return
		case <-ticker.C:
			// 获取当前时间
			now := time.Now().UnixNano()
			// 获取上次心跳时间
			lastAck := atomic.LoadInt64(&b.lastHeartbeatAck)

			// 如果超过1秒没有收到心跳应答
			if now-lastAck > int64(time.Second) {
				log.Printf("心跳超时，上次心跳: %v, 当前时间: %v, 差值: %v ms",
					lastAck, now, (now-lastAck)/int64(time.Millisecond))
				b.Close()
				b.exitProgram()
				return
			}
		}
	}
}

// 处理来自Proxy的控制消息
func (b *Bridge) handleControlMessages() {
	decoder := json.NewDecoder(b.controlStream)

	for {
		select {
		case <-b.closeCh:
			return
		default:
			var msg types.ControlMessage
			if err := decoder.Decode(&msg); err != nil {
				if err == io.EOF {
					// EOF表示连接已正常关闭
					log.Printf("控制流收到EOF，连接已关闭")
				} else {
					// 其他错误可能表示连接异常断开
					log.Printf("读取控制消息失败: %v，连接可能已断开", err)
				}
				b.Close()
				b.exitProgram()
				return
			}

			// 更新最后收到心跳的时间
			atomic.StoreInt64(&b.lastHeartbeatAck, time.Now().UnixNano())

			// 处理消息
			switch msg.Type {
			case types.MessageTypeForwardData:
				b.handleForwardDataMessage(msg)
			case "heartbeat":
				// 收到心跳应答，不需要额外处理
			default:
				log.Printf("收到未知类型的消息: %s", msg.Type)
			}
		}
	}
}

// 处理转发数据的消息
func (b *Bridge) handleForwardDataMessage(msg types.ControlMessage) {
	// 将Payload转换为map
	payloadJSON, err := json.Marshal(msg.Payload)
	if err != nil {
		log.Printf("序列化payload失败: %v", err)
		return
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		log.Printf("解析ForwardData payload失败: %v", err)
		return
	}

	protocol, ok := payload["protocol"].(string)
	if !ok {
		log.Printf("无效的protocol字段")
		return
	}

	portValue, ok := payload["port"]
	if !ok {
		log.Printf("无效的port字段")
		return
	}

	port := fmt.Sprintf("%v", portValue)

	streamIDValue, ok := payload["stream_id"].(float64)
	if !ok {
		log.Printf("无效的stream_id字段")
		return
	}

	streamID := uint64(streamIDValue)
	portKey := fmt.Sprintf("%s:%s", protocol, port)

	// 查找或创建到目标的连接
	targetConn, err := b.getOrCreateTargetConnection(portKey, protocol, port, payload)
	if err != nil {
		log.Printf("创建目标连接失败: %v", err)
		return
	}

	// 获取数据流
	streamObj, ok := b.dataStreams.Load(streamID)
	if !ok {
		log.Printf("找不到streamID为%d的数据流", streamID)
		return
	}

	stream := streamObj.(quic.Stream)

	// 处理不同协议的数据转发
	if protocol == "tcp" {
		tcpConn := targetConn.(*net.TCPConn)

		// 双向复制数据
		go func() {
			_, err := io.Copy(stream, tcpConn)
			if err != nil && err != io.EOF {
				log.Printf("从TCP复制到QUIC流失败: %v", err)
			}
			tcpConn.Close()
			stream.Close()
		}()

		go func() {
			_, err := io.Copy(tcpConn, stream)
			if err != nil && err != io.EOF {
				log.Printf("从QUIC流复制到TCP失败: %v", err)
			}
			stream.Close()
			tcpConn.Close()
		}()
	} else if protocol == "udp" {
		// UDP需要特殊处理
		udpConn := targetConn.(*net.UDPConn)
		clientAddrStr, hasClientAddr := payload["client_addr"].(string)

		if !hasClientAddr {
			log.Printf("UDP转发缺少client_addr字段")
			return
		}

		// 创建一个缓冲来存储和转发UDP数据
		buffer := make([]byte, 4096)

		// 从QUIC流读取并发送到UDP目标
		go func() {
			for {
				n, err := stream.Read(buffer)
				if err != nil {
					if err == io.EOF {
						log.Printf("QUIC流已关闭(EOF)")
					} else {
						log.Printf("从QUIC流读取失败: %v", err)
					}
					break
				}

				_, err = udpConn.Write(buffer[:n])
				if err != nil {
					log.Printf("写入UDP失败: %v", err)
					break
				}
			}
		}()

		// 从UDP目标读取并发送到QUIC流
		clientAddr, err := net.ResolveUDPAddr("udp", clientAddrStr)
		if err != nil {
			log.Printf("解析客户端地址失败: %v", err)
			return
		}

		// 使用goroutine从UDP读取并写入QUIC流
		go func() {
			for {
				udpConn.SetReadDeadline(time.Now().Add(10 * time.Second))
				n, addr, err := udpConn.ReadFromUDP(buffer)
				if err != nil {
					netErr, ok := err.(net.Error)
					if ok && netErr.Timeout() {
						// 超时，继续等待
						continue
					}
					log.Printf("读取UDP失败: %v", err)
					break
				}

				// 只处理来自目标地址的数据
				if addr.String() == clientAddr.String() {
					_, err = stream.Write(buffer[:n])
					if err != nil {
						log.Printf("写入QUIC流失败: %v", err)
						break
					}
				}
			}
		}()
	}
}

// 获取或创建到目标的连接
func (b *Bridge) getOrCreateTargetConnection(portKey, protocol, portStr string, payload map[string]interface{}) (interface{}, error) {
	// 从Proxy的端口映射找到对应的目标地址和端口
	val, exists := b.targets.Load(portKey)
	if exists {
		return val, nil
	}

	// 在PortMappings中查找对应的映射
	targetAddr, targetPort := b.findTargetForPort(portKey)
	if targetAddr == "" || targetPort == 0 {
		return nil, fmt.Errorf("找不到端口%s的目标", portKey)
	}

	var conn interface{}
	var err error

	if protocol == "tcp" {
		// 创建TCP连接
		tcpAddr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf("%s:%d", targetAddr, targetPort))
		if err != nil {
			return nil, err
		}

		tcpConn, err := net.DialTCP("tcp", nil, tcpAddr)
		if err != nil {
			return nil, err
		}

		conn = tcpConn
		b.targets.Store(portKey, tcpConn)
	} else if protocol == "udp" {
		// 创建UDP连接
		udpAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", targetAddr, targetPort))
		if err != nil {
			return nil, err
		}

		udpConn, err := net.DialUDP("udp", nil, udpAddr)
		if err != nil {
			return nil, err
		}

		conn = udpConn
		b.targets.Store(portKey, udpConn)
	} else {
		return nil, fmt.Errorf("不支持的协议: %s", protocol)
	}

	return conn, err
}

// 查找端口对应的目标
func (b *Bridge) findTargetForPort(portKey string) (string, int) {
	// 这里应该根据实际情况从配置中查找
	// 简化起见，先使用硬编码返回值
	var protocol string
	var port int
	fmt.Sscanf(portKey, "%s:%d", &protocol, &port)

	// 从main.go中传入的portMappings查找
	// 这里实际应用中应该保存这些映射信息
	return "localhost", 8888 // 示例值，实际应用中应该根据真实映射返回
}

// 接受数据流
func (b *Bridge) acceptDataStreams() {
	for {
		select {
		case <-b.closeCh:
			return
		default:
			stream, err := b.connection.AcceptStream(context.Background())
			if err != nil {
				log.Printf("bridge 接受数据流失败: %v，连接可能已断开", err)
				b.Close()
				b.exitProgram()
				return
			}

			log.Printf("接受数据流: %d", stream.StreamID())
			b.dataStreams.Store(stream.StreamID(), stream)
		}
	}
}

// 程序退出
func (b *Bridge) exitProgram() {
	log.Println("检测到连接断开，程序即将退出...")
	b.Close()
	// 给日志一点时间写入
	time.Sleep(500 * time.Millisecond)
	os.Exit(1)
}

// Close 关闭Bridge
func (b *Bridge) Close() {
	b.closeOnce.Do(func() {
		b.mu.Lock()
		b.closed = true
		b.mu.Unlock()

		close(b.closeCh)

		// 关闭所有连接
		b.dataStreams.Range(func(key, value interface{}) bool {
			stream := value.(quic.Stream)
			stream.Close()
			return true
		})

		b.targets.Range(func(key, value interface{}) bool {
			switch conn := value.(type) {
			case *net.TCPConn:
				conn.Close()
			case *net.UDPConn:
				conn.Close()
			}
			return true
		})

		if b.controlStream != nil {
			b.controlStream.Close()
		}

		if b.connection != nil {
			b.connection.CloseWithError(0, "Bridge正常关闭")
		}

		log.Printf("Bridge已关闭: %s", b.ID)
	})
}

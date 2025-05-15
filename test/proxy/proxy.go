package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"porttunnel/types"

	"github.com/quic-go/quic-go"
)

// Proxy QUIC代理服务器
type Proxy struct {
	listener      *quic.Listener
	bridges       map[string]*Bridge      // bridgeID -> Bridge
	portListeners map[string]net.Listener // "tcp:8080" -> listener
	udpListeners  map[string]*net.UDPConn // "udp:8080" -> conn
	portBridges   map[string][]string     // "tcp:8080" -> [bridgeID1, bridgeID2]
	mu            sync.RWMutex
}

// Bridge 表示一个已连接的Bridge
type Bridge struct {
	ID            string
	Connection    quic.Connection
	ControlStream quic.Stream
	DataStreams   map[string]quic.Stream // "tcp:8080" -> stream
	LastHeartbeat int64                  // 最后一次收到心跳的时间戳
	mu            sync.RWMutex
}

// NewProxy 创建新的Proxy实例
func NewProxy() *Proxy {
	return &Proxy{
		bridges:       make(map[string]*Bridge),
		portListeners: make(map[string]net.Listener),
		udpListeners:  make(map[string]*net.UDPConn),
		portBridges:   make(map[string][]string),
	}
}

// Start 启动Proxy服务
func (p *Proxy) Start(port int) error {
	// 生成TLS配置
	tlsConfig := generateTLSConfig()

	// 设置QUIC配置
	quicConfig := &quic.Config{}

	// 启动QUIC监听器
	addr := fmt.Sprintf("0.0.0.0:%d", port)
	var err error
	p.listener, err = quic.ListenAddr(addr, tlsConfig, quicConfig)
	if err != nil {
		log.Printf("启动QUIC监听失败: %v", err)
		os.Exit(1)
	}

	log.Printf("代理服务器启动在端口 %d", port)

	// 在后台监听连接
	go p.acceptConnections()
	return nil
}

// 接受新的连接
func (p *Proxy) acceptConnections() {
	for {
		conn, err := p.listener.Accept(context.Background())
		if err != nil {
			log.Printf("接受连接失败: %v", err)
			continue
		}

		log.Printf("接受了新的Bridge连接: %s", conn.RemoteAddr())

		// 为每个连接创建一个新的goroutine
		go p.handleConnection(conn)
	}
}

// 处理一个QUIC连接
func (p *Proxy) handleConnection(conn quic.Connection) {
	// 等待控制流
	stream, err := conn.AcceptStream(context.Background())
	if err != nil {
		log.Printf("接受控制流失败: %v", err)
		conn.CloseWithError(1, "控制流创建失败")
		return
	}

	log.Printf("接受控制流: %d", stream.StreamID())

	// 等待Bridge发送注册信息
	var msg types.ControlMessage
	decoder := json.NewDecoder(stream)
	if err := decoder.Decode(&msg); err != nil {
		log.Printf("解析控制消息失败: %v", err)
		conn.CloseWithError(1, "解析控制消息失败")
		return
	}

	if msg.Type != types.MessageTypeRegisterMappings {
		log.Printf("预期收到注册消息，实际收到: %s", msg.Type)
		conn.CloseWithError(1, "预期收到注册消息")
		return
	}

	// 处理注册信息
	payloadJSON, err := json.Marshal(msg.Payload)
	if err != nil {
		log.Printf("序列化payload失败: %v", err)
		return
	}

	var registerPayload types.RegisterMappingsPayload
	if err := json.Unmarshal(payloadJSON, &registerPayload); err != nil {
		log.Printf("解析RegisterMappingsPayload失败: %v", err)
		return
	}

	// 注册Bridge
	bridge := &Bridge{
		ID:            registerPayload.BridgeID,
		Connection:    conn,
		ControlStream: stream,
		DataStreams:   make(map[string]quic.Stream),
		LastHeartbeat: time.Now().UnixNano(), // 初始化心跳时间戳
	}

	p.mu.Lock()
	p.bridges[bridge.ID] = bridge
	p.mu.Unlock()

	log.Printf("Bridge注册成功: %s, 映射端口数: %d", bridge.ID, len(registerPayload.Mappings))

	// 处理端口映射
	for _, mapping := range registerPayload.Mappings {
		p.handlePortMapping(bridge, mapping)
	}

	// 监听控制流消息
	go p.handleControlMessages(bridge)
}

// 处理单个端口映射
func (p *Proxy) handlePortMapping(bridge *Bridge, mapping types.PortMapping) {
	portKey := fmt.Sprintf("%s:%d", mapping.Protocol, mapping.ServerPort)

	p.mu.Lock()
	defer p.mu.Unlock()

	// 将Bridge添加到端口映射表
	p.portBridges[portKey] = append(p.portBridges[portKey], bridge.ID)

	// 检查是否已经有这个端口的监听器
	if mapping.Protocol == "tcp" {
		if _, exists := p.portListeners[portKey]; !exists {
			// 创建新的TCP监听器
			go p.createTCPListener(mapping.ServerPort, portKey)
		}
	} else if mapping.Protocol == "udp" {
		if _, exists := p.udpListeners[portKey]; !exists {
			// 创建新的UDP监听器
			go p.createUDPListener(mapping.ServerPort, portKey)
		}
	}
}

// 创建TCP监听器
func (p *Proxy) createTCPListener(port int, portKey string) {
	listener, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		log.Printf("创建TCP监听器失败: %v", err)
		return
	}

	p.mu.Lock()
	p.portListeners[portKey] = listener
	p.mu.Unlock()

	log.Printf("创建TCP监听器: %s", portKey)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("接受TCP连接失败: %v", err)
			continue
		}

		go p.handleTCPConnection(conn, portKey)
	}
}

// 创建UDP监听器
func (p *Proxy) createUDPListener(port int, portKey string) {
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		log.Printf("解析UDP地址失败: %v", err)
		return
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Printf("创建UDP监听器失败: %v", err)
		return
	}

	p.mu.Lock()
	p.udpListeners[portKey] = conn
	p.mu.Unlock()

	log.Printf("创建UDP监听器: %s", portKey)

	buffer := make([]byte, 4096)
	for {
		n, clientAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			log.Printf("读取UDP数据失败: %v", err)
			continue
		}

		go p.handleUDPPacket(conn, clientAddr, buffer[:n], portKey)
	}
}

// 处理TCP连接
func (p *Proxy) handleTCPConnection(conn net.Conn, portKey string) {
	defer conn.Close()

	// 负载均衡选择Bridge
	bridgeID := p.selectBridge(portKey)
	if bridgeID == "" {
		log.Printf("没有可用的Bridge处理端口: %s", portKey)
		return
	}

	p.mu.RLock()
	bridge, exists := p.bridges[bridgeID]
	p.mu.RUnlock()

	if !exists {
		log.Printf("找不到Bridge: %s", bridgeID)
		return
	}

	// 获取或创建数据流
	dataStream, err := p.getOrCreateDataStream(bridge, portKey)
	if err != nil {
		log.Printf("获取数据流失败: %v", err)
		return
	}

	// 生成唯一的流ID用于关联请求和响应
	streamID := dataStream.StreamID()

	// 发送转发请求
	protocol, port := getProtocolAndPort(portKey)
	msg := types.ControlMessage{
		Type: types.MessageTypeForwardData,
		Payload: map[string]interface{}{
			"protocol":  protocol,
			"port":      port,
			"stream_id": streamID,
		},
	}

	encoder := json.NewEncoder(bridge.ControlStream)
	if err := encoder.Encode(msg); err != nil {
		log.Printf("发送转发请求失败: %v", err)
		return
	}

	log.Printf("TCP转发请求已发送: %s -> Bridge[%s], StreamID: %d", portKey, bridgeID, streamID)

	// 在TCP连接和QUIC流之间双向复制数据
	errChan := make(chan error, 2)

	// 从TCP客户端读取数据写入QUIC流
	go func() {
		_, err := io.Copy(dataStream, conn)
		if err != nil && err != io.EOF {
			log.Printf("从TCP客户端复制到QUIC流失败: %v", err)
		}
		// 关闭QUIC流的写入方向，表示没有更多数据要发送
		dataStream.Close()
		errChan <- err
	}()

	// 从QUIC流读取数据写入TCP客户端
	go func() {
		_, err := io.Copy(conn, dataStream)
		if err != nil && err != io.EOF {
			log.Printf("从QUIC流复制到TCP客户端失败: %v", err)
		}
		// 关闭TCP连接的写入方向
		conn.(*net.TCPConn).CloseWrite()
		errChan <- err
	}()

	// 等待任一方向的数据复制完成或出错
	<-errChan
	log.Printf("TCP连接处理完成: %s, StreamID: %d", portKey, streamID)
}

// 处理UDP数据包
func (p *Proxy) handleUDPPacket(conn *net.UDPConn, clientAddr *net.UDPAddr, data []byte, portKey string) {
	// 负载均衡选择Bridge
	bridgeID := p.selectBridge(portKey)
	if bridgeID == "" {
		log.Printf("没有可用的Bridge处理端口: %s", portKey)
		return
	}

	p.mu.RLock()
	bridge, exists := p.bridges[bridgeID]
	p.mu.RUnlock()

	if !exists {
		log.Printf("找不到Bridge: %s", bridgeID)
		return
	}

	// 为每个客户端地址创建一个会话密钥
	sessionKey := fmt.Sprintf("%s:%s", portKey, clientAddr.String())

	// 获取或创建关联的数据流
	bridge.mu.Lock()
	dataStream, ok := bridge.DataStreams[sessionKey]
	if !ok {
		var err error
		dataStream, err = bridge.Connection.OpenStreamSync(context.Background())
		if err != nil {
			bridge.mu.Unlock()
			log.Printf("为UDP会话创建流失败: %v", err)
			return
		}
		bridge.DataStreams[sessionKey] = dataStream
		log.Printf("为UDP客户端 %s 创建新流: %d", clientAddr.String(), dataStream.StreamID())

		// 启动一个协程处理从Bridge返回的UDP数据
		go p.handleUDPResponse(conn, clientAddr, dataStream, sessionKey, bridge)
	}
	bridge.mu.Unlock()

	// 发送转发请求
	protocol, port := getProtocolAndPort(portKey)
	msg := types.ControlMessage{
		Type: types.MessageTypeForwardData,
		Payload: map[string]interface{}{
			"protocol":    protocol,
			"port":        port,
			"stream_id":   dataStream.StreamID(),
			"client_addr": clientAddr.String(),
			"session_key": sessionKey,
		},
	}

	encoder := json.NewEncoder(bridge.ControlStream)
	if err := encoder.Encode(msg); err != nil {
		log.Printf("发送UDP转发请求失败: %v", err)
		return
	}

	// 将收到的UDP数据发送到QUIC流
	_, err := dataStream.Write(data)
	if err != nil {
		log.Printf("写入UDP数据到QUIC流失败: %v", err)

		// 清理资源
		bridge.mu.Lock()
		delete(bridge.DataStreams, sessionKey)
		bridge.mu.Unlock()

		dataStream.Close()
		return
	}

	log.Printf("已转发UDP包: %s -> Bridge[%s], 客户端: %s, 数据大小: %d字节",
		portKey, bridgeID, clientAddr.String(), len(data))
}

// 处理从Bridge返回的UDP响应
func (p *Proxy) handleUDPResponse(conn *net.UDPConn, clientAddr *net.UDPAddr, dataStream quic.Stream, sessionKey string, bridge *Bridge) {
	buffer := make([]byte, 4096)

	for {
		// 从QUIC流读取Bridge返回的数据
		n, err := dataStream.Read(buffer)
		if err != nil {
			if err != io.EOF {
				log.Printf("从QUIC流读取UDP响应失败: %v", err)
			}
			// 清理会话
			bridge.mu.Lock()
			delete(bridge.DataStreams, sessionKey)
			bridge.mu.Unlock()

			dataStream.Close()
			return
		}

		// 没有数据，继续等待
		if n == 0 {
			continue
		}

		// 将数据转发回UDP客户端
		_, err = conn.WriteToUDP(buffer[:n], clientAddr)
		if err != nil {
			log.Printf("发送UDP响应到客户端失败: %v", err)
			break
		}

		log.Printf("已转发UDP响应: Bridge -> %s, 客户端: %s, 数据大小: %d字节",
			clientAddr.String(), clientAddr.String(), n)
	}
}

// 负载均衡选择Bridge
func (p *Proxy) selectBridge(portKey string) string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	bridgeIDs, exists := p.portBridges[portKey]
	if !exists || len(bridgeIDs) == 0 {
		return ""
	}

	// 简单轮询负载均衡
	now := time.Now().UnixNano()
	index := now % int64(len(bridgeIDs))
	return bridgeIDs[index]
}

// 获取或创建数据流
func (p *Proxy) getOrCreateDataStream(bridge *Bridge, portKey string) (quic.Stream, error) {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()

	// 检查是否已有该端口的数据流
	if stream, exists := bridge.DataStreams[portKey]; exists {
		return stream, nil
	}

	// 创建新的数据流
	stream, err := bridge.Connection.OpenStreamSync(context.Background())
	if err != nil {
		return nil, err
	}

	bridge.DataStreams[portKey] = stream
	return stream, nil
}

// 处理控制流消息
func (p *Proxy) handleControlMessages(bridge *Bridge) {
	decoder := json.NewDecoder(bridge.ControlStream)

	// 启动心跳超时检测
	go p.checkBridgeHeartbeatTimeout(bridge)

	for {
		var msg types.ControlMessage
		if err := decoder.Decode(&msg); err != nil {
			if err != io.EOF {
				log.Printf("读取控制消息失败: %v", err)
			}
			p.removeBridge(bridge.ID)
			return
		}

		// 更新最后收到心跳的时间
		bridge.mu.Lock()
		bridge.LastHeartbeat = time.Now().UnixNano()
		bridge.mu.Unlock()

		// 处理各种控制消息
		switch msg.Type {
		case "heartbeat":
			// 发送心跳应答
			response := types.ControlMessage{
				Type: "heartbeat",
				Payload: map[string]interface{}{
					"timestamp": time.Now().UnixNano(),
					"ack":       true,
				},
			}

			encoder := json.NewEncoder(bridge.ControlStream)
			if err := encoder.Encode(response); err != nil {
				log.Printf("发送心跳应答失败: %v", err)
				p.removeBridge(bridge.ID)
				return
			}
		// 这里可以添加更多的消息类型处理
		default:
			log.Printf("收到未知类型的消息: %s", msg.Type)
		}
	}
}

// 检查bridge心跳超时
func (p *Proxy) checkBridgeHeartbeatTimeout(bridge *Bridge) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-time.After(time.Millisecond): // 非阻塞检查
			// 检查bridge是否已移除
			p.mu.RLock()
			_, exists := p.bridges[bridge.ID]
			p.mu.RUnlock()

			if !exists {
				return // bridge已被移除，停止检查
			}

			// 获取当前时间和上次心跳时间
			now := time.Now().UnixNano()
			bridge.mu.RLock()
			lastHeartbeat := bridge.LastHeartbeat
			bridgeID := bridge.ID
			bridge.mu.RUnlock()

			// 如果超过2秒没有收到心跳
			if now-lastHeartbeat > int64(2*time.Second) {
				log.Printf("Bridge %s 心跳超时，已断开连接", bridgeID)
				p.removeBridge(bridgeID)
				return
			}
		case <-ticker.C:
			// 定时检查
		}
	}
}

// 移除Bridge
func (p *Proxy) removeBridge(bridgeID string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	bridge, exists := p.bridges[bridgeID]
	if !exists {
		return
	}

	// 关闭所有数据流
	bridge.mu.Lock()
	for _, stream := range bridge.DataStreams {
		stream.Close()
	}
	bridge.mu.Unlock()

	// 从端口映射表中移除
	for portKey, ids := range p.portBridges {
		var newIDs []string
		for _, id := range ids {
			if id != bridgeID {
				newIDs = append(newIDs, id)
			}
		}

		if len(newIDs) == 0 {
			// 如果没有Bridge处理这个端口，关闭监听器
			if listener, ok := p.portListeners[portKey]; ok {
				listener.Close()
				delete(p.portListeners, portKey)
			}

			if conn, ok := p.udpListeners[portKey]; ok {
				conn.Close()
				delete(p.udpListeners, portKey)
			}

			delete(p.portBridges, portKey)
		} else {
			p.portBridges[portKey] = newIDs
		}
	}

	// 从Bridge表中删除
	delete(p.bridges, bridgeID)
	log.Printf("Bridge已移除: %s", bridgeID)
}

// 辅助函数：从portKey中获取协议和端口
func getProtocolAndPort(portKey string) (protocol string, port string) {
	var proto, portStr string
	fmt.Sscanf(portKey, "%s:%s", &proto, &portStr)
	return proto, portStr
}

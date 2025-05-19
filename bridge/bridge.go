package bridge

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"porttunnel/help"
	"time"

	"github.com/quic-go/quic-go"
)

// Bridge QUIC客户端，连接到Proxy服务器
type Bridge struct {
	connection    quic.Connection
	controlStream quic.Stream
}

// NewBridge 创建一个新的Bridge实例
func NewBridge() *Bridge {
	return &Bridge{}
}

// Start 启动Bridge，连接到Proxy并发送端口映射
func (b *Bridge) Start(proxyAddr string, portMappings []help.PortMappingConfig) error {
	// TLS配置
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"quic-tunnel"},
	}

	// QUIC配置
	quicConfig := &quic.Config{}
	log.Println("开始连接到Proxy: %s", proxyAddr)

	connected := false
	go func() {
		time.Sleep(time.Second * 10)
		if !connected {
			log.Println("连接Proxy超时，程序退出")
			os.Exit(1)
		}
	}()
	// 连接到Proxy
	conn, err := quic.DialAddr(context.Background(), proxyAddr, tlsConfig, quicConfig)
	if err != nil {
		return fmt.Errorf("连接Proxy失败: %w", err)
	}
	connected = true

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

	// 发送注册消息
	msg := help.ControlMessage{
		Type:    help.MessageTypeRegisterMappings,
		Payload: help.RegisterMappingsPayload{Mappings: portMappings},
	}

	encoder := json.NewEncoder(b.controlStream)
	if err := encoder.Encode(msg); err != nil {
		conn.CloseWithError(1, "发送注册消息失败")
		log.Println("发送注册消息失败，程序3秒后退出")
		time.Sleep(time.Second * 10)
		os.Exit(1)
	}

	log.Printf("已发送端口映射注册消息，映射数量: %d", len(portMappings))
	// 打印注册的端口信息
	for _, mapping := range portMappings {
		log.Printf("注册的端口信息: %s:%d -> %s:%d", mapping.Protocol, mapping.ServerPort, mapping.ResourceAddr, mapping.ResourcePort)
	}

	// // 在后台处理控制流消息
	// go b.handleControlMessages()

	// 在后台接受数据流
	go b.acceptDataStreams()

	// 启动心跳发送
	go b.sendHeartbeats()

	return nil
}

func (b *Bridge) acceptDataStreams() {
	errcount := 0
	for {
		stream, err := b.connection.AcceptStream(context.Background())
		if err != nil {
			log.Printf("bridge 接受数据流失败: %w, errcount: %d", err, errcount)
			errcount++
			if errcount > 10 {
				log.Println("接受数据流失败次数过多，程序退出")
				os.Exit(1)
			}
			time.Sleep(1 * time.Second)
			continue
		}
		errcount = 0
		go b.handleDataStream(stream)
	}
}

func (b *Bridge) handleDataStream(stream quic.Stream) {
	log.Printf("Bridge 接受数据流: %d", stream.StreamID())

	// 读取ForwardDataPayload
	var msg help.ControlMessage
	decoder := json.NewDecoder(stream)
	if err := decoder.Decode(&msg); err != nil {
		log.Printf("Bridge 读取ControlMessage失败: %w", err)
		stream.Close()
		return
	}

	if msg.Type != help.MessageTypeForwardData {
		log.Printf("Bridge 预期收到ForwardDataPayload，实际收到: %s", msg.Type)
		stream.Close()
		return
	}
	// 读取ForwardDataPayload
	// 从msg.Payload中读取ForwardDataPayload
	payloadJSON, err := json.Marshal(msg.Payload)
	if err != nil {
		log.Printf("Bridge 序列化payload失败: %w", err)
		stream.Close()
		return
	}
	var payload help.ForwardDataPayload
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		log.Printf("Bridge 读取ForwardDataPayload失败: %w", err)
		stream.Close()
		return
	}

	log.Printf("Bridge 读取ForwardDataPayload成功: %v", payload)

	// 根据payload.Protocol选择处理方式
	switch payload.Mapping.Protocol {
	case "tcp":
		b.handleTCPStream(stream, &payload.Mapping)
	case "udp":
		log.Printf("Bridge UDP代理暂未实现")
		stream.Close()
		return
	default:
		log.Printf("Bridge 未知协议: %s", payload.Mapping.Protocol)
		stream.Close()
		return
	}
}

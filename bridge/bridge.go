package bridge

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
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

	// 发送注册消息
	msg := help.ControlMessage{
		Type:    help.MessageTypeRegisterMappings,
		Payload: help.RegisterMappingsPayload{Mappings: portMappings},
	}

	encoder := json.NewEncoder(b.controlStream)
	if err := encoder.Encode(msg); err != nil {
		conn.CloseWithError(1, "发送注册消息失败")
		return fmt.Errorf("发送注册消息失败: %w", err)
	}

	log.Printf("已发送端口映射注册消息，映射数量: %d", len(portMappings))

	// // 在后台处理控制流消息
	// go b.handleControlMessages()

	// 在后台接受数据流
	go b.acceptDataStreams()

	// // 启动心跳发送
	// go b.sendHeartbeats()

	// // 启动心跳超时检测
	// go b.checkHeartbeatTimeout()

	return nil
}

func (b *Bridge) acceptDataStreams() {
	for {
		stream, err := b.connection.AcceptStream(context.Background())
		if err != nil {
			log.Printf("bridge 接受数据流失败: %w", err)
			time.Sleep(1 * time.Second)
			continue
		}

		go b.handleDataStream(stream)
	}
}

func (b *Bridge) handleDataStream(stream quic.Stream) {
	log.Printf("接受数据流: %d", stream.StreamID())

	// 读取ForwardDataPayload
	var payload help.ForwardDataPayload
	decoder := json.NewDecoder(stream)
	if err := decoder.Decode(&payload); err != nil {
		log.Printf("读取ForwardDataPayload失败: %w", err)
		return
	}

	log.Printf("读取ForwardDataPayload成功: %v", payload)

	// 根据payload.Protocol选择处理方式
	switch payload.Mapping.Protocol {
	case "tcp":
		b.handleTCPStream(stream, &payload.Mapping)
	}
}

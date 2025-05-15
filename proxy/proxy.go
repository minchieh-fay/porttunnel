package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"porttunnel/help"
	"porttunnel/proxy/tcpproxymanager"

	"github.com/quic-go/quic-go"
)

// Proxy QUIC代理服务器
type Proxy struct {
	listener        *quic.Listener
	tcpProxyManager *tcpproxymanager.TCPProxyManager
}

func NewProxy() *Proxy {
	return &Proxy{
		tcpProxyManager: tcpproxymanager.NewTCPProxyManager(),
	}
}

func (p *Proxy) Start(port int) error {
	// 生成TLS配置
	tlsConfig := help.GenerateTLSConfig()
	// 设置QUIC配置
	quicConfig := &quic.Config{}

	log.Printf("Proxy: 开始启动QUIC监听器于端口: %d", port)
	// 启动QUIC监听器
	addr := fmt.Sprintf("0.0.0.0:%d", port)
	var err error
	p.listener, err = quic.ListenAddr(addr, tlsConfig, quicConfig)
	if err != nil {
		log.Printf("启动QUIC监听失败: %v", err)
		os.Exit(1)
	}
	// 在后台监听连接
	go p.acceptConnections()
	return nil
}

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

func (p *Proxy) handleConnection(conn quic.Connection) {
	// 处理连接
	// 等待控制流
	stream, err := conn.AcceptStream(context.Background())
	if err != nil {
		log.Printf("接受控制流失败: %v", err)
		conn.CloseWithError(1, "控制流创建失败")
		return
	}

	log.Printf("接受控制流: %d", stream.StreamID())

	// 等待Bridge发送注册信息
	var msg help.ControlMessage
	decoder := json.NewDecoder(stream)
	if err := decoder.Decode(&msg); err != nil {
		log.Printf("解析控制消息失败: %v", err)
		conn.CloseWithError(1, "解析控制消息失败")
		return
	}

	if msg.Type != help.MessageTypeRegisterMappings {
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

	var registerPayload help.RegisterMappingsPayload
	if err := json.Unmarshal(payloadJSON, &registerPayload); err != nil {
		log.Printf("解析RegisterMappingsPayload失败: %v", err)
		return
	}

	log.Printf("注册信息: %v", registerPayload)

	for _, mapping := range registerPayload.Mappings {
		log.Printf("注册信息: %v", mapping)
		if mapping.Protocol == "tcp" {
			p.tcpProxyManager.Push(conn, &mapping)
			continue
		}

		if mapping.Protocol == "udp" {
			//p.udpProxyManager.Push(conn, &mapping)
			log.Printf("UDP代理暂未实现")
			continue
		}

		log.Printf("未知协议: %s", mapping.Protocol)
		continue
	}
}

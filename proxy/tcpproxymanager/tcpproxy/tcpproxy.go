package tcpproxy

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"porttunnel/help"
	"time"

	"github.com/quic-go/quic-go"
)

type MappingConfigWithQuicInfo struct {
	config   *help.PortMappingConfig
	quicConn quic.Connection
}

type TCPProxy struct {
	port     int
	listener *net.Listener
	// stream数组
	//streams []*quic.Stream
	configs []*MappingConfigWithQuicInfo
}

func NewTCPProxy(port int) *TCPProxy {
	return &TCPProxy{
		port:     port,
		listener: nil,
	}
}

func (p *TCPProxy) Push(conn quic.Connection, config *help.PortMappingConfig) error {
	mcqi := &MappingConfigWithQuicInfo{
		config:   config,
		quicConn: conn,
	}
	p.configs = append(p.configs, mcqi)
	return nil
}

func (p *TCPProxy) Start() error {
	// 创建listener
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", p.port))
	if err != nil {
		log.Printf("在端口%d创建tcp listener失败: %v", p.port, err)
		return err
	}
	p.listener = &listener

	// 启动listener
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				log.Printf("接受连接失败: %v", err)
				time.Sleep(1 * time.Second)
				continue
			}
			log.Printf("接受连接成功: %v", conn)
			go p.StreamCopy(conn)
		}
	}()

	return nil
}

var TcpCount int

func (p *TCPProxy) StreamCopy(conn net.Conn) error {
	if len(p.configs) == 0 {
		log.Printf("没有可用的bridge")
		return fmt.Errorf("没有可用的bridge")
	}

	// 通过conn的来源ip+端口  以及 configs长度 来确定使用哪个stream， 使用源hash等方法计算出一个
	// 使得来自同一个源的请求尽量使用同一个quic
	remoteAddr := conn.RemoteAddr().String()
	var hashVal uint32
	for _, c := range remoteAddr {
		hashVal = hashVal*31 + uint32(c)
	}
	index := int(hashVal) % len(p.configs)
	mcqi := p.configs[index]

	// 创建stream
	stream, err := mcqi.quicConn.OpenStream()

	if err != nil {
		log.Printf("打开流失败: %v %v", err, mcqi.config)
		p.deleteOneConfig(index)
		return p.StreamCopy(conn)
	}

	// 先发送一个控制包，告诉quic，这个stream是用来干啥的
	// 发送注册消息
	msg := help.ControlMessage{
		Type:    help.MessageTypeForwardData,
		Payload: help.ForwardDataPayload{Mapping: *mcqi.config},
	}
	encoder := json.NewEncoder(stream)
	if err := encoder.Encode(msg); err != nil {
		stream.Close()
		log.Printf("发送注册消息失败: %v,%v", mcqi, err)

		p.deleteOneConfig(index)
		return p.StreamCopy(conn)
	}

	// 开始拷贝数据  stream
	// stream->conn   conn->stream
	// 任意一个关闭，都关闭stream
	tmpcount := TcpCount
	TcpCount++

	done := make(chan bool)
	go func() {
		_, err := io.Copy(stream, conn)
		if err != nil {
			log.Printf("stream->conn %d 失败: %v", tmpcount, err)
		}
		done <- true
	}()
	go func() {
		_, err := io.Copy(conn, stream)
		if err != nil {
			log.Printf("conn->stream %d 失败: %v", tmpcount, err)
		}
		done <- true
	}()
	<-done
	stream.Close()
	conn.Close()
	log.Printf("stream<->conn %d 完成", tmpcount)
	return nil
}

package bridge

import (
	"fmt"
	"io"
	"log"
	"net"
	"porttunnel/help"

	"github.com/quic-go/quic-go"
)

var TcpCount int

func (b *Bridge) handleTCPStream(stream quic.Stream, mappingconfig *help.PortMappingConfig) {
	log.Printf("处理TCP流: %v", mappingconfig)

	// 从mappingconfig读取资源地址和端口
	resourceAddr := mappingconfig.ResourceAddr
	resourcePort := mappingconfig.ResourcePort

	// 连接资源地址和端口
	resourceConn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", resourceAddr, resourcePort))
	if err != nil {
		stream.Close()
		log.Printf("连接资源地址和端口失败: %v", err)
		return
	}

	tmpcount := TcpCount
	TcpCount++

	done := make(chan bool)
	go func() {
		_, err := io.Copy(stream, resourceConn)
		if err != nil {
			log.Printf("stream->conn %d 失败: %v", tmpcount, err)
		}
		done <- true
	}()
	go func() {
		_, err := io.Copy(resourceConn, stream)
		if err != nil {
			log.Printf("conn->stream %d 失败: %v", tmpcount, err)
		}
		done <- true
	}()
	<-done
	stream.Close()
	resourceConn.Close()
	log.Printf("stream<->conn %d 完成", tmpcount)
}

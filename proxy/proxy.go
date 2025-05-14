package proxy

import (
	"context"
	"fmt"

	"github.com/quic-go/quic-go"
)

type Proxy struct {
	bridgeConns []*BridgeConn
}

func NewProxy() *Proxy {
	return &Proxy{}
}

func (p *Proxy) Start(port int) {
	tlsConfig := generateTLSConfig()
	listener, err := quic.ListenAddr(fmt.Sprintf(":%d", port), tlsConfig, nil)
	if err != nil {
		panic(err)
	}
	defer listener.Close()

	for {
		conn, err := listener.Accept(context.Background())
		if err != nil {
			panic(err)
		}
		go p.handleConn(conn)
	}
}

func (p *Proxy) handleConn(conn quic.Connection) {
	stream, err := conn.AcceptStream(context.Background())
	if err != nil {
		panic(err)
	}
	bridgeConn := NewBridgeConn()
	p.bridgeConns = append(p.bridgeConns, bridgeConn)
	go bridgeConn.handleStream(stream)
}

package proxy

import "github.com/quic-go/quic-go"

type BridgeConn struct {
	stream quic.Stream
}

func NewBridgeConn() *BridgeConn {
	return &BridgeConn{}
}

func (b *BridgeConn) handleStream(stream quic.Stream) {

}

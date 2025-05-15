package tcpproxymanager

import (
	"porttunnel/help"
	"porttunnel/proxy/tcpproxymanager/tcpproxy"

	"github.com/quic-go/quic-go"
)

type TCPProxyManager struct {
	proxyMap map[int]*tcpproxy.TCPProxy
}

func NewTCPProxyManager() *TCPProxyManager {
	return &TCPProxyManager{
		proxyMap: make(map[int]*tcpproxy.TCPProxy),
	}
}

func (m *TCPProxyManager) Push(conn quic.Connection, config *help.PortMappingConfig) error {
	// 判断proxyMap
	proxy, ok := m.proxyMap[config.ServerPort]
	if !ok {
		// 创建proxy
		proxy = tcpproxy.NewTCPProxy(config.ServerPort)
		proxy.Start()
		m.proxyMap[config.ServerPort] = proxy
	}
	proxy.Push(conn, config)
	return nil
}

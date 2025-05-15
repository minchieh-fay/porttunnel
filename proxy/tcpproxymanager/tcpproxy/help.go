package tcpproxy

func (p *TCPProxy) deleteOneConfig(indx int) {
	p.configs = append(p.configs[:indx], p.configs[indx+1:]...)
}

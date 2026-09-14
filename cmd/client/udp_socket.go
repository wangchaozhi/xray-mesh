package main

import "net"

// openRelayUDPSocket creates an unconnected UDP socket in the same address
// family as the relay. Keeping it unconnected is required for NAT traversal:
// the coordinator observes this local port, and the same socket can later send
// authenticated probe datagrams to a peer candidate without creating a second
// NAT mapping.
func openRelayUDPSocket(remote *net.UDPAddr) (*net.UDPConn, error) {
	network := "udp4"
	local := &net.UDPAddr{IP: net.IPv4zero, Port: 0}
	if remote.IP.To4() == nil {
		network = "udp6"
		local = &net.UDPAddr{IP: net.IPv6unspecified, Port: 0}
	}
	return net.ListenUDP(network, local)
}

func sameUDPAddr(a, b *net.UDPAddr) bool {
	if a == nil || b == nil || a.Port != b.Port || a.Zone != b.Zone {
		return false
	}
	return a.IP.Equal(b.IP)
}

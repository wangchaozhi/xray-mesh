package router

import "net/netip"

var (
	mdnsIPv4 = netip.MustParseAddr("224.0.0.251")
	ssdpIPv4 = netip.MustParseAddr("239.255.255.250")
)

// PrefixClassifier sends traffic for the overlay prefix to peers, selected
// discovery multicast to the discovery relay, and everything else to the
// Internet egress path.
type PrefixClassifier struct {
	MeshPrefix netip.Prefix
}

func (c PrefixClassifier) Classify(p Packet) Class {
	if !p.Destination.IsValid() {
		return ClassUnknown
	}
	if p.Destination == mdnsIPv4 || p.Destination == ssdpIPv4 {
		return ClassDiscovery
	}
	if c.MeshPrefix.IsValid() && c.MeshPrefix.Contains(p.Destination) {
		return ClassPeer
	}
	return ClassInternet
}

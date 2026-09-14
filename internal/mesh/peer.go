package mesh

import "net/netip"

// Peer is a node participating in a mesh network.
type Peer struct {
	NodeID    string     `json:"node_id"`
	VirtualIP netip.Addr `json:"-"`
}

// PeerView is the JSON representation returned by the control plane.
type PeerView struct {
	NodeID    string `json:"node_id"`
	VirtualIP string `json:"virtual_ip"`
}

func (p Peer) View() PeerView {
	return PeerView{NodeID: p.NodeID, VirtualIP: p.VirtualIP.String()}
}

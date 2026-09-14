package mesh

import "net/netip"

// Peer is a node participating in a mesh network.
type Peer struct {
	NodeID       string     `json:"node_id"`
	VirtualIP    netip.Addr `json:"-"`
	SessionToken string     `json:"-"`
}

// PeerView is the JSON representation returned when listing peers.
type PeerView struct {
	NodeID    string `json:"node_id"`
	VirtualIP string `json:"virtual_ip"`
}

// RegistrationView is returned only to the registering peer. SessionToken is
// intentionally omitted from peer-list responses.
type RegistrationView struct {
	NodeID        string `json:"node_id"`
	VirtualIP     string `json:"virtual_ip"`
	NetworkPrefix string `json:"network_prefix"`
	SessionToken  string `json:"session_token"`
}

func (p Peer) View() PeerView {
	return PeerView{NodeID: p.NodeID, VirtualIP: p.VirtualIP.String()}
}

func (p Peer) Registration(prefix netip.Prefix) RegistrationView {
	return RegistrationView{
		NodeID:        p.NodeID,
		VirtualIP:     p.VirtualIP.String(),
		NetworkPrefix: prefix.String(),
		SessionToken:  p.SessionToken,
	}
}

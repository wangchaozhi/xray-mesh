package mesh

import (
	"net/netip"
	"time"
)

// Peer is a node participating in a mesh network.
type Peer struct {
	NodeID       string     `json:"node_id"`
	VirtualIP    netip.Addr `json:"-"`
	SessionToken string     `json:"-"`
	LastSeen     time.Time  `json:"-"`
}

// PeerView is the JSON representation returned when listing peers.
type PeerView struct {
	NodeID    string    `json:"node_id"`
	VirtualIP string    `json:"virtual_ip"`
	LastSeen  time.Time `json:"last_seen"`
}

// RegistrationView is returned only to the registering peer. SessionToken is
// intentionally omitted from peer-list responses.
type RegistrationView struct {
	NodeID                   string `json:"node_id"`
	VirtualIP                string `json:"virtual_ip"`
	NetworkPrefix            string `json:"network_prefix"`
	SessionToken             string `json:"session_token"`
	HeartbeatIntervalSeconds int    `json:"heartbeat_interval_seconds,omitempty"`
	LeaseTTLSeconds          int    `json:"lease_ttl_seconds,omitempty"`
}

func (p Peer) View() PeerView {
	return PeerView{NodeID: p.NodeID, VirtualIP: p.VirtualIP.String(), LastSeen: p.LastSeen}
}

func (p Peer) Registration(prefix netip.Prefix) RegistrationView {
	return RegistrationView{
		NodeID:        p.NodeID,
		VirtualIP:     p.VirtualIP.String(),
		NetworkPrefix: prefix.String(),
		SessionToken:  p.SessionToken,
	}
}

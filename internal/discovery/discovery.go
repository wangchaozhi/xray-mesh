package discovery

import (
	"context"
	"net/netip"
)

type Protocol string

const (
	MDNS Protocol = "mdns"
	SSDP Protocol = "ssdp"
)

// Message is a bounded discovery datagram that may be replicated to selected
// mesh peers. The future implementation must include loop prevention and rate
// limiting.
type Message struct {
	Protocol Protocol
	Source   netip.AddrPort
	Payload  []byte
}

// Relay forwards selected discovery messages across the mesh.
type Relay interface {
	Publish(context.Context, Message) error
	Subscribe(context.Context) (<-chan Message, error)
}

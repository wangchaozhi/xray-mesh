package router

import (
	"context"
	"net/netip"
)

// Packet is the minimum representation needed by the mesh router. Payload is
// expected to contain one complete layer-3 packet once the TUN implementation
// is connected.
type Packet struct {
	Source      netip.Addr
	Destination netip.Addr
	Payload     []byte
}

type Class int

const (
	ClassUnknown Class = iota
	ClassPeer
	ClassInternet
	ClassDiscovery
)

// Classifier decides which data-plane path should receive a packet.
type Classifier interface {
	Classify(Packet) Class
}

// Sink receives packets for a selected data-plane path.
type Sink interface {
	WritePacket(context.Context, Packet) error
}

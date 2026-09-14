package transport

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/wangchaozhi/xray-mesh/internal/mesh"
	"github.com/wangchaozhi/xray-mesh/internal/packet"
)

var ErrUnauthorized = errors.New("unauthorized relay frame")

var (
	mdnsMulticast = netip.MustParseAddr("224.0.0.251")
	ssdpMulticast = netip.MustParseAddr("239.255.255.250")
)

type peerRegistry interface {
	GetByToken(string) (mesh.Peer, bool)
	GetByVirtualIP(netip.Addr) (mesh.Peer, bool)
	List() []mesh.Peer
}

// Relay forwards raw IPv4 packets between registered peers. It is a
// development relay, not an encrypted tunnel. Deploy it only on a trusted path
// or behind an existing secure transport.
type Relay struct {
	registry peerRegistry
	conn     *net.UDPConn

	mu        sync.RWMutex
	endpoints map[string]*net.UDPAddr
}

func ListenRelay(addr string, registry peerRegistry) (*Relay, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, err
	}
	return &Relay{registry: registry, conn: conn, endpoints: make(map[string]*net.UDPAddr)}, nil
}

func (r *Relay) Addr() net.Addr { return r.conn.LocalAddr() }

func (r *Relay) Close() error { return r.conn.Close() }

func (r *Relay) Serve(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		_ = r.conn.Close()
	}()

	buf := make([]byte, 64*1024)
	for {
		n, src, err := r.conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if err := r.handleDatagram(buf[:n], src); err != nil {
			log.Printf("relay drop from %s: %v", src, err)
		}
	}
}

func (r *Relay) handleDatagram(b []byte, src *net.UDPAddr) error {
	frame, err := ParseFrame(b)
	if err != nil {
		return err
	}
	if frame.Type == FrameDeliver {
		return ErrUnauthorized
	}
	peer, ok := r.registry.GetByToken(frame.Token)
	if !ok {
		return ErrUnauthorized
	}
	r.bind(peer.NodeID, src)

	if frame.Type == FrameHello {
		return nil
	}
	if len(frame.Payload) > 65535 {
		return fmt.Errorf("IPv4 packet exceeds relay limit")
	}
	sourceIP, destinationIP, err := packet.IPv4Endpoints(frame.Payload)
	if err != nil {
		return err
	}
	if sourceIP != peer.VirtualIP {
		return fmt.Errorf("source IP %s does not match peer lease %s", sourceIP, peer.VirtualIP)
	}

	if isDiscoveryPacket(frame.Payload, destinationIP) {
		return r.broadcast(peer.NodeID, frame.Payload)
	}
	destinationPeer, ok := r.registry.GetByVirtualIP(destinationIP)
	if !ok {
		return fmt.Errorf("destination %s is not a registered peer", destinationIP)
	}
	return r.deliver(destinationPeer.NodeID, frame.Payload)
}

func isDiscoveryPacket(payload []byte, destinationIP netip.Addr) bool {
	_, destinationPort, err := packet.IPv4UDPPorts(payload)
	if err != nil {
		return false
	}
	switch destinationIP {
	case mdnsMulticast:
		return destinationPort == 5353
	case ssdpMulticast:
		return destinationPort == 1900
	default:
		return false
	}
}

func (r *Relay) bind(nodeID string, addr *net.UDPAddr) {
	copyAddr := *addr
	r.mu.Lock()
	r.endpoints[nodeID] = &copyAddr
	r.mu.Unlock()
}

func (r *Relay) endpoint(nodeID string) (*net.UDPAddr, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	addr, ok := r.endpoints[nodeID]
	if !ok {
		return nil, false
	}
	copyAddr := *addr
	return &copyAddr, true
}

func (r *Relay) deliver(nodeID string, payload []byte) error {
	addr, ok := r.endpoint(nodeID)
	if !ok {
		return fmt.Errorf("peer %s has no active relay endpoint", nodeID)
	}
	frame, err := MarshalDeliver(payload)
	if err != nil {
		return err
	}
	_ = r.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_, err = r.conn.WriteToUDP(frame, addr)
	return err
}

func (r *Relay) broadcast(sourceNode string, payload []byte) error {
	for _, peer := range r.registry.List() {
		if peer.NodeID == sourceNode {
			continue
		}
		if _, ok := r.endpoint(peer.NodeID); !ok {
			continue
		}
		if err := r.deliver(peer.NodeID, payload); err != nil {
			return err
		}
	}
	return nil
}

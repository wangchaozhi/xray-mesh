package transport

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log"
	"net"
	"net/netip"
	"sort"
	"sync"
	"time"

	"github.com/wangchaozhi/xray-mesh/internal/mesh"
	"github.com/wangchaozhi/xray-mesh/internal/packet"
)

var (
	ErrUnauthorized         = errors.New("unauthorized relay frame")
	ErrDiscoveryRateLimited = errors.New("discovery traffic rate limited")
)

var (
	mdnsMulticast = netip.MustParseAddr("224.0.0.251")
	ssdpMulticast = netip.MustParseAddr("239.255.255.250")
)

const (
	defaultDiscoveryRate  = 20.0
	defaultDiscoveryBurst = 40
)

var defaultDiscoveryDedupWindow = 750 * time.Millisecond

type RelayOptions struct {
	DiscoveryRate        float64
	DiscoveryBurst       int
	DiscoveryDedupWindow time.Duration
}

type ObservedCandidate struct {
	NodeID     string    `json:"node_id"`
	Endpoint   string    `json:"endpoint"`
	ObservedAt time.Time `json:"observed_at"`
}

type peerRegistry interface {
	GetByToken(string) (mesh.Peer, bool)
	GetByVirtualIP(netip.Addr) (mesh.Peer, bool)
	List() []mesh.Peer
}

type discoveryPeerState struct {
	tokens     float64
	lastRefill time.Time
	seen       map[[32]byte]time.Time
}

type relayEndpoint struct {
	addr       *net.UDPAddr
	observedAt time.Time
}

type Relay struct {
	registry peerRegistry
	conn     *net.UDPConn

	mu        sync.RWMutex
	endpoints map[string]relayEndpoint

	discoveryMu sync.Mutex
	discovery   map[string]*discoveryPeerState
	options     RelayOptions
	now         func() time.Time
}

func ListenRelay(addr string, registry peerRegistry) (*Relay, error) {
	return ListenRelayWithOptions(addr, registry, RelayOptions{DiscoveryDedupWindow: -1})
}

func ListenRelayWithOptions(addr string, registry peerRegistry, options RelayOptions) (*Relay, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, err
	}
	options = normalizeRelayOptions(options)
	return &Relay{
		registry:  registry,
		conn:      conn,
		endpoints: make(map[string]relayEndpoint),
		discovery: make(map[string]*discoveryPeerState),
		options:   options,
		now:       time.Now,
	}, nil
}

func normalizeRelayOptions(options RelayOptions) RelayOptions {
	if options.DiscoveryRate <= 0 {
		options.DiscoveryRate = defaultDiscoveryRate
	}
	if options.DiscoveryBurst <= 0 {
		options.DiscoveryBurst = defaultDiscoveryBurst
	}
	if options.DiscoveryDedupWindow < 0 {
		options.DiscoveryDedupWindow = defaultDiscoveryDedupWindow
	}
	return options
}

func (r *Relay) Addr() net.Addr { return r.conn.LocalAddr() }
func (r *Relay) Close() error   { return r.conn.Close() }

func (r *Relay) ForgetPeer(nodeID string) {
	r.mu.Lock()
	delete(r.endpoints, nodeID)
	r.mu.Unlock()

	r.discoveryMu.Lock()
	delete(r.discovery, nodeID)
	r.discoveryMu.Unlock()
}

// Candidates returns the coordinator-observed UDP source endpoint for every
// peer that has sent a valid relay frame. These are server-reflexive candidates
// learned opportunistically from the existing relay path; they are not proof
// that direct connectivity is currently possible.
func (r *Relay) Candidates() []ObservedCandidate {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ObservedCandidate, 0, len(r.endpoints))
	for nodeID, endpoint := range r.endpoints {
		out = append(out, ObservedCandidate{
			NodeID:     nodeID,
			Endpoint:   endpoint.addr.String(),
			ObservedAt: endpoint.observedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NodeID < out[j].NodeID })
	return out
}

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
			if errors.Is(err, ErrDiscoveryRateLimited) {
				continue
			}
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
		duplicate, err := r.guardDiscovery(peer.NodeID, frame.Payload)
		if err != nil {
			return err
		}
		if duplicate {
			return nil
		}
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

func (r *Relay) guardDiscovery(nodeID string, payload []byte) (bool, error) {
	now := r.now()
	fingerprint := sha256.Sum256(payload)

	r.discoveryMu.Lock()
	defer r.discoveryMu.Unlock()

	state := r.discovery[nodeID]
	if state == nil {
		state = &discoveryPeerState{
			tokens:     float64(r.options.DiscoveryBurst),
			lastRefill: now,
			seen:       make(map[[32]byte]time.Time),
		}
		r.discovery[nodeID] = state
	}

	if window := r.options.DiscoveryDedupWindow; window > 0 {
		for key, seenAt := range state.seen {
			if now.Sub(seenAt) >= window {
				delete(state.seen, key)
			}
		}
		if seenAt, ok := state.seen[fingerprint]; ok && now.Sub(seenAt) < window {
			return true, nil
		}
	}

	elapsed := now.Sub(state.lastRefill).Seconds()
	if elapsed > 0 {
		state.tokens += elapsed * r.options.DiscoveryRate
		if max := float64(r.options.DiscoveryBurst); state.tokens > max {
			state.tokens = max
		}
		state.lastRefill = now
	}
	if state.tokens < 1 {
		return false, ErrDiscoveryRateLimited
	}
	state.tokens--
	if r.options.DiscoveryDedupWindow > 0 {
		state.seen[fingerprint] = now
	}
	return false, nil
}

func (r *Relay) bind(nodeID string, addr *net.UDPAddr) {
	copyAddr := *addr
	r.mu.Lock()
	r.endpoints[nodeID] = relayEndpoint{addr: &copyAddr, observedAt: r.now().UTC()}
	r.mu.Unlock()
}

func (r *Relay) endpoint(nodeID string) (*net.UDPAddr, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	endpoint, ok := r.endpoints[nodeID]
	if !ok {
		return nil, false
	}
	copyAddr := *endpoint.addr
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

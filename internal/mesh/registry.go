package mesh

import (
	"errors"
	"net/netip"
	"sort"
	"strings"
	"sync"
)

var ErrInvalidNodeID = errors.New("node ID must not be empty")

type Registry struct {
	mu        sync.RWMutex
	allocator *Allocator
	peers     map[string]Peer
}

func NewRegistry(prefix netip.Prefix) (*Registry, error) {
	allocator, err := NewAllocator(prefix)
	if err != nil {
		return nil, err
	}
	return &Registry{allocator: allocator, peers: make(map[string]Peer)}, nil
}

func (r *Registry) Register(nodeID string) (Peer, error) {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return Peer{}, ErrInvalidNodeID
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.peers[nodeID]; ok {
		return existing, nil
	}
	ip, err := r.allocator.Allocate(nodeID)
	if err != nil {
		return Peer{}, err
	}
	peer := Peer{NodeID: nodeID, VirtualIP: ip}
	r.peers[nodeID] = peer
	return peer, nil
}

func (r *Registry) Get(nodeID string) (Peer, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.peers[nodeID]
	return p, ok
}

func (r *Registry) List() []Peer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Peer, 0, len(r.peers))
	for _, p := range r.peers {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NodeID < out[j].NodeID })
	return out
}

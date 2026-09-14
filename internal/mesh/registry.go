package mesh

import (
	"crypto/rand"
	"encoding/hex"
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
	prefix    netip.Prefix
	peers     map[string]Peer
	byIP      map[netip.Addr]string
	byToken   map[string]string
}

func NewRegistry(prefix netip.Prefix) (*Registry, error) {
	allocator, err := NewAllocator(prefix)
	if err != nil {
		return nil, err
	}
	return &Registry{
		allocator: allocator,
		prefix:    prefix.Masked(),
		peers:     make(map[string]Peer),
		byIP:      make(map[netip.Addr]string),
		byToken:   make(map[string]string),
	}, nil
}

func (r *Registry) Prefix() netip.Prefix { return r.prefix }

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
	token, err := newSessionToken()
	if err != nil {
		return Peer{}, err
	}
	peer := Peer{NodeID: nodeID, VirtualIP: ip, SessionToken: token}
	r.peers[nodeID] = peer
	r.byIP[ip] = nodeID
	r.byToken[token] = nodeID
	return peer, nil
}

func newSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (r *Registry) Get(nodeID string) (Peer, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.peers[nodeID]
	return p, ok
}

func (r *Registry) GetByVirtualIP(ip netip.Addr) (Peer, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	nodeID, ok := r.byIP[ip]
	if !ok {
		return Peer{}, false
	}
	p, ok := r.peers[nodeID]
	return p, ok
}

func (r *Registry) GetByToken(token string) (Peer, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	nodeID, ok := r.byToken[token]
	if !ok {
		return Peer{}, false
	}
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

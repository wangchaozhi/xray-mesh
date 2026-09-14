package p2p

import (
	"errors"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/wangchaozhi/xray-mesh/internal/telemetry"
)

type CandidateKind string

const (
	CandidateRelayObserved CandidateKind = "relay_observed"
	PathRelay                           = "relay"
	PathDirect                          = "direct"
)

type Candidate struct {
	NodeID     string        `json:"node_id"`
	VirtualIP  string        `json:"virtual_ip"`
	Endpoint   string        `json:"endpoint"`
	Kind       CandidateKind `json:"kind"`
	ObservedAt time.Time     `json:"observed_at"`
}

type Selection struct {
	Kind     string `json:"kind"`
	Endpoint string `json:"endpoint,omitempty"`
}

type directState struct {
	endpoint     string
	healthyUntil time.Time
}

// Selector deliberately defaults to relay. A candidate is not enough to select
// a direct path: a later probe layer must explicitly mark that endpoint healthy.
type Selector struct {
	mu        sync.Mutex
	directTTL time.Duration
	direct    map[string]directState
	now       func() time.Time
}

func NewSelector(directTTL time.Duration) *Selector {
	if directTTL <= 0 {
		directTTL = 30 * time.Second
	}
	return &Selector{
		directTTL: directTTL,
		direct:    make(map[string]directState),
		now:       time.Now,
	}
}

func (s *Selector) MarkDirectHealthy(nodeID, endpoint string, observedAt time.Time) error {
	nodeID = strings.TrimSpace(nodeID)
	endpoint = strings.TrimSpace(endpoint)
	if nodeID == "" {
		return errors.New("node ID is required")
	}
	if _, err := netip.ParseAddrPort(endpoint); err != nil {
		return err
	}
	if observedAt.IsZero() {
		observedAt = s.now()
	}
	s.mu.Lock()
	s.direct[nodeID] = directState{endpoint: endpoint, healthyUntil: observedAt.Add(s.directTTL)}
	s.mu.Unlock()
	telemetry.P2P.IncDirectHealthy()
	return nil
}

func (s *Selector) MarkDirectFailed(nodeID string) {
	s.mu.Lock()
	delete(s.direct, strings.TrimSpace(nodeID))
	s.mu.Unlock()
}

func (s *Selector) Select(nodeID string) Selection {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.direct[strings.TrimSpace(nodeID)]
	if !ok || !now.Before(state.healthyUntil) {
		if ok {
			delete(s.direct, strings.TrimSpace(nodeID))
		}
		return Selection{Kind: PathRelay}
	}
	return Selection{Kind: PathDirect, Endpoint: state.endpoint}
}

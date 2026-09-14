package p2p

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrTicketNodeRequired = errors.New("both ticket nodes are required")
	ErrTicketSelfPair     = errors.New("P2P ticket requires two different nodes")
)

const defaultProbeTicketTTL = 30 * time.Second

// ProbeTicket is a short-lived proof shared only with the two peers named by
// SourceNode and TargetNode. It is intended for authenticating direct-connect
// probes, not long-lived payload encryption or durable peer identity.
type ProbeTicket struct {
	Ticket     string    `json:"ticket"`
	SourceNode string    `json:"source_node"`
	TargetNode string    `json:"target_node"`
	IssuedAt   time.Time `json:"issued_at"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type TicketManager struct {
	mu      sync.Mutex
	ttl     time.Duration
	tickets map[string]ProbeTicket
	now     func() time.Time
}

func NewTicketManager(ttl time.Duration) *TicketManager {
	if ttl <= 0 {
		ttl = defaultProbeTicketTTL
	}
	return &TicketManager{
		ttl:     ttl,
		tickets: make(map[string]ProbeTicket),
		now:     time.Now,
	}
}

func (m *TicketManager) TTL() time.Duration { return m.ttl }

func (m *TicketManager) Issue(sourceNode, targetNode string) (ProbeTicket, error) {
	sourceNode = strings.TrimSpace(sourceNode)
	targetNode = strings.TrimSpace(targetNode)
	if sourceNode == "" || targetNode == "" {
		return ProbeTicket{}, ErrTicketNodeRequired
	}
	if sourceNode == targetNode {
		return ProbeTicket{}, ErrTicketSelfPair
	}
	token, err := newProbeTicketToken()
	if err != nil {
		return ProbeTicket{}, err
	}
	now := m.now().UTC()
	ticket := ProbeTicket{
		Ticket:     token,
		SourceNode: sourceNode,
		TargetNode: targetNode,
		IssuedAt:   now,
		ExpiresAt:  now.Add(m.ttl),
	}
	m.mu.Lock()
	m.pruneLocked(now)
	m.tickets[token] = ticket
	m.mu.Unlock()
	return ticket, nil
}

// ListForNode returns only live tickets where nodeID is one of the two peers.
// This prevents an authenticated but unrelated peer from enumerating probe
// credentials for other pairs.
func (m *TicketManager) ListForNode(nodeID string) []ProbeTicket {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return nil
	}
	now := m.now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked(now)
	out := make([]ProbeTicket, 0)
	for _, ticket := range m.tickets {
		if ticket.SourceNode == nodeID || ticket.TargetNode == nodeID {
			out = append(out, ticket)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ExpiresAt.Equal(out[j].ExpiresAt) {
			return out[i].Ticket < out[j].Ticket
		}
		return out[i].ExpiresAt.Before(out[j].ExpiresAt)
	})
	return out
}

// Verify confirms that token is live and belongs to exactly the requested pair.
// Pair order is ignored so the same ticket can authenticate probe and ack.
func (m *TicketManager) Verify(token, nodeA, nodeB string) bool {
	token = strings.TrimSpace(token)
	nodeA = strings.TrimSpace(nodeA)
	nodeB = strings.TrimSpace(nodeB)
	if token == "" || nodeA == "" || nodeB == "" || nodeA == nodeB {
		return false
	}
	now := m.now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked(now)
	ticket, ok := m.tickets[token]
	if !ok {
		return false
	}
	return (ticket.SourceNode == nodeA && ticket.TargetNode == nodeB) ||
		(ticket.SourceNode == nodeB && ticket.TargetNode == nodeA)
}

// ForgetNode removes every outstanding ticket involving nodeID. Lease expiry
// should call this so stale direct-probe credentials cannot outlive a peer.
func (m *TicketManager) ForgetNode(nodeID string) {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return
	}
	m.mu.Lock()
	for token, ticket := range m.tickets {
		if ticket.SourceNode == nodeID || ticket.TargetNode == nodeID {
			delete(m.tickets, token)
		}
	}
	m.mu.Unlock()
}

func (m *TicketManager) Prune() {
	now := m.now().UTC()
	m.mu.Lock()
	m.pruneLocked(now)
	m.mu.Unlock()
}

func (m *TicketManager) pruneLocked(now time.Time) {
	for token, ticket := range m.tickets {
		if !now.Before(ticket.ExpiresAt) {
			delete(m.tickets, token)
		}
	}
}

func newProbeTicketToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

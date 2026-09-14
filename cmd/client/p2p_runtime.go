package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/wangchaozhi/xray-mesh/internal/mesh"
	"github.com/wangchaozhi/xray-mesh/internal/p2p"
	"github.com/wangchaozhi/xray-mesh/internal/transport"
)

const (
	p2pPollInterval       = 3 * time.Second
	p2pRelayHelloInterval = 10 * time.Second
	p2pCandidateMaxAge    = 30 * time.Second
	p2pPendingTTL         = 10 * time.Second
	p2pTicketIssueRetry   = 10 * time.Second
)

type pendingProbe struct {
	probe  p2p.ProbeFrame
	ticket p2p.ProbeTicket
	target string
	sentAt time.Time
}

type p2pRuntime struct {
	server   string
	nodeID   string
	token    string
	conn     *net.UDPConn
	relay    *net.UDPAddr
	client   *http.Client
	selector *p2p.Selector

	mu         sync.RWMutex
	tickets    map[string]p2p.ProbeTicket
	candidates map[string]p2p.Candidate
	pending    map[string]pendingProbe
	lastIssued map[string]time.Time
	now        func() time.Time
}

func newP2PRuntime(server string, registration mesh.RegistrationView, conn *net.UDPConn, relay *net.UDPAddr) *p2pRuntime {
	return &p2pRuntime{
		server:     strings.TrimRight(server, "/"),
		nodeID:     registration.NodeID,
		token:      registration.SessionToken,
		conn:       conn,
		relay:      relay,
		client:     &http.Client{Timeout: 5 * time.Second},
		selector:   p2p.NewSelector(30 * time.Second),
		tickets:    make(map[string]p2p.ProbeTicket),
		candidates: make(map[string]p2p.Candidate),
		pending:    make(map[string]pendingProbe),
		lastIssued: make(map[string]time.Time),
		now:        time.Now,
	}
}

func (r *p2pRuntime) Start(ctx context.Context) {
	go r.pollLoop(ctx)
	go r.relayHelloLoop(ctx)
}

func (r *p2pRuntime) Selection(nodeID string) p2p.Selection {
	return r.selector.Select(nodeID)
}

func (r *p2pRuntime) HandleDatagram(data []byte, source *net.UDPAddr) error {
	frame, err := p2p.ParseProbeFrame(data)
	if err != nil {
		if errors.Is(err, p2p.ErrNotProbeDatagram) {
			return nil
		}
		return err
	}
	ticket, ok := r.ticketForID(frame.TicketID)
	if !ok {
		return fmt.Errorf("unknown P2P probe ticket %s", frame.TicketID)
	}
	now := r.now().UTC()

	switch frame.Type {
	case p2p.ProbeTypeProbe:
		if frame.TargetNode != r.nodeID {
			return p2p.ErrProbeAuthFailed
		}
		if err := p2p.VerifyProbe(frame, ticket, now); err != nil {
			return err
		}
		ack, err := p2p.NewProbeAck(frame, ticket, now)
		if err != nil {
			return err
		}
		wire, err := p2p.MarshalProbeFrame(ack)
		if err != nil {
			return err
		}
		if _, err := r.conn.WriteToUDP(wire, source); err != nil {
			return fmt.Errorf("send P2P probe ack: %w", err)
		}
		return nil

	case p2p.ProbeTypeAck:
		pending, ok := r.peekPending(frame.Nonce)
		if !ok {
			return errors.New("P2P probe ack has no matching pending nonce")
		}
		if err := p2p.VerifyProbeAck(frame, pending.probe, pending.ticket, now); err != nil {
			return err
		}
		r.deletePending(frame.Nonce)
		if err := r.selector.MarkDirectHealthy(pending.target, source.String(), now); err != nil {
			return err
		}
		log.Printf("P2P direct probe healthy peer=%s endpoint=%s", pending.target, source)
		return nil
	default:
		return p2p.ErrInvalidProbeFrame
	}
}

func (r *p2pRuntime) pollLoop(ctx context.Context) {
	r.runPollCycle(ctx)
	ticker := time.NewTicker(p2pPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.runPollCycle(ctx)
		}
	}
}

func (r *p2pRuntime) runPollCycle(ctx context.Context) {
	tickets, err := r.fetchTickets(ctx)
	if err != nil {
		log.Printf("P2P ticket refresh: %v", err)
		return
	}
	r.replaceTickets(tickets)

	candidates, err := r.fetchCandidates(ctx)
	if err != nil {
		log.Printf("P2P candidate refresh: %v", err)
		return
	}
	r.replaceCandidates(candidates)

	if err := r.issueNeededTickets(ctx); err != nil {
		log.Printf("P2P ticket issue: %v", err)
	}
	r.sendProbes()
}

func (r *p2pRuntime) relayHelloLoop(ctx context.Context) {
	hello, err := transport.MarshalHello(r.token)
	if err != nil {
		log.Printf("P2P relay keepalive disabled: %v", err)
		return
	}
	ticker := time.NewTicker(p2pRelayHelloInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := r.conn.WriteToUDP(hello, r.relay); err != nil && ctx.Err() == nil {
				log.Printf("P2P relay UDP keepalive: %v", err)
			}
		}
	}
}

func (r *p2pRuntime) fetchTickets(ctx context.Context) ([]p2p.ProbeTicket, error) {
	var tickets []p2p.ProbeTicket
	if err := r.getJSON(ctx, "/v1/p2p/tickets", &tickets); err != nil {
		return nil, err
	}
	return tickets, nil
}

func (r *p2pRuntime) fetchCandidates(ctx context.Context) ([]p2p.Candidate, error) {
	var candidates []p2p.Candidate
	if err := r.getJSON(ctx, "/v1/candidates", &candidates); err != nil {
		return nil, err
	}
	return candidates, nil
}

func (r *p2pRuntime) getJSON(ctx context.Context, path string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.server+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.token)
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("%s returned %s: %s", path, resp.Status, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}

func (r *p2pRuntime) issueTicket(ctx context.Context, target string) (p2p.ProbeTicket, error) {
	body, err := json.Marshal(map[string]string{"target_node": target})
	if err != nil {
		return p2p.ProbeTicket{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.server+"/v1/p2p/tickets", bytes.NewReader(body))
	if err != nil {
		return p2p.ProbeTicket{}, err
	}
	req.Header.Set("Authorization", "Bearer "+r.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return p2p.ProbeTicket{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return p2p.ProbeTicket{}, fmt.Errorf("issue ticket for %s returned %s: %s", target, resp.Status, strings.TrimSpace(string(responseBody)))
	}
	var ticket p2p.ProbeTicket
	if err := json.NewDecoder(resp.Body).Decode(&ticket); err != nil {
		return p2p.ProbeTicket{}, err
	}
	return ticket, nil
}

func (r *p2pRuntime) replaceTickets(tickets []p2p.ProbeTicket) {
	now := r.now().UTC()
	next := make(map[string]p2p.ProbeTicket)
	for _, ticket := range tickets {
		if ticket.SourceNode != r.nodeID && ticket.TargetNode != r.nodeID {
			continue
		}
		if !ticket.ExpiresAt.IsZero() && !now.Before(ticket.ExpiresAt) {
			continue
		}
		id, err := p2p.TicketIdentifier(ticket)
		if err != nil {
			continue
		}
		next[id] = ticket
	}
	r.mu.Lock()
	for id, ticket := range r.tickets {
		if _, ok := next[id]; !ok && (ticket.ExpiresAt.IsZero() || now.Before(ticket.ExpiresAt)) {
			// Keep a just-issued local ticket until the next coordinator list has a
			// chance to include it.
			next[id] = ticket
		}
	}
	r.tickets = next
	for nonce, pending := range r.pending {
		if now.Sub(pending.sentAt) >= p2pPendingTTL {
			delete(r.pending, nonce)
		}
	}
	r.mu.Unlock()
}

func (r *p2pRuntime) replaceCandidates(candidates []p2p.Candidate) {
	now := r.now().UTC()
	next := make(map[string]p2p.Candidate)
	for _, candidate := range candidates {
		if candidate.NodeID == "" || candidate.NodeID == r.nodeID || strings.TrimSpace(candidate.Endpoint) == "" {
			continue
		}
		if candidate.ObservedAt.IsZero() || now.Sub(candidate.ObservedAt) > p2pCandidateMaxAge || candidate.ObservedAt.After(now.Add(5*time.Second)) {
			continue
		}
		next[candidate.NodeID] = candidate
	}
	r.mu.Lock()
	r.candidates = next
	r.mu.Unlock()
}

func (r *p2pRuntime) issueNeededTickets(ctx context.Context) error {
	now := r.now().UTC()
	r.mu.RLock()
	candidates := make([]p2p.Candidate, 0, len(r.candidates))
	for _, candidate := range r.candidates {
		candidates = append(candidates, candidate)
	}
	tickets := make([]p2p.ProbeTicket, 0, len(r.tickets))
	for _, ticket := range r.tickets {
		tickets = append(tickets, ticket)
	}
	lastIssued := make(map[string]time.Time, len(r.lastIssued))
	for nodeID, issuedAt := range r.lastIssued {
		lastIssued[nodeID] = issuedAt
	}
	r.mu.RUnlock()

	for _, candidate := range candidates {
		target := candidate.NodeID
		// Lexicographic ownership prevents both peers from continuously issuing
		// tickets for the same pair.
		if r.nodeID >= target || hasLiveSourceTicket(tickets, r.nodeID, target, now) {
			continue
		}
		if last, ok := lastIssued[target]; ok && now.Sub(last) < p2pTicketIssueRetry {
			continue
		}
		ticket, err := r.issueTicket(ctx, target)
		if err != nil {
			return err
		}
		id, err := p2p.TicketIdentifier(ticket)
		if err != nil {
			return err
		}
		r.mu.Lock()
		r.tickets[id] = ticket
		r.lastIssued[target] = now
		r.mu.Unlock()
	}
	return nil
}

func hasLiveSourceTicket(tickets []p2p.ProbeTicket, source, target string, now time.Time) bool {
	for _, ticket := range tickets {
		if ticket.SourceNode == source && ticket.TargetNode == target && (ticket.ExpiresAt.IsZero() || now.Before(ticket.ExpiresAt)) {
			return true
		}
	}
	return false
}

func (r *p2pRuntime) sendProbes() {
	now := r.now().UTC()
	r.mu.RLock()
	pairs := make([]struct {
		ticket    p2p.ProbeTicket
		candidate p2p.Candidate
	}, 0)
	for _, ticket := range r.tickets {
		if ticket.SourceNode != r.nodeID || (!ticket.ExpiresAt.IsZero() && !now.Before(ticket.ExpiresAt)) {
			continue
		}
		candidate, ok := r.candidates[ticket.TargetNode]
		if !ok {
			continue
		}
		pairs = append(pairs, struct {
			ticket    p2p.ProbeTicket
			candidate p2p.Candidate
		}{ticket: ticket, candidate: candidate})
	}
	r.mu.RUnlock()

	for _, pair := range pairs {
		if err := r.sendProbe(pair.ticket, pair.candidate); err != nil {
			log.Printf("P2P probe peer=%s: %v", pair.ticket.TargetNode, err)
		}
	}
}

func (r *p2pRuntime) sendProbe(ticket p2p.ProbeTicket, candidate p2p.Candidate) error {
	probe, err := p2p.NewProbe(ticket)
	if err != nil {
		return err
	}
	wire, err := p2p.MarshalProbeFrame(probe)
	if err != nil {
		return err
	}
	endpoint, err := net.ResolveUDPAddr("udp", candidate.Endpoint)
	if err != nil {
		return err
	}
	pending := pendingProbe{probe: probe, ticket: ticket, target: ticket.TargetNode, sentAt: r.now().UTC()}
	r.mu.Lock()
	r.pending[probe.Nonce] = pending
	r.mu.Unlock()
	if _, err := r.conn.WriteToUDP(wire, endpoint); err != nil {
		r.mu.Lock()
		delete(r.pending, probe.Nonce)
		r.mu.Unlock()
		return err
	}
	return nil
}

func (r *p2pRuntime) ticketForID(ticketID string) (p2p.ProbeTicket, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ticket, ok := r.tickets[ticketID]
	return ticket, ok
}

func (r *p2pRuntime) peekPending(nonce string) (pendingProbe, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	pending, ok := r.pending[nonce]
	return pending, ok
}

func (r *p2pRuntime) deletePending(nonce string) {
	r.mu.Lock()
	delete(r.pending, nonce)
	r.mu.Unlock()
}

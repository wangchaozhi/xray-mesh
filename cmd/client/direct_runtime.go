package main

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"

	"github.com/wangchaozhi/xray-mesh/internal/p2p"
	"github.com/wangchaozhi/xray-mesh/internal/packet"
)

// SendDirectPayload attempts to send one mesh packet over a currently healthy
// direct path. A false return means the caller must use the relay fallback.
func (r *p2pRuntime) SendDirectPayload(destinationIP netip.Addr, payload []byte) (bool, error) {
	now := r.now().UTC()
	candidate, ok := r.candidateForVirtualIP(destinationIP)
	if !ok {
		return false, nil
	}
	selection := r.selector.Select(candidate.NodeID)
	if selection.Kind != p2p.PathDirect || strings.TrimSpace(selection.Endpoint) == "" {
		return false, nil
	}
	ticket, ok := r.liveTicketForPeer(candidate.NodeID, now)
	if !ok {
		return false, nil
	}
	wire, err := p2p.SealDirectPayload(ticket, r.nodeID, candidate.NodeID, payload, now)
	if err != nil {
		if errors.Is(err, p2p.ErrProbeTicketExpired) {
			return false, nil
		}
		return false, err
	}
	endpoint, err := net.ResolveUDPAddr("udp", selection.Endpoint)
	if err != nil {
		r.selector.MarkDirectFailed(candidate.NodeID)
		return false, err
	}
	if _, err := r.conn.WriteToUDP(wire, endpoint); err != nil {
		r.selector.MarkDirectFailed(candidate.NodeID)
		return false, err
	}
	return true, nil
}

// HandleDirectPayload authenticates and decrypts one non-relay datagram. A
// false handled result means the datagram belongs to another P2P control frame
// such as probe/probe_ack.
func (r *p2pRuntime) HandleDirectPayload(data []byte, source *net.UDPAddr, localVirtualIP netip.Addr) ([]byte, bool, error) {
	ticketID, err := p2p.DirectPayloadTicketID(data)
	if err != nil {
		if errors.Is(err, p2p.ErrNotDirectPayload) {
			return nil, false, nil
		}
		return nil, true, err
	}
	ticket, ok := r.ticketForID(ticketID)
	if !ok {
		return nil, true, fmt.Errorf("unknown direct payload ticket %s", ticketID)
	}
	now := r.now().UTC()
	sourceNode, _, payload, err := p2p.OpenDirectPayload(data, ticket, r.nodeID, now)
	if err != nil {
		return nil, true, err
	}
	candidate, ok := r.candidateForNode(sourceNode)
	if !ok {
		return nil, true, fmt.Errorf("direct payload source %s has no live candidate", sourceNode)
	}
	expectedSourceIP, err := netip.ParseAddr(candidate.VirtualIP)
	if err != nil {
		return nil, true, fmt.Errorf("invalid candidate virtual IP for %s: %w", sourceNode, err)
	}
	sourceIP, destinationIP, err := packet.IPv4Endpoints(payload)
	if err != nil {
		return nil, true, err
	}
	if sourceIP != expectedSourceIP || destinationIP != localVirtualIP {
		return nil, true, fmt.Errorf("direct payload virtual IP mismatch src=%s want=%s dst=%s want=%s", sourceIP, expectedSourceIP, destinationIP, localVirtualIP)
	}
	replayKey, err := p2p.DirectPayloadReplayKey(data)
	if err != nil {
		return nil, true, err
	}
	if !r.acceptDirectReplayKey(replayKey, ticket.ExpiresAt, now) {
		return nil, true, ErrDirectPayloadReplay
	}
	if err := r.selector.MarkDirectHealthy(sourceNode, source.String(), now); err != nil {
		return nil, true, err
	}
	return payload, true, nil
}

func (r *p2pRuntime) candidateForVirtualIP(ip netip.Addr) (p2p.Candidate, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, candidate := range r.candidates {
		if candidate.VirtualIP == ip.String() {
			return candidate, true
		}
	}
	return p2p.Candidate{}, false
}

func (r *p2pRuntime) candidateForNode(nodeID string) (p2p.Candidate, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	candidate, ok := r.candidates[nodeID]
	return candidate, ok
}

func (r *p2pRuntime) liveTicketForPeer(peerNode string, now time.Time) (p2p.ProbeTicket, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, ticket := range r.tickets {
		if !ticket.ExpiresAt.IsZero() && !now.Before(ticket.ExpiresAt) {
			continue
		}
		if (ticket.SourceNode == r.nodeID && ticket.TargetNode == peerNode) ||
			(ticket.SourceNode == peerNode && ticket.TargetNode == r.nodeID) {
			return ticket, true
		}
	}
	return p2p.ProbeTicket{}, false
}

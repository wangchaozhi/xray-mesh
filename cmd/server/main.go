package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/netip"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/wangchaozhi/xray-mesh/internal/mesh"
	"github.com/wangchaozhi/xray-mesh/internal/p2p"
	"github.com/wangchaozhi/xray-mesh/internal/transport"
)

type registerRequest struct {
	NodeID string `json:"node_id"`
}

type probeTicketRequest struct {
	TargetNode string `json:"target_node"`
}

type api struct {
	registry          *mesh.Registry
	relay             *transport.Relay
	tickets           *p2p.TicketManager
	heartbeatInterval time.Duration
	leaseTTL          time.Duration
}

func main() {
	listen := flag.String("listen", "127.0.0.1:8666", "HTTP listen address")
	relayListen := flag.String("relay-listen", "127.0.0.1:8667", "UDP relay listen address")
	prefixText := flag.String("prefix", "10.66.0.0/24", "virtual IPv4 prefix")
	peerLease := flag.Duration("peer-lease", 60*time.Second, "peer lease TTL without a successful heartbeat")
	heartbeatInterval := flag.Duration("heartbeat-interval", 15*time.Second, "heartbeat interval advertised to clients")
	p2pTicketTTL := flag.Duration("p2p-ticket-ttl", 30*time.Second, "TTL for pair-scoped direct-probe tickets")
	discoveryRate := flag.Float64("discovery-rate", 20, "sustained mDNS/SSDP packets per second allowed per peer")
	discoveryBurst := flag.Int("discovery-burst", 40, "maximum mDNS/SSDP token-bucket burst per peer")
	discoveryDedup := flag.Duration("discovery-dedup", 750*time.Millisecond, "suppress identical discovery packets from one peer during this window; 0 disables")
	flag.Parse()

	if *peerLease < time.Second {
		log.Fatal("-peer-lease must be at least 1s")
	}
	if *heartbeatInterval < time.Second {
		log.Fatal("-heartbeat-interval must be at least 1s")
	}
	if *heartbeatInterval >= *peerLease {
		log.Fatal("-heartbeat-interval must be shorter than -peer-lease")
	}
	if *p2pTicketTTL <= 0 || *p2pTicketTTL > 5*time.Minute {
		log.Fatal("-p2p-ticket-ttl must be greater than zero and no more than 5m")
	}
	if *discoveryRate <= 0 {
		log.Fatal("-discovery-rate must be greater than zero")
	}
	if *discoveryBurst <= 0 {
		log.Fatal("-discovery-burst must be greater than zero")
	}
	if *discoveryDedup < 0 {
		log.Fatal("-discovery-dedup must not be negative")
	}

	prefix, err := netip.ParsePrefix(*prefixText)
	if err != nil {
		log.Fatalf("invalid prefix: %v", err)
	}
	registry, err := mesh.NewRegistry(prefix)
	if err != nil {
		log.Fatalf("registry: %v", err)
	}
	tickets := p2p.NewTicketManager(*p2pTicketTTL)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	relay, err := transport.ListenRelayWithOptions(*relayListen, registry, transport.RelayOptions{
		DiscoveryRate:        *discoveryRate,
		DiscoveryBurst:       *discoveryBurst,
		DiscoveryDedupWindow: *discoveryDedup,
	})
	if err != nil {
		log.Fatalf("UDP relay: %v", err)
	}
	defer relay.Close()
	go func() {
		if err := relay.Serve(ctx); err != nil {
			log.Printf("UDP relay stopped: %v", err)
			stop()
		}
	}()

	go runLeaseSweeper(ctx, registry, relay, tickets, *peerLease, *heartbeatInterval)

	a := &api{registry: registry, relay: relay, tickets: tickets, heartbeatInterval: *heartbeatInterval, leaseTTL: *peerLease}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("POST /v1/peers", a.registerPeer)
	mux.HandleFunc("GET /v1/peers", a.listPeers)
	mux.HandleFunc("POST /v1/heartbeat", a.heartbeat)
	mux.HandleFunc("GET /v1/candidates", a.listCandidates)
	mux.HandleFunc("POST /v1/p2p/tickets", a.issueProbeTicket)
	mux.HandleFunc("GET /v1/p2p/tickets", a.listProbeTickets)

	httpServer := &http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	log.Printf("xray-mesh coordinator HTTP=%s UDP=%s prefix=%s peer_lease=%s heartbeat=%s p2p_ticket_ttl=%s discovery_rate=%.1f/s discovery_burst=%d discovery_dedup=%s", *listen, relay.Addr(), prefix, *peerLease, *heartbeatInterval, *p2pTicketTTL, *discoveryRate, *discoveryBurst, *discoveryDedup)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func runLeaseSweeper(ctx context.Context, registry *mesh.Registry, relay *transport.Relay, tickets *p2p.TicketManager, leaseTTL, heartbeatInterval time.Duration) {
	interval := heartbeatInterval
	if third := leaseTTL / 3; third > 0 && third < interval {
		interval = third
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			expired := registry.ExpireBefore(now.UTC().Add(-leaseTTL))
			for _, peer := range expired {
				relay.ForgetPeer(peer.NodeID)
				if tickets != nil {
					tickets.ForgetNode(peer.NodeID)
				}
				log.Printf("expired peer node=%s virtual_ip=%s", peer.NodeID, peer.VirtualIP)
			}
			if tickets != nil {
				tickets.Prune()
			}
		}
	}
}

func (a *api) registerPeer(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req registerRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	peer, err := a.registry.Register(req.NodeID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	registration := peer.Registration(a.registry.Prefix())
	registration.HeartbeatIntervalSeconds = durationSeconds(a.heartbeatInterval)
	registration.LeaseTTLSeconds = durationSeconds(a.leaseTTL)
	writeJSON(w, http.StatusCreated, registration)
}

func (a *api) heartbeat(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.authenticatedPeer(r); !ok {
		http.Error(w, "invalid or expired session", http.StatusUnauthorized)
		return
	}
	token := bearerToken(r.Header.Get("Authorization"))
	if _, ok := a.registry.TouchByToken(token, time.Now().UTC()); !ok {
		http.Error(w, "invalid or expired session", http.StatusUnauthorized)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) listCandidates(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.authenticatedPeer(r); !ok {
		http.Error(w, "invalid or expired session", http.StatusUnauthorized)
		return
	}
	observed := a.relay.Candidates()
	candidates := make([]p2p.Candidate, 0, len(observed))
	for _, candidate := range observed {
		peer, ok := a.registry.Get(candidate.NodeID)
		if !ok {
			continue
		}
		candidates = append(candidates, p2p.Candidate{
			NodeID:     peer.NodeID,
			VirtualIP:  peer.VirtualIP.String(),
			Endpoint:   candidate.Endpoint,
			Kind:       p2p.CandidateRelayObserved,
			ObservedAt: candidate.ObservedAt,
		})
	}
	writeJSON(w, http.StatusOK, candidates)
}

func (a *api) issueProbeTicket(w http.ResponseWriter, r *http.Request) {
	source, ok := a.authenticatedPeer(r)
	if !ok {
		http.Error(w, "invalid or expired session", http.StatusUnauthorized)
		return
	}
	if a.tickets == nil {
		http.Error(w, "P2P tickets unavailable", http.StatusServiceUnavailable)
		return
	}
	defer r.Body.Close()
	var req probeTicketRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	targetNode := strings.TrimSpace(req.TargetNode)
	if targetNode == "" {
		http.Error(w, "target_node is required", http.StatusBadRequest)
		return
	}
	if targetNode == source.NodeID {
		http.Error(w, p2p.ErrTicketSelfPair.Error(), http.StatusBadRequest)
		return
	}
	if _, ok := a.registry.Get(targetNode); !ok {
		http.Error(w, "target peer not found", http.StatusNotFound)
		return
	}
	ticket, err := a.tickets.Issue(source.NodeID, targetNode)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, ticket)
}

func (a *api) listProbeTickets(w http.ResponseWriter, r *http.Request) {
	peer, ok := a.authenticatedPeer(r)
	if !ok {
		http.Error(w, "invalid or expired session", http.StatusUnauthorized)
		return
	}
	if a.tickets == nil {
		http.Error(w, "P2P tickets unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, a.tickets.ListForNode(peer.NodeID))
}

func (a *api) authenticatedPeer(r *http.Request) (mesh.Peer, bool) {
	token := bearerToken(r.Header.Get("Authorization"))
	if token == "" {
		return mesh.Peer{}, false
	}
	return a.registry.GetByToken(token)
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, prefix))
}

func durationSeconds(d time.Duration) int {
	seconds := int(d / time.Second)
	if seconds < 1 {
		return 1
	}
	return seconds
}

func (a *api) listPeers(w http.ResponseWriter, _ *http.Request) {
	peers := a.registry.List()
	views := make([]mesh.PeerView, 0, len(peers))
	for _, p := range peers {
		views = append(views, p.View())
	}
	writeJSON(w, http.StatusOK, views)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

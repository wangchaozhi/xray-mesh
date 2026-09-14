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
	"syscall"
	"time"

	"github.com/wangchaozhi/xray-mesh/internal/mesh"
	"github.com/wangchaozhi/xray-mesh/internal/transport"
)

type registerRequest struct {
	NodeID string `json:"node_id"`
}

type api struct {
	registry *mesh.Registry
}

func main() {
	listen := flag.String("listen", "127.0.0.1:8666", "HTTP listen address")
	relayListen := flag.String("relay-listen", "127.0.0.1:8667", "UDP relay listen address")
	prefixText := flag.String("prefix", "10.66.0.0/24", "virtual IPv4 prefix")
	discoveryRate := flag.Float64("discovery-rate", 20, "sustained mDNS/SSDP packets per second allowed per peer")
	discoveryBurst := flag.Int("discovery-burst", 40, "maximum mDNS/SSDP token-bucket burst per peer")
	discoveryDedup := flag.Duration("discovery-dedup", 750*time.Millisecond, "suppress identical discovery packets from one peer during this window; 0 disables")
	flag.Parse()

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

	a := &api{registry: registry}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("POST /v1/peers", a.registerPeer)
	mux.HandleFunc("GET /v1/peers", a.listPeers)

	httpServer := &http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	log.Printf("xray-mesh coordinator HTTP=%s UDP=%s prefix=%s discovery_rate=%.1f/s discovery_burst=%d discovery_dedup=%s", *listen, relay.Addr(), prefix, *discoveryRate, *discoveryBurst, *discoveryDedup)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
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
	writeJSON(w, http.StatusCreated, peer.Registration(a.registry.Prefix()))
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

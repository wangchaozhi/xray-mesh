package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/netip"

	"github.com/wangchaozhi/xray-mesh/internal/mesh"
)

type registerRequest struct {
	NodeID string `json:"node_id"`
}

type api struct {
	registry *mesh.Registry
}

func main() {
	listen := flag.String("listen", "127.0.0.1:8666", "HTTP listen address")
	prefixText := flag.String("prefix", "10.66.0.0/24", "virtual IPv4 prefix")
	flag.Parse()

	prefix, err := netip.ParsePrefix(*prefixText)
	if err != nil {
		log.Fatalf("invalid prefix: %v", err)
	}
	registry, err := mesh.NewRegistry(prefix)
	if err != nil {
		log.Fatalf("registry: %v", err)
	}

	a := &api{registry: registry}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("POST /v1/peers", a.registerPeer)
	mux.HandleFunc("GET /v1/peers", a.listPeers)

	log.Printf("xray-mesh coordinator listening on %s, prefix=%s", *listen, prefix)
	log.Fatal(http.ListenAndServe(*listen, mux))
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
	writeJSON(w, http.StatusCreated, peer.View())
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

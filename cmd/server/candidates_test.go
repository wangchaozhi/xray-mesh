package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/wangchaozhi/xray-mesh/internal/mesh"
	"github.com/wangchaozhi/xray-mesh/internal/p2p"
	"github.com/wangchaozhi/xray-mesh/internal/transport"
)

func TestListCandidatesRequiresAuthentication(t *testing.T) {
	registry, _ := mesh.NewRegistry(netip.MustParsePrefix("10.66.0.0/24"))
	relay, err := transport.ListenRelay("127.0.0.1:0", registry)
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	a := &api{registry: registry, relay: relay}

	req := httptest.NewRequest(http.MethodGet, "/v1/candidates", nil)
	rec := httptest.NewRecorder()
	a.listCandidates(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestListCandidatesReturnsObservedPeerEndpoint(t *testing.T) {
	registry, err := mesh.NewRegistry(netip.MustParsePrefix("10.66.0.0/24"))
	if err != nil {
		t.Fatal(err)
	}
	alpha, _ := registry.Register("alpha")
	beta, _ := registry.Register("beta")
	relay, err := transport.ListenRelay("127.0.0.1:0", registry)
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = relay.Serve(ctx) }()

	remote := relay.Addr().(*net.UDPAddr)
	conn, err := net.DialUDP("udp", nil, remote)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	hello, _ := transport.MarshalHello(beta.SessionToken)
	if _, err := conn.Write(hello); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(time.Second)
	for len(relay.Candidates()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if len(relay.Candidates()) == 0 {
		t.Fatal("relay did not observe beta endpoint")
	}

	a := &api{registry: registry, relay: relay}
	req := httptest.NewRequest(http.MethodGet, "/v1/candidates", nil)
	req.Header.Set("Authorization", "Bearer "+alpha.SessionToken)
	rec := httptest.NewRecorder()
	a.listCandidates(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var candidates []p2p.Candidate
	if err := json.NewDecoder(rec.Body).Decode(&candidates); err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates=%#v", candidates)
	}
	candidate := candidates[0]
	if candidate.NodeID != "beta" || candidate.VirtualIP != beta.VirtualIP.String() || candidate.Kind != p2p.CandidateRelayObserved {
		t.Fatalf("candidate=%#v", candidate)
	}
	if candidate.Endpoint != conn.LocalAddr().String() {
		t.Fatalf("endpoint=%q want=%q", candidate.Endpoint, conn.LocalAddr())
	}
}

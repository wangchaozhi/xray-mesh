package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/wangchaozhi/xray-mesh/internal/mesh"
)

func TestRegisterPeerReturnsLeasePolicy(t *testing.T) {
	registry, err := mesh.NewRegistry(netip.MustParsePrefix("10.66.0.0/24"))
	if err != nil {
		t.Fatal(err)
	}
	a := &api{registry: registry, heartbeatInterval: 15 * time.Second, leaseTTL: 60 * time.Second}

	req := httptest.NewRequest(http.MethodPost, "/v1/peers", bytes.NewBufferString(`{"node_id":"alpha"}`))
	req.Body = io.NopCloser(bytes.NewBufferString(`{"node_id":"alpha"}`))
	rec := httptest.NewRecorder()
	a.registerPeer(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var registration mesh.RegistrationView
	if err := json.NewDecoder(rec.Body).Decode(&registration); err != nil {
		t.Fatal(err)
	}
	if registration.HeartbeatIntervalSeconds != 15 || registration.LeaseTTLSeconds != 60 {
		t.Fatalf("heartbeat=%d lease=%d", registration.HeartbeatIntervalSeconds, registration.LeaseTTLSeconds)
	}
	if registration.SessionToken == "" {
		t.Fatal("missing session token")
	}
}

func TestHeartbeatRequiresCurrentBearerToken(t *testing.T) {
	registry, err := mesh.NewRegistry(netip.MustParsePrefix("10.66.0.0/24"))
	if err != nil {
		t.Fatal(err)
	}
	peer, err := registry.Register("alpha")
	if err != nil {
		t.Fatal(err)
	}
	a := &api{registry: registry, heartbeatInterval: 15 * time.Second, leaseTTL: 60 * time.Second}

	badReq := httptest.NewRequest(http.MethodPost, "/v1/heartbeat", nil)
	badReq.Header.Set("Authorization", "Bearer stale-token")
	badRec := httptest.NewRecorder()
	a.heartbeat(badRec, badReq)
	if badRec.Code != http.StatusUnauthorized {
		t.Fatalf("bad token status=%d", badRec.Code)
	}

	before := peer.LastSeen
	goodReq := httptest.NewRequest(http.MethodPost, "/v1/heartbeat", nil)
	goodReq.Header.Set("Authorization", "Bearer "+peer.SessionToken)
	goodRec := httptest.NewRecorder()
	a.heartbeat(goodRec, goodReq)
	if goodRec.Code != http.StatusNoContent {
		t.Fatalf("good token status=%d body=%s", goodRec.Code, goodRec.Body.String())
	}
	updated, ok := registry.Get("alpha")
	if !ok || updated.LastSeen.Before(before) {
		t.Fatalf("heartbeat did not refresh last_seen: before=%s after=%s", before, updated.LastSeen)
	}
}

func TestBearerToken(t *testing.T) {
	if got := bearerToken("Bearer abc123"); got != "abc123" {
		t.Fatalf("got %q", got)
	}
	if got := bearerToken("Basic abc123"); got != "" {
		t.Fatalf("unexpected token %q", got)
	}
}

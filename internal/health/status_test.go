package health

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTrackerHealthTransitions(t *testing.T) {
	tracker := New("node-a", "10.66.0.2", "10.66.0.0/24", true, false)
	if tracker.Healthy() {
		t.Fatal("Healthy() = true while mesh is starting")
	}

	tracker.SetMesh(StateRunning, nil)
	if !tracker.Healthy() {
		t.Fatal("Healthy() = false while enabled mesh is running")
	}

	tracker.SetMesh(StateFailed, errors.New("relay down"))
	if tracker.Healthy() {
		t.Fatal("Healthy() = true after mesh failure")
	}
	if got := tracker.Snapshot().Mesh.Error; got != "relay down" {
		t.Fatalf("mesh error = %q, want relay down", got)
	}
}

func TestHealthHandler(t *testing.T) {
	tracker := New("node-a", "10.66.0.2", "10.66.0.0/24", false, true)
	handler := tracker.Handler()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("starting health status = %d, want %d", resp.Code, http.StatusServiceUnavailable)
	}

	tracker.SetXray(StateRunning, nil)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("running health status = %d, want %d", resp.Code, http.StatusOK)
	}

	statusReq := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	statusResp := httptest.NewRecorder()
	handler.ServeHTTP(statusResp, statusReq)
	var snap Snapshot
	if err := json.NewDecoder(statusResp.Body).Decode(&snap); err != nil {
		t.Fatal(err)
	}
	if snap.NodeID != "node-a" || snap.Xray.State != StateRunning {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}
}

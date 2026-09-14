package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSendHeartbeatUsesBearerToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method=%s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token-123" {
			t.Fatalf("Authorization=%q", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := &http.Client{Timeout: time.Second}
	if err := sendHeartbeat(context.Background(), client, server.URL, "token-123"); err != nil {
		t.Fatal(err)
	}
}

func TestSendHeartbeatUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "expired", http.StatusUnauthorized)
	}))
	defer server.Close()

	err := sendHeartbeat(context.Background(), &http.Client{Timeout: time.Second}, server.URL, "bad")
	if !errors.Is(err, errHeartbeatUnauthorized) {
		t.Fatalf("error=%v, want errHeartbeatUnauthorized", err)
	}
}

func TestHeartbeatLoopStopsAfterConsecutiveFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "temporary failure", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	waitCh := heartbeatLoop(ctx, &http.Client{Timeout: time.Second}, server.URL, "token", 5*time.Millisecond)

	select {
	case err := <-waitCh:
		if err == nil {
			t.Fatal("heartbeat loop returned nil error")
		}
	case <-ctx.Done():
		t.Fatal("heartbeat loop did not stop after consecutive failures")
	}
}

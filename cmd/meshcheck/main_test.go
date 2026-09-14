package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/wangchaozhi/xray-mesh/internal/health"
	"github.com/wangchaozhi/xray-mesh/internal/telemetry"
)

func TestParsePingRTT(t *testing.T) {
	for _, tc := range []struct {
		name   string
		output string
		want   time.Duration
		ok     bool
	}{
		{name: "normal", output: "64 bytes from 10.66.0.3: time=12.4 ms", want: 12400 * time.Microsecond, ok: true},
		{name: "submillisecond", output: "64 bytes from 10.66.0.3: time<1 ms", want: time.Millisecond, ok: true},
		{name: "missing", output: "1 packets transmitted, 0 received", ok: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parsePingRTT(tc.output)
			if ok != tc.ok {
				t.Fatalf("ok=%t, want %t", ok, tc.ok)
			}
			if ok && got != tc.want {
				t.Fatalf("RTT=%s, want %s", got, tc.want)
			}
		})
	}
}

func TestSummarizePings(t *testing.T) {
	got := summarizePings(4, []time.Duration{3 * time.Millisecond, time.Millisecond, 2 * time.Millisecond})
	if got.Attempts != 4 || got.Success != 3 {
		t.Fatalf("counts=%+v", got)
	}
	if got.MinMS != 1 || got.AvgMS != 2 || got.MaxMS != 3 {
		t.Fatalf("RTT summary=%+v", got)
	}
}

func TestReadStatus(t *testing.T) {
	want := health.Snapshot{
		NodeID:        "node-a",
		VirtualIP:     "10.66.0.2",
		NetworkPrefix: "10.66.0.0/24",
		Mesh:          health.ServiceStatus{Enabled: true, State: health.StateRunning},
		P2P: telemetry.P2PSnapshot{
			DirectHealthyMarks: 2,
			DirectTX:           10,
			DirectRX:           9,
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/status" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(want)
	}))
	defer server.Close()

	c := &checker{
		cfg: config{statusURL: server.URL + "/v1/status", peerIP: netip.MustParseAddr("10.66.0.3")},
		client: server.Client(),
	}
	got, err := c.readStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.NodeID != want.NodeID || got.VirtualIP != want.VirtualIP || got.P2P.DirectTX != want.P2P.DirectTX {
		t.Fatalf("status=%+v, want=%+v", got, want)
	}
}

func TestSleepContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if err := sleepContext(ctx, time.Second); err == nil {
		t.Fatal("expected cancellation error")
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("canceled sleep returned too slowly")
	}
}

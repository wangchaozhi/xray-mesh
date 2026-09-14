package transport

import (
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/wangchaozhi/xray-mesh/internal/mesh"
)

func TestRelayCandidatesTrackObservedEndpoint(t *testing.T) {
	registry, err := mesh.NewRegistry(netip.MustParsePrefix("10.66.0.0/24"))
	if err != nil {
		t.Fatal(err)
	}
	peer, err := registry.Register("alpha")
	if err != nil {
		t.Fatal(err)
	}
	relay, err := ListenRelay("127.0.0.1:0", registry)
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()

	now := time.Unix(1000, 0).UTC()
	relay.now = func() time.Time { return now }
	addr := &net.UDPAddr{IP: net.ParseIP("203.0.113.10"), Port: 45678}
	hello, err := MarshalHello(peer.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if err := relay.handleDatagram(hello, addr); err != nil {
		t.Fatal(err)
	}

	candidates := relay.Candidates()
	if len(candidates) != 1 {
		t.Fatalf("candidates=%#v", candidates)
	}
	candidate := candidates[0]
	if candidate.NodeID != "alpha" || candidate.Endpoint != "203.0.113.10:45678" || !candidate.ObservedAt.Equal(now) {
		t.Fatalf("candidate=%#v", candidate)
	}

	relay.ForgetPeer("alpha")
	if candidates := relay.Candidates(); len(candidates) != 0 {
		t.Fatalf("candidate survived ForgetPeer: %#v", candidates)
	}
}

func TestRelayCandidatesRefreshTimestampAndEndpoint(t *testing.T) {
	registry, _ := mesh.NewRegistry(netip.MustParsePrefix("10.66.0.0/24"))
	peer, _ := registry.Register("alpha")
	relay, err := ListenRelay("127.0.0.1:0", registry)
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()

	now := time.Unix(2000, 0).UTC()
	relay.now = func() time.Time { return now }
	hello, _ := MarshalHello(peer.SessionToken)
	if err := relay.handleDatagram(hello, &net.UDPAddr{IP: net.ParseIP("203.0.113.10"), Port: 40000}); err != nil {
		t.Fatal(err)
	}

	now = now.Add(time.Second)
	if err := relay.handleDatagram(hello, &net.UDPAddr{IP: net.ParseIP("203.0.113.10"), Port: 40001}); err != nil {
		t.Fatal(err)
	}
	candidate := relay.Candidates()[0]
	if candidate.Endpoint != "203.0.113.10:40001" || !candidate.ObservedAt.Equal(now) {
		t.Fatalf("candidate not refreshed: %#v", candidate)
	}
}

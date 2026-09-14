package main

import (
	"errors"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/wangchaozhi/xray-mesh/internal/mesh"
	"github.com/wangchaozhi/xray-mesh/internal/p2p"
)

func TestDirectPayloadReplayRejected(t *testing.T) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	now := time.Now().UTC()
	alphaIP := netip.MustParseAddr("10.66.0.2")
	betaIP := netip.MustParseAddr("10.66.0.3")
	ticket := p2p.ProbeTicket{
		Ticket:     "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		SourceNode: "alpha",
		TargetNode: "beta",
		IssuedAt:   now.Add(-time.Second),
		ExpiresAt:  now.Add(time.Minute),
	}
	ticketID, err := p2p.TicketIdentifier(ticket)
	if err != nil {
		t.Fatal(err)
	}
	runtime := newP2PRuntime("http://127.0.0.1", mesh.RegistrationView{NodeID: "beta", SessionToken: "b"}, conn, conn.LocalAddr().(*net.UDPAddr))
	runtime.now = func() time.Time { return now }
	runtime.tickets[ticketID] = ticket
	runtime.candidates["alpha"] = p2p.Candidate{NodeID: "alpha", VirtualIP: alphaIP.String(), Endpoint: "127.0.0.1:30001", ObservedAt: now}
	source := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 30001}
	packet := ipv4PacketForDirectReplayTest(alphaIP, betaIP)

	wire, err := p2p.SealDirectPayload(ticket, "alpha", "beta", packet, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, handled, err := runtime.HandleDirectPayload(wire, source, betaIP); !handled || err != nil {
		t.Fatalf("first delivery handled=%t err=%v", handled, err)
	}
	if _, handled, err := runtime.HandleDirectPayload(wire, source, betaIP); !handled || !errors.Is(err, ErrDirectPayloadReplay) {
		t.Fatalf("replay handled=%t err=%v, want ErrDirectPayloadReplay", handled, err)
	}

	freshWire, err := p2p.SealDirectPayload(ticket, "alpha", "beta", packet, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, handled, err := runtime.HandleDirectPayload(freshWire, source, betaIP); !handled || err != nil {
		t.Fatalf("fresh nonce handled=%t err=%v", handled, err)
	}
}

func ipv4PacketForDirectReplayTest(src, dst netip.Addr) []byte {
	p := make([]byte, 20)
	p[0] = 0x45
	s := src.As4()
	d := dst.As4()
	copy(p[12:16], s[:])
	copy(p[16:20], d[:])
	return p
}

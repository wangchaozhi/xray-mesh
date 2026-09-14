package main

import (
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/wangchaozhi/xray-mesh/internal/mesh"
	"github.com/wangchaozhi/xray-mesh/internal/p2p"
)

func TestDirectRuntimeRoundTripAndVirtualIPBinding(t *testing.T) {
	alphaConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer alphaConn.Close()
	betaConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer betaConn.Close()

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

	alpha := newP2PRuntime("http://127.0.0.1", mesh.RegistrationView{NodeID: "alpha", SessionToken: "a"}, alphaConn, alphaConn.LocalAddr().(*net.UDPAddr))
	beta := newP2PRuntime("http://127.0.0.1", mesh.RegistrationView{NodeID: "beta", SessionToken: "b"}, betaConn, betaConn.LocalAddr().(*net.UDPAddr))
	alpha.now = func() time.Time { return now }
	beta.now = func() time.Time { return now }
	alpha.tickets[ticketID] = ticket
	beta.tickets[ticketID] = ticket
	alpha.candidates["beta"] = p2p.Candidate{NodeID: "beta", VirtualIP: betaIP.String(), Endpoint: betaConn.LocalAddr().String(), ObservedAt: now}
	beta.candidates["alpha"] = p2p.Candidate{NodeID: "alpha", VirtualIP: alphaIP.String(), Endpoint: alphaConn.LocalAddr().String(), ObservedAt: now}
	if err := alpha.selector.MarkDirectHealthy("beta", betaConn.LocalAddr().String(), now); err != nil {
		t.Fatal(err)
	}

	packet := ipv4PacketForDirectTest(alphaIP, betaIP)
	sent, err := alpha.SendDirectPayload(betaIP, packet)
	if err != nil {
		t.Fatalf("send direct: %v", err)
	}
	if !sent {
		t.Fatal("expected direct send")
	}

	buf := make([]byte, 2048)
	_ = betaConn.SetReadDeadline(time.Now().Add(time.Second))
	n, source, err := betaConn.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	got, handled, err := beta.HandleDirectPayload(buf[:n], source, betaIP)
	if err != nil {
		t.Fatalf("handle direct: %v", err)
	}
	if !handled || string(got) != string(packet) {
		t.Fatalf("handled=%t got=%x want=%x", handled, got, packet)
	}
	selection := beta.Selection("alpha")
	if selection.Kind != p2p.PathDirect || selection.Endpoint != source.String() {
		t.Fatalf("beta selection=%#v", selection)
	}

	spoofed := ipv4PacketForDirectTest(netip.MustParseAddr("10.66.0.99"), betaIP)
	wire, err := p2p.SealDirectPayload(ticket, "alpha", "beta", spoofed, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, handled, err := beta.HandleDirectPayload(wire, source, betaIP); !handled || err == nil {
		t.Fatalf("spoofed packet handled=%t err=%v; want handled with rejection", handled, err)
	}
}

func TestDirectRuntimeFallsBackWithoutHealthyPath(t *testing.T) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	runtime := newP2PRuntime("http://127.0.0.1", mesh.RegistrationView{NodeID: "alpha", SessionToken: "a"}, conn, conn.LocalAddr().(*net.UDPAddr))
	runtime.candidates["beta"] = p2p.Candidate{NodeID: "beta", VirtualIP: "10.66.0.3", Endpoint: "127.0.0.1:9999", ObservedAt: time.Now().UTC()}
	sent, err := runtime.SendDirectPayload(netip.MustParseAddr("10.66.0.3"), ipv4PacketForDirectTest(netip.MustParseAddr("10.66.0.2"), netip.MustParseAddr("10.66.0.3")))
	if err != nil {
		t.Fatal(err)
	}
	if sent {
		t.Fatal("expected relay fallback when direct path is not healthy")
	}
}

func ipv4PacketForDirectTest(src, dst netip.Addr) []byte {
	p := make([]byte, 20)
	p[0] = 0x45
	s := src.As4()
	d := dst.As4()
	copy(p[12:16], s[:])
	copy(p[16:20], d[:])
	return p
}

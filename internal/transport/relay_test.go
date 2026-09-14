package transport

import (
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/wangchaozhi/xray-mesh/internal/mesh"
)

func TestRelayForwardsPeerPacket(t *testing.T) {
	registry, err := mesh.NewRegistry(netip.MustParsePrefix("10.66.0.0/24"))
	if err != nil {
		t.Fatal(err)
	}
	alpha, _ := registry.Register("alpha")
	beta, _ := registry.Register("beta")

	relay, err := ListenRelay("127.0.0.1:0", registry)
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()

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

	alphaHello, _ := MarshalHello(alpha.SessionToken)
	betaHello, _ := MarshalHello(beta.SessionToken)
	if err := relay.handleDatagram(alphaHello, alphaConn.LocalAddr().(*net.UDPAddr)); err != nil {
		t.Fatal(err)
	}
	if err := relay.handleDatagram(betaHello, betaConn.LocalAddr().(*net.UDPAddr)); err != nil {
		t.Fatal(err)
	}

	payload := ipv4Packet(alpha.VirtualIP, beta.VirtualIP)
	frame, _ := MarshalPacket(alpha.SessionToken, payload)
	if err := relay.handleDatagram(frame, alphaConn.LocalAddr().(*net.UDPAddr)); err != nil {
		t.Fatal(err)
	}

	if err := betaConn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 2048)
	n, _, err := betaConn.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	delivered, err := ParseFrame(buf[:n])
	if err != nil {
		t.Fatal(err)
	}
	if delivered.Type != FrameDeliver || string(delivered.Payload) != string(payload) {
		t.Fatalf("unexpected delivered frame: %#v", delivered)
	}
}

func TestRelayBroadcastsMDNS(t *testing.T) {
	registry, _ := mesh.NewRegistry(netip.MustParsePrefix("10.66.0.0/24"))
	alpha, _ := registry.Register("alpha")
	beta, _ := registry.Register("beta")
	relay, err := ListenRelay("127.0.0.1:0", registry)
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()

	alphaConn, _ := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	defer alphaConn.Close()
	betaConn, _ := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	defer betaConn.Close()

	alphaHello, _ := MarshalHello(alpha.SessionToken)
	betaHello, _ := MarshalHello(beta.SessionToken)
	if err := relay.handleDatagram(alphaHello, alphaConn.LocalAddr().(*net.UDPAddr)); err != nil {
		t.Fatal(err)
	}
	if err := relay.handleDatagram(betaHello, betaConn.LocalAddr().(*net.UDPAddr)); err != nil {
		t.Fatal(err)
	}

	payload := udpIPv4Packet(alpha.VirtualIP, mdnsMulticast, 5353, 5353)
	frame, _ := MarshalPacket(alpha.SessionToken, payload)
	if err := relay.handleDatagram(frame, alphaConn.LocalAddr().(*net.UDPAddr)); err != nil {
		t.Fatal(err)
	}

	_ = betaConn.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 2048)
	n, _, err := betaConn.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	delivered, err := ParseFrame(buf[:n])
	if err != nil {
		t.Fatal(err)
	}
	if delivered.Type != FrameDeliver || string(delivered.Payload) != string(payload) {
		t.Fatalf("unexpected discovery delivery: %#v", delivered)
	}
}

func TestDiscoveryGuardDedupDoesNotConsumeTokens(t *testing.T) {
	registry, _ := mesh.NewRegistry(netip.MustParsePrefix("10.66.0.0/24"))
	relay, err := ListenRelayWithOptions("127.0.0.1:0", registry, RelayOptions{
		DiscoveryRate:        1,
		DiscoveryBurst:       1,
		DiscoveryDedupWindow: 750 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()

	now := time.Unix(100, 0)
	relay.now = func() time.Time { return now }
	first := udpIPv4Packet(netip.MustParseAddr("10.66.0.2"), mdnsMulticast, 5353, 5353)

	duplicate, err := relay.guardDiscovery("alpha", first)
	if err != nil || duplicate {
		t.Fatalf("first packet duplicate=%v err=%v", duplicate, err)
	}
	duplicate, err = relay.guardDiscovery("alpha", first)
	if err != nil || !duplicate {
		t.Fatalf("second identical packet duplicate=%v err=%v", duplicate, err)
	}

	second := append([]byte(nil), first...)
	second = append(second, 0x01)
	duplicate, err = relay.guardDiscovery("alpha", second)
	if duplicate || !errors.Is(err, ErrDiscoveryRateLimited) {
		t.Fatalf("new packet duplicate=%v err=%v, want rate limited", duplicate, err)
	}

	now = now.Add(time.Second)
	duplicate, err = relay.guardDiscovery("alpha", second)
	if err != nil || duplicate {
		t.Fatalf("refilled packet duplicate=%v err=%v", duplicate, err)
	}
}

func TestDiscoveryGuardIsPerPeer(t *testing.T) {
	registry, _ := mesh.NewRegistry(netip.MustParsePrefix("10.66.0.0/24"))
	relay, err := ListenRelayWithOptions("127.0.0.1:0", registry, RelayOptions{
		DiscoveryRate:        1,
		DiscoveryBurst:       1,
		DiscoveryDedupWindow: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()

	now := time.Unix(200, 0)
	relay.now = func() time.Time { return now }
	payload := udpIPv4Packet(netip.MustParseAddr("10.66.0.2"), mdnsMulticast, 5353, 5353)

	if duplicate, err := relay.guardDiscovery("alpha", payload); err != nil || duplicate {
		t.Fatalf("alpha duplicate=%v err=%v", duplicate, err)
	}
	if duplicate, err := relay.guardDiscovery("beta", payload); err != nil || duplicate {
		t.Fatalf("beta duplicate=%v err=%v", duplicate, err)
	}
}

func TestRelayRejectsWrongDiscoveryPort(t *testing.T) {
	registry, _ := mesh.NewRegistry(netip.MustParsePrefix("10.66.0.0/24"))
	alpha, _ := registry.Register("alpha")
	relay, err := ListenRelay("127.0.0.1:0", registry)
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()

	payload := udpIPv4Packet(alpha.VirtualIP, mdnsMulticast, 5353, 9999)
	frame, _ := MarshalPacket(alpha.SessionToken, payload)
	err = relay.handleDatagram(frame, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345})
	if err == nil {
		t.Fatal("expected non-mDNS port to be rejected as an unknown destination")
	}
}

func TestRelayRejectsSpoofedSource(t *testing.T) {
	registry, _ := mesh.NewRegistry(netip.MustParsePrefix("10.66.0.0/24"))
	alpha, _ := registry.Register("alpha")
	beta, _ := registry.Register("beta")
	relay, err := ListenRelay("127.0.0.1:0", registry)
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()

	frame, _ := MarshalPacket(alpha.SessionToken, ipv4Packet(beta.VirtualIP, alpha.VirtualIP))
	err = relay.handleDatagram(frame, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345})
	if err == nil || errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected source-spoofing error, got %v", err)
	}
}

func ipv4Packet(src, dst netip.Addr) []byte {
	p := make([]byte, 20)
	p[0] = 0x45
	s := src.As4()
	d := dst.As4()
	copy(p[12:16], s[:])
	copy(p[16:20], d[:])
	return p
}

func udpIPv4Packet(src, dst netip.Addr, srcPort, dstPort uint16) []byte {
	p := make([]byte, 28)
	p[0] = 0x45
	p[9] = 17
	s := src.As4()
	d := dst.As4()
	copy(p[12:16], s[:])
	copy(p[16:20], d[:])
	binary.BigEndian.PutUint16(p[20:22], srcPort)
	binary.BigEndian.PutUint16(p[22:24], dstPort)
	binary.BigEndian.PutUint16(p[24:26], 8)
	return p
}

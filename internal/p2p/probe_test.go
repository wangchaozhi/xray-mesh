package p2p

import (
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

func testProbeTicket(now time.Time) ProbeTicket {
	return ProbeTicket{
		Ticket:     strings.Repeat("11", 32),
		SourceNode: "alpha",
		TargetNode: "beta",
		IssuedAt:   now,
		ExpiresAt:  now.Add(30 * time.Second),
	}
}

func TestProbeAndAckAuthentication(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	ticket := testProbeTicket(now)
	probe, err := newProbeWithNonce(ticket, strings.Repeat("22", 16))
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyProbe(probe, ticket, now); err != nil {
		t.Fatalf("VerifyProbe() error=%v", err)
	}
	ack, err := NewProbeAck(probe, ticket, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyProbeAck(ack, probe, ticket, now); err != nil {
		t.Fatalf("VerifyProbeAck() error=%v", err)
	}

	tampered := ack
	tampered.Nonce = strings.Repeat("33", 16)
	if err := VerifyProbeAck(tampered, probe, ticket, now); !errors.Is(err, ErrProbeAuthFailed) {
		t.Fatalf("tampered ack error=%v", err)
	}
}

func TestProbeTicketExpiry(t *testing.T) {
	now := time.Unix(2000, 0).UTC()
	ticket := testProbeTicket(now)
	probe, err := newProbeWithNonce(ticket, strings.Repeat("44", 16))
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyProbe(probe, ticket, ticket.ExpiresAt); !errors.Is(err, ErrProbeTicketExpired) {
		t.Fatalf("error=%v", err)
	}
}

func TestProbeWireRejectsNonProbeDatagram(t *testing.T) {
	if _, err := ParseProbeFrame([]byte("ordinary relay frame")); !errors.Is(err, ErrNotProbeDatagram) {
		t.Fatalf("error=%v", err)
	}
}

func TestProbeAckRoundTripOverUDP(t *testing.T) {
	now := time.Unix(3000, 0).UTC()
	ticket := testProbeTicket(now)
	probe, err := newProbeWithNonce(ticket, strings.Repeat("55", 16))
	if err != nil {
		t.Fatal(err)
	}

	alpha, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer alpha.Close()
	beta, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer beta.Close()

	probeWire, err := MarshalProbeFrame(probe)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := alpha.WriteToUDP(probeWire, beta.LocalAddr().(*net.UDPAddr)); err != nil {
		t.Fatal(err)
	}

	_ = beta.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 2048)
	n, source, err := beta.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	receivedProbe, err := ParseProbeFrame(buf[:n])
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyProbe(receivedProbe, ticket, now); err != nil {
		t.Fatal(err)
	}
	ack, err := NewProbeAck(receivedProbe, ticket, now)
	if err != nil {
		t.Fatal(err)
	}
	ackWire, err := MarshalProbeFrame(ack)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := beta.WriteToUDP(ackWire, source); err != nil {
		t.Fatal(err)
	}

	_ = alpha.SetReadDeadline(time.Now().Add(time.Second))
	n, _, err = alpha.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	receivedAck, err := ParseProbeFrame(buf[:n])
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyProbeAck(receivedAck, probe, ticket, now); err != nil {
		t.Fatal(err)
	}
}

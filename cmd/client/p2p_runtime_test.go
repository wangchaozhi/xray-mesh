package main

import (
	"net"
	"testing"
	"time"

	"github.com/wangchaozhi/xray-mesh/internal/mesh"
	"github.com/wangchaozhi/xray-mesh/internal/p2p"
)

func TestForgedAckDoesNotConsumePendingProbe(t *testing.T) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	now := time.Unix(1000, 0).UTC()
	ticket := p2p.ProbeTicket{
		Ticket:     "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		SourceNode: "alpha",
		TargetNode: "beta",
		IssuedAt:   now.Add(-time.Second),
		ExpiresAt:  now.Add(time.Minute),
	}
	probe, err := p2p.NewProbe(ticket)
	if err != nil {
		t.Fatal(err)
	}
	ticketID, err := p2p.TicketIdentifier(ticket)
	if err != nil {
		t.Fatal(err)
	}

	runtime := newP2PRuntime("http://127.0.0.1", mesh.RegistrationView{
		NodeID:       "alpha",
		SessionToken: "session-token",
	}, conn, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 9999})
	runtime.now = func() time.Time { return now }
	runtime.tickets[ticketID] = ticket
	runtime.pending[probe.Nonce] = pendingProbe{
		probe:  probe,
		ticket: ticket,
		target: "beta",
		sentAt: now,
	}

	ack, err := p2p.NewProbeAck(probe, ticket, now)
	if err != nil {
		t.Fatal(err)
	}
	forged := ack
	if forged.MAC[0] == '0' {
		forged.MAC = "1" + forged.MAC[1:]
	} else {
		forged.MAC = "0" + forged.MAC[1:]
	}
	wire, err := p2p.MarshalProbeFrame(forged)
	if err != nil {
		t.Fatal(err)
	}
	source := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 30001}
	if err := runtime.HandleDatagram(wire, source); err == nil {
		t.Fatal("expected forged ack to fail authentication")
	}
	if _, ok := runtime.peekPending(probe.Nonce); !ok {
		t.Fatal("forged ack consumed pending probe")
	}

	validWire, err := p2p.MarshalProbeFrame(ack)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.HandleDatagram(validWire, source); err != nil {
		t.Fatalf("valid ack failed: %v", err)
	}
	if _, ok := runtime.peekPending(probe.Nonce); ok {
		t.Fatal("valid ack did not consume pending probe")
	}
	selection := runtime.Selection("beta")
	if selection.Path != p2p.PathDirect || selection.Endpoint != source.String() {
		t.Fatalf("selection=%#v, want direct %s", selection, source)
	}
}

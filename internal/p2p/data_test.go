package p2p

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

func testDirectTicket(now time.Time) ProbeTicket {
	return ProbeTicket{
		Ticket:     "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		SourceNode: "alpha",
		TargetNode: "beta",
		IssuedAt:   now.Add(-time.Second),
		ExpiresAt:  now.Add(time.Minute),
	}
}

func TestDirectPayloadRoundTripBothDirections(t *testing.T) {
	now := time.Now().UTC()
	ticket := testDirectTicket(now)
	payload := []byte{0x45, 0, 0, 20, 1, 2, 3, 4}

	for _, tc := range []struct {
		source string
		target string
	}{
		{source: "alpha", target: "beta"},
		{source: "beta", target: "alpha"},
	} {
		wire, err := SealDirectPayload(ticket, tc.source, tc.target, payload, now)
		if err != nil {
			t.Fatalf("seal %s->%s: %v", tc.source, tc.target, err)
		}
		source, target, got, err := OpenDirectPayload(wire, ticket, tc.target, now)
		if err != nil {
			t.Fatalf("open %s->%s: %v", tc.source, tc.target, err)
		}
		if source != tc.source || target != tc.target || !bytes.Equal(got, payload) {
			t.Fatalf("round trip source=%q target=%q payload=%x", source, target, got)
		}
	}
}

func TestDirectPayloadRejectsTamperingAndWrongTarget(t *testing.T) {
	now := time.Now().UTC()
	ticket := testDirectTicket(now)
	wire, err := SealDirectPayload(ticket, "alpha", "beta", []byte("secret-packet"), now)
	if err != nil {
		t.Fatal(err)
	}

	tampered := append([]byte(nil), wire...)
	tampered[len(tampered)-1] ^= 0x01
	if _, _, _, err := OpenDirectPayload(tampered, ticket, "beta", now); !errors.Is(err, ErrDirectPayloadAuthFail) {
		t.Fatalf("tamper error=%v, want ErrDirectPayloadAuthFail", err)
	}
	if _, _, _, err := OpenDirectPayload(wire, ticket, "gamma", now); !errors.Is(err, ErrDirectPayloadAuthFail) {
		t.Fatalf("wrong target error=%v, want ErrDirectPayloadAuthFail", err)
	}
}

func TestDirectPayloadRejectsExpiredTicketAndTrailingBytes(t *testing.T) {
	now := time.Now().UTC()
	ticket := testDirectTicket(now)
	wire, err := SealDirectPayload(ticket, "alpha", "beta", []byte("packet"), now)
	if err != nil {
		t.Fatal(err)
	}
	withTrailing := append(append([]byte(nil), wire...), 0)
	if _, _, _, err := OpenDirectPayload(withTrailing, ticket, "beta", now); !errors.Is(err, ErrInvalidDirectPayload) {
		t.Fatalf("trailing bytes error=%v, want ErrInvalidDirectPayload", err)
	}
	if _, _, _, err := OpenDirectPayload(wire, ticket, "beta", ticket.ExpiresAt); !errors.Is(err, ErrProbeTicketExpired) {
		t.Fatalf("expired error=%v, want ErrProbeTicketExpired", err)
	}
}

func TestDirectPayloadRejectsNonPairSender(t *testing.T) {
	now := time.Now().UTC()
	ticket := testDirectTicket(now)
	if _, err := SealDirectPayload(ticket, "alpha", "gamma", []byte("packet"), now); !errors.Is(err, ErrInvalidDirectPayload) {
		t.Fatalf("error=%v, want ErrInvalidDirectPayload", err)
	}
}

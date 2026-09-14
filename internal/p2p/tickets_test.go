package p2p

import (
	"errors"
	"testing"
	"time"
)

func TestProbeTicketPairScopeAndVisibility(t *testing.T) {
	manager := NewTicketManager(30 * time.Second)
	now := time.Unix(1000, 0).UTC()
	manager.now = func() time.Time { return now }

	ticket, err := manager.Issue("alpha", "beta")
	if err != nil {
		t.Fatal(err)
	}
	if len(ticket.Ticket) != 64 {
		t.Fatalf("ticket length=%d", len(ticket.Ticket))
	}
	if !ticket.IssuedAt.Equal(now) || !ticket.ExpiresAt.Equal(now.Add(30*time.Second)) {
		t.Fatalf("ticket timestamps=%#v", ticket)
	}
	if !manager.Verify(ticket.Ticket, "alpha", "beta") || !manager.Verify(ticket.Ticket, "beta", "alpha") {
		t.Fatal("ticket did not verify for its pair")
	}
	if manager.Verify(ticket.Ticket, "alpha", "gamma") {
		t.Fatal("ticket verified for unrelated peer")
	}

	if got := manager.ListForNode("alpha"); len(got) != 1 || got[0].Ticket != ticket.Ticket {
		t.Fatalf("alpha tickets=%#v", got)
	}
	if got := manager.ListForNode("beta"); len(got) != 1 || got[0].Ticket != ticket.Ticket {
		t.Fatalf("beta tickets=%#v", got)
	}
	if got := manager.ListForNode("gamma"); len(got) != 0 {
		t.Fatalf("gamma should not see pair ticket: %#v", got)
	}
}

func TestProbeTicketExpiryAndForgetNode(t *testing.T) {
	manager := NewTicketManager(10 * time.Second)
	now := time.Unix(2000, 0).UTC()
	manager.now = func() time.Time { return now }

	ab, err := manager.Issue("alpha", "beta")
	if err != nil {
		t.Fatal(err)
	}
	ac, err := manager.Issue("alpha", "charlie")
	if err != nil {
		t.Fatal(err)
	}

	manager.ForgetNode("beta")
	if manager.Verify(ab.Ticket, "alpha", "beta") {
		t.Fatal("forgotten peer ticket still verifies")
	}
	if !manager.Verify(ac.Ticket, "alpha", "charlie") {
		t.Fatal("unrelated ticket was removed")
	}

	now = now.Add(11 * time.Second)
	if manager.Verify(ac.Ticket, "alpha", "charlie") {
		t.Fatal("expired ticket still verifies")
	}
	if got := manager.ListForNode("alpha"); len(got) != 0 {
		t.Fatalf("expired tickets not pruned: %#v", got)
	}
}

func TestProbeTicketRejectsInvalidPairs(t *testing.T) {
	manager := NewTicketManager(time.Second)
	if _, err := manager.Issue("", "beta"); !errors.Is(err, ErrTicketNodeRequired) {
		t.Fatalf("error=%v", err)
	}
	if _, err := manager.Issue("alpha", "alpha"); !errors.Is(err, ErrTicketSelfPair) {
		t.Fatalf("error=%v", err)
	}
}

func TestProbeTicketDefaultTTL(t *testing.T) {
	manager := NewTicketManager(0)
	if manager.TTL() != defaultProbeTicketTTL {
		t.Fatalf("TTL=%s", manager.TTL())
	}
}

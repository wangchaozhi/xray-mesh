package mesh

import (
	"net/netip"
	"testing"
)

func TestRegistryRegister(t *testing.T) {
	r, err := NewRegistry(netip.MustParsePrefix("10.66.0.0/24"))
	if err != nil {
		t.Fatal(err)
	}

	alpha, err := r.Register("alpha")
	if err != nil {
		t.Fatal(err)
	}
	beta, err := r.Register("beta")
	if err != nil {
		t.Fatal(err)
	}
	if alpha.VirtualIP == beta.VirtualIP {
		t.Fatalf("duplicate lease %s", alpha.VirtualIP)
	}
	if alpha.SessionToken == "" || alpha.SessionToken == beta.SessionToken {
		t.Fatal("expected unique non-empty session tokens")
	}

	again, err := r.Register("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if again != alpha {
		t.Fatalf("registration is not idempotent: %#v != %#v", again, alpha)
	}

	byIP, ok := r.GetByVirtualIP(alpha.VirtualIP)
	if !ok || byIP != alpha {
		t.Fatalf("virtual IP lookup failed: %#v %v", byIP, ok)
	}
	byToken, ok := r.GetByToken(alpha.SessionToken)
	if !ok || byToken != alpha {
		t.Fatalf("token lookup failed: %#v %v", byToken, ok)
	}
}

func TestRegistryRejectsBlankNode(t *testing.T) {
	r, err := NewRegistry(netip.MustParsePrefix("10.66.0.0/24"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Register("   "); err != ErrInvalidNodeID {
		t.Fatalf("got %v, want %v", err, ErrInvalidNodeID)
	}
}

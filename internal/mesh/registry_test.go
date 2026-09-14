package mesh

import (
	"net/netip"
	"testing"
	"time"
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
	if alpha.LastSeen.IsZero() || beta.LastSeen.IsZero() {
		t.Fatal("expected registration to initialize LastSeen")
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

func TestRegistryTouchAndExpire(t *testing.T) {
	r, err := NewRegistry(netip.MustParsePrefix("10.66.0.0/24"))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Unix(1000, 0).UTC()
	alpha, err := r.registerAt("alpha", start)
	if err != nil {
		t.Fatal(err)
	}
	beta, err := r.registerAt("beta", start)
	if err != nil {
		t.Fatal(err)
	}

	touchedAt := start.Add(30 * time.Second)
	touched, ok := r.TouchByToken(alpha.SessionToken, touchedAt)
	if !ok || !touched.LastSeen.Equal(touchedAt) {
		t.Fatalf("touch failed: %#v %v", touched, ok)
	}

	expired := r.ExpireBefore(start.Add(20 * time.Second))
	if len(expired) != 1 || expired[0].NodeID != beta.NodeID {
		t.Fatalf("expired=%#v, want beta only", expired)
	}
	if _, ok := r.Get(beta.NodeID); ok {
		t.Fatal("expired peer remains in node lookup")
	}
	if _, ok := r.GetByVirtualIP(beta.VirtualIP); ok {
		t.Fatal("expired peer remains in IP lookup")
	}
	if _, ok := r.GetByToken(beta.SessionToken); ok {
		t.Fatal("expired peer remains in token lookup")
	}
	if _, ok := r.Get(alpha.NodeID); !ok {
		t.Fatal("recently touched peer was expired")
	}
}

func TestExpiredLeaseReleasesVirtualIP(t *testing.T) {
	r, err := NewRegistry(netip.MustParsePrefix("10.66.0.0/30"))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Unix(2000, 0).UTC()
	alpha, err := r.registerAt("alpha", start)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.registerAt("beta", start); err != ErrPoolExhausted {
		t.Fatalf("second registration error=%v, want ErrPoolExhausted", err)
	}

	expired := r.ExpireBefore(start)
	if len(expired) != 1 || expired[0].NodeID != "alpha" {
		t.Fatalf("expired=%#v", expired)
	}
	beta, err := r.registerAt("beta", start.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if beta.VirtualIP != alpha.VirtualIP {
		t.Fatalf("released address not reused: beta=%s alpha=%s", beta.VirtualIP, alpha.VirtualIP)
	}
	if beta.SessionToken == alpha.SessionToken {
		t.Fatal("new lease reused old session token")
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

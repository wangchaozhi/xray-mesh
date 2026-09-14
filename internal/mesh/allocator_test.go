package mesh

import (
	"errors"
	"net/netip"
	"testing"
)

func TestAllocatorStableLease(t *testing.T) {
	a, err := NewAllocator(netip.MustParsePrefix("10.66.0.0/29"))
	if err != nil {
		t.Fatal(err)
	}

	first, err := a.Allocate("alpha")
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.Allocate("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("lease changed: %s != %s", first, second)
	}
	if want := netip.MustParseAddr("10.66.0.2"); first != want {
		t.Fatalf("got %s, want %s", first, want)
	}
}

func TestAllocatorExhaustion(t *testing.T) {
	a, err := NewAllocator(netip.MustParsePrefix("10.66.0.0/30"))
	if err != nil {
		t.Fatal(err)
	}
	// /30 leaves only .2 after reserving .0, .1 and .3.
	if _, err := a.Allocate("alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Allocate("beta"); !errors.Is(err, ErrPoolExhausted) {
		t.Fatalf("got %v, want ErrPoolExhausted", err)
	}
}

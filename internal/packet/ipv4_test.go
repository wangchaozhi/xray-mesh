package packet

import (
	"errors"
	"net/netip"
	"testing"
)

func TestIPv4Endpoints(t *testing.T) {
	p := make([]byte, 20)
	p[0] = 0x45
	copy(p[12:16], []byte{10, 66, 0, 2})
	copy(p[16:20], []byte{10, 66, 0, 3})

	src, dst, err := IPv4Endpoints(p)
	if err != nil {
		t.Fatal(err)
	}
	if src != netip.MustParseAddr("10.66.0.2") || dst != netip.MustParseAddr("10.66.0.3") {
		t.Fatalf("got %s -> %s", src, dst)
	}
}

func TestIPv4EndpointsRejectsIPv6(t *testing.T) {
	p := make([]byte, 40)
	p[0] = 0x60
	if _, _, err := IPv4Endpoints(p); !errors.Is(err, ErrNotIPv4) {
		t.Fatalf("got %v, want ErrNotIPv4", err)
	}
}

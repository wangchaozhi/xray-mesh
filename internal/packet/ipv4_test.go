package packet

import (
	"encoding/binary"
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

func TestIPv4UDPPorts(t *testing.T) {
	p := make([]byte, 28)
	p[0] = 0x45
	p[9] = 17
	binary.BigEndian.PutUint16(p[20:22], 5353)
	binary.BigEndian.PutUint16(p[22:24], 5353)

	src, dst, err := IPv4UDPPorts(p)
	if err != nil {
		t.Fatal(err)
	}
	if src != 5353 || dst != 5353 {
		t.Fatalf("ports = %d -> %d", src, dst)
	}
}

func TestIPv4UDPPortsRejectsTCP(t *testing.T) {
	p := make([]byte, 28)
	p[0] = 0x45
	p[9] = 6
	if _, _, err := IPv4UDPPorts(p); !errors.Is(err, ErrNotUDP) {
		t.Fatalf("got %v, want ErrNotUDP", err)
	}
}

func TestIPv4UDPPortsRejectsNonInitialFragment(t *testing.T) {
	p := make([]byte, 28)
	p[0] = 0x45
	p[9] = 17
	binary.BigEndian.PutUint16(p[6:8], 1)
	if _, _, err := IPv4UDPPorts(p); !errors.Is(err, ErrFragmented) {
		t.Fatalf("got %v, want ErrFragmented", err)
	}
}

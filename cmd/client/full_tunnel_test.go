package main

import (
	"context"
	"net/netip"
	"testing"
)

func TestResolveProtectedPrefixesWithLiteralHosts(t *testing.T) {
	mesh := netip.MustParsePrefix("10.66.0.0/24")
	prefixes, err := resolveProtectedPrefixes(
		context.Background(),
		"http://203.0.113.9:8666",
		"203.0.113.9:8667",
		mesh,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(prefixes) != 2 {
		t.Fatalf("prefix count = %d, want 2: %#v", len(prefixes), prefixes)
	}
	if prefixes[0] != mesh.Masked() {
		t.Fatalf("first prefix = %s, want mesh %s", prefixes[0], mesh.Masked())
	}
	if prefixes[1] != netip.MustParsePrefix("203.0.113.9/32") {
		t.Fatalf("control prefix = %s", prefixes[1])
	}
}

func TestResolveHostIPv4LiteralIPv6IsIgnored(t *testing.T) {
	prefixes, err := resolveHostIPv4(context.Background(), "2001:db8::1")
	if err != nil {
		t.Fatal(err)
	}
	if len(prefixes) != 0 {
		t.Fatalf("IPv6 literal produced IPv4 prefixes: %#v", prefixes)
	}
}

//go:build linux

package main

import (
	"context"
	"testing"
)

func TestResolveRelayIPv4Literal(t *testing.T) {
	addrs, err := resolveRelayIPv4(context.Background(), "203.0.113.10")
	if err != nil {
		t.Fatal(err)
	}
	if len(addrs) != 1 || addrs[0].String() != "203.0.113.10" {
		t.Fatalf("addrs=%v", addrs)
	}
}

func TestResolveRelayIPv4RejectsIPv6(t *testing.T) {
	if _, err := resolveRelayIPv4(context.Background(), "2001:db8::1"); err == nil {
		t.Fatal("expected IPv6 relay to be rejected")
	}
}

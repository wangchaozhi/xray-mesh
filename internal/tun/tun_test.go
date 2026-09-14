package tun

import (
	"reflect"
	"testing"
)

func TestDiscoveryCommands(t *testing.T) {
	got := discoveryCommands("xrmesh0")
	want := [][]string{
		{"link", "set", "dev", "xrmesh0", "multicast", "on"},
		{"route", "replace", "224.0.0.251/32", "dev", "xrmesh0", "scope", "link"},
		{"route", "replace", "239.255.255.250/32", "dev", "xrmesh0", "scope", "link"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("discoveryCommands() = %#v, want %#v", got, want)
	}
}

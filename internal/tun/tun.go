package tun

import (
	"context"
	"net/netip"
)

var discoveryDestinations = []string{
	"224.0.0.251/32",   // mDNS
	"239.255.255.250/32", // SSDP
}

// Device abstracts a layer-3 TUN device. OS-specific implementations will be
// added behind this interface.
type Device interface {
	Name() string
	Configure(context.Context, netip.Prefix) error
	ReadPacket(context.Context, []byte) (int, error)
	WritePacket(context.Context, []byte) (int, error)
	Close() error
}

func discoveryCommands(name string) [][]string {
	commands := [][]string{
		{"link", "set", "dev", name, "multicast", "on"},
	}
	for _, destination := range discoveryDestinations {
		commands = append(commands, []string{"route", "replace", destination, "dev", name, "scope", "link"})
	}
	return commands
}

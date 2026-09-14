package tun

import (
	"context"
	"net/netip"
)

// Device abstracts a layer-3 TUN device. OS-specific implementations will be
// added behind this interface.
type Device interface {
	Name() string
	Configure(context.Context, netip.Prefix) error
	ReadPacket(context.Context, []byte) (int, error)
	WritePacket(context.Context, []byte) (int, error)
	Close() error
}

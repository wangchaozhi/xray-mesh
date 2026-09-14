//go:build !linux

package tun

import (
	"context"
	"errors"
)

var ErrUnsupported = errors.New("TUN implementation is currently available only on Linux")

func Open(string) (Device, error) { return nil, ErrUnsupported }

func ConfigureDiscovery(context.Context, Device) error { return ErrUnsupported }

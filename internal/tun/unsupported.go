//go:build !linux

package tun

import (
	"errors"
)

var ErrUnsupported = errors.New("TUN implementation is currently available only on Linux")

func Open(string) (Device, error) { return nil, ErrUnsupported }

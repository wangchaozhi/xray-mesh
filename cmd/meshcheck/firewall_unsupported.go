//go:build !linux

package main

import (
	"context"
	"errors"
)

type relayOnlyFirewall struct{}

func installRelayOnlyFirewall(context.Context, string) (*relayOnlyFirewall, error) {
	return nil, errors.New("-force-fallback is supported only on Linux")
}

func (f *relayOnlyFirewall) Close(context.Context) error { return nil }

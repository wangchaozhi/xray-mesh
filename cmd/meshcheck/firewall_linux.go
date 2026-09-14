//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

type relayOnlyFirewall struct {
	binary string
	chain  string
	once   sync.Once
	err    error
}

func installRelayOnlyFirewall(ctx context.Context, relayAddr string) (*relayOnlyFirewall, error) {
	if os.Geteuid() != 0 {
		return nil, errors.New("-force-fallback requires root so meshcheck can install temporary iptables rules")
	}
	binary, err := exec.LookPath("iptables")
	if err != nil {
		return nil, errors.New("iptables not found; install an iptables-compatible frontend or run without -force-fallback")
	}
	host, portText, err := net.SplitHostPort(relayAddr)
	if err != nil {
		return nil, fmt.Errorf("parse -relay %q: %w", relayAddr, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid relay UDP port %q", portText)
	}
	ips, err := resolveRelayIPv4(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("relay host %q has no IPv4 address", host)
	}

	chain := fmt.Sprintf("XRMCHK%x", os.Getpid())
	fw := &relayOnlyFirewall{binary: binary, chain: chain}
	if err := fw.run(ctx, "-N", chain); err != nil {
		return nil, fmt.Errorf("create fallback firewall chain: %w", err)
	}
	rollback := true
	defer func() {
		if rollback {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = fw.cleanup(cleanupCtx)
		}
	}()

	for _, ip := range ips {
		if err := fw.run(ctx, "-A", chain, "-p", "udp", "-d", ip.String(), "--dport", strconv.Itoa(port), "-j", "RETURN"); err != nil {
			return nil, fmt.Errorf("allow relay UDP %s:%d: %w", ip, port, err)
		}
	}
	if err := fw.run(ctx, "-A", chain, "-p", "udp", "-j", "DROP"); err != nil {
		return nil, fmt.Errorf("add non-relay UDP drop rule: %w", err)
	}
	if err := fw.run(ctx, "-I", "OUTPUT", "1", "-p", "udp", "-j", chain); err != nil {
		return nil, fmt.Errorf("activate fallback firewall chain: %w", err)
	}
	rollback = false
	return fw, nil
}

func resolveRelayIPv4(ctx context.Context, host string) ([]netip.Addr, error) {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" {
		return nil, errors.New("relay host is empty")
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		if !addr.Is4() {
			return nil, errors.New("forced fallback currently supports an IPv4 relay only")
		}
		return []netip.Addr{addr}, nil
	}
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip4", host)
	if err != nil {
		return nil, fmt.Errorf("resolve relay host %q: %w", host, err)
	}
	seen := make(map[netip.Addr]struct{})
	out := make([]netip.Addr, 0, len(addrs))
	for _, addr := range addrs {
		if !addr.Is4() {
			continue
		}
		if _, ok := seen[addr]; ok {
			continue
		}
		seen[addr] = struct{}{}
		out = append(out, addr)
	}
	return out, nil
}

func (f *relayOnlyFirewall) run(ctx context.Context, args ...string) error {
	fullArgs := append([]string{"-w", "3"}, args...)
	cmd := exec.CommandContext(ctx, f.binary, fullArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", f.binary, strings.Join(fullArgs, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (f *relayOnlyFirewall) Close(ctx context.Context) error {
	f.once.Do(func() {
		f.err = f.cleanup(ctx)
	})
	return f.err
}

func (f *relayOnlyFirewall) cleanup(ctx context.Context) error {
	var errs []error
	if err := f.run(ctx, "-D", "OUTPUT", "-p", "udp", "-j", f.chain); err != nil {
		errs = append(errs, err)
	}
	if err := f.run(ctx, "-F", f.chain); err != nil {
		errs = append(errs, err)
	}
	if err := f.run(ctx, "-X", f.chain); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

//go:build linux

package tun

import (
	"context"
	"encoding/binary"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"
)

const (
	tunSetIFF = 0x400454ca
	iffTUN    = 0x0001
	iffNoPI   = 0x1000
	ifNameLen = 16
)

type linuxDevice struct {
	name string
	file *os.File
}

// Open creates an L3 TUN interface. The caller normally needs CAP_NET_ADMIN.
func Open(name string) (Device, error) {
	if strings.TrimSpace(name) == "" {
		name = "xrmesh0"
	}
	if len(name) >= ifNameLen {
		return nil, fmt.Errorf("TUN name %q is too long", name)
	}
	f, err := os.OpenFile("/dev/net/tun", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open /dev/net/tun: %w", err)
	}

	var req [40]byte
	copy(req[:ifNameLen], name)
	binary.LittleEndian.PutUint16(req[16:18], iffTUN|iffNoPI)
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), tunSetIFF, uintptr(unsafe.Pointer(&req[0])))
	if errno != 0 {
		_ = f.Close()
		return nil, fmt.Errorf("TUNSETIFF: %w", errno)
	}
	actual := strings.TrimRight(string(req[:ifNameLen]), "\x00")
	return &linuxDevice{name: actual, file: f}, nil
}

func (d *linuxDevice) Name() string { return d.name }

func (d *linuxDevice) Configure(ctx context.Context, addr netip.Prefix) error {
	if !addr.IsValid() || !addr.Addr().Is4() {
		return fmt.Errorf("TUN address must be a valid IPv4 prefix")
	}
	commands := [][]string{
		{"link", "set", "dev", d.name, "mtu", "1280", "up"},
		{"addr", "replace", addr.String(), "dev", d.name},
	}
	for _, args := range commands {
		cmd := exec.CommandContext(ctx, "ip", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("ip %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func (d *linuxDevice) ReadPacket(_ context.Context, b []byte) (int, error) {
	return d.file.Read(b)
}

func (d *linuxDevice) WritePacket(_ context.Context, b []byte) (int, error) {
	return d.file.Write(b)
}

func (d *linuxDevice) Close() error { return d.file.Close() }

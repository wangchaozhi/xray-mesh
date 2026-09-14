package packet

import (
	"errors"
	"net/netip"
)

var (
	ErrTooShort = errors.New("packet is too short")
	ErrNotIPv4  = errors.New("packet is not IPv4")
	ErrBadIHL   = errors.New("invalid IPv4 header length")
)

// IPv4Endpoints returns the source and destination addresses from one IPv4
// packet. It validates only the fields needed by the mesh data plane.
func IPv4Endpoints(b []byte) (src, dst netip.Addr, err error) {
	if len(b) < 20 {
		return netip.Addr{}, netip.Addr{}, ErrTooShort
	}
	if b[0]>>4 != 4 {
		return netip.Addr{}, netip.Addr{}, ErrNotIPv4
	}
	ihl := int(b[0]&0x0f) * 4
	if ihl < 20 || ihl > len(b) {
		return netip.Addr{}, netip.Addr{}, ErrBadIHL
	}
	src = netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]})
	dst = netip.AddrFrom4([4]byte{b[16], b[17], b[18], b[19]})
	return src, dst, nil
}

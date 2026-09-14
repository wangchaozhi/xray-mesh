package packet

import (
	"encoding/binary"
	"errors"
	"net/netip"
)

var (
	ErrTooShort   = errors.New("packet is too short")
	ErrNotIPv4    = errors.New("packet is not IPv4")
	ErrBadIHL     = errors.New("invalid IPv4 header length")
	ErrNotUDP     = errors.New("packet is not UDP")
	ErrUDPTooShort = errors.New("UDP header is too short")
	ErrFragmented = errors.New("non-initial IPv4 fragment")
)

// IPv4Endpoints returns the source and destination addresses from one IPv4
// packet. It validates only the fields needed by the mesh data plane.
func IPv4Endpoints(b []byte) (src, dst netip.Addr, err error) {
	ihl, err := ipv4HeaderLength(b)
	if err != nil {
		return netip.Addr{}, netip.Addr{}, err
	}
	_ = ihl
	src = netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]})
	dst = netip.AddrFrom4([4]byte{b[16], b[17], b[18], b[19]})
	return src, dst, nil
}

// IPv4UDPPorts returns the UDP source and destination ports from an IPv4
// packet. Non-initial fragments are rejected because they do not contain the
// transport header needed for discovery classification.
func IPv4UDPPorts(b []byte) (srcPort, dstPort uint16, err error) {
	ihl, err := ipv4HeaderLength(b)
	if err != nil {
		return 0, 0, err
	}
	if b[9] != 17 {
		return 0, 0, ErrNotUDP
	}
	fragment := binary.BigEndian.Uint16(b[6:8])
	if fragment&0x1fff != 0 {
		return 0, 0, ErrFragmented
	}
	if len(b) < ihl+8 {
		return 0, 0, ErrUDPTooShort
	}
	return binary.BigEndian.Uint16(b[ihl : ihl+2]), binary.BigEndian.Uint16(b[ihl+2 : ihl+4]), nil
}

func ipv4HeaderLength(b []byte) (int, error) {
	if len(b) < 20 {
		return 0, ErrTooShort
	}
	if b[0]>>4 != 4 {
		return 0, ErrNotIPv4
	}
	ihl := int(b[0]&0x0f) * 4
	if ihl < 20 || ihl > len(b) {
		return 0, ErrBadIHL
	}
	return ihl, nil
}

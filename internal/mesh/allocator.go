package mesh

import (
	"errors"
	"net/netip"
	"sync"
)

var ErrPoolExhausted = errors.New("virtual IP pool exhausted")

// Allocator leases IPv4 addresses from a prefix. Network, gateway (.1), and
// broadcast are reserved. It is safe for concurrent use.
type Allocator struct {
	mu     sync.Mutex
	prefix netip.Prefix
	next   uint32
	byNode map[string]netip.Addr
	used   map[netip.Addr]string
}

func NewAllocator(prefix netip.Prefix) (*Allocator, error) {
	if !prefix.IsValid() || !prefix.Addr().Is4() {
		return nil, errors.New("allocator requires a valid IPv4 prefix")
	}
	if prefix.Bits() > 30 {
		return nil, errors.New("prefix is too small for peer allocation")
	}
	return &Allocator{
		prefix: prefix.Masked(),
		next:   2,
		byNode: make(map[string]netip.Addr),
		used:   make(map[netip.Addr]string),
	}, nil
}

func (a *Allocator) Allocate(nodeID string) (netip.Addr, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if ip, ok := a.byNode[nodeID]; ok {
		return ip, nil
	}

	base := a.prefix.Addr().As4()
	hostBits := 32 - a.prefix.Bits()
	capacity := uint32(1) << uint(hostBits)
	// Offset 0 is network, 1 is reserved as a future gateway, and the final
	// address is reserved as broadcast semantics for IPv4-style LAN behavior.
	for offset := a.next; offset < capacity-1; offset++ {
		candidate := addIPv4(base, offset)
		if _, exists := a.used[candidate]; exists {
			continue
		}
		a.byNode[nodeID] = candidate
		a.used[candidate] = nodeID
		a.next = offset + 1
		return candidate, nil
	}
	return netip.Addr{}, ErrPoolExhausted
}

func addIPv4(base [4]byte, offset uint32) netip.Addr {
	v := uint32(base[0])<<24 | uint32(base[1])<<16 | uint32(base[2])<<8 | uint32(base[3])
	v += offset
	return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
}

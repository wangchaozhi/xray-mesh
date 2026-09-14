package xray

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"sort"
	"strings"
)

const (
	defaultTUNTag  = "xray-mesh-tun"
	defaultTUNName = "xraymesh0"
	defaultTUNMTU  = 1500
)

var defaultTUNGateway = netip.MustParsePrefix("172.30.255.1/30")

// TunnelProfileOptions controls the temporary full-tunnel profile generated
// from an existing Xray JSON config. Protected prefixes are removed from the
// IPv4 routes that Xray installs, keeping mesh/control traffic outside Xray's
// TUN and preventing recursive routing.
type TunnelProfileOptions struct {
	Tag       string
	Name      string
	MTU       int
	Gateway   netip.Prefix
	Protected []netip.Prefix
}

// BuildFullTunnelConfig preserves the supplied Xray configuration and appends
// one IPv4 TUN inbound. Existing TUN inbounds are rejected to avoid ambiguous
// ownership of default routes.
func BuildFullTunnelConfig(source []byte, opts TunnelProfileOptions) ([]byte, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(source, &root); err != nil {
		return nil, fmt.Errorf("parse xray config: %w", err)
	}

	var inbounds []json.RawMessage
	if raw, ok := root["inbounds"]; ok && len(raw) != 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &inbounds); err != nil {
			return nil, fmt.Errorf("parse xray inbounds: %w", err)
		}
	}
	for _, raw := range inbounds {
		var meta struct {
			Protocol string `json:"protocol"`
			Tag      string `json:"tag"`
		}
		if err := json.Unmarshal(raw, &meta); err != nil {
			return nil, fmt.Errorf("parse xray inbound: %w", err)
		}
		if strings.EqualFold(strings.TrimSpace(meta.Protocol), "tun") {
			return nil, fmt.Errorf("xray config already contains a tun inbound%s", formatTag(meta.Tag))
		}
	}

	normalizeTunnelOptions(&opts)
	protected := append([]netip.Prefix(nil), opts.Protected...)
	protected = append(protected, opts.Gateway.Masked())
	routes, err := IPv4RoutesExcluding(protected)
	if err != nil {
		return nil, err
	}
	routeText := make([]string, 0, len(routes))
	for _, prefix := range routes {
		routeText = append(routeText, prefix.String())
	}

	inbound := map[string]any{
		"tag":      opts.Tag,
		"protocol": "tun",
		"settings": map[string]any{
			"name":                   opts.Name,
			"mtu":                    opts.MTU,
			"gateway":                []string{opts.Gateway.String()},
			"autoSystemRoutingTable": routeText,
			"autoOutboundsInterface": "auto",
		},
	}
	rawInbound, err := json.Marshal(inbound)
	if err != nil {
		return nil, fmt.Errorf("encode tun inbound: %w", err)
	}
	inbounds = append(inbounds, rawInbound)
	rawInbounds, err := json.Marshal(inbounds)
	if err != nil {
		return nil, fmt.Errorf("encode xray inbounds: %w", err)
	}
	root["inbounds"] = rawInbounds

	result, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode xray config: %w", err)
	}
	return append(result, '\n'), nil
}

// WriteFullTunnelConfig writes a temporary 0600 config derived from sourcePath.
// The returned cleanup function removes it.
func WriteFullTunnelConfig(sourcePath string, opts TunnelProfileOptions) (string, func(), error) {
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		return "", nil, fmt.Errorf("read xray config: %w", err)
	}
	config, err := BuildFullTunnelConfig(source, opts)
	if err != nil {
		return "", nil, err
	}

	file, err := os.CreateTemp("", "xray-mesh-*.json")
	if err != nil {
		return "", nil, fmt.Errorf("create temporary xray config: %w", err)
	}
	path := file.Name()
	cleanup := func() { _ = os.Remove(path) }
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		cleanup()
		return "", nil, fmt.Errorf("chmod temporary xray config: %w", err)
	}
	if _, err := file.Write(config); err != nil {
		_ = file.Close()
		cleanup()
		return "", nil, fmt.Errorf("write temporary xray config: %w", err)
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("close temporary xray config: %w", err)
	}
	return path, cleanup, nil
}

// IPv4RoutesExcluding returns a CIDR cover of all IPv4 addresses except the
// protected prefixes. IPv6 prefixes are ignored because this iteration does
// not install an IPv6 default route.
func IPv4RoutesExcluding(protected []netip.Prefix) ([]netip.Prefix, error) {
	routes := []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0")}
	for _, cut := range protected {
		if !cut.IsValid() {
			return nil, errors.New("protected prefix is invalid")
		}
		if !cut.Addr().Is4() {
			continue
		}
		cut = cut.Masked()
		next := make([]netip.Prefix, 0, len(routes)+8)
		for _, base := range routes {
			next = append(next, subtractPrefix(base, cut)...)
		}
		routes = next
	}

	sort.Slice(routes, func(i, j int) bool {
		ai := binary.BigEndian.Uint32(routes[i].Addr().AsSlice())
		aj := binary.BigEndian.Uint32(routes[j].Addr().AsSlice())
		if ai != aj {
			return ai < aj
		}
		return routes[i].Bits() < routes[j].Bits()
	})
	return routes, nil
}

func normalizeTunnelOptions(opts *TunnelProfileOptions) {
	if strings.TrimSpace(opts.Tag) == "" {
		opts.Tag = defaultTUNTag
	}
	if strings.TrimSpace(opts.Name) == "" {
		opts.Name = defaultTUNName
	}
	if opts.MTU <= 0 {
		opts.MTU = defaultTUNMTU
	}
	if !opts.Gateway.IsValid() {
		opts.Gateway = defaultTUNGateway
	}
}

func subtractPrefix(base, cut netip.Prefix) []netip.Prefix {
	if !base.Overlaps(cut) {
		return []netip.Prefix{base}
	}
	if cut.Bits() <= base.Bits() && cut.Contains(base.Addr()) {
		return nil
	}
	if base.Bits() >= 32 {
		return nil
	}

	bits := base.Bits()
	first := netip.PrefixFrom(base.Addr(), bits+1).Masked()
	addr := base.Addr().As4()
	value := binary.BigEndian.Uint32(addr[:])
	value |= uint32(1) << uint(31-bits)
	var secondAddr [4]byte
	binary.BigEndian.PutUint32(secondAddr[:], value)
	second := netip.PrefixFrom(netip.AddrFrom4(secondAddr), bits+1).Masked()

	result := subtractPrefix(first, cut)
	return append(result, subtractPrefix(second, cut)...)
}

func formatTag(tag string) string {
	if strings.TrimSpace(tag) == "" {
		return ""
	}
	return fmt.Sprintf(" (tag %q)", tag)
}

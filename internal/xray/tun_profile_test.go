package xray

import (
	"encoding/json"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
)

func TestIPv4RoutesExcluding(t *testing.T) {
	protected := []netip.Prefix{
		netip.MustParsePrefix("10.66.0.0/24"),
		netip.MustParsePrefix("203.0.113.9/32"),
	}
	routes, err := IPv4RoutesExcluding(protected)
	if err != nil {
		t.Fatal(err)
	}

	for _, blocked := range []netip.Addr{
		netip.MustParseAddr("10.66.0.1"),
		netip.MustParseAddr("203.0.113.9"),
	} {
		if coveredBy(routes, blocked) {
			t.Fatalf("protected address %s is still covered by generated routes", blocked)
		}
	}
	for _, allowed := range []netip.Addr{
		netip.MustParseAddr("1.1.1.1"),
		netip.MustParseAddr("8.8.8.8"),
		netip.MustParseAddr("192.0.2.1"),
	} {
		if !coveredBy(routes, allowed) {
			t.Fatalf("ordinary address %s is not covered by generated routes", allowed)
		}
	}
}

func TestBuildFullTunnelConfig(t *testing.T) {
	source := []byte(`{
  "log": {"loglevel": "warning"},
  "inbounds": [{"tag":"socks-in","protocol":"socks","port":1080}],
  "outbounds": [{"tag":"proxy","protocol":"vless","settings":{}}]
}`)
	config, err := BuildFullTunnelConfig(source, TunnelProfileOptions{
		Name:      "xraymesh-test",
		Gateway:   netip.MustParsePrefix("172.30.255.1/30"),
		Protected: []netip.Prefix{netip.MustParsePrefix("10.66.0.0/24")},
	})
	if err != nil {
		t.Fatal(err)
	}

	var root struct {
		Inbounds []struct {
			Tag      string `json:"tag"`
			Protocol string `json:"protocol"`
			Settings struct {
				Name                   string   `json:"name"`
				Gateway                []string `json:"gateway"`
				AutoSystemRoutingTable []string `json:"autoSystemRoutingTable"`
				AutoOutboundsInterface string   `json:"autoOutboundsInterface"`
			} `json:"settings"`
		} `json:"inbounds"`
		Outbounds []json.RawMessage `json:"outbounds"`
	}
	if err := json.Unmarshal(config, &root); err != nil {
		t.Fatal(err)
	}
	if len(root.Inbounds) != 2 {
		t.Fatalf("inbounds count = %d, want 2", len(root.Inbounds))
	}
	got := root.Inbounds[1]
	if got.Protocol != "tun" || got.Tag != defaultTUNTag {
		t.Fatalf("generated inbound = protocol %q tag %q", got.Protocol, got.Tag)
	}
	if got.Settings.Name != "xraymesh-test" {
		t.Fatalf("tun name = %q", got.Settings.Name)
	}
	if got.Settings.AutoOutboundsInterface != "auto" {
		t.Fatalf("autoOutboundsInterface = %q", got.Settings.AutoOutboundsInterface)
	}
	if len(got.Settings.Gateway) != 1 || got.Settings.Gateway[0] != "172.30.255.1/30" {
		t.Fatalf("gateway = %#v", got.Settings.Gateway)
	}
	if len(root.Outbounds) != 1 {
		t.Fatalf("existing outbounds were not preserved")
	}

	routes := make([]netip.Prefix, 0, len(got.Settings.AutoSystemRoutingTable))
	for _, text := range got.Settings.AutoSystemRoutingTable {
		prefix, err := netip.ParsePrefix(text)
		if err != nil {
			t.Fatalf("generated route %q: %v", text, err)
		}
		routes = append(routes, prefix)
	}
	if coveredBy(routes, netip.MustParseAddr("10.66.0.2")) {
		t.Fatal("mesh prefix leaked into Xray auto routing table")
	}
	if coveredBy(routes, netip.MustParseAddr("172.30.255.1")) {
		t.Fatal("Xray TUN gateway leaked into its own auto routing table")
	}
	if !coveredBy(routes, netip.MustParseAddr("1.1.1.1")) {
		t.Fatal("ordinary Internet address is not routed into Xray TUN")
	}
}

func TestBuildFullTunnelConfigRejectsExistingTUN(t *testing.T) {
	source := []byte(`{"inbounds":[{"tag":"existing","protocol":"tun","settings":{}}]}`)
	if _, err := BuildFullTunnelConfig(source, TunnelProfileOptions{}); err == nil {
		t.Fatal("expected existing TUN inbound to be rejected")
	}
}

func TestWriteFullTunnelConfig(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(sourcePath, []byte(`{"outbounds":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	path, cleanup, err := WriteFullTunnelConfig(sourcePath, TunnelProfileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("temporary config mode = %o, want 600", info.Mode().Perm())
	}
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("cleanup did not remove temporary config: %v", err)
	}
}

func coveredBy(routes []netip.Prefix, addr netip.Addr) bool {
	for _, prefix := range routes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

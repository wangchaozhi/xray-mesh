package router

import (
	"net/netip"
	"testing"
)

func TestPrefixClassifier(t *testing.T) {
	c := PrefixClassifier{MeshPrefix: netip.MustParsePrefix("10.66.0.0/24")}
	cases := []struct {
		dst  string
		want Class
	}{
		{"10.66.0.3", ClassPeer},
		{"224.0.0.251", ClassDiscovery},
		{"239.255.255.250", ClassDiscovery},
		{"1.1.1.1", ClassInternet},
	}
	for _, tc := range cases {
		got := c.Classify(Packet{Destination: netip.MustParseAddr(tc.dst)})
		if got != tc.want {
			t.Fatalf("dst=%s got=%v want=%v", tc.dst, got, tc.want)
		}
	}
}

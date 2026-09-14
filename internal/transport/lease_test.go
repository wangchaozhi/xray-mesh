package transport

import (
	"net"
	"net/netip"
	"testing"

	"github.com/wangchaozhi/xray-mesh/internal/mesh"
)

func TestForgetPeerClearsEndpointAndDiscoveryState(t *testing.T) {
	registry, err := mesh.NewRegistry(netip.MustParsePrefix("10.66.0.0/24"))
	if err != nil {
		t.Fatal(err)
	}
	peer, err := registry.Register("alpha")
	if err != nil {
		t.Fatal(err)
	}
	relay, err := ListenRelay("127.0.0.1:0", registry)
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()

	relay.bind(peer.NodeID, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345})
	payload := udpIPv4Packet(peer.VirtualIP, mdnsMulticast, 5353, 5353)
	if duplicate, err := relay.guardDiscovery(peer.NodeID, payload); err != nil || duplicate {
		t.Fatalf("guard duplicate=%v err=%v", duplicate, err)
	}
	if _, ok := relay.endpoint(peer.NodeID); !ok {
		t.Fatal("expected relay endpoint before ForgetPeer")
	}
	if _, ok := relay.discovery[peer.NodeID]; !ok {
		t.Fatal("expected discovery state before ForgetPeer")
	}

	relay.ForgetPeer(peer.NodeID)
	if _, ok := relay.endpoint(peer.NodeID); ok {
		t.Fatal("relay endpoint remains after ForgetPeer")
	}
	if _, ok := relay.discovery[peer.NodeID]; ok {
		t.Fatal("discovery state remains after ForgetPeer")
	}
}

package main

import (
	"net"
	"testing"
	"time"
)

func TestOpenRelayUDPSocketCanSendToMultipleDestinations(t *testing.T) {
	relay, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	peer, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()

	conn, err := openRelayUDPSocket(relay.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if _, err := conn.WriteToUDP([]byte("relay"), relay.LocalAddr().(*net.UDPAddr)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.WriteToUDP([]byte("peer"), peer.LocalAddr().(*net.UDPAddr)); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 64)
	_ = relay.SetReadDeadline(time.Now().Add(time.Second))
	_, relaySource, err := relay.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	_ = peer.SetReadDeadline(time.Now().Add(time.Second))
	_, peerSource, err := peer.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	if !sameUDPAddr(relaySource, peerSource) {
		t.Fatalf("different source mappings: relay=%s peer=%s", relaySource, peerSource)
	}
}

func TestSameUDPAddr(t *testing.T) {
	a := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234}
	b := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234}
	c := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1235}
	if !sameUDPAddr(a, b) {
		t.Fatal("equal UDP addresses did not match")
	}
	if sameUDPAddr(a, c) {
		t.Fatal("different UDP addresses matched")
	}
}

package transport

import (
	"bytes"
	"testing"
)

func TestPacketFrameRoundTrip(t *testing.T) {
	payload := []byte{1, 2, 3, 4}
	b, err := MarshalPacket("token-123", payload)
	if err != nil {
		t.Fatal(err)
	}
	f, err := ParseFrame(b)
	if err != nil {
		t.Fatal(err)
	}
	if f.Type != FramePacket || f.Token != "token-123" || !bytes.Equal(f.Payload, payload) {
		t.Fatalf("unexpected frame: %#v", f)
	}
}

func TestDeliverHasNoToken(t *testing.T) {
	b, err := MarshalDeliver([]byte{9})
	if err != nil {
		t.Fatal(err)
	}
	f, err := ParseFrame(b)
	if err != nil {
		t.Fatal(err)
	}
	if f.Type != FrameDeliver || f.Token != "" {
		t.Fatalf("unexpected frame: %#v", f)
	}
}

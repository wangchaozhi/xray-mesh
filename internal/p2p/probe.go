package p2p

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	ProbeProtocol = "xray-mesh-p2p/1"
	ProbeTypeProbe = "probe"
	ProbeTypeAck   = "probe_ack"
)

var (
	probeWirePrefix       = []byte("XRMP2P1\n")
	ErrNotProbeDatagram   = errors.New("not an xray-mesh P2P probe datagram")
	ErrInvalidProbeFrame  = errors.New("invalid P2P probe frame")
	ErrProbeTicketExpired = errors.New("P2P probe ticket expired")
	ErrProbeAuthFailed    = errors.New("P2P probe authentication failed")
)

// ProbeFrame is a small authenticated control frame. It is deliberately
// separate from mesh payload framing; successful probe/ack only establishes
// reachability and must not by itself authorize arbitrary payload traffic.
type ProbeFrame struct {
	Protocol   string `json:"protocol"`
	Type       string `json:"type"`
	TicketID   string `json:"ticket_id"`
	SourceNode string `json:"source_node"`
	TargetNode string `json:"target_node"`
	Nonce      string `json:"nonce"`
	MAC        string `json:"mac"`
}

func TicketIdentifier(ticket ProbeTicket) (string, error) {
	secret, err := probeTicketSecret(ticket)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(secret)
	return hex.EncodeToString(digest[:]), nil
}

func NewProbe(ticket ProbeTicket) (ProbeFrame, error) {
	if strings.TrimSpace(ticket.SourceNode) == "" || strings.TrimSpace(ticket.TargetNode) == "" || ticket.SourceNode == ticket.TargetNode {
		return ProbeFrame{}, ErrInvalidProbeFrame
	}
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return ProbeFrame{}, err
	}
	return newProbeWithNonce(ticket, hex.EncodeToString(nonceBytes))
}

func newProbeWithNonce(ticket ProbeTicket, nonce string) (ProbeFrame, error) {
	ticketID, err := TicketIdentifier(ticket)
	if err != nil {
		return ProbeFrame{}, err
	}
	frame := ProbeFrame{
		Protocol:   ProbeProtocol,
		Type:       ProbeTypeProbe,
		TicketID:   ticketID,
		SourceNode: ticket.SourceNode,
		TargetNode: ticket.TargetNode,
		Nonce:      strings.TrimSpace(nonce),
	}
	if err := validateProbeShape(frame); err != nil {
		return ProbeFrame{}, err
	}
	frame.MAC, err = signProbeFrame(frame, ticket)
	if err != nil {
		return ProbeFrame{}, err
	}
	return frame, nil
}

func NewProbeAck(probe ProbeFrame, ticket ProbeTicket, now time.Time) (ProbeFrame, error) {
	if err := VerifyProbe(probe, ticket, now); err != nil {
		return ProbeFrame{}, err
	}
	ack := ProbeFrame{
		Protocol:   ProbeProtocol,
		Type:       ProbeTypeAck,
		TicketID:   probe.TicketID,
		SourceNode: probe.TargetNode,
		TargetNode: probe.SourceNode,
		Nonce:      probe.Nonce,
	}
	mac, err := signProbeFrame(ack, ticket)
	if err != nil {
		return ProbeFrame{}, err
	}
	ack.MAC = mac
	return ack, nil
}

func VerifyProbe(frame ProbeFrame, ticket ProbeTicket, now time.Time) error {
	if err := validateProbeShape(frame); err != nil {
		return err
	}
	if frame.Type != ProbeTypeProbe {
		return ErrInvalidProbeFrame
	}
	if frame.SourceNode != ticket.SourceNode || frame.TargetNode != ticket.TargetNode {
		return ErrProbeAuthFailed
	}
	return verifyProbeAuth(frame, ticket, now)
}

func VerifyProbeAck(ack, probe ProbeFrame, ticket ProbeTicket, now time.Time) error {
	if err := VerifyProbe(probe, ticket, now); err != nil {
		return err
	}
	if err := validateProbeShape(ack); err != nil {
		return err
	}
	if ack.Type != ProbeTypeAck || ack.SourceNode != probe.TargetNode || ack.TargetNode != probe.SourceNode || ack.Nonce != probe.Nonce || ack.TicketID != probe.TicketID {
		return ErrProbeAuthFailed
	}
	return verifyProbeAuth(ack, ticket, now)
}

func verifyProbeAuth(frame ProbeFrame, ticket ProbeTicket, now time.Time) error {
	if !ticket.ExpiresAt.IsZero() && !now.UTC().Before(ticket.ExpiresAt.UTC()) {
		return ErrProbeTicketExpired
	}
	ticketID, err := TicketIdentifier(ticket)
	if err != nil {
		return err
	}
	if frame.TicketID != ticketID {
		return ErrProbeAuthFailed
	}
	expected, err := signProbeFrame(frame, ticket)
	if err != nil {
		return err
	}
	provided, err := hex.DecodeString(frame.MAC)
	if err != nil || len(provided) != sha256.Size {
		return ErrProbeAuthFailed
	}
	expectedBytes, _ := hex.DecodeString(expected)
	if !hmac.Equal(provided, expectedBytes) {
		return ErrProbeAuthFailed
	}
	return nil
}

func MarshalProbeFrame(frame ProbeFrame) ([]byte, error) {
	if err := validateProbeShape(frame); err != nil {
		return nil, err
	}
	body, err := json.Marshal(frame)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(probeWirePrefix)+len(body))
	out = append(out, probeWirePrefix...)
	out = append(out, body...)
	return out, nil
}

func ParseProbeFrame(data []byte) (ProbeFrame, error) {
	if !bytes.HasPrefix(data, probeWirePrefix) {
		return ProbeFrame{}, ErrNotProbeDatagram
	}
	decoder := json.NewDecoder(bytes.NewReader(data[len(probeWirePrefix):]))
	decoder.DisallowUnknownFields()
	var frame ProbeFrame
	if err := decoder.Decode(&frame); err != nil {
		return ProbeFrame{}, fmt.Errorf("%w: %v", ErrInvalidProbeFrame, err)
	}
	if decoder.More() {
		return ProbeFrame{}, ErrInvalidProbeFrame
	}
	if err := validateProbeShape(frame); err != nil {
		return ProbeFrame{}, err
	}
	return frame, nil
}

func validateProbeShape(frame ProbeFrame) error {
	if frame.Protocol != ProbeProtocol || (frame.Type != ProbeTypeProbe && frame.Type != ProbeTypeAck) {
		return ErrInvalidProbeFrame
	}
	if strings.TrimSpace(frame.SourceNode) == "" || strings.TrimSpace(frame.TargetNode) == "" || frame.SourceNode == frame.TargetNode {
		return ErrInvalidProbeFrame
	}
	if decoded, err := hex.DecodeString(frame.TicketID); err != nil || len(decoded) != sha256.Size {
		return ErrInvalidProbeFrame
	}
	if decoded, err := hex.DecodeString(frame.Nonce); err != nil || len(decoded) != 16 {
		return ErrInvalidProbeFrame
	}
	if frame.MAC != "" {
		if decoded, err := hex.DecodeString(frame.MAC); err != nil || len(decoded) != sha256.Size {
			return ErrInvalidProbeFrame
		}
	}
	return nil
}

func signProbeFrame(frame ProbeFrame, ticket ProbeTicket) (string, error) {
	secret, err := probeTicketSecret(ticket)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, secret)
	for _, value := range []string{frame.Protocol, frame.Type, frame.TicketID, frame.SourceNode, frame.TargetNode, frame.Nonce} {
		_, _ = mac.Write([]byte(value))
		_, _ = mac.Write([]byte{0})
	}
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func probeTicketSecret(ticket ProbeTicket) ([]byte, error) {
	secret, err := hex.DecodeString(strings.TrimSpace(ticket.Ticket))
	if err != nil || len(secret) != 32 {
		return nil, ErrProbeAuthFailed
	}
	return secret, nil
}

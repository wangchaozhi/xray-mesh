package p2p

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	directPayloadPrefix      = []byte("XRMP2PD1")
	ErrNotDirectPayload      = errors.New("not an xray-mesh direct payload datagram")
	ErrInvalidDirectPayload  = errors.New("invalid direct payload frame")
	ErrDirectPayloadAuthFail = errors.New("direct payload authentication failed")
)

const directPayloadHeaderLen = 8 + 32 + 12 + 2 + 2 + 4

// SealDirectPayload encrypts one raw mesh packet for the named peer pair using
// a short-lived pair ticket. Either direction of the ticket pair is allowed.
func SealDirectPayload(ticket ProbeTicket, sourceNode, targetNode string, payload []byte, now time.Time) ([]byte, error) {
	sourceNode = strings.TrimSpace(sourceNode)
	targetNode = strings.TrimSpace(targetNode)
	if sourceNode == "" || targetNode == "" || sourceNode == targetNode || !ticketMatchesPair(ticket, sourceNode, targetNode) {
		return nil, ErrInvalidDirectPayload
	}
	if !ticket.ExpiresAt.IsZero() && !now.UTC().Before(ticket.ExpiresAt.UTC()) {
		return nil, ErrProbeTicketExpired
	}
	if len(sourceNode) > 65535 || len(targetNode) > 65535 || len(payload) > 65535 {
		return nil, ErrInvalidDirectPayload
	}

	secret, err := probeTicketSecret(ticket)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(secret)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if aead.NonceSize() != 12 {
		return nil, fmt.Errorf("unexpected GCM nonce size %d", aead.NonceSize())
	}

	ticketIDHex, err := TicketIdentifier(ticket)
	if err != nil {
		return nil, err
	}
	ticketID, err := hex.DecodeString(ticketIDHex)
	if err != nil || len(ticketID) != 32 {
		return nil, ErrInvalidDirectPayload
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	aad := directPayloadAAD(ticketID, sourceNode, targetNode)
	ciphertext := aead.Seal(nil, nonce, payload, aad)

	out := make([]byte, directPayloadHeaderLen+len(sourceNode)+len(targetNode)+len(ciphertext))
	copy(out[:8], directPayloadPrefix)
	copy(out[8:40], ticketID)
	copy(out[40:52], nonce)
	binary.BigEndian.PutUint16(out[52:54], uint16(len(sourceNode)))
	binary.BigEndian.PutUint16(out[54:56], uint16(len(targetNode)))
	binary.BigEndian.PutUint32(out[56:60], uint32(len(ciphertext)))
	offset := directPayloadHeaderLen
	copy(out[offset:offset+len(sourceNode)], sourceNode)
	offset += len(sourceNode)
	copy(out[offset:offset+len(targetNode)], targetNode)
	offset += len(targetNode)
	copy(out[offset:], ciphertext)
	return out, nil
}

// OpenDirectPayload authenticates and decrypts one direct payload frame. The
// expectedTarget check prevents a valid pair frame from being accepted by the
// wrong local node.
func OpenDirectPayload(data []byte, ticket ProbeTicket, expectedTarget string, now time.Time) (sourceNode, targetNode string, payload []byte, err error) {
	if !bytes.HasPrefix(data, directPayloadPrefix) {
		return "", "", nil, ErrNotDirectPayload
	}
	if len(data) < directPayloadHeaderLen {
		return "", "", nil, ErrInvalidDirectPayload
	}
	if !ticket.ExpiresAt.IsZero() && !now.UTC().Before(ticket.ExpiresAt.UTC()) {
		return "", "", nil, ErrProbeTicketExpired
	}

	ticketID := data[8:40]
	nonce := data[40:52]
	sourceLen := int(binary.BigEndian.Uint16(data[52:54]))
	targetLen := int(binary.BigEndian.Uint16(data[54:56]))
	cipherLen := int(binary.BigEndian.Uint32(data[56:60]))
	if sourceLen == 0 || targetLen == 0 || cipherLen < 16 {
		return "", "", nil, ErrInvalidDirectPayload
	}
	expectedLen := directPayloadHeaderLen + sourceLen + targetLen + cipherLen
	if expectedLen != len(data) {
		return "", "", nil, ErrInvalidDirectPayload
	}
	offset := directPayloadHeaderLen
	sourceNode = string(data[offset : offset+sourceLen])
	offset += sourceLen
	targetNode = string(data[offset : offset+targetLen])
	offset += targetLen
	ciphertext := data[offset:]

	if strings.TrimSpace(expectedTarget) == "" || targetNode != strings.TrimSpace(expectedTarget) || !ticketMatchesPair(ticket, sourceNode, targetNode) {
		return "", "", nil, ErrDirectPayloadAuthFail
	}
	ticketIDHex, err := TicketIdentifier(ticket)
	if err != nil {
		return "", "", nil, err
	}
	expectedTicketID, _ := hex.DecodeString(ticketIDHex)
	if !hmac.Equal(ticketID, expectedTicketID) {
		return "", "", nil, ErrDirectPayloadAuthFail
	}

	secret, err := probeTicketSecret(ticket)
	if err != nil {
		return "", "", nil, err
	}
	block, err := aes.NewCipher(secret)
	if err != nil {
		return "", "", nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", nil, err
	}
	aad := directPayloadAAD(ticketID, sourceNode, targetNode)
	plaintext, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return "", "", nil, ErrDirectPayloadAuthFail
	}
	return sourceNode, targetNode, plaintext, nil
}

func ticketMatchesPair(ticket ProbeTicket, nodeA, nodeB string) bool {
	return (ticket.SourceNode == nodeA && ticket.TargetNode == nodeB) ||
		(ticket.SourceNode == nodeB && ticket.TargetNode == nodeA)
}

func directPayloadAAD(ticketID []byte, sourceNode, targetNode string) []byte {
	aad := make([]byte, 0, len(directPayloadPrefix)+len(ticketID)+len(sourceNode)+len(targetNode)+2)
	aad = append(aad, directPayloadPrefix...)
	aad = append(aad, ticketID...)
	aad = append(aad, sourceNode...)
	aad = append(aad, 0)
	aad = append(aad, targetNode...)
	aad = append(aad, 0)
	return aad
}

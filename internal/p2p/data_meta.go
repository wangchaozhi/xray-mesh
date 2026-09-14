package p2p

import (
	"bytes"
	"encoding/hex"
)

// DirectPayloadTicketID returns the public ticket identifier carried by a
// direct payload datagram without decrypting the payload. It is used only to
// select the local short-lived pair ticket needed for AEAD verification.
func DirectPayloadTicketID(data []byte) (string, error) {
	if !bytes.HasPrefix(data, directPayloadPrefix) {
		return "", ErrNotDirectPayload
	}
	if len(data) < directPayloadHeaderLen {
		return "", ErrInvalidDirectPayload
	}
	return hex.EncodeToString(data[8:40]), nil
}

// DirectPayloadReplayKey returns a stable identifier for one encrypted
// datagram: public ticket ID plus the per-datagram GCM nonce. Receivers can use
// it only after successful authentication to reject replay of a previously
// accepted ciphertext during the ticket lifetime.
func DirectPayloadReplayKey(data []byte) (string, error) {
	if !bytes.HasPrefix(data, directPayloadPrefix) {
		return "", ErrNotDirectPayload
	}
	if len(data) < directPayloadHeaderLen {
		return "", ErrInvalidDirectPayload
	}
	return hex.EncodeToString(data[8:52]), nil
}

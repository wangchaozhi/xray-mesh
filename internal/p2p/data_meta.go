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

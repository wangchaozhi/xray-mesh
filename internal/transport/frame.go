package transport

import (
	"encoding/binary"
	"errors"
)

const (
	frameVersion  = 1
	maxTokenBytes = 512
)

var (
	ErrFrameTooShort  = errors.New("frame is too short")
	ErrFrameVersion   = errors.New("unsupported frame version")
	ErrFrameType      = errors.New("unsupported frame type")
	ErrTokenTooLong   = errors.New("session token is too long")
	ErrMalformedFrame = errors.New("malformed frame")
)

type FrameType byte

const (
	FrameHello   FrameType = 1
	FramePacket  FrameType = 2
	FrameDeliver FrameType = 3
)

type Frame struct {
	Type    FrameType
	Token   string
	Payload []byte
}

func MarshalHello(token string) ([]byte, error) {
	return marshal(FrameHello, token, nil)
}

func MarshalPacket(token string, payload []byte) ([]byte, error) {
	return marshal(FramePacket, token, payload)
}

func MarshalDeliver(payload []byte) ([]byte, error) {
	return marshal(FrameDeliver, "", payload)
}

func marshal(t FrameType, token string, payload []byte) ([]byte, error) {
	if len(token) > maxTokenBytes || len(token) > 0xffff {
		return nil, ErrTokenTooLong
	}
	b := make([]byte, 4+len(token)+len(payload))
	b[0] = frameVersion
	b[1] = byte(t)
	binary.BigEndian.PutUint16(b[2:4], uint16(len(token)))
	copy(b[4:], token)
	copy(b[4+len(token):], payload)
	return b, nil
}

func ParseFrame(b []byte) (Frame, error) {
	if len(b) < 4 {
		return Frame{}, ErrFrameTooShort
	}
	if b[0] != frameVersion {
		return Frame{}, ErrFrameVersion
	}
	t := FrameType(b[1])
	if t != FrameHello && t != FramePacket && t != FrameDeliver {
		return Frame{}, ErrFrameType
	}
	tokenLen := int(binary.BigEndian.Uint16(b[2:4]))
	if tokenLen > maxTokenBytes || 4+tokenLen > len(b) {
		return Frame{}, ErrMalformedFrame
	}
	if t == FrameDeliver && tokenLen != 0 {
		return Frame{}, ErrMalformedFrame
	}
	return Frame{
		Type:    t,
		Token:   string(b[4 : 4+tokenLen]),
		Payload: append([]byte(nil), b[4+tokenLen:]...),
	}, nil
}

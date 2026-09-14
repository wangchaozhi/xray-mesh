package xray

import (
	"context"

	"github.com/wangchaozhi/xray-mesh/internal/router"
)

// Outbound is the integration seam for an existing Xray deployment. The mesh
// project deliberately does not reimplement VLESS/REALITY or other transports.
type Outbound interface {
	Start(context.Context) error
	WritePacket(context.Context, router.Packet) error
	Close() error
}

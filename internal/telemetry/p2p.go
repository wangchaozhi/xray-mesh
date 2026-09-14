package telemetry

import "sync/atomic"

type P2PSnapshot struct {
	ProbeHealthy     uint64 `json:"probe_healthy"`
	DirectTX         uint64 `json:"direct_tx"`
	DirectRX         uint64 `json:"direct_rx"`
	DirectFallbacks  uint64 `json:"direct_fallbacks"`
	ReplayDrops      uint64 `json:"replay_drops"`
}

type P2PCounters struct {
	probeHealthy    atomic.Uint64
	directTX        atomic.Uint64
	directRX        atomic.Uint64
	directFallbacks atomic.Uint64
	replayDrops     atomic.Uint64
}

var P2P P2PCounters

func (c *P2PCounters) IncProbeHealthy()    { c.probeHealthy.Add(1) }
func (c *P2PCounters) IncDirectTX()        { c.directTX.Add(1) }
func (c *P2PCounters) IncDirectRX()        { c.directRX.Add(1) }
func (c *P2PCounters) IncDirectFallback()  { c.directFallbacks.Add(1) }
func (c *P2PCounters) IncReplayDrop()      { c.replayDrops.Add(1) }

func (c *P2PCounters) Snapshot() P2PSnapshot {
	return P2PSnapshot{
		ProbeHealthy:    c.probeHealthy.Load(),
		DirectTX:        c.directTX.Load(),
		DirectRX:        c.directRX.Load(),
		DirectFallbacks: c.directFallbacks.Load(),
		ReplayDrops:     c.replayDrops.Load(),
	}
}

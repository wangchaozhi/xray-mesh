package telemetry

import "testing"

func TestP2PCountersSnapshot(t *testing.T) {
	var counters P2PCounters
	counters.IncDirectHealthy()
	counters.IncDirectTX()
	counters.IncDirectTX()
	counters.IncDirectRX()
	counters.IncDirectFallback()
	counters.IncReplayDrop()

	snap := counters.Snapshot()
	if snap.DirectHealthyMarks != 1 || snap.DirectTX != 2 || snap.DirectRX != 1 || snap.DirectFallbacks != 1 || snap.ReplayDrops != 1 {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}
}

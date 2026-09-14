# Real deployment validation

`cmd/meshcheck` validates the live two-node mesh path from one Linux node. It uses the local `/v1/status` counters plus ICMP ping traffic to verify that the peer is reachable, the encrypted direct path becomes active, and optionally that relay fallback still works when direct UDP is deliberately blocked.

## Prerequisites

Both nodes should run the client with the mesh TUN, P2P probing/direct payload support, and a local status endpoint enabled. For example:

```bash
sudo ./client \
  -server http://COORDINATOR:8666 \
  -relay COORDINATOR:8667 \
  -node node-a \
  -tun \
  -p2p-probe \
  -status-listen 127.0.0.1:8670
```

Run the equivalent command on node B with a different node ID. Confirm each node has a different mesh virtual IPv4 address.

Build the checker:

```bash
go build -o meshcheck ./cmd/meshcheck
```

The checker requires the system `ping` command. The direct-only validation does not require root.

## Direct path validation

From node A, target node B's mesh virtual IP:

```bash
./meshcheck -peer 10.66.0.3
```

The checker performs these phases:

1. `baseline-connectivity`: verifies the peer is reachable over the mesh.
2. `direct-path`: sends peer traffic until the local `direct_tx` counter increases while ping remains successful.
3. It records min/average/max ping RTT plus P2P counters before and after each phase.

A successful direct phase proves that at least one encrypted direct payload was sent by this client. The status counters remain process-lifetime counters, so an otherwise busy node can add background traffic to the measurements; use quiet test nodes for clean results.

## Full direct -> relay fallback -> direct recovery validation

The optional fallback test temporarily blocks all outbound UDP except the configured relay endpoint. This is intentionally intrusive and therefore requires Linux root privileges.

```bash
sudo ./meshcheck \
  -peer 10.66.0.3 \
  -force-fallback \
  -relay 203.0.113.20:8667
```

The checker creates a temporary `XRMCHK...` iptables chain. The chain:

- returns immediately for UDP sent to the relay IPv4 address and relay port;
- drops other outbound UDP;
- is inserted at the top of `OUTPUT` only for the fallback phase;
- is removed before the direct recovery phase and on normal cancellation/error paths.

This can interrupt DNS, QUIC, WireGuard, games, voice/video, and any other UDP traffic on the checker host while the fallback phase is active. Run the full test on a dedicated test node or during a maintenance window.

Direct UDP writes can still look locally successful while the firewall silently drops the datagrams. For that reason meshcheck does not expect immediate fallback. It waits for the live direct state/ticket to age out, then requires all confirmation pings to succeed while `direct_tx` stays unchanged. The default fallback timeout is 55 seconds; the direct selector currently uses a roughly 30-second health lifetime.

After the temporary firewall chain is removed, the `direct-recovery` phase requires `direct_tx` to increase again while peer traffic succeeds.

## JSON output

For automation or CI-style collection:

```bash
./meshcheck -peer 10.66.0.3 -json
```

The report contains per-phase ping counts, RTTs, and the P2P counter snapshots:

- `direct_healthy_marks`
- `direct_tx`
- `direct_rx`
- `direct_fallbacks`
- `replay_drops`

## Useful tuning flags

```text
-status             local /v1/status URL
-samples            successful ping samples per phase (default 5)
-ping-timeout       timeout for one ping (default 2s)
-poll-interval      transition polling interval (default 1s)
-direct-timeout     time allowed to observe direct TX (default 25s)
-fallback-timeout   time allowed for relay fallback (default 55s)
-fallback-quiet     direct_tx quiet period before fallback confirmation (default 3s)
-recovery-timeout   time allowed for direct recovery (default 25s)
```

## If the process is killed while fallback rules are active

Normal exit, errors, SIGINT, and SIGTERM attempt automatic cleanup. `SIGKILL`, a host crash, or a hard power loss cannot run cleanup code. If needed, inspect temporary chains:

```bash
sudo iptables -S OUTPUT | grep XRMCHK
sudo iptables -S | grep XRMCHK
```

For a stale chain named `XRMCHK1234`, remove it explicitly:

```bash
sudo iptables -D OUTPUT -p udp -j XRMCHK1234
sudo iptables -F XRMCHK1234
sudo iptables -X XRMCHK1234
```

## Interpreting failures

- Baseline failure: peer routing, relay registration, TUN setup, or ICMP reachability is broken before P2P is considered.
- Direct timeout: candidates/tickets/probes never produced a healthy direct path, NAT traversal is blocked, or the peer did not enable `-p2p-probe`.
- Fallback timeout: relay UDP was not actually preserved, the node has unrelated background direct traffic that keeps `direct_tx` moving, or the direct state did not age out before the configured timeout.
- Recovery timeout: candidates/tickets/probes did not recover after the temporary UDP block was removed.

The checker treats direct P2P as an optimization: relay connectivity is still the fail-safe path.
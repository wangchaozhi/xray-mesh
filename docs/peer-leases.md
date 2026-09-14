# Peer leases and heartbeats

The coordinator treats every registered peer as a time-bounded lease instead of a permanent registry entry.

Default policy:

- heartbeat interval advertised to clients: `15s`;
- lease TTL: `60s`;
- heartbeat endpoint: `POST /v1/heartbeat`;
- authentication: `Authorization: Bearer <session_token>`.

The values are configurable on the coordinator:

```bash
go run ./cmd/server \
  -listen 0.0.0.0:8666 \
  -relay-listen 0.0.0.0:8667 \
  -heartbeat-interval 15s \
  -peer-lease 60s
```

`-heartbeat-interval` must be shorter than `-peer-lease`; both must be at least one second.

## Registration

`POST /v1/peers` now returns the lease policy together with the peer's virtual IP and session token:

```json
{
  "node_id": "laptop",
  "virtual_ip": "10.66.0.2",
  "network_prefix": "10.66.0.0/24",
  "session_token": "...",
  "heartbeat_interval_seconds": 15,
  "lease_ttl_seconds": 60
}
```

Re-registering the same `node_id` while its lease is still active preserves the virtual IP and token but refreshes `last_seen`. This makes a quick client restart safe without allocating a second address.

## Client behavior

Long-running clients send heartbeats at the interval advertised by the coordinator. A `401` or `403` heartbeat response is treated as an expired/invalid lease and the client exits immediately. Other heartbeat errors are treated as transient; the client exits after three consecutive failures rather than silently continuing with a lease that the coordinator may soon reclaim.

In `-full-tunnel` mode, the coordinator address is already excluded from the generated Xray TUN route set, so heartbeat traffic follows the direct control path and cannot recursively enter the Xray tunnel.

## Expiry cleanup

When a peer exceeds the lease TTL, the coordinator removes all state tied to the lease:

- node registry entry;
- virtual-IP lookup;
- session-token lookup;
- allocator reservation, making the virtual IP reusable;
- relay UDP endpoint;
- discovery dedup/rate-limit state.

After expiry, packets using the old session token fail relay authentication and an HTTP heartbeat using that token returns `401`.

This is still a prototype identity model: the bearer session token is not a replacement for a durable device identity or authenticated key exchange. A later phase should add persistent device keys and explicit authorization before exposing the coordinator to untrusted networks.

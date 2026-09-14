# P2P candidate control plane

The current peer data plane still uses the coordinator UDP relay. This phase adds the control-plane information needed for a later direct UDP path without pretending that a candidate endpoint is already reachable.

## Relay-observed candidates

Whenever a peer sends a valid authenticated relay frame, the coordinator records the UDP source address it observes. This is exposed as a `relay_observed` candidate with an observation timestamp.

Authenticated peers can query:

```text
GET /v1/candidates
Authorization: Bearer <session_token>
```

Example response:

```json
[
  {
    "node_id": "phone",
    "virtual_ip": "10.66.0.3",
    "endpoint": "203.0.113.18:42117",
    "kind": "relay_observed",
    "observed_at": "2026-09-14T07:15:00Z"
  }
]
```

The endpoint is similar to a server-reflexive candidate learned from the existing relay path, but it is **not STUN** and it is **not proof of direct connectivity**. NAT type, mapping lifetime, firewalls, carrier NAT, and symmetric NAT can all make the endpoint unusable for direct traffic.

Candidate data is protected by the same current session-token authentication used by heartbeat. Unauthenticated or expired sessions receive `401` and cannot enumerate peer public endpoints.

Lease expiry also removes the relay-observed candidate because the relay endpoint state is forgotten together with the peer lease.

## Path selection

`internal/p2p.Selector` implements a relay-first policy:

1. a peer with no confirmed direct health always uses `relay`;
2. a later probe layer may call `MarkDirectHealthy(node, endpoint, time)` after a successful authenticated direct probe;
3. direct health expires after a TTL and automatically falls back to `relay`;
4. `MarkDirectFailed` immediately returns the peer to relay.

This means candidate discovery alone never switches traffic away from the known-working coordinator relay.

## Next phase

The next direct-connectivity phase should add an authenticated peer-to-peer probe/ack exchange and NAT hole-punch attempts. It should not reuse another peer's session token as a direct-traffic credential. A pair-scoped coordinator-issued proof or durable device identity is needed before direct mesh packets can safely bypass the relay.

# Authenticated P2P probe protocol

This phase defines the direct-connectivity probe/ack wire format and prepares the client UDP socket for NAT traversal. It does **not** yet switch mesh payload traffic to direct paths.

## Why the relay socket must be reused

The coordinator candidate feed is learned from the source address of authenticated UDP relay traffic. If a client sends direct probes from a separate UDP socket, many NATs will allocate a different external port and the coordinator-observed candidate will point at the wrong mapping.

The client therefore now opens an unconnected UDP socket and uses that same local port for relay traffic. The socket can send to both the coordinator relay and, in the next phase, peer candidates. Relay-delivery frames are accepted only when the datagram source matches the configured coordinator relay address; other sources are currently ignored until the live probe handler is enabled.

## Probe frames

Direct probe datagrams use a distinct wire prefix:

```text
XRMP2P1\n
```

followed by strict JSON containing:

- protocol version (`xray-mesh-p2p/1`);
- frame type (`probe` or `probe_ack`);
- SHA-256 ticket identifier (the ticket secret itself is not placed in the datagram);
- source and target node IDs;
- a random 128-bit nonce;
- HMAC-SHA256 authentication tag.

The HMAC key is the 256-bit pair-scoped probe ticket issued by the coordinator. The MAC covers protocol, type, ticket ID, source node, target node, and nonce using an unambiguous NUL-separated encoding.

## Probe flow

1. The initiator obtains a short-lived pair ticket from the coordinator.
2. Both peers know the same ticket through their authenticated coordinator APIs.
3. The initiator creates a `probe` with a random nonce and sends it to the target's relay-observed candidate.
4. The target verifies ticket scope, expiry, ticket ID, pair direction, and HMAC.
5. The target returns `probe_ack` to the UDP source address it actually observed, swapping source/target and echoing the nonce.
6. The initiator accepts the acknowledgement only if it verifies under the same ticket and matches the outstanding probe nonce.
7. Only after this succeeds may the path selector mark the peer direct path healthy.

An old acknowledgement cannot satisfy a new probe because each probe uses a new random nonce. Tickets are also short lived and are removed when either peer lease expires.

## Parser behavior

The parser rejects:

- non-probe wire prefixes;
- unknown JSON fields;
- trailing JSON values;
- missing/invalid ticket IDs, nonces, or MACs;
- wrong protocol/type values;
- self-pairs or blank node IDs;
- invalid HMACs and expired tickets.

## Current boundary

`internal/p2p` already has a real UDP round-trip test for authenticated probe/ack, and the mesh client now owns a reusable unconnected UDP socket. The production client still ignores non-relay datagrams in this phase. The next iteration will add coordinator candidate/ticket polling plus a live probe handler on that same socket and will call `Selector.MarkDirectHealthy` only after a verified acknowledgement.

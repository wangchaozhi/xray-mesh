# Pair-scoped P2P probe tickets

Direct-connectivity probing must not require peers to reveal their coordinator session tokens to each other. `xray-mesh` therefore uses short-lived pair-scoped probe tickets as a separate credential domain.

## Issuing a ticket

The initiating peer authenticates to the coordinator with its normal session token and names one currently registered target:

```text
POST /v1/p2p/tickets
Authorization: Bearer <session_token>
Content-Type: application/json

{"target_node":"phone"}
```

A successful response contains a random 256-bit ticket and its pair/expiry metadata:

```json
{
  "ticket": "...",
  "source_node": "laptop",
  "target_node": "phone",
  "issued_at": "2026-09-14T07:30:00Z",
  "expires_at": "2026-09-14T07:30:30Z"
}
```

The default TTL is `30s` and can be changed on the coordinator with `-p2p-ticket-ttl`. The configured value must be greater than zero and no more than five minutes.

A peer cannot issue a ticket to itself or to a node that is not currently registered.

## Retrieving tickets

Either member of the pair can retrieve its live tickets with its own coordinator session token:

```text
GET /v1/p2p/tickets
Authorization: Bearer <session_token>
```

The coordinator returns only tickets where the authenticated node is the source or target. An unrelated authenticated peer cannot enumerate or retrieve ticket secrets for another pair.

## Verification semantics

`internal/p2p.TicketManager.Verify(ticket, nodeA, nodeB)` accepts the ticket only while it is live and only for the exact two-node pair. Pair ordering is ignored so one credential can authenticate both a probe and its acknowledgement.

Tickets are intentionally not long-lived payload keys. A later direct-probe protocol should use a ticket to authenticate a short probe/ack exchange, then continue to rely on relay fallback unless direct reachability has been confirmed by the path selector.

## Lease interaction

When a peer lease expires, every outstanding ticket involving that peer is deleted immediately alongside its relay endpoint and candidate state. Expired tickets are also pruned on issue/list/verify operations and by the existing lease sweeper.

This separation is deliberate:

- **session token:** authenticates one peer to the coordinator;
- **probe ticket:** authenticates one short-lived peer pair interaction;
- **future durable device key:** should eventually authenticate long-lived device identity and secure direct payload transport.

Peers should never transmit their coordinator session tokens directly to another peer.

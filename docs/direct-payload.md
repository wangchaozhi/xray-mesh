# Direct payload framing

This phase adds the authenticated/encrypted payload format that a later pump integration can use for direct peer traffic.

Direct payload datagrams use:

- a short-lived pair-scoped P2P ticket as key material;
- AES-256-GCM with a fresh 96-bit nonce per datagram;
- the public ticket identifier and source/target node IDs as authenticated associated data;
- strict frame length validation with no trailing bytes;
- explicit ticket-expiry checks.

The runtime helpers add two additional policy checks around the cryptographic frame:

1. a sender uses direct transport only when the path selector currently marks the peer healthy and a live pair ticket still exists;
2. a receiver verifies that the decrypted IPv4 source equals the candidate virtual IP for the authenticated source node and that the IPv4 destination equals its own mesh virtual IP.

If a direct path is missing, stale, ticketless, or a UDP write fails, `SendDirectPayload` reports that the caller must use relay fallback. A UDP write failure also clears the selector's direct-health state.

This PR intentionally does not yet change the TUN pump's packet-path choice. The next iteration will wire these helpers into the existing relay path and keep relay as the immediate fallback.
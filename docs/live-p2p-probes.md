# Experimental live P2P probes

`xray-mesh` can now run authenticated direct UDP reachability probes while continuing to carry all mesh payload traffic through the coordinator relay.

Enable the experiment with:

```bash
sudo go run ./cmd/client \
  -server http://SERVER_IP:8666 \
  -relay SERVER_IP:8667 \
  -node laptop \
  -tun \
  -p2p-probe
```

`-p2p-probe` requires `-tun`.

## What it does

When enabled, the client:

1. keeps the relay-observed UDP mapping alive on the same socket used by the mesh data plane;
2. polls authenticated relay-observed peer candidates from the coordinator;
3. obtains short-lived pair-scoped probe tickets;
4. sends authenticated `probe` frames to candidate endpoints;
5. replies to valid inbound probes with authenticated `probe_ack` frames;
6. marks a peer's direct path healthy only after a valid ACK for a locally pending nonce.

The ACK handler verifies the pair ticket, HMAC, nonce and direction before consuming the pending probe. A forged ACK therefore cannot invalidate a legitimate outstanding probe.

## What it does not do yet

A healthy direct path is currently only reachability state. Raw mesh payload packets still use the relay. This intentionally separates NAT reachability validation from payload-path switching so fallback behavior can be tested independently.

Relay fallback remains the only payload path until a later iteration adds an authenticated direct payload frame and fail-safe selector integration.

## NAT limitation

The coordinator-observed candidate is not a STUN result and does not guarantee peer-to-peer reachability. Symmetric NAT, carrier-grade NAT and restrictive firewalls can still prevent a direct probe from succeeding. Probe failure does not affect relay connectivity.

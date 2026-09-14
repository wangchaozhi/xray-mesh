# Direct-first with relay fallback

With `-tun -p2p-probe`, peer unicast traffic can now use an authenticated/encrypted direct UDP path after the live probe selector marks that peer healthy.

The outbound decision is deliberately fail-safe:

1. classify the TUN packet;
2. mDNS/SSDP discovery always uses the coordinator relay fan-out;
3. ordinary peer unicast asks the P2P runtime to send an AES-GCM direct payload;
4. if the path is not healthy, no fresh pair ticket exists, the endpoint cannot be resolved, or the direct UDP write fails, the same IPv4 packet is immediately sent through the existing relay path;
5. a direct UDP write/endpoint failure also clears the selector's direct-health state so following packets stay on relay until a later authenticated probe succeeds.

Inbound non-relay datagrams are processed in this order:

1. try authenticated/encrypted direct payload;
2. if it is a direct payload, verify AEAD, ticket lifetime, node pair, inner IPv4 source and local destination before writing it to the TUN;
3. if it is not a direct payload, try the authenticated `probe` / `probe_ack` control protocol;
4. invalid direct/control datagrams are dropped without terminating the mesh data plane.

This means P2P optimization is optional. A failed NAT traversal attempt does not remove the coordinator relay as the working path.

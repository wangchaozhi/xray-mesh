# Discovery overlay

`xray-mesh` can optionally route the two common IPv4 service-discovery multicast destinations through the mesh TUN:

- mDNS: `224.0.0.251:5353/udp`
- SSDP: `239.255.255.250:1900/udp`

The coordinator relay fans valid discovery packets out to every active peer except the sender. The client-side `-discovery-overlay` flag installs host routes for those multicast destinations on `xrmesh0`, which makes packets that use normal system routing enter the mesh data plane.

Example:

```bash
sudo go run ./cmd/client \
  -server http://SERVER_IP:8666 \
  -relay SERVER_IP:8667 \
  -node laptop \
  -tun \
  -discovery-overlay
```

It can be combined with Xray full-tunnel mode. The discovery `/32` routes are more specific than the generated Internet routes, so mDNS/SSDP continue to use the mesh TUN while ordinary IPv4 Internet traffic uses the Xray TUN.

## Relay protection

Discovery fan-out is guarded per peer before it is broadcast. The default coordinator policy is:

- sustained discovery rate: `20` packets/second per peer;
- burst capacity: `40` packets per peer;
- exact-packet duplicate window: `750ms` per peer.

An identical discovery packet repeated by the same peer inside the duplicate window is silently suppressed and does not consume a rate-limit token. Different packets consume token-bucket capacity; when the bucket is empty, further discovery packets from that peer are dropped until tokens refill. Ordinary virtual-IP unicast traffic does not use this limiter.

The coordinator settings are configurable:

```bash
go run ./cmd/server \
  -listen 0.0.0.0:8666 \
  -relay-listen 0.0.0.0:8667 \
  -discovery-rate 20 \
  -discovery-burst 40 \
  -discovery-dedup 750ms
```

Set `-discovery-dedup 0` to disable exact-packet duplicate suppression. The rate and burst values must remain greater than zero.

## Important limitation

This is an experimental L3 multicast path, not a complete Ethernet bridge or mDNS reflector. Applications that explicitly bind discovery traffic to a particular physical interface may continue to use only that interface and may not emit packets through `xrmesh0`. A later reflector mode can capture/re-emit discovery on selected physical interfaces when broader compatibility is required.

The feature is opt-in because adding host routes for multicast destinations can change how applications perform local discovery. If an application must discover both local-LAN and remote-mesh devices, verify its interface behavior before enabling this mode system-wide.

## Relay validation

The relay does not fan out arbitrary traffic merely because it targets a multicast IP. It verifies both destination address and UDP destination port before broadcast:

- `224.0.0.251` must use destination UDP port `5353`;
- `239.255.255.250` must use destination UDP port `1900`.

Non-matching packets fall back to normal peer routing and are rejected when the multicast destination is not a registered virtual peer.

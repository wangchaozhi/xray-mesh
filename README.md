# xray-mesh

`xray-mesh` is an experimental Go project that combines a small overlay-network control plane with an Xray/VLESS Internet egress integration point.

The project is intentionally split into two responsibilities:

- **Mesh / east-west traffic:** assign stable virtual IPs, register peers, route peer-to-peer packets, and later support local-network discovery forwarding.
- **Xray / north-south traffic:** hand Internet-bound traffic to an existing Xray/VLESS deployment instead of inventing a new obfuscation or censorship-evasion protocol.

## Status

Early prototype. The current tree now provides a compilable control plane plus a Linux-only peer data-plane MVP:

- client and server commands;
- peer registration over HTTP;
- deterministic virtual IPv4 allocation from `10.66.0.0/24`;
- concurrency-safe peer registry;
- packet/router interfaces;
- a Linux TUN implementation for overlay peer traffic;
- a development UDP relay with per-registration session tokens and source-IP anti-spoofing;
- packet classification for mesh, mDNS/SSDP discovery, and future Internet egress;
- discovery and Xray adapter interfaces/stubs;
- unit tests and GitHub Actions CI.

The peer data plane is intentionally an MVP: the UDP relay is **not encrypted**, Internet egress is not wired to Xray yet, and the control plane is not production-authenticated.

## Architecture

```text
+---------------- client ----------------+
|                                        |
|  Apps                                  |
|    |                                   |
|   TUN  <-- Linux MVP device            |
|    |                                   |
|  Mesh Router                           |
|    |\                                  |
|    | +--> Peer traffic --> Mesh tunnel |
|    |                                   |
|    +----> Internet --> Xray/VLESS      |
+-------------------|--------------------+
                    |
               coordinator
                    |
        peer registry / virtual IPs
```

### Planned data plane

```text
peer A (10.66.0.2) <---- coordinator UDP relay ----> peer B (10.66.0.3)
          \
           +---- Internet-bound packets ----> Xray/VLESS adapter ----> Internet
```

Discovery protocols such as mDNS and SSDP will be handled as explicit, bounded relays rather than blindly flooding every packet.

## Goals

1. One client process can participate in a private overlay network.
2. Each peer receives a virtual IP.
3. Overlay peer traffic is routed directly through the mesh data plane.
4. Internet-bound traffic can be handed to an existing Xray/VLESS stack.
5. Selected discovery traffic can later be relayed across the overlay.
6. Keep the mesh layer transport-agnostic so the underlying tunnel can evolve independently.

## Non-goals

- Reimplementing VLESS, REALITY, or other Xray transports.
- Designing new traffic-obfuscation or censorship-evasion protocols.
- Bridging arbitrary Ethernet broadcasts by default.
- Treating a shared Xray subscription URL as peer identity.

## Run the prototype

Start the coordinator and UDP relay:

```bash
go run ./cmd/server \
  -listen 0.0.0.0:8666 \
  -relay-listen 0.0.0.0:8667
```

A registration-only client still works without elevated privileges:

```bash
go run ./cmd/client -server http://127.0.0.1:8666 -node laptop
```

On Linux, enable the peer data plane with a TUN device (normally requires root or `CAP_NET_ADMIN`):

```bash
sudo go run ./cmd/client \
  -server http://SERVER_IP:8666 \
  -relay SERVER_IP:8667 \
  -node laptop \
  -tun
```

A second peer will receive another address in `10.66.0.0/24`; after both peers have started with `-tun`, traffic to their virtual IPs is relayed through the coordinator. The MVP does **not** route general Internet traffic into the TUN yet.

Run tests:

```bash
go test ./...
```

## Roadmap

### Phase 1 — control plane

- [x] peer model
- [x] virtual IPv4 allocator
- [x] peer registry
- [x] HTTP registration API
- [ ] peer authentication
- [ ] leases / heartbeats / expiry
- [ ] signed network configuration

### Phase 2 — data plane

- [x] Linux TUN implementation
- [x] packet classification
- [x] coordinator-relayed peer packet transport
- [ ] direct peer-to-peer transport / NAT traversal
- [ ] MTU and fragmentation strategy
- [ ] NAT / egress policy

### Phase 3 — Xray integration

- [ ] Xray process adapter
- [ ] SOCKS/transparent egress adapter
- [ ] policy routing for selected destinations
- [ ] lifecycle and health reporting

### Phase 4 — discovery

- [ ] mDNS relay
- [ ] SSDP relay
- [ ] per-network discovery allowlists
- [ ] loop suppression and rate limiting

## Security model

The current HTTP registration endpoint is development-only and unauthenticated, and the UDP relay is not encrypted. Do not expose this prototype directly to untrusted networks. A production version needs authenticated peer identity, replay protection, authorization, lease expiry, encrypted transport, token rotation, and stronger endpoint binding. The Xray integration point is intentionally separate so an existing secure transport can later carry the data plane without inventing a new obfuscation protocol.

## License

No license selected yet.

# xray-mesh

`xray-mesh` is an experimental Go project that combines a small overlay-network control plane with an Xray/VLESS Internet egress integration point.

The project is intentionally split into two responsibilities:

- **Mesh / east-west traffic:** assign stable virtual IPs, register peers, route peer-to-peer packets, and later support local-network discovery forwarding.
- **Xray / north-south traffic:** hand Internet-bound traffic to an existing Xray/VLESS deployment instead of inventing a new obfuscation or censorship-evasion protocol.

## Status

Early prototype. The current tree provides a compilable control-plane skeleton:

- client and server commands;
- peer registration over HTTP;
- deterministic virtual IPv4 allocation from `10.66.0.0/24`;
- concurrency-safe peer registry;
- packet/router interfaces;
- TUN, discovery, and Xray adapter interfaces/stubs;
- unit tests and GitHub Actions CI.

It does **not** yet create a real TUN device or forward production traffic.

## Architecture

```text
+---------------- client ----------------+
|                                        |
|  Apps                                  |
|    |                                   |
|   TUN  <-- future real device          |
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
peer A (10.66.0.2) <---- encrypted transport ----> peer B (10.66.0.3)
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

Start the coordinator:

```bash
go run ./cmd/server -listen 127.0.0.1:8666
```

Register a peer:

```bash
go run ./cmd/client -server http://127.0.0.1:8666 -node laptop
```

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

- [ ] real TUN implementation
- [ ] packet classification
- [ ] peer-to-peer packet transport
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

The current HTTP registration endpoint is development-only and unauthenticated. Do not expose it to the public Internet. A production version needs authenticated peer identity, replay protection, authorization, lease expiry, and encrypted transport.

## License

No license selected yet.

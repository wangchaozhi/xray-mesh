# xray-mesh

`xray-mesh` is an experimental Go project that combines a small overlay-network control plane with an Xray/VLESS Internet egress integration point.

The project is intentionally split into two responsibilities:

- **Mesh / east-west traffic:** assign stable virtual IPs, register peers, route peer-to-peer packets, and later support local-network discovery forwarding.
- **Xray / north-south traffic:** supervise an existing Xray Core deployment instead of reimplementing VLESS, REALITY, or other transport protocols.

## Status

Early prototype. The current tree provides a control plane, a Linux-only peer data-plane MVP, Xray process supervision, and an optional local runtime-status API:

- client and server commands;
- peer registration over HTTP;
- deterministic virtual IPv4 allocation from `10.66.0.0/24`;
- concurrency-safe peer registry;
- Linux TUN implementation for overlay peer traffic;
- development UDP relay with per-registration session tokens and source-IP anti-spoofing;
- packet classification for mesh, mDNS/SSDP discovery, and future Internet egress;
- Xray config validation plus child-process lifecycle supervision;
- `/healthz` and `/v1/status` runtime endpoints for mesh/Xray state;
- unit tests and GitHub Actions CI.

The peer data plane is intentionally an MVP: the UDP relay is **not encrypted**, general Internet traffic is not routed into the mesh TUN, and the control plane is not production-authenticated.

## Architecture

```text
+-------------------- client ---------------------+
|                                                 |
| Apps                                            |
|  |                                              |
| mesh TUN ----> peer/discovery traffic ----------+--> mesh relay
|                                                 |
| Xray supervisor --> existing Xray Core ---------+--> Internet
|                                                 |
| local status API --> mesh/Xray health           |
+-------------------------------------------------+
                         |
                    coordinator
                         |
             peer registry / virtual IPs
```

The current design deliberately keeps the mesh TUN scoped to the overlay prefix. That avoids routing Xray's own server connection back into the mesh TUN and creating a recursive routing loop. Full/selected Internet routing will be added later with explicit policy routing.

## Goals

1. One client process can participate in a private overlay network.
2. Each peer receives a virtual IP.
3. Overlay peer traffic is routed through the mesh data plane.
4. The same client can supervise an existing Xray Core instance for Internet egress.
5. Runtime health is observable before more complex transparent routing is added.
6. Selected discovery traffic can later be relayed across the overlay.
7. Keep the mesh layer transport-agnostic so the underlying tunnel can evolve independently.

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

On Linux, enable the peer mesh TUN (normally requires root or `CAP_NET_ADMIN`):

```bash
sudo go run ./cmd/client \
  -server http://SERVER_IP:8666 \
  -relay SERVER_IP:8667 \
  -node laptop \
  -tun
```

To let the same client validate, start, monitor, and stop an existing Xray Core process, provide the Xray binary and a normal Xray JSON config:

```bash
sudo go run ./cmd/client \
  -server http://SERVER_IP:8666 \
  -relay SERVER_IP:8667 \
  -node laptop \
  -tun \
  -xray-bin /usr/local/bin/xray \
  -xray-config /etc/xray/config.json \
  -status-listen 127.0.0.1:8670
```

Before starting Xray, `xray-mesh` runs Xray's config test command. If the child process exits unexpectedly, the client reports the failure instead of silently continuing. `SIGINT`/`SIGTERM` is shared across the mesh and Xray lifecycle.

When `-status-listen` is set, the client exposes:

```text
GET /healthz
GET /v1/status
```

`/healthz` returns HTTP 200 only when every enabled runtime service is in the `running` state; otherwise it returns 503. The status listener is disabled by default and should normally be bound to loopback while the project is in prototype stage.

At this stage Xray and the mesh are managed by one client process, but Internet packets are **not yet injected into Xray by the mesh TUN**. Existing Xray system proxy/TUN configuration can continue to provide Internet egress independently while `xrmesh0` carries only overlay traffic.

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

- [x] Xray process supervisor
- [x] config validation and lifecycle monitoring
- [x] runtime health/status reporting
- [ ] generated Xray configuration/profile model
- [ ] SOCKS/transparent egress adapter
- [ ] policy routing for selected destinations
- [ ] loop-safe full-tunnel mode

### Phase 4 — discovery

- [ ] mDNS relay
- [ ] SSDP relay
- [ ] per-network discovery allowlists
- [ ] loop suppression and rate limiting

## Security model

The current HTTP registration endpoint is development-only and unauthenticated, and the UDP relay is not encrypted. Do not expose this prototype directly to untrusted networks. A production version needs authenticated peer identity, replay protection, authorization, lease expiry, encrypted transport, token rotation, and stronger endpoint binding. The Xray integration intentionally uses an existing Xray binary/config instead of inventing a new transport or obfuscation protocol. The runtime status endpoint is also development-oriented and should not be exposed publicly without authentication.

## License

No license selected yet.

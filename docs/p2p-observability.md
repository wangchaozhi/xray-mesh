# P2P observability

When the client status server is enabled, `GET /v1/status` now includes a `p2p` object with process-lifetime counters:

- `direct_healthy_marks`: number of times a peer direct endpoint was authenticated and marked/refreshed healthy;
- `direct_tx`: encrypted mesh packets successfully sent on a direct UDP path;
- `direct_rx`: encrypted direct packets successfully authenticated, replay-checked and delivered to the TUN;
- `direct_fallbacks`: packets for which a path was considered direct but the direct send could not be used and the caller fell back to relay;
- `replay_drops`: authenticated direct ciphertexts rejected because the same ticket+nonce had already been accepted.

Example:

```json
{
  "p2p": {
    "direct_healthy_marks": 7,
    "direct_tx": 182,
    "direct_rx": 176,
    "direct_fallbacks": 3,
    "replay_drops": 0
  }
}
```

The counters use atomics and do not change `/healthz` semantics. P2P direct transport remains an optimization; mesh health still depends on the existing mesh/Xray service states rather than requiring a direct path.
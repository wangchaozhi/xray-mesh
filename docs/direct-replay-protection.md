# Direct payload replay protection

AES-GCM authenticates direct peer datagrams, but authentication alone does not stop an attacker from retransmitting an already valid ciphertext. The direct receive path therefore keeps a short-lived replay cache.

The replay key is the public pair-ticket identifier plus the 96-bit GCM nonce. A replay key is recorded only after all of the following checks succeed:

- the datagram decrypts and authenticates under the live pair ticket;
- the authenticated node pair is valid;
- the inner IPv4 source matches the authenticated peer's mesh virtual IP;
- the inner IPv4 destination matches the local mesh virtual IP.

Only then is the `ticketID + nonce` accepted for the first time. A second datagram with the same key is dropped as a replay.

Replay-cache entries expire no later than their short-lived pair ticket, so stale nonce state is pruned continuously rather than accumulating for the lifetime of the process.
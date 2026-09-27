# Architecture

The system models **hosts**, **services**, and **directed links** independently. Identifiers are stable strings. Nothing in the code is specific to DMIT, ATT, or SeaFree.

```text
Browser ──HTTPS──> Go hub ──SQLite
                    ^
                    │ signed reports over a private network
                    │
                Go agents
```

The hub probes its assigned links and accepts reports from registered agents. Agent-to-hub transport should be private (for example Tailscale); per-agent HMAC-SHA256 signatures authenticate messages and prevent tampering. Timestamp, nonce, and database uniqueness prevent replay. Keys are configured independently so one agent can be revoked.

Each link chooses ICMP, TCP, or both, its destination and port, interval, and whether MTR applies. Agent probes are deliberately limited to static configured destinations; the hub cannot send arbitrary commands to agents. A Tailscale peer address can be attached to a link for direct/DERP state and per-peer byte counters. Interface byte counters are separate because tunnel payload bytes and provider-billed bytes differ.

Normal minute samples are kept for 24 hours, then represented by hourly summaries. Anomalies and their MTR reports last up to 30 days. A configured storage ceiling and minimum free-space threshold remove the oldest detail first. MTR runs only on anomalies (with cooldown) and occasional baseline captures. Intermediate-hop ICMP loss alone is not treated as end-to-end loss.

The UI is a static Next.js export embedded in the Go executable. Go provides authenticated JSON endpoints and a same-origin node-label edit endpoint. Public IP route metadata is fetched from RIPEstat by the hub and cached in memory; Tailscale and loopback addresses are never sent to that service. The public UI has HTTPS, authentication, rate limiting, body/time limits, and security headers. Volumetric DDoS requires upstream protection or source-IP restrictions; application code cannot prevent line saturation.

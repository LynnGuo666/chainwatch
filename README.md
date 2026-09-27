# Chainwatch

Lightweight distributed network monitoring. A Go hub collects signed reports from Go agents and serves a Next.js dashboard embedded in the same binary.

## Design

- **Hosts** are physical or virtual machines. An agent runs on each monitored host.
- **Links** are directed probes from one host to an address or service. A host can have any number of links. The hub is also an agent.
- **Services** are named TCP targets, such as a local Xray inbound. A 3x-ui entry synchronized from a remote node is not treated as a local service.
- Agents submit reports to the hub over a private transport such as Tailscale. Each agent has its own HMAC key; requests include a timestamp and nonce.
- The hub stores minute samples for 24 hours, hourly aggregates for 30 days, and anomaly/MTR details for 30 days. Retention and byte limits are configurable.
- The public dashboard uses HTTPS and read-only APIs. No shell, file, task, or proxy management endpoint exists.

Next.js uses `output: 'export'`; `next build` writes `web/out`, which is embedded into the Go binary. Next.js does not run on the server. API routes and server actions are intentionally implemented in Go.

## What it measures

- Directed ICMP and TCP probes, latency, jitter, loss, and reachability.
- Tailscale direct/DERP path and per-peer received/sent byte counters.
- Configured host network-interface traffic, load, memory, and disk.
- Low-frequency baseline MTR and anomaly-triggered MTR with a cooldown.
- Normal minute samples for 24 hours; hourly summaries and anomaly details for 30 days, with a bounded SQLite database.

The dashboard uses [Mantine](https://mantine.dev/) components and [Recharts](https://recharts.github.io/) charts. Go serves the exported files and all read-only APIs on one HTTPS port. The report listener binds a private IP and accepts only signed reports from registered agents.

## Build and deploy

The [GitHub Actions workflow](.github/workflows/build.yml) builds and tests on every push and pull request, then uploads Linux amd64 and arm64 binaries. A `v*` tag also publishes release assets. Build locally with `npm ci --prefix web && npm run build --prefix web && go test ./... && go build -o bin/chainwatch .`.

See [architecture](docs/architecture.md) for the data model and [deployment](docs/deployment.md) for private configuration, keys, TLS, and systemd units. Example configurations use reserved documentation IP addresses; keep real keys and deployment configuration out of Git.

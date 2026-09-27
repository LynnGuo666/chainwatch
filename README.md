# Chainwatch

Lightweight distributed network monitoring. A Go hub collects signed reports from Go agents and serves a Next.js dashboard embedded in the same binary.

## Design

- **Hosts** are physical or virtual machines. An agent runs on each monitored host.
- **Links** are directed probes from one host to an address or service. A host can have any number of links. The hub is also an agent.
- **Services** are named TCP targets, such as a local Xray inbound. A 3x-ui entry synchronized from a remote node is not treated as a local service.
- Agents submit reports to the hub over a private transport such as Tailscale. Each agent has its own HMAC key; requests include a timestamp and nonce.
- The hub stores minute samples for 24 hours, hourly aggregates for 30 days, and anomaly/MTR details for 30 days. Retention and byte limits are configurable.
- The public dashboard uses HTTPS. Authenticated operators can edit node display names and notes; no shell, file, task, or proxy management endpoint exists.

Next.js uses `output: 'export'`; `next build` writes `web/out`, which is embedded into the Go binary. Next.js does not run on the server. API routes and server actions are intentionally implemented in Go.

## What it measures

- Directed ICMP and TCP probes, latency, jitter, loss, and reachability.
- Tailscale direct/DERP path and per-peer received/sent byte counters.
- Configured host network-interface traffic, load, memory, and disk.
- Low-frequency baseline MTR and anomaly-triggered MTR with a cooldown.
- Normal minute samples for 24 hours; hourly summaries and anomaly details for 30 days, with a bounded SQLite database.

The dashboard uses [shadcn/ui](https://ui.shadcn.com/) components and its Recharts-based chart component. It shows incident timelines, explicit source IP → destination IP:port routes, public/Tailscale and reverse-direction comparisons, and adjacent MTR data. Optional `public_ip` and `tailscale_ip` fields on the hub and configured nodes identify each route's source; node display names and notes are editable in the dashboard. Public IP prefix and origin AS/holder are fetched from [RIPEstat Network Info](https://stat.ripe.net/docs/data-api/api-endpoints/network-info) and [AS Overview](https://stat.ripe.net/docs/data-api/api-endpoints/as-overview) with a one-hour in-memory cache. Conclusions are evidence-ranked hypotheses; intermediate-hop ICMP loss alone is not diagnosed as an ISP failure. Go serves the exported files and APIs on one HTTPS port. The report listener binds a private IP and accepts only signed reports from registered agents.

## Versions and login

`v0.1.0` is legacy. Use `v0.3.1` or newer: the dashboard shows hub, agent, frontend, and build versions. The build version is the Git commit SHA injected by Actions. Old agents are rejected with HTTP 426 and must be upgraded alongside the hub; historical records without version fields show `legacy · 请更新新版`. Each future change increments the version as specified in [AGENTS.md](AGENTS.md).

The dashboard has a shadcn/ui login page. A successful HTTPS login issues a 12-hour Secure, HttpOnly, SameSite=Strict session cookie; restarting the hub invalidates sessions. The old browser Basic Auth prompt is removed.

## Build and deploy

The [GitHub Actions workflow](.github/workflows/build.yml) builds and tests on every push and pull request, then uploads Linux amd64 and arm64 binaries. A `v*` tag also publishes release assets. Build locally with `npm ci --prefix web && npm run build --prefix web && go test ./... && go build -o bin/chainwatch .`.

See [architecture](docs/architecture.md) for the data model and [deployment](docs/deployment.md) for private configuration, keys, TLS, and systemd units. Example configurations use reserved documentation IP addresses; keep real keys and deployment configuration out of Git.

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

## Repository roadmap

1. Topology and security contract.
2. Go hub and agent, probes, signed reports, bounded SQLite storage.
3. Next.js dashboard with Mantine components and Recharts charts.
4. GitHub Actions build, tests, and Linux artifact.

See [architecture](docs/architecture.md) for the data model and [deployment](docs/deployment.md) for installation once implemented.

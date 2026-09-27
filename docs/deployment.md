# Deployment

## Build

Requires Go 1.27, Node.js 24 and npm. The release build runs:

```sh
cd web && npm ci && npm run build
cd .. && go test ./... && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o bin/chainwatch .
```

`web/out` is embedded in the binary. The server needs no Node.js runtime. GitHub Actions runs the same steps and uploads the Linux binary.

Build hub and agents from the same release commit (`v0.5.0` or newer). The hub rejects reports from agents on a different version with HTTP 426; update the agent before the hub when upgrading from `v0.1.0`. Actions injects the commit SHA into the Go binary and frontend build.

## Configure

Copy [hub.example.json](../configs/hub.example.json) and [agent.example.json](../configs/agent.example.json) to private files outside the repository. Replace every sample IP, ID, service, certificate and secret. Run `chainwatch gen-key` once per agent and put that value on the agent and in the hub's matching `nodes[].key`. Run `chainwatch hash-password` and enter a long password on stdin; put its hash in `web_password_hash`.

Give configuration files mode `0600`. Add each agent as a separate node with its own key and optional exact `source_ip`. Set `public_ip` and `tailscale_ip` on the hub and each configured node so the dashboard shows the source IP of each directed probe. Names and notes can then be edited in the authenticated dashboard without changing probe IDs. Keep the report listener on a private Tailscale address. HTTP reporting is rejected unless the hub address is Tailscale/loopback; use HTTPS for other private networks. The public dashboard listener needs an IP- or domain-valid TLS certificate.

The Traffic tab is configured on the hub. Set each node’s quota, UTC reset day, and billing mode there, then optionally enter the provider’s current-cycle usage as a calibration. The new accounting table starts empty on first launch and never imports historical probe or NIC samples. Calibration is scoped to the current cycle and does not change the underlying NIC counters.

Only attach `tailscale_peer` to one link for each source/peer pair, to avoid double counting that peer's traffic in summaries. Interface byte counters represent the host's NIC traffic; Tailscale peer counters represent tunnel traffic and do not equal provider billing.

Use `"protocol": "tcp"` for a public service that intentionally does not answer ICMP. Otherwise blocked Ping packets appear as 100% loss even while the real TCP service is reachable.

Start the hub with `chainwatch --config /etc/chainwatch/hub.json` and each agent with `chainwatch --config /etc/chainwatch/agent.json`. The binaries need `ping`, `mtr`, and `tailscale` available for those metrics. A TCP-only link can run without ping. Agent probes only the statically configured addresses; the hub has no remote execution endpoint.

## HTTPS and access

Do not put the dashboard on a public port before setting a strong password and TLS. IP certificates with short lifetimes must be renewed by their issuer and the hub restarted after renewal. Limit access upstream if possible. Software rate limiting does not prevent a bandwidth-saturating DDoS attack.

Open `/login` to sign in. The old Basic Auth browser prompt is no longer used. Sessions expire after 12 hours and are invalidated when the hub restarts.

For systemd, create a dedicated `chainwatch` system user, load private config and TLS files with `LoadCredential`, use `StateDirectory=chainwatch` with mode `0700`, and restart the hub after certificate renewal. Example units are in [`deploy/systemd`](../deploy/systemd). The hub and agent should each have a private `0600` config file. The hub stores its SQLite database and WAL under the state directory.

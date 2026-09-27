# Chainwatch contributor rules

## Versioning

- The released `v0.1.0` build is **legacy**. Do not rewrite that tag or release.
- Every future functional change must increment the version before it is merged or deployed. Use SemVer: patch for fixes, minor for new capabilities, major for incompatible changes.
- Keep the hub, agent, and frontend version identifiers explicit. They may match, but each must be reported separately in the dashboard. The build identifier must be the source commit SHA (or a local `dev` marker) and must also be visible.
- Update the Go version constant, `web/package.json` version, frontend displayed version, README/release notes, and build metadata together. CI must inject the commit SHA into both the Go binary and Next.js build.
- Do not accept legacy agent reports in the new hub. Return HTTP 426 with an upgrade instruction. Historical records without version fields display `legacy · 请更新新版`.
- Make a separate Git commit for each coherent stage. Before tagging, run frontend typecheck/build, Go tests/vet, and the integration smoke test. Tag a version only after CI succeeds on its source commit.

## Operations

- Never commit production keys, password hashes, or server-specific configuration.
- Do not change 3x-ui or SeaFree's server as part of Chainwatch deployment.
- Before replacing a deployed binary, verify its SHA-256 against the release artifact, then confirm both services and authenticated reporting after restart.

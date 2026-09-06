# Test Plan

This plan covers local validation, fake-transport acceptance tests, and live infrastructure tests. Live tests require explicit operator authorization.

## Principles

- Compare documented support with `route-steward capabilities`.
- Keep real inventory, SSH material, provider URLs, subscription tokens, client artifacts, raw observations, and recovery archives under ignored `private/` or another explicit private root.
- Run preflight before every local, remote, or external-publication mutation. Execute only when `ready=true`.
- Use only operator-owned or owner-authorized, dedicated, rebuildable Ubuntu 24.04 amd64 hosts for live deployment tests.
- Do not put live infrastructure or private subscription delivery in default CI.

## L0 Capability Smoke Test

Goal: confirm that the executable exposes the Go machine interface.

Normal URL-first use:

```powershell
route-steward capabilities
```

Focused discovery for a known operation:

```powershell
route-steward capabilities --operation add-server
```

Source-development checkout with Go 1.27 already available:

```powershell
go run ./cmd/route-steward capabilities
```

Pass criteria:

- The response is JSON with `success=true`.
- `product` is `route-steward` and `interface` is `agent-machine-surface`.
- Full discovery exposes the supported operation and driver projections.
- Focused discovery returns one operation contract without the full operation list or driver matrix.
- Product version behavior matches `version.txt`.

## L1 Source Validation

Goal: verify the Go engine, schemas, capability metadata, renderers, preflight logic, MCP interface, migration state, recovery, context projections, and sanitized failures. This path requires Go 1.27.

Commands:

```powershell
go mod verify
go test ./... -timeout 180s
go vet ./...
gofmt -l .
```

Windows full local validation:

```powershell
./scripts/Validate-Local.ps1
```

Pass criteria:

- Go module verification succeeds.
- All `internal/steward`, `cmd/route-steward`, and `tests/userjourney` tests pass.
- `go vet` reports no issues.
- `gofmt -l .` prints no files.
- Capability metadata and driver projections are tested near the Go implementation.
- Focused Server, Link, Route, Provider, Profile, and ClientTarget context projections preserve only relevant relationships.
- Ambiguous focused context fails closed.
- Machine failures, sanitized context, schema-1 compatibility, recovery tamper checks, MCP annotations, and operation enums remain stable.

## L2 Static Safety And Public Repository Boundary

Goal: prevent secrets, generated artifacts, live addresses, unsupported host assumptions, and stale localization from entering the public tree.

Commands:

```powershell
./scripts/Check-NoSecrets.ps1
./scripts/Test-Templates.ps1
./tests/Test-Version.ps1
./tests/Test-Agent.ps1
```

Optional local shell validation:

```bash
find server -type f -name '*.sh' -print0 | xargs -0 -n1 bash -n
shellcheck server/*.sh
```

Pass criteria:

- No private keys, tokens, subscription URLs, live public addresses, generated client artifacts, or recovery archives are tracked.
- Server shell payloads parse.
- Static checks cover server safety properties, Worker privacy, CI triggers, ignored paths, source encoding, and repository layout without pinning prose.
- The agent integration test covers the machine envelope, focused discovery, focused context, preflight, privacy, and assisted recovery without duplicating renderer and driver internals already owned by Go tests.
- Non-README Chinese documentation is not tracked. `README.zh-CN.md` is the localized entry point; operating documentation stays in English.

## L3 Worker Subscription Delivery

Goal: prove that the optional Cloudflare Worker remains a narrow private configuration delivery surface.

Commands:

```powershell
cd worker
npm ci --no-audit --no-fund
npm run types
npm run typecheck
npm test
npm run deploy:dry-run
npm audit --audit-level=low
```

Pass criteria:

- Only the exact token path returns the subscription body.
- `HEAD` does not return the body.
- Root requests, malformed tokens, and incorrect tokens fail closed.
- Write methods are rejected.
- Missing secrets fail closed.
- Wrangler dry-run builds without publishing.
- `npm audit --audit-level=low` reports no unresolved low-or-higher risk.

## L4 Fake-Transport CLI And MCP User Journey

Goal: exercise the installed binary lifecycle without connecting to real SSH hosts.

Command:

```powershell
go test ./tests/userjourney
```

Coverage:

- Clean bootstrap is idempotent.
- Missing state, invalid SSH users, and missing SSH keys are blocked before transport.
- Server, Link, direct Route, relay Route, Provider, Profile, and ClientTarget desired objects are saved and reloaded as current schema-2 inventory.
- Fake `ssh` and `scp` confirm that the validated user is passed as a separate transport option rather than rebuilt as `user@host`.
- Deploy adopts a semantically equivalent remote payload.
- Mihomo and Shadowrocket artifacts render under the private delivery root.
- Audit and drift move from unsynchronized to in-sync when matching evidence is present.
- Backup remains inside the local secret-prompt boundary.
- Stdio MCP uses the installed binary and the same Go engine.
- MCP focused capability input returns one operation contract.
- MCP focused context input returns the requested object and direct consumers without full-project context or private values.

## L5 Live Direct Route Smoke Test

Goal: prove that one direct Route can be deployed, audited, rendered, and validated with real client traffic on a dedicated host.

Prerequisites:

- One dedicated, rebuildable Ubuntu 24.04 amd64 host.
- Operator confirmation that `compute.host_ownership=dedicated`.
- A readable SSH private key under the selected private root.
- Reachable UDP ingress port.
- Operator confirmation that the host and network use fit `docs/OPERATING-BOUNDARY.md`.

Flow:

```powershell
go run ./cmd/route-steward execute --operation bootstrap --private-dir private
go run ./cmd/route-steward preflight --operation add-server --private-dir private --context-stdin
go run ./cmd/route-steward execute --operation add-server --private-dir private --context-stdin
go run ./cmd/route-steward execute --operation add-route --private-dir private --context-stdin
go run ./cmd/route-steward preflight --operation deploy-route --target <route-id> --private-dir private
go run ./cmd/route-steward execute --operation deploy-route --target <route-id> --private-dir private
go run ./cmd/route-steward execute --operation render-client --private-dir private
go run ./cmd/route-steward audit --target <route-id> --private-dir private
go run ./cmd/route-steward health --target <route-id> --private-dir private
```

Pass criteria:

- Deploy preflight reports `ready=true` and `authorization_class=remote-write`.
- Deploy succeeds and the Route state is `deployed`.
- Audit category is `in-sync`.
- Health status is `healthy`.
- Returned data omits SSH key paths, Provider URLs, subscription tokens, and exact public IPs unless `include_public_ip=true` is explicitly requested.
- Client artifacts are written only under the private delivery root.

## L6 Live Relay Route And WireGuard Link

Goal: prove that a single-hop WireGuard relay can be deployed and validated with real traffic.

Prerequisites:

- Two dedicated, rebuildable Ubuntu 24.04 amd64 hosts.
- Network reachability from entry to exit.
- An RST-owned WireGuard interface, port, and subnet.

Coverage:

- Add two Servers.
- Add one WireGuard Link.
- Add a relay Route with explicit entry, exit, and link.
- Deploy preflight returns `ready=true`.
- Deploy, audit, and health succeed.
- Health includes bounded relay/WireGuard evidence.
- Drift changes from true to false after matching audit, health, and render evidence are present.

## L7 Port Hopping

Goal: prove that 2-to-8 consecutive UDP port hopping remains consistent across validation, deployment, audit, health, rendering, and migration.

Coverage:

- Create a direct Route with a `port_hopping` range.
- Create a relay Route with a `port_hopping` range.
- Reject invalid ranges: one port, more than eight ports, non-consecutive values, or overlaps with existing live ranges.
- Render Mihomo, Karing, Shadowrocket, and Hysteria2 headless targets.
- Run audit and health for the hopping Route.

Pass criteria:

- UFW opens only the declared UDP range.
- Hysteria2 payload keeps `port` at the range start and carries the full range.
- Mihomo and Karing output the `ports` field.
- Shadowrocket output uses the standard multi-port Hysteria URI authority.
- Headless official-client JSON uses the same range.
- Invalid ranges fail before mutation.

## L8 ClientTarget Import And Runtime Checks

Goal: confirm that rendered artifacts are accepted by supported clients without manual editing.

Matrix:

| Renderer | Verification |
| --- | --- |
| `mihomo` | Import into a Clash Verge-compatible client; nodes and rules are present; TLS pinning and salamander obfuscation remain intact. |
| `karing` | Import the local Clash YAML on Windows, macOS, Linux, iOS, Android, or tvOS without editing fields. |
| `shadowrocket` offline | Generated node-import HTML has no external page resources and imports nodes successfully. |
| `shadowrocket` subscription | Worker token URL returns the config, wrong tokens fail closed, and token rotation affects only that ClientTarget. |
| `hysteria2` | `route-steward proxy --check --target <client-target-id>` succeeds and the runtime listens only on loopback. |

Pass criteria:

- Each ClientTarget uses the expected Profile and Route selection.
- Disabled Routes do not appear in output.
- Mihomo process-name routing, when configured, creates an `Applications` selection group and puts `PROCESS-NAME` rules after private-address direct rules and before geography rules.
- Karing output retains the tested TLS identity fields and SHA-256 fingerprint.
- A headless target selects exactly one enabled Route. Concurrent local runtimes use distinct loopback ports.

## L9 Migration, Rollback, And Recovery

Goal: prove that replacement is overlap-first and retryable without damaging the currently working path.

Coverage:

- Replace a direct Route entry Server.
- Replace a relay Route entry Server.
- Replace a relay Route exit Server.
- Replace a hopping relay exit and allocate a same-width, non-overlapping replacement range.
- Inject deployment failure, unhealthy health result, render failure, subscription publication failure, and rollback-pending states.
- Re-run the same `migrate-route` and confirm that the checkpoint preserves source and replacement identity.
- Restore a backup into a new private root and confirm observed evidence is reset and SSH key paths are rebound.

Pass criteria:

- The old Route remains enabled until replacement health is healthy.
- Client artifacts do not switch before healthy replacement traffic is proven.
- Render or publication failures restore old client output or record rollback-pending without overstating success.
- Completion leaves old remote capacity unretired and reports that retirement needs separate authorization.
- Recovery performs no remote mutation.

## L10 Release Gate

Goal: ensure the published tree is the same behavior validated by CI.

Hosted CI must pass:

- Go tests on Ubuntu 24.04, Windows, and macOS.
- Static validation.
- Windows `Validate-Local.ps1`.
- Release candidate archive build.
- Archived binary user journey.
- Worker install, type generation, typecheck, tests, dry-run, and audit.
- Aggregate `CI` job.

Before marking a PR ready:

- Confirm open GitHub issues are zero or explicitly out of scope.
- Record version impact in the PR body: `none`, `patch`, `minor`, or `major`.
- For a version change, run `scripts/Bump-Version.ps1` once from the current target-branch version.
- Review the final diff for private state, live addresses, generated client files, and recovery archives.
- Let hosted CI validate the same commit that will merge.

## Local Environment Notes

- Source validation requires Go 1.27 from `PATH` or an existing repository-local toolchain. Normal operation uses a matching Release binary.
- Worker validation requires Node 24, npm, and the pinned Worker dependencies.
- ShellCheck is required in hosted CI. A local machine without ShellCheck cannot fully replace CI.
- Live network validation requires dedicated test hosts and current operator authorization, and should stay outside default CI.

## Schema-2 Profile routing

Validation covers ordered generic Profile rules (`domain_suffix`, `geosite`, `geoip`), direct and Route actions, invalid matcher/action input, included/enabled Route references, deterministic renderer order, schema-1 in-memory translation, canonical schema-2 persistence, and schema-1 recovery archive restoration.

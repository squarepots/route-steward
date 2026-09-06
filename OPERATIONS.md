# Operations

This is the command and state reference for agents and contributors. New users can start with the [Quickstart](docs/QUICKSTART.md).

## Machine interface

The `route-steward` executable emits sanitized JSON and uses the private root selected by `--private-dir` (default `./private`). `route-steward mcp` exposes the same Go engine over local stdio.

The normal sequence is:

```text
capabilities
  ↓
bootstrap when private state is absent
  ↓
context + drift
  ↓
gather missing local/external facts
  ↓
create the requested objects
  ↓
preflight(operation, target, context)
  ↓
ready=false → gather/ask/stop
ready=true  → execute
  ↓
audit and validate
```

Every mutation requires preflight with `ready=true`. Send private operation context over stdin where possible.

## Agent result envelope

Machine responses use a stable top-level shape:

```json
{
  "schema_version": 1,
  "command": "preflight",
  "success": true,
  "code": "ok",
  "data": {}
}
```

`success=false` means the requested action did not complete. For a blocked mutation, inspect `missing_context`, `conflicts`, `authorized`, and `expected_effects`. Raw lower-level diagnostics stay local.

## Private state

```text
<private>/inventory.json    desired Server / Link / Route / Provider / Profile / ClientTarget state
<private>/secrets/          credentials, Provider URLs, subscription state, payloads
<private>/observed.json     sanitized audit and health evidence
<private>/migrations.json   resumable migration checkpoints
<private>/delivery/         generated ClientTarget files and render hashes
<private>/recovery/         encrypted recovery artifacts
<private>/tools/            verified runtime helper cache
<private>/health/           temporary health-check configuration
```

Use `context` and `drift` for normal inspection. Read raw private files only when an operation requires them, and keep their contents out of chat.

## Inventory schema

Inventory schema `2` contains:

- **Server** declares `compute.driver=byo-ssh`.
- **Link** declares `driver=wireguard`.
- **Route** declares `ingress.driver=hysteria2`.
- **Provider** declares `source_type=mihomo-http` and a local `source_secret_ref`.
- **Profile** selects Routes, optional Providers, and ordered generic routing rules.
- **ClientTarget** references a Profile and stores renderer/delivery settings. Renderer-specific fields such as Mihomo process names stay on the ClientTarget.

Schema-1 inventory remains readable through deterministic compatibility translation. A subsequent desired-state write persists schema 2. `version.txt` stores product SemVer separately.

## Capability discovery

Run `route-steward capabilities` for operations, drivers, renderers, required context, effects, executors, and authorization classes.

`route-steward migrations --private-dir <directory>` returns sanitized checkpoint summaries after an agent or process restart.

## Bootstrap

Bootstrap creates empty schema-2 inventory plus schema-1 secret index and observed state, then creates the output directories. It is idempotent for a complete private root and rejects partial initialization.

## Structured desired-state operations

Creation operations accept structured context and return JSON.

### Server

Adding a Server records its ID, public network facts, SSH user and key path, ownership, and optional provider metadata. It updates local inventory without connecting to the server.

### Link

A new WireGuard Link references entry and exit Servers, allocates its interface, port, and subnet, and generates keys locally. Deployment is a separate operation.

### Route

A new direct or relay Route references existing topology, allocates a listener when needed, generates Hysteria2 credentials and certificates locally, and starts disabled with state `pending`. Successful deployment enables it. Optional `port_hopping` uses 2–8 consecutive UDP ports beginning at `listen_port`; deployment opens that UFW range, grants the service capability required by Hysteria, and rejects listener conflicts.

### Provider

A generic Provider is optional. Its URL is stored only in local secret storage. Provider update is transactional and removal is blocked while a Profile references it.

### Profile / ClientTarget

Profiles store Route and Provider selection plus ordered generic routing rules. A rule matches a domain suffix, geosite category, or geoip category and selects either direct handling or an enabled Route included by the Profile. ClientTargets store renderer and delivery settings. References block unsafe Profile, Provider, and subscription-backed ClientTarget removal.

## Deployment ownership

Route deployment runs the embedded server scripts and keeps lower-level output out of the JSON response.

RST owns its `/usr/local/lib/route-steward`, `/etc/route-steward`, `/var/lib/route-steward`, `route-steward-*` systemd units, `route-steward-hysteria` runtime user, `wg-rst*` Link interfaces, generated files, and individually named policy files. Initial host preparation installs the RST-required package set, an RST-named SSH key-only drop-in, and a UFW baseline. A host marker makes that preparation one-time for current releases.

Use a dedicated, rebuildable Ubuntu 24.04 amd64 host. Deployment and uninstall leave unrelated Xray, Hysteria, WireGuard, service, package, and firewall state in place. Older Route Steward releases may already have changed swap/fstab, SMTP egress, sysctl/BBR, journald, unattended-upgrades, or vnstat; current deployment does not recreate those settings and does not automatically reverse them.

An already-deployed Route is audited before another deployment. Drifted or undetermined state blocks the operation until the discrepancy is understood.

After deployment, audit updates observed evidence and rendering records ClientTarget hashes. During migration, rendering waits until the replacement passes health.

## Audit and drift

Remote audit converts server results into typed evidence. Raw SSH and server diagnostics stay local.

Supported drift categories include `service-missing`, `remote-config-mismatch`, `firewall-network-mismatch`, `wireguard-link-mismatch`, `hysteria-listener-mismatch`, `certificate-mismatch`, `egress-mismatch`, `client-render-stale`, `undetermined`, and in-sync/disabled/never-audited informational states.

Observed evidence can be regenerated. Repair requires a supported operation and preflight.

## Connection health

`route-steward health --target <route-id>` audits the Route, starts the pinned official Hysteria2 client with a temporary loopback HTTP proxy, and sends requests to ipify's IPv4/IPv6 endpoints and Cloudflare's trace endpoint. It reports server reachability, audit, handshake, Internet and DNS access, exit identity, address families, latency, and relay WireGuard state. Hopping Routes use their rendered multi-port client configuration.

Health leaves inventory and remote state unchanged. It may download the checksum-verified helper into `<private>/tools/` and records sanitized evidence in `observed.json`. Temporary configuration under `<private>/health/` contains live Route credentials, uses private permissions, and is removed afterward. Public IP values require an explicit request. A failed health check supplies evidence for a later decision.

## ClientTargets

Renderers consume a ClientTarget plus its referenced Profile.

Current renderers:

- `mihomo` — private Hysteria2 Routes plus zero or more explicitly included generic Providers, with file or optional private-subscription delivery and optional target-scoped process-name rules;
- `karing` — private Clash YAML tested with Karing 1.2.23.2606 and Hysteria2 certificate pinning;
- `shadowrocket` — offline node import or target-scoped private subscription import;
- `hysteria2` — official-client JSON for one explicitly selected managed Route, with HTTP and SOCKS5 sharing one loopback listener.

A Provider is optional. New Profiles start with no routing rules. Schema-1 Profiles are upgraded in memory: historical explicit service bindings become geosite Route rules, `balanced-cn` becomes its equivalent direct rules, and `privacy` or blank becomes an empty rule set. A successful desired-state write persists canonical schema 2.

Mihomo process routing uses `ClientTarget.mihomo_process_names` with plain executable or package names. Generated YAML sets strict process matching, adds an `Applications` group with `DIRECT` and `Private Routes`, and places `PROCESS-NAME` rules after private-address rules and before ordered Profile routing rules. Its explicit `GLOBAL` group contains managed nodes, `DIRECT`, `REJECT`, and included Provider sets. Sanitized context reports only the number of configured process names. Client applications control TUN, system proxy, host routing, and DNS capture.

The headless renderer defaults to `127.0.0.1:1080` and `auto` ingress selection (IPv4, then IPv6). It accepts only IP-literal loopback listeners. Separate concurrent targets need separate ports.

For port hopping, Mihomo and Karing receive `ports`, Shadowrocket receives the range in its Hysteria URI, and headless JSON carries the range with a 30-second UDP hop interval.

`route-steward proxy --target <client-target> --check` renders the target, obtains the verified official Hysteria2 client, makes one HTTP request through the Route, compares its IPv4 exit with inventory, and stops. Plain `proxy` runs in the foreground for supervision by the caller. The check contacts ipify and omits the observed address.

New renderer support requires implementation and tests in Route Steward.

## Private subscription

Subscription state belongs to one ClientTarget. Publication accepts a Mihomo or Shadowrocket target, uses its Worker/host identity and bearer token, exports the renderer-specific configuration, deploys the Worker, and verifies that the endpoint returns the exact generated body. Mihomo publication also writes a private subscription-reference artifact for one-time import into a Clash Verge-compatible client; later publication updates the same URL.

Token rotation is a `credential-change`, requires explicit current approval, and recovers interrupted publication through a local pending token. It changes only the selected ClientTarget and has a dedicated command outside generic MCP execution.

Cloudflare authentication comes from the user's local Cloudflare/Wrangler environment. Route Steward keeps authentication tokens out of output and changes only the selected Worker.

Cloud-hosted agents may see the tool arguments required for an operation, including server IP, SSH username, local key path, and selected IDs. Use an offline runtime when those values must remain local to the operator machine.

## Migration

Migration records each stage in private state:

1. inspect current Route/dependencies;
2. add replacement Server/Link/Route capacity as required;
3. deploy replacement while old capacity remains enabled;
4. audit and run a real Hysteria2 health check against the replacement;
5. after a healthy result, switch the relevant Profile selections, render affected ClientTargets, and republish subscription-backed targets;
6. if rendering/publication fails or the process is interrupted, restore the old selection before retrying and recheck replacement health;
7. record the completed switch while leaving old remote services and cloud capacity intact;
8. leave retirement for a later user request.

`migrate-route` can register the replacement Server from structured context or use an existing Server. Relay replacement creates a matching WireGuard Link. A hopping direct Route or relay-entry replacement retains its range. A relay-exit replacement reserves the next same-width, non-overlapping range on the shared entry while both Routes are active. A blocked workflow returns `workflow-blocked`; retry with the recorded source Route and replacement Server.

## Backup and recovery

Backup creates an encrypted archive containing canonical schema-2 inventory, secrets, active migration checkpoints, and required SSH material. Regenerable observed evidence is not included in new archives. The password is entered through a local 7-Zip prompt.

Recovery restores to a clean private root, verifies the SHA-256 manifest, rejects unsafe paths and symlinks, translates supported schema-1 inventory when needed, relocates SSH material, validates current inventory, and resets observed evidence. Restored migrations are marked `recovery-revalidation-required` and repeat deployment and health checks.

## Contributor/debug interfaces

The PowerShell entry point forwards older callers to the executable. Embedded Bash payloads implement remote host changes.

Add missing product behavior to the Go engine with capability metadata and tests.

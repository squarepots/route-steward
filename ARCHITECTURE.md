# Architecture

Route Steward turns an agent's instructions into local state, server changes, and private client files.

```text
User request
    ↓
AI agent + repository Skill
    ↓
route-steward CLI or local stdio MCP
    ↓
Go engine
    ├─ private state and secrets
    ├─ preflight and operations
    ├─ SSH deployment and audit
    ├─ client rendering
    ├─ subscription publishing
    └─ migration and recovery
```

All supported agent runtimes call the same executable and Go engine.

## Product objects and drivers

- **Server** — bring-your-own Linux compute reached over SSH. Driver: `byo-ssh`.
- **Link** — connection between two Servers. Driver: single-hop `wireguard`.
- **Route** — logical network path offered to ClientTargets. A `direct` Route uses one Server; a `relay` Route references ingress Server, egress Server, and Link. Initial ingress driver: `hysteria2`.
- **Provider** — optional upstream third-party node source. Initial source type: generic `mihomo-http`.
- **Policy** — legacy schema-1 routing input retained for compatibility.
- **Profile** — reusable Route and Provider selection with ordered generic routing.
- **ClientTarget** — renderer and delivery settings for one Profile. Renderers: `mihomo`, `karing`, `shadowrocket`, and `hysteria2`.
- **Private subscription** — delivery state for one Shadowrocket ClientTarget.

## State layers

```text
<private>/inventory.json    desired infrastructure and client state
<private>/secrets/          credentials, keys, URLs, and subscription state
<private>/observed.json     sanitized audit and health results
<private>/delivery/         generated client files and render hashes
<private>/migrations.json   resumable migration state
<private>/recovery/         encrypted recovery artifacts
```

`<private>` is an ignored local directory or an external path supplied through `--private-dir`. Inventory and secrets supply operation inputs. Audit, health, and render evidence can be regenerated.

## State compatibility

Inventory schema `2` stores Servers, Links, Routes, Providers, Profiles, and ClientTargets. Schema-1 state is translated at load time; legacy policy and China/service fields are not current state. Product SemVer is stored separately in `version.txt`.

## Neutral bootstrap

Bootstrap creates empty schema-2 inventory, secret index, observed state, and private output directories. The agent adds objects after gathering the user's setup.

## Preflight

Each mutation combines the requested intent with discovered context:

```text
intent + discovered context
        ↓
preflight
  ├─ capability supported?
  ├─ state schema current?
  ├─ target unambiguous?
  ├─ required local state/secrets/access present?
  ├─ dependencies/conflicts known?
  ├─ expected effects known?
  └─ authorization class satisfied?
        ↓
ready=true → execution
ready=false → gather context / ask human / stop
```

The Go engine returns missing context, conflicts, expected effects, authorization class, and readiness for each requested operation.

## Agent interfaces

The Go `route-steward` executable provides the CLI and local stdio MCP server on Linux, macOS, and Windows for amd64 and arm64. Both interfaces use the same engine. System OpenSSH provides transport, and embedded `server/*.sh` files implement remote changes. The PowerShell entry point forwards older callers.

Private structured context can be passed over stdin. Subscription-token rotation uses its dedicated command and approval path.

## Network model

Initial supported topology:

```text
direct:
client → Hysteria2 entry/exit Server → declared exit

relay:
client → Hysteria2 entry Server → WireGuard Link → exit Server/NAT → declared exit
```

Each Link receives an RST-named interface, UDP port, and subnet. Deployment and uninstall manage RST-owned resources and named policy files. Initial host preparation installs only the RST-required package, SSH, and firewall baseline documented in [Operations](OPERATIONS.md#remote-ownership). Supported hosts remain dedicated and rebuildable until broader host sharing is proven.

A Route may use a 2–8-port Hysteria UDP hopping range. Inventory, deployment, audit, client rendering, and migration all carry that range. A relay-exit replacement reserves a same-width, non-overlapping range while both paths are live.

## ClientTarget rendering

A renderer resolves a ClientTarget, its Profile, the selected Routes, and optional Providers.

- Mihomo ClientTargets use private file or optional private-subscription delivery and may compose managed Routes with explicitly selected generic Providers. Optional `PROCESS-NAME` routing stays on the Mihomo ClientTarget and renders a manual `DIRECT` / Profile-route selection group.
- Karing ClientTargets use tested private Clash YAML and retain SHA-256 certificate pinning for every managed Hysteria2 node.
- Shadowrocket ClientTargets render private Hysteria2 node imports or use optional target-scoped subscription delivery.
- Hysteria2 ClientTargets select one enabled Route, render official-client JSON, and expose HTTP/SOCKS5 on an IP-literal loopback listener.

Hopping Routes use Hysteria's multi-port endpoint in every renderer. The headless client uses a 30-second UDP hop interval.

Output filenames derive from ClientTarget IDs. Profiles store ordered `routing.rules`. A rule matches `domain_suffix`, `geosite`, or `geoip` and sends matching traffic either directly or through an enabled Route included by the Profile. Schema-1 policy, China-direct, and service bindings are translated when old state is loaded and are not canonical schema-2 fields.

Successful rendering records a hash-only manifest. Missing or outdated output becomes `client-render-stale`.

The `proxy` command renders a Hysteria2 target and uses the same pinned official client as Route health. Check mode sends a real HTTP request through the declared Route; run mode stays in the foreground.

## Private subscription delivery

The optional Worker publishes one private Mihomo or Shadowrocket subscription:

```text
ClientTarget + Profile + Route state
  → renderer-specific Mihomo YAML or Shadowrocket node export
  → subscription body + token hash as Worker secrets
  → isolated token-protected HTTPS endpoint
  → Shadowrocket refresh
```

Subscription state belongs to one ClientTarget. Different subscription-backed ClientTargets cannot share the same Worker identity or host in the current single-body design.

Token rotation is recoverable and changes one ClientTarget. The Worker stores the subscription body and token hash as secrets and serves the configuration from a non-cacheable HTTPS endpoint.

## Desired / observed / drift

Audit compares remote state with inventory and stores sanitized evidence in `observed.json`. Raw SSH and server diagnostics stay local.

Current drift taxonomy distinguishes RST service absence, remote configuration mismatch, firewall/network mismatch, WireGuard mismatch, Hysteria2 listener mismatch, certificate mismatch, egress mismatch, stale/missing ClientTarget renders, and undetermined state.

An already-deployed Route is audited before another deployment. Drifted or undetermined state must be investigated first.

## Migration and recovery

Infrastructure migration keeps the current Route available while replacement capacity is tested:

1. add replacement desired capacity;
2. deploy it while the current Route remains working;
3. audit the replacement;
4. render/update client delivery;
5. leave old capacity available until the user requests retirement.

Recovery verifies the encrypted archive manifest and paths, relocates private SSH material, validates current state, and resets observed evidence. The user decides any later remote change through the usual preflight.

See `docs/COMPATIBILITY.md` for current support and `SECURITY.md` / `docs/THREAT-MODEL.md` for security boundaries.

# Architecture

Route Steward turns an agent's instructions into private desired state, validated infrastructure changes, and client configuration.

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

The CLI and MCP surface call the same Go engine. Current supported drivers, renderers, limits, and platform baselines come from `route-steward capabilities`; [Compatibility](docs/COMPATIBILITY.md) is the readable projection of that support.

## Product objects

- **Server** — compute the operator is authorized to administer.
- **Link** — a managed connection between two Servers.
- **Route** — a direct or relay proxy path. A direct Route uses one Server; a relay Route references an entry Server, exit Server, and Link.
- **Provider** — an optional external node source referenced by Profiles.
- **Profile** — reusable Route and Provider selection with ordered generic routing.
- **ClientTarget** — renderer and delivery settings for one Profile.
- **Private subscription** — delivery state owned by one subscription-backed ClientTarget.

These objects describe user intent and stable relationships. Protocol and client support stay in capability metadata instead of being duplicated here.

## State layers

```text
<private>/inventory.json    desired infrastructure and client state
<private>/secrets/          credentials, keys, URLs, and subscription state
<private>/observed.json     sanitized audit and health evidence
<private>/delivery/         generated client files and render hashes
<private>/migrations.json   resumable migration state
<private>/recovery/         encrypted recovery artifacts
```

`<private>` is an ignored local directory or an external path supplied through `--private-dir`. Inventory and secrets are canonical operation inputs. Audit, health, and render evidence can be regenerated.

## State compatibility

Inventory schema 2 stores Servers, Links, Routes, Providers, Profiles, and ClientTargets. Schema-1 inventory is translated at load time. Legacy policy and regional/service routing fields are compatibility input and are not current state. Product SemVer is stored separately in `version.txt`.

Auxiliary state formats keep their own schema versions. A format version is changed only when that format changes.

## Neutral bootstrap

Bootstrap creates empty current inventory, secret index, observed state, and private output directories. It does not create a topology, Profile, or client choice for the user.

## Preflight

Each mutation combines requested intent with the relevant current context:

```text
intent + relevant context
        ↓
preflight
  ├─ capability supported?
  ├─ state format current?
  ├─ target unambiguous?
  ├─ required state, secrets, and access present?
  ├─ dependencies and conflicts known?
  ├─ expected effects known?
  └─ authorization satisfied?
        ↓
ready=true → execution
ready=false → gather context / ask human / stop
```

The Go engine returns missing context, conflicts, expected effects, authorization class, and readiness. Mutations do not bypass this gate.

## Agent context surface

Full `capabilities` is discovery for an unknown task. `capabilities --operation <id>` returns one operation contract when the operation is already known.

Full `context` is a sanitized project view. `context --target <id>` returns one object and the direct relationships needed to reason about it. A focused Route view can name the Profiles and ClientTargets that consume it without expanding unrelated Profile routing rules. A focused Profile view includes its routing intent because those rules belong to that Profile.

Target IDs may be reused across object kinds. A focused lookup with multiple matches fails with candidate kinds rather than choosing one.

The context surface omits credentials, server addresses, local key paths, Provider URLs, subscription URLs and tokens, raw diagnostics, generated configuration, and concrete Mihomo process names. [Privacy](docs/PRIVACY.md) owns the model-visible data policy.

## Agent interfaces

The native executable provides the CLI and local stdio MCP server. Both carry the same machine envelope and call the same engine. The PowerShell agent entry point is a compatibility forwarder to the native executable.

Private structured operation context can be passed over stdin. Operations that require a local secret prompt use their dedicated local workflow.

## Network model

```text
direct:
client → proxy entry/exit Server → declared exit

relay:
client → proxy entry Server → managed Link → exit Server → declared exit
```

Deployment manages resources owned by Route Steward for the selected Route and Link. Exact host prerequisites, preparation effects, uninstall ownership, and command behavior are documented in [Operations](OPERATIONS.md). Current host, protocol, and topology support is documented in [Compatibility](docs/COMPATIBILITY.md).

## Client rendering

A renderer resolves a ClientTarget, its Profile, selected Routes, optional Providers, and the secrets needed to produce the target artifact. Output is generated inside the private root.

Profiles own ordered generic routing rules. Renderer-specific settings stay on the ClientTarget. Client applications continue to own runtime choices such as active profile, selector state, TUN, and system proxy unless an implemented operation explicitly says otherwise.

Rendering builds and validates candidate output before replacing an existing artifact. Successful output records a target-scoped hash manifest used to detect missing or stale delivery artifacts.

The headless client path and Route health use the supported client runtime through the same pinned identity rules. Exact renderer behavior and current client baselines belong to capability metadata and Compatibility.

## Private subscription delivery

Optional subscription delivery publishes the rendered body for one eligible ClientTarget through its configured publisher:

```text
ClientTarget + Profile + Route state
  → renderer-specific subscription body
  → target-owned publication state and credential
  → private HTTPS endpoint
  → client refresh
```

Publication state and credentials belong to one ClientTarget. A publication result distinguishes rendered content, external publication, and later client refresh where the client runtime cannot be observed directly.

## Desired, observed, and drift

Inventory represents desired state. Audit and health read current remote behavior and store timestamped sanitized evidence. Drift compares desired state with that evidence and with generated client artifacts.

Observed evidence is historical after it is recorded. Decisions that require current remote truth use a fresh audit or health check instead of treating an old green record as current reality.

## Migration and recovery

Route replacement is a resumable transaction. Replacement capacity is created and validated while the current Route remains available. Client selection changes only after the replacement passes the required checks. Old capacity remains available until retirement is separately authorized.

Recovery verifies the encrypted archive, restores canonical private state, relocates private paths where required, and resets regenerable observed evidence. Restored infrastructure is audited before later remote mutation.

Operational phases and recovery commands belong to [Operations](OPERATIONS.md). Security boundaries and compromise response belong to [Security](SECURITY.md) and the [Threat Model](docs/THREAT-MODEL.md).

# Operations

This is the command, state, host, migration, and recovery reference for agents and contributors. New users can start with the [Quickstart](docs/QUICKSTART.md).

## Machine interface

The `route-steward` executable emits sanitized JSON and uses the private root selected by `--private-dir` (default `./private`). `route-steward mcp` exposes the same Go engine over local stdio.

Use the smallest read that supports the current task:

```text
route-steward capabilities --operation <operation>
route-steward context --private-dir <dir> --target <object-id>
```

Full `capabilities` and `context` remain available for discovery. Read `drift`, `audit`, `health`, or `migrations` when current observed or remote evidence affects the requested decision.

Every mutation runs preflight and requires `ready=true`. Send private structured context over stdin where possible.

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

Blocked mutations expose missing context, conflicts, authorization, and expected effects. Partial failures may report the completed stage, whether state changed, and a safe retry instruction. Raw lower-level diagnostics stay local.

## Private state

```text
<private>/inventory.json    desired state
<private>/secrets/          credentials, Provider URLs, subscription state and payloads
<private>/observed.json     sanitized audit and health evidence
<private>/migrations.json   resumable migration checkpoints
<private>/delivery/         generated ClientTarget files and render hashes
<private>/recovery/         encrypted recovery artifacts
<private>/tools/            verified runtime helper cache
<private>/health/           temporary health-check configuration
```

Read raw private files only when an operation requires them. Keep their contents out of chat and tracked files.

## Desired state

Inventory schema 2 contains:

- **Server** with BYO SSH compute facts;
- **Link** for one WireGuard hop;
- **Route** for a direct or relay Hysteria2 path;
- optional **Provider** for a Mihomo HTTP source;
- **Profile** for Route, Provider, and ordered routing selection;
- **ClientTarget** for renderer and delivery settings.

Schema-1 inventory remains readable through deterministic compatibility translation. A later desired-state write persists schema 2.

## Focused context

`context --target <id>` returns one object plus its direct relationships. It avoids expanding unrelated Profiles and routing rules. A Profile target includes its own routing rules because they are part of that Profile's intent. If the same ID exists in multiple object kinds, focused context fails with candidate kinds instead of guessing.

## Deployment ownership

RST owns its `/usr/local/lib/route-steward`, `/etc/route-steward`, `/var/lib/route-steward`, `route-steward-*` systemd units, `route-steward-hysteria` runtime user, `wg-rst*` interfaces, generated files, and RST-named policy files.

Current initial preparation installs the required package set, an RST-named SSH key-only drop-in, and the UFW baseline. A host marker makes current preparation one-time. Use a dedicated, rebuildable Ubuntu 24.04 amd64 host. Existing unrelated services, packages, networking software, WireGuard configuration, and firewall rules stay outside RST ownership.

Older releases may have changed swap/fstab, SMTP egress, sysctl/BBR, journald, unattended-upgrades, or vnstat. Current deployment does not recreate those settings and does not silently reverse them.

An already-deployed Route is audited before overwrite. Drifted or undetermined state blocks ordinary deployment until the discrepancy is understood.

## Audit and health

`audit` compares one supported remote Route with desired state and stores bounded evidence. `drift` compares desired state with the available observed and render evidence.

`health --target <route-id>` audits the Route, starts the pinned official Hysteria2 client with a temporary loopback proxy, and makes Internet and DNS requests through the Route. It checks exit identity, supported address families, latency, and relay WireGuard state. Public IP values are returned only when explicitly requested.

Observed evidence is historical. Run a current audit or health check when the decision depends on current remote state.

## ClientTargets

Current renderers and support are listed in [Compatibility](docs/COMPATIBILITY.md). Rendering validates staged output before replacing the current artifact. A failed validation leaves the previous usable file in place.

Mihomo and Shadowrocket ClientTargets may use private subscription delivery. Publication uses the selected target's Worker and token state, publishes the generated body, and verifies the endpoint returns that body. Token rotation is target-scoped and requires explicit current approval.

Client applications continue to own active profile selection, system proxy, TUN, and other runtime capture settings.

## Migration

`migrate-route` records a resumable checkpoint in private state. It creates replacement capacity, deploys and validates it while the old path remains available, switches affected Profile and ClientTarget output after health succeeds, republishes subscription-backed targets, and records completion.

If rendering or publication fails after a remote change, the workflow records the stage and restores or preserves the old selection as required before retry. Completed migration leaves old remote capacity available until the user requests retirement.

## Backup and recovery

Backup creates an encrypted archive containing canonical inventory, secrets, active migration checkpoints, and required SSH material. Regenerable observed evidence is excluded from new archives. The password is entered through a local 7-Zip prompt.

Recovery restores to a clean private root, verifies the archive manifest and paths, relocates private material, accepts supported schema-1 inventory, validates current state, recreates empty observed evidence, and marks restored migrations for revalidation before remote work continues.

## Contributor interfaces

The PowerShell agent entry point forwards compatible calls to the native executable. Embedded Bash payloads implement remote host changes. Add product behavior to the Go engine with focused tests and machine capability metadata.

---
name: route-steward
description: Operate Route Steward from natural-language intent. Use when a user provides this repository URL or asks to create, inspect, repair, migrate, render, back up, or recover a supported self-hosted network path.
---

# Route Steward

Route Steward lets an AI agent set up and manage private proxies on servers the user is authorized to administer.

## Start from a repository URL

When the user gives you the repository or asks to use RST:

1. Open or safely clone the repository, then read its `AGENTS.md` and this Skill.
2. Run `route-steward capabilities` to discover current operations and supported drivers. Prefer an installed release or download the matching OS/architecture archive with the user's existing GitHub or browser access. A source checkout may use `go run ./cmd/route-steward` when Go 1.27 is already available.
3. Run `go test ./...` before trusting modified source with real infrastructure.
4. Read `docs/OPERATING-BOUNDARY.md` and confirm the selected resources are operator-controlled or administered with the owner's permission.
5. Run `bootstrap` when private state is absent. For an existing setup, start with `context` and `drift`.
6. Explain the dedicated-host requirement and the host changes described below.
7. Gather the facts required for the requested Route, Profile, and ClientTarget.
8. Run preflight for every mutation. Resolve its missing context and conflicts, and execute only when `ready=true`.
9. Audit remote changes and render affected clients. Use `health` for real Route traffic or `proxy --check` for a headless Hysteria2 ClientTarget.
10. Use `migrate-route` for server replacement. On `workflow-blocked`, retry with the recorded source and replacement identities. Retire old capacity only after a separate user request.

Representative calls are in [references/operations.md](references/operations.md).

## Product model

- `Server`: dedicated, rebuildable Ubuntu 24.04 amd64 compute reached over SSH
- `Link`: one WireGuard hop between two Servers
- `Route`: direct or relay network path with Hysteria2 ingress
- `Provider`: optional Mihomo HTTP node source stored as a local secret
- `Profile`: reusable Route and Provider selection with ordered generic routing rules
- `ClientTarget`: renderer and delivery identity that references a Profile

Capability discovery defines the currently implemented operations, drivers, and renderers. See `docs/COMPATIBILITY.md` for the readable support matrix.

Host preparation changes UFW, RST-required packages, SSH key-only policy, and UFW baseline. Use a dedicated, rebuildable host.

## Operating rules

Use Route Steward operations for changes. Raw SSH is reserved for read-only diagnosis when no product diagnostic covers the question. Keep raw output local.

Investigate drifted or undetermined remote state before deployment. A drift report supplies evidence; the requested operation and preflight determine the authorized action.

The generated files support Mihomo/Clash Verge-compatible clients, Karing, Shadowrocket, and a headless Hysteria2 runtime as listed by `capabilities`. Client details are in [references/clients.md](references/clients.md).

Subscription-token rotation changes one Shadowrocket ClientTarget credential. It requires explicit current approval and is excluded from generic MCP execution.

Server replacement and recovery procedures are in [references/migration-recovery.md](references/migration-recovery.md).

## External facts

Use current authoritative sources for changing facts such as VPS offerings, firewall requirements, and client import behavior. Then map the result to an implemented capability and preflight. See [references/research.md](references/research.md).

## Secrets and reports

Store operational state under the selected private root. Keep tokens, keys, subscription URLs, live node URIs, server addresses, local key paths, generated configs, and raw diagnostics out of chat and public files unless the user requests one necessary disclosure.

A cloud model may receive operation inputs such as a server address, SSH username, key path, and object IDs. Use non-identifying IDs or an offline runtime when appropriate.

After an operation, report:

- what changed;
- what was validated;
- any remaining decision or failure;
- relevant host, privacy, or availability effects.

When the requested outcome is missing from capability discovery, explain the gap. Product support requires an implementation and tests in the Route Steward core.

# Route Steward repository instructions

Route Steward lets an AI agent set up and manage private proxies on infrastructure the user is authorized to administer.

## Operating Route Steward

1. Read `.agents/skills/route-steward/SKILL.md`.
2. Run `route-steward capabilities` before planning an operation. For normal use, run an installed release or download the matching archive from GitHub Releases. A source checkout may use `go run ./cmd/route-steward` when Go 1.27 is already available.
3. Run `go test ./...` before using modified source with real infrastructure.
4. Apply `docs/OPERATING-BOUNDARY.md` to the selected servers, accounts, and networks.
5. Bootstrap when private state is absent. Otherwise read `context` and `drift` first.
6. Preflight every mutation and execute only when `ready=true`.
7. Audit meaningful remote changes. Use `health` when client traffic matters and `proxy --check` for a headless Hysteria2 target.
8. Replace a server with `migrate-route`. Retry a blocked migration with its recorded source and replacement identities. Retiring old capacity requires a separate user request.

## Repository boundaries

The Go executable owns local state, preflight, rendering, deployment orchestration, drift, subscriptions, migration, and recovery. It also serves the local stdio MCP interface. Remote changes are implemented by the embedded `server/*.sh` payloads. `agent/route-steward-agent.ps1` forwards older callers to the executable.

Keep real inventory, observations, credentials, Provider and subscription URLs, SSH material, generated client files, and recovery archives under an ignored private root. Never copy them into tracked files, issues, logs, or chat. Use non-identifying IDs and report artifact paths relative to the private root.

Use Route Steward operations for configuration changes. Raw SSH is limited to read-only diagnosis when the product lacks a suitable diagnostic. Investigate drifted or undetermined remote state before another deployment.

Host preparation installs the RST-required package set, SSH key-only drop-in, and UFW baseline. Deployment supports dedicated, rebuildable Ubuntu 24.04 amd64 hosts. RST deployment and uninstall operate on RST-owned resources and named policy files; settings left by older Route Steward releases remain in place unless the operator changes them separately.

Subscription-token rotation affects one ClientTarget and requires explicit current approval.

## Documentation ownership

- `README.md`: product overview and entry point
- `docs/QUICKSTART.md`: first use
- `docs/COMPATIBILITY.md`: implemented support
- `OPERATIONS.md`: machine operations and remote ownership
- `docs/OPERATING-BOUNDARY.md`: infrastructure and network conditions
- `SECURITY.md`: trust, credentials, preflight, and reporting
- `docs/THREAT-MODEL.md`: compromise scenarios and response
- `docs/PRIVACY.md`: model, provider, and network visibility
- repository Skill: the agent operating procedure
- research records: dated decisions and evidence

Put a fact in its owning document and link to it elsewhere.

## Development and release

Treat every tracked file as public. Preserve the AGPL-3.0-only license and vendored notices.

Before a PR is ready:

1. review the final diff and update from the target branch;
2. classify version impact as `none`, `patch`, `minor`, or `major`;
3. record the decision and reason in the PR body;
4. for a version change, run `scripts/Bump-Version.ps1` once from the current target-branch version;
5. run full local validation and let hosted CI validate the same commit.

Follow `docs/RELEASING.md`. Git mutations and publication still require the active user's authorization.

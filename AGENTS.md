# Route Steward repository instructions

Route Steward lets AI agents set up and maintain private proxy routes on infrastructure the user is authorized to administer.

For Route Steward operations, follow `.agents/skills/route-steward/SKILL.md`.

## Repository boundaries

The Go executable owns local state, preflight, rendering, deployment orchestration, drift, subscriptions, migration, recovery, and the local stdio MCP interface. Remote changes are implemented by embedded `server/*.sh` payloads. `agent/route-steward-agent.ps1` is a compatibility forwarder.

Keep real inventory, observations, credentials, Provider and subscription URLs, SSH material, generated client files, and recovery archives under an ignored private root. Treat every tracked file as public. Use synthetic IDs and reserved example addresses in tracked tests and docs.

Use product operations for configuration changes. Raw SSH is limited to read-only diagnosis when no product diagnostic covers the question. Remote writes must stay inside RST ownership and pass preflight.

## Documentation ownership

- `README.md`: product overview and entry point
- `docs/QUICKSTART.md`: first use
- `docs/COMPATIBILITY.md`: readable projection of implemented support
- `OPERATIONS.md`: machine operations, state, host effects, migration, and recovery
- `ARCHITECTURE.md`: design and ownership boundaries
- `docs/OPERATING-BOUNDARY.md`: infrastructure and network conditions
- `SECURITY.md`: trust, credentials, preflight, and reporting
- `docs/THREAT-MODEL.md`: compromise scenarios and response
- `docs/PRIVACY.md`: model, provider, and network visibility
- repository Skill: agent operating procedure
- research records: dated external evidence

Put mutable facts in their owner and link to them elsewhere.

## Development and release

Preserve the AGPL-3.0-only license and vendored notices.

Run focused tests while editing. Before a PR is ready, review the final diff, update from the target branch, run the full validation suite, and let hosted CI validate the final commit.

Record one version impact in the PR body: `none`, `patch`, `minor`, or `major`. For a version change, run `scripts/Bump-Version.ps1` once from the current target version. Follow `docs/RELEASING.md`.

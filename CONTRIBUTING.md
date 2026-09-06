# Contributing

Route Steward lets AI agents set up and maintain private proxy routes on user-controlled servers.

## Contribution licensing

Route Steward is licensed under `AGPL-3.0-only`. Contributions use the same license. Every contributed commit must carry a Developer Certificate of Origin sign-off:

```text
git commit -s
```

## Read the context for your change

Always read `AGENTS.md`. Load the other owner documents when the change affects their domain:

- architecture, state, or ownership: `ARCHITECTURE.md`;
- agent operating procedure: `.agents/skills/route-steward/SKILL.md`;
- trust, credentials, or remote authority: `SECURITY.md` and, when relevant, `docs/THREAT-MODEL.md`;
- supported hosts, clients, drivers, or limits: `docs/COMPATIBILITY.md`;
- command, host, migration, or recovery behavior: `OPERATIONS.md`;
- releases and versions: `docs/RELEASING.md`.

A documentation-only change does not require reading unrelated operational or security material.

## Product changes

Changes should improve a specific user outcome, reliability, safety, or maintainability. A new driver, renderer, or operation needs implementation, focused behavior tests, machine capability metadata, and the appropriate compatibility documentation.

Use one owner for mutable facts. Link to that owner from other prose instead of copying implementation details across README, agent instructions, and runbooks.

## Public examples and private data

Use synthetic IDs and reserved values such as `192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24`, `2001:db8::/32`, and `example.invalid`.

Generated client files, credentials, recovery archives, local inventory and observed state, Provider URLs, SSH material, and subscription state stay outside the tracked tree.

## Translations

`README.md` owns the canonical product landing page. Translated READMEs are localized landing pages: keep the product meaning, start prompt, and links aligned while routing changing technical details to the canonical docs.

## Validation

Run focused tests while editing. Before review, run:

```text
go test ./...
go vet ./...
```

`scripts/Validate-Local.ps1` runs the complete public-tree, compatibility, Worker, Bash, and ShellCheck suite. Hosted PR CI validates the supported build and release surfaces.

## Pull requests and versions

Use an outcome-oriented title. Review the final diff and record one `Version impact`: `none`, `patch`, `minor`, or `major`.

For a version change, run `scripts/Bump-Version.ps1` once from the current target version. Release automation publishes the version present on `main`. See `docs/RELEASING.md`.

## Third-party material

Preserve pinned dependency metadata, license files, and vendored notices. Server-side service and path identifiers are deployed compatibility surfaces and require migration consideration before they change.

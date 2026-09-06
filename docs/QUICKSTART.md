# Quickstart

Route Steward helps an AI agent set up and manage a private proxy on VPS servers you control. The normal interface is one native executable for Linux, macOS, and Windows.

## 1. Download Route Steward

Download the archive for your operating system and architecture from [GitHub Releases](https://github.com/squarepots/route-steward/releases). Run `route-steward` (or `route-steward.exe`) from that location or add it to `PATH`.

Normal use requires no language toolchain. Source development uses Go 1.27:

```text
go run ./cmd/route-steward capabilities
go test ./...
```

The optional Cloudflare Worker publisher uses Node.js.

## 2. Give the repository to an agent

Paste this into Codex or another agent that can read files and run local commands:

```text
Open https://github.com/squarepots/route-steward and help me set up and manage a private proxy on servers I control. Read AGENTS.md and .agents/skills/route-steward/SKILL.md, use the Route Steward release for this computer, and begin with route-steward capabilities.
```

The agent discovers current support first, then creates private state if needed:

```text
route-steward capabilities
route-steward bootstrap --private-dir ./private
route-steward context --private-dir ./private
route-steward drift --private-dir ./private
```

## 3. Prepare the first proxy

The agent will ask for the facts declared by capability discovery:

- one dedicated, rebuildable Ubuntu 24.04 amd64 VPS for a direct route, or two for a relay;
- each server's public address, valid Unix SSH username, and local private-key path;
- whether you want a direct connection or a two-server relay;
- the client you plan to use.

Use non-identifying IDs such as `entry-a`, `route-a`, and `desktop-a`. Route Steward stores real operational values only in the selected private directory.

See [Compatibility](COMPATIBILITY.md) for supported routes, port hopping, and clients. Initial server preparation changes host-wide settings, so use a dedicated, rebuildable host.

## 4. Let the agent operate the workflow

```text
capabilities → bootstrap when absent → context and drift
→ create desired Server / Link / Route / Profile / ClientTarget objects
→ preflight → execute → audit → render
```

Preflight returns missing facts, conflicts, expected effects, authorization class, and `ready`. The agent resolves those results before execution.

## 5. Verify the result

The agent should report the Route state, audit result, and client artifact path relative to the private root. Audit checks managed server configuration. This command tests real client traffic:

```text
route-steward health --private-dir ./private --target route-a
```

For a headless Hysteria2 target, use `route-steward proxy --private-dir ./private --target <id> --check`.

For Clash Verge-compatible desktop use, a Mihomo ClientTarget can remain a private local file or use `publish-subscription` to create a stable private URL. Import that subscription once in the client; later Route Steward publications update the same URL. Route Steward does not change the client's TUN, system-proxy, active-profile, or selector settings.

Returned results omit credentials, absolute local paths, Provider URLs, subscription tokens, node URIs, and raw SSH output. Generated client files remain inside the private root.

## Later changes and recovery

For an existing setup, ask the agent to inspect `context`, `drift`, and any migration checkpoint before making changes. `migrate-route` tests replacement capacity before switching client output and leaves retirement of the old capacity for a later user request.

`route-steward backup` and `route-steward recover` use a local 7-Zip password prompt. See [Operations](../OPERATIONS.md) for command details and [Security](../SECURITY.md), [Privacy](PRIVACY.md), and the [operating boundary](OPERATING-BOUNDARY.md) before deployment.

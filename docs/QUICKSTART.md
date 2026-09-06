# Quickstart

Route Steward lets an AI agent set up and maintain private proxy routes on servers you control.

## 1. Install Route Steward

Download the archive for your operating system and architecture from [GitHub Releases](https://github.com/squarepots/route-steward/releases). Run the `route-steward` executable from that directory or add it to `PATH`.

Source development uses Go 1.27. The optional Cloudflare subscription publisher uses Node.js.

## 2. Give the repository to your agent

```text
Open https://github.com/squarepots/route-steward and use its Route Steward skill to set up or manage a private proxy on servers I control.
```

Describe the outcome in normal language. Useful facts include:

- the VPS or VPSs you control and their SSH access;
- whether you want a direct route or a relay through another server;
- how you want the routes used;
- the client applications you use, such as Clash Verge or Shadowrocket.

The agent uses capability discovery and private project context to determine the required Route Steward operations.

## 3. Let the agent prepare and validate the route

For a new setup, Route Steward creates a private state directory and records the required server, route, profile, and client state. Mutations pass preflight before execution.

Remote deployment is followed by checks appropriate to the requested result. A real Route traffic check can be run with:

```text
route-steward health --private-dir ./private --target <route-id>
```

For supported Mihomo/Clash Verge-compatible clients and Shadowrocket, Route Steward can generate private local configuration. Mihomo and Shadowrocket targets may also use an optional stable private subscription URL.

## 4. Continue from existing state

Ask the agent to inspect the relevant existing object before changing it. Route Steward supports focused reads:

```text
route-steward capabilities --operation <operation>
route-steward context --private-dir ./private --target <object-id>
```

Use full `capabilities` or `context` when the agent needs discovery across the project. Drift, audit, health, and migration status are read when current remote or historical evidence affects the requested operation.

Server replacement uses the resumable `migrate-route` workflow. Backup and recovery use a local 7-Zip password prompt.

Current support is in [Compatibility](COMPATIBILITY.md). Command, state, host, migration, and recovery details are in [Operations](../OPERATIONS.md). Read [Security](../SECURITY.md), [Privacy](PRIVACY.md), and the [operating boundary](OPERATING-BOUNDARY.md) for trust and visibility rules.

# Route Steward

[English](README.md) · [简体中文](README.zh-CN.md) · [日本語](README.ja.md) · [Español](README.es.md) · [Português (Brasil)](README.pt-BR.md)

[Quickstart](docs/QUICKSTART.md) · [FAQ](docs/FAQ.md) · [Compatibility](docs/COMPATIBILITY.md) · [Security](SECURITY.md) · [Releases](https://github.com/squarepots/route-steward/releases)

[![Validation](https://github.com/squarepots/route-steward/actions/workflows/ci.yml/badge.svg)](https://github.com/squarepots/route-steward/actions/workflows/ci.yml)

**Set up and maintain private proxy routes on servers you control with an AI agent.**

Tell the agent which servers you have, how you want to use the routes, and which clients you use. Route Steward deploys the supported proxy path, verifies real traffic, and produces private configuration for clients such as Clash Verge and Shadowrocket.

## Give the URL to an AI agent

```text
Open https://github.com/squarepots/route-steward and use its Route Steward skill to set up or manage a private proxy on servers I control.
```

See the [Quickstart](docs/QUICKSTART.md) for installation and first use.

## What it manages

- direct Hysteria2 routes and two-server WireGuard relays;
- route health, drift inspection, and resumable server replacement;
- private client configuration for supported desktop, mobile, and headless clients;
- optional private subscription delivery for Mihomo/Clash Verge-compatible clients and Shadowrocket.

Current host, client, protocol, and delivery support is listed in [Compatibility](docs/COMPATIBILITY.md). Operational effects and recovery are documented in [Operations](OPERATIONS.md).

Private state, credentials, generated client files, and recovery material stay under the private directory you select. A cloud AI runtime may still process operation inputs it needs. See [Privacy](docs/PRIVACY.md) and [Security](SECURITY.md).

Use servers, accounts, and network resources you own or are authorized to administer. See the [operating boundary](docs/OPERATING-BOUNDARY.md).

Route Steward is [AGPL-3.0-only](LICENSE). Vendored notices are in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) and [client/vendor/NOTICE.md](client/vendor/NOTICE.md).

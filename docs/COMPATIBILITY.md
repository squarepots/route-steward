# Compatibility

This page is the readable support matrix for the current release. `route-steward capabilities` is the machine-readable source for implemented operations and drivers. Use `route-steward capabilities --operation <id>` when only one operation contract is needed.

## Host and compute

| Capability | Supported value |
| --- | --- |
| Compute | Bring-your-own server over SSH |
| Operating system | Ubuntu 24.04 |
| Architecture | amd64 |
| Ownership | Dedicated, rebuildable host |
| SSH identity | Valid Unix username and local private-key path |

Current host preparation and uninstall effects are documented in [Operations](../OPERATIONS.md#deployment-ownership).

## Network topology

| Capability | Supported value |
| --- | --- |
| Ingress | Hysteria2 |
| Direct Route | Client → Hysteria2 Server → declared exit |
| Relay Route | Client → Hysteria2 entry → one WireGuard Link → exit/NAT |
| Link | Single-hop WireGuard over an RST-managed interface |
| Address families | Hysteria2 ingress supports IPv4 and IPv6; relay Link uses IPv4 |
| Port hopping | Optional bounded consecutive UDP range beginning at the Route listener |

## Desired state

Inventory schema 2 stores Server, Link, Route, optional Provider, Profile, and ClientTarget state. Schema-1 inventory and recovery archives remain readable through deterministic compatibility translation.

Profiles select Routes, optional Providers, and ordered generic routing rules. Current rule match types are `domain_suffix`, `geosite`, and `geoip`; actions are `direct` or an enabled included Route.

## Clients

| Renderer | Supported behavior |
| --- | --- |
| Mihomo | Private YAML for Mihomo/Clash Verge-compatible clients; local file or optional private subscription; Profile routing; optional Providers and process-name rules |
| Karing | Private Clash YAML with Hysteria2 certificate pinning |
| Shadowrocket | Private node import or optional private subscription |
| Hysteria2 | Official-client JSON plus foreground loopback HTTP/SOCKS5 runtime for one selected Route |

Client applications own their active profile, selector state, system proxy, TUN, and other runtime capture settings.

## Private subscription delivery

Mihomo and Shadowrocket ClientTargets may publish through an isolated Cloudflare Worker endpoint. Subscription state and bearer credentials are target-scoped. Publication verifies the returned body against the generated configuration.

## Validation and maintenance

- `audit` checks supported remote Route state without changing it.
- `health` runs an on-demand real Hysteria2 traffic check for direct and relay Routes.
- `migrate-route` replaces direct Routes or either relay endpoint through a resumable overlap workflow.
- encrypted local backup and recovery preserve durable private state and supported legacy inventory.

Detailed command behavior, host effects, migration, and recovery are in [Operations](../OPERATIONS.md). Security and visibility boundaries are in [Security](../SECURITY.md) and [Privacy](PRIVACY.md).

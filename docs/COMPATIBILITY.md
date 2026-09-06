# Compatibility

This page lists support in the current release. Run `route-steward capabilities` for the corresponding machine-readable list.

## Host and compute

| Capability | Supported value |
| --- | --- |
| Compute | Bring-your-own server over SSH |
| Operating system | Ubuntu 24.04 |
| Architecture | amd64 |
| Ownership | Dedicated, rebuildable host with `compute.host_ownership=dedicated` |
| SSH identity | Valid Unix username and local private-key path |

Initial setup installs the RST-required package, SSH key-only, and UFW baseline. See [Operations](../OPERATIONS.md#remote-ownership) for the exact effects and uninstall behavior.

## Network topology

| Capability | Supported value |
| --- | --- |
| Ingress | Hysteria2, with the server binary version and SHA-256 pinned in deployment code |
| Direct Route | Client → Hysteria2 Server → declared exit |
| Relay Route | Client → Hysteria2 entry → one WireGuard Link → exit/NAT |
| Link | Single-hop WireGuard over an isolated RST interface/port/subnet |
| Address families | Hysteria2 client ingress supports IPv4 and IPv6; the relay Link uses IPv4 |
| Optional port hopping | One `port_hopping` range of 2–8 consecutive UDP ports, starting at the Route listener; requires the server's nftables or iptables helper and opens that exact UFW range |

## Desired state

Inventory schema `2` stores:

- Server with `compute.driver=byo-ssh`;
- Link with `driver=wireguard`;
- Route with `ingress.driver=hysteria2`;
- optional Provider with `source_type=mihomo-http`;
- Profile for reusable Route, Provider, and ordered generic routing selection;
- ClientTarget for renderer and delivery identity.

Older schema-1 Profiles may contain `privacy`, `balanced-cn`, China-direct state, or historical service bindings. RST translates those values into schema-2 routing rules on load; new state uses only generic rules. Recovery accepts schema-1 archives, restores canonical schema 2, and resets observed evidence.

## Clients and rendering

| Capability | Supported behavior |
| --- | --- |
| Mihomo | Private YAML file or optional private subscription for Mihomo/Clash Verge-compatible clients, with explicit `GLOBAL`/emergency selection, Provider `use` composition, ordered Profile routing, and optional target-scoped `PROCESS-NAME` routing; compatibility baseline Mihomo 1.19.27 |
| Karing | Private Clash YAML imported from a local file; compatibility baseline 1.2.23.2606; Windows, macOS, Linux, iOS, Android, and tvOS |
| Shadowrocket offline | Private node-import HTML generated without external page resources |
| Shadowrocket subscription | Optional isolated Cloudflare Worker delivery for one ClientTarget |
| Hysteria2 headless | Private official-client JSON plus foreground loopback HTTP/SOCKS5 runtime for one selected Route |
| Profile routing | Ordered `domain_suffix`, `geosite`, and `geoip` matches with `direct` or enabled included Route actions |

A ClientTarget selects the renderer and delivery method. Its Profile selects Routes, optional Providers, and routing. The generated Mihomo/Karing YAML leaves TUN, system proxy, host routing, and active-profile settings to the client application.

Mihomo receives an explicit `GLOBAL` group containing managed nodes, `DIRECT`, `REJECT`, and included Provider sets. Optional `mihomo_process_names` accepts up to 32 plain executable or package names and creates an `Applications` group. Concrete names remain private.

Karing output requires certificate fingerprints on every managed Hysteria2 node and is imported as a local Clash file. A headless Hysteria2 target selects one enabled Route and binds HTTP/SOCKS5 to an IP-literal loopback address; `auto` ingress tries IPv4 and then IPv6.

Port hopping is rendered for all four clients. The headless client uses Hysteria's 30-second hop interval. See the dated [client](CLIENT-RESEARCH.md) and [reliability](RELIABILITY-RESEARCH.md) records for selection evidence.

## Providers

The optional `mihomo-http` Provider accepts an HTTP or HTTPS source URL stored in local secret storage. RST remains fully usable with zero Providers.

## Agent interfaces

| Interface | Supported behavior |
| --- | --- |
| Repository Skill | Operating instructions for AI agents |
| `route-steward` | Native sanitized JSON CLI for Linux, macOS, and Windows on amd64/arm64 |
| `route-steward mcp` | In-process local stdio MCP over the same Go engine |
| `agent/route-steward-agent.ps1` | Compatibility forwarder for older callers |
| AI runtime | A tool-capable runtime that can read the repository and invoke the executable |

## Audit, drift, migration, and recovery

Audit covers RST services and configuration, firewall and network state, WireGuard, Hysteria2 listeners and certificates, egress, and ClientTarget renders.

`health` supports direct and relay Routes and checks a real client handshake, Internet and DNS access, exit identity, supported address families, request latency, and relay state. It is an on-demand test; packet loss is currently unsupported. `proxy --check` performs a corresponding traffic test for one headless target.

`migrate-route` supports direct Route replacement and either endpoint of a relay. It tests replacement traffic before switching affected ClientTargets and preserves the old capacity. Recovery verifies the encrypted archive, relocates SSH material, translates supported schema-1 inventory when necessary, validates current state, and resets observed evidence.

## Optional Cloudflare delivery

The Worker delivers a token-protected Mihomo YAML or Shadowrocket node subscription for one isolated ClientTarget. Payloads are split into bounded Worker-secret chunks; the current RST limit is 240000 UTF-8 bytes. Mihomo responses include a YAML content type and a 24-hour subscription refresh hint. Subscription-token rotation is target-scoped, requires explicit current approval, and leaves Route and other ClientTarget credentials unchanged.

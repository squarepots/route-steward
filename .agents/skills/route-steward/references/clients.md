# ClientTargets

A `Profile` selects Routes, optional Providers, and ordered generic routing rules. A `ClientTarget` references a Profile and selects the renderer and delivery method. Multiple targets can reuse one Profile.

## Renderers

| Renderer | Output and use |
| --- | --- |
| `mihomo` | Private YAML for Mihomo/Clash Verge-compatible clients. Supports ordered Profile routing, a `GLOBAL` selector, Providers, optional `mihomo_process_names`, and optional private subscription delivery for Clash Verge-compatible clients. |
| `karing` | Private Clash YAML for Karing 1.2.23.2606 on Windows, macOS, Linux, iOS, Android, and tvOS. Import the generated file locally without editing it. |
| `shadowrocket` | Offline node import or optional private subscription delivery. |
| `hysteria2` | Official-client JSON for one enabled Route plus an HTTP/SOCKS5 listener bound to loopback. |

Client applications control TUN, system proxy, active profiles, host routes, and other runtime capture settings.

Choose a renderer from the user's client when the match is clear. For an unfamiliar or version-sensitive client, check authoritative documentation and the current RST compatibility matrix.

## Mihomo and Providers

`mihomo_process_names` accepts plain executable or package names. Rendering creates an `Applications` group, sets strict process matching, and places process rules after private-address rules and before Profile routing rules. Sanitized output reports the count of configured names.

Profiles may include zero or more Providers. Mihomo exposes them through `Private Routes` and the explicit `GLOBAL` selector. Karing uses the shared routing rules without the Mihomo-specific `GLOBAL` group.

## Headless Hysteria2

One target selects one enabled Route from its Profile. The listener is an IP-literal loopback address; concurrent targets need distinct ports. `auto` ingress tries IPv4 and then IPv6.

Use `route-steward proxy --target <id> --check` before depending on a target. It renders the JSON, obtains the pinned official client when needed, makes a real HTTP request, compares the exit with desired state, and stops. Plain `proxy` runs in the foreground.

## Private subscriptions

Mihomo and Shadowrocket targets can publish through an isolated Cloudflare Worker. Each subscription-backed ClientTarget has its own Worker/host identity and bearer token. The Worker stores the private configuration in bounded secret chunks and returns it over a non-cacheable endpoint. A Mihomo target also writes a private subscription-reference artifact for one-time import into Clash Verge-compatible clients; later RST publication updates the same URL. Client applications still own the active profile, selected group, system proxy, and TUN settings.

Token rotation changes only the selected target and requires explicit current approval.

## Private output

Keep generated configuration, import pages, subscription URLs, and full node URIs inside the private root. Render drift records artifact identities and hashes without exposing file contents.

# FAQ

## What does Route Steward do?

Route Steward helps an AI agent set up and manage a private proxy on VPS servers you control. It checks each change, configures the servers, and generates private client files.

## What do I need before I start?

You need the Route Steward release on a Linux, macOS, or Windows computer with a tool-capable AI agent. A direct Route uses one dedicated, rebuildable Ubuntu 24.04 amd64 VPS with SSH key access; a relay uses two. Supported clients are listed in [Compatibility](COMPATIBILITY.md).

## How do I install it?

Download the archive for your operating system and architecture from [GitHub Releases](https://github.com/squarepots/route-steward/releases). Run it in place, add it to `PATH`, or set `RST_ROUTE_STEWARD_BIN`. Source development uses Go 1.27.

## Why must the server be dedicated?

Current initial setup installs the RST-required package set, an SSH key-only drop-in, and the UFW baseline. RST still supports dedicated, rebuildable hosts so proxy deployment and host ownership stay unambiguous while broader shared-host support remains unproven. Older releases may have left additional host-wide settings in place.

## Can I start by giving the GitHub link to an AI agent?

Yes. Use the prompt in the [Quickstart](QUICKSTART.md). The agent reads the repository instructions, runs capability discovery, explains the prerequisites, and asks for the details needed for your Route.

## Will the AI model see my server details?

It may. Operation arguments can include a server address, SSH username, local key path, and selected IDs. RST sanitizes returned results, but a cloud runtime can still process the inputs it needs. Use non-identifying IDs and an offline runtime when those inputs must remain local.

## How are private files protected?

Inventory, credentials, generated client files, observed evidence, and recovery archives stay in the selected local private directory and are excluded from Git. They are plaintext unless your operating system, disk, or backup layer encrypts them. Portable recovery archives are encrypted through a local 7-Zip password prompt.

## What happens when a route changes unexpectedly?

Audit records the observed server state, and drift reports the type of mismatch. Route Steward requires the discrepancy to be understood before another deployment.

## Does audit prove that the proxy carries Internet traffic?

No. Audit checks supported server configuration, services, listeners, relay state, and server-side exit evidence. The separate `health` operation runs a real pinned Hysteria2 client, makes Internet and hostname requests through the Route, compares the observed public exit with desired state, and reports IPv4/IPv6 where declared. Treat configuration audit and end-to-end connection health as separate results.

## Does health expose my public IP or run monitoring continuously?

No. Health runs on demand. It contacts ipify's address endpoints and Cloudflare's trace endpoint through the proxy, stores the result locally, and reports whether the observed exit matches. Exact public IP values require an explicit request. Packet loss is currently unsupported.

## When should I use port hopping?

Use `port_hopping` when a network persistently throttles or filters particular UDP destination ports. RST supports one consecutive range of 2–8 ports beginning at the Route listener and renders it for every supported client. It cannot carry traffic across a network that blocks UDP entirely. See [Compatibility](COMPATIBILITY.md).

## Can Mihomo route one application differently?

Yes. A Mihomo ClientTarget can list plain executable or Android package names in `mihomo_process_names`. The generated `Applications` group lets the user select `DIRECT` or `Private Routes` for those processes.

## How are Profile routing rules and providers selected?

A Profile stores included Routes, optional Providers, and ordered generic routing rules. Rules may match a domain suffix, geosite category, or geoip category and select either direct handling or an enabled included Route. Mihomo lists included Provider sets in both `Private Routes` and its explicit `GLOBAL` group. Karing uses the same Profile routing rules.

## How does server replacement avoid interruption?

`migrate-route` creates and tests replacement capacity before switching affected ClientTargets. If a stage returns `workflow-blocked`, retry the same source Route and replacement Server. The completed migration leaves the old remote capacity available for a later user-requested retirement.

## Can a Linux server, script, or backend use a Route without a GUI app?

Yes. A `hysteria2` ClientTarget selects one enabled Route and a loopback port. `route-steward proxy --target <id> --check` tests real HTTP traffic and exit identity; without `--check`, it runs a local HTTP/SOCKS5 proxy in the foreground. Set the application's proxy setting to that loopback address.

## Which hosts, topologies, and clients work?

See [Compatibility](COMPATIBILITY.md) or run `route-steward capabilities`.

## What does the optional subscription Worker do?

It delivers one private Mihomo or Shadowrocket ClientTarget from an isolated Cloudflare Worker endpoint. The bearer token is target-scoped. Route Steward splits the private body into bounded Worker-secret chunks and currently limits the complete UTF-8 payload to 240000 bytes. Cloudflare remains inside that delivery path's privacy boundary.

## What operating conditions apply?

Each deployment uses servers, accounts, and network resources owned by the operator or administered with the resource owner's authorization. Applicable laws, carrier requirements, provider terms, and organizational policies shape the selected topology. See [Operating boundary](OPERATING-BOUNDARY.md).

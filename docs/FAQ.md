# FAQ

## What does Route Steward do?

Route Steward helps an AI agent set up and maintain private proxy routes on VPS servers you control. It records desired state locally, deploys supported routes, verifies them, and produces private client configuration.

## What do I need before I start?

A supported Route Steward release, a tool-capable AI agent, and authorized SSH access to the VPS or VPSs you want to use. See [Compatibility](COMPATIBILITY.md) for the current host and client support.

## Can I use Clash Verge?

Yes. Route Steward can generate Mihomo-compatible configuration for Clash Verge-compatible clients. A target may use a private local file or an optional stable private subscription URL.

## Can I use Shadowrocket?

Yes. Route Steward can generate private node import material or publish a target-scoped private subscription through the optional Cloudflare delivery path.

## Can different traffic use different routes?

Yes. A Profile may contain ordered routing rules that select direct handling or one of its included Routes. Current match types and renderer support are listed in [Compatibility](COMPATIBILITY.md).

## How does Route Steward know a route works?

Audit checks supported remote configuration and network state. `health` runs a real Hysteria2 client through the Route and checks Internet access, DNS, and declared exit identity. Use the evidence needed for the task rather than treating every inspection command as mandatory.

## What happens when I replace a server?

`migrate-route` creates and validates replacement capacity before switching affected client output. The workflow records a checkpoint so it can resume after interruption. See [Operations](../OPERATIONS.md).

## Where are my secrets stored?

Operational state, credentials, generated client files, subscription material, and recovery data stay under the selected private directory and outside the tracked repository. See [Privacy](PRIVACY.md) and [Security](../SECURITY.md).

## Will a cloud AI model see server information?

It may process operation inputs such as server addresses, SSH usernames, local key paths, and selected object IDs. Returned machine results are sanitized. Use an offline runtime when those inputs must remain local.

## Can the agent inspect only the relevant part of a project?

Yes. Use focused machine reads when the target is known:

```text
route-steward capabilities --operation <operation>
route-steward context --target <object-id>
```

Full discovery remains available when the task spans the project or the correct operation is unknown.

## Which hosts, topologies, clients, and delivery methods are supported?

See [Compatibility](COMPATIBILITY.md) or run `route-steward capabilities`.

## What operating conditions apply?

Use servers, accounts, client devices, and delivery accounts you own or are authorized to administer. See the [operating boundary](OPERATING-BOUNDARY.md).

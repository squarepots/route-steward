# Privacy boundary

This document describes what Route Steward, the chosen AI runtime, remote servers, and optional Cloudflare delivery can see.

## What stays local by product design

The Go engine reads and writes the selected private directory. Inventory, credentials, Provider URLs, subscription state, generated client files, observed evidence, and recovery archives stay outside the tracked source tree. Route Steward has no product telemetry or hosted database.

## What the AI runtime may receive

The runtime that operates RST may send prompts and tool arguments to its model provider. Depending on the operation, that can include server addresses, SSH usernames, local key paths, and stable Route/Profile/ClientTarget IDs. Migration checkpoints retain the supplied replacement Server context inside the selected private directory so an interrupted workflow can resume.

Returned machine results avoid server addresses, internal absolute paths, credentials, Provider URLs, subscription tokens, and raw diagnostics. Full `context` may include all Profile routing match values because they are desired routing intent. When one object is already known, `context --target <id>` returns only that object and its direct relationships; unrelated Profile rules are omitted.

Use an offline model or runtime when the model provider must not receive required operation arguments. Private Git storage and ignored files do not prevent a cloud model from receiving tool inputs.

## Local protection

Private state is plaintext by default. Protect the directory with operating-system permissions, encrypted disks where appropriate, controlled backups, and careful chat/log handling. Encrypted recovery archives are portable backups. Rotate exposed credentials after a compromise.

Never place real addresses, credentials, subscription URLs or tokens, SSH material, generated client files, recovery archives, or private state in public issues. Use synthetic reproductions for ordinary bug reports.

## Remote visibility

SSH/VPS providers see the network and account metadata inherent to their role. An optional Cloudflare subscription Worker can see request metadata such as source IP, time, and User-Agent. An on-demand `health` check sends small requests through the managed proxy to ipify address endpoints and Cloudflare trace. Headless `proxy --check` contacts the ipify IPv4 endpoint. Those services see the Route's exit IP and request metadata. Destination services see the exit IP and normal application-layer metadata. Route Steward provides no anonymity guarantee.

Health stores bounded status, time, latency, and match results in the local observed state. Exact public IP values are omitted from normal agent output and returned only when explicitly requested.

## Profile routing values

Profile routing match values are desired state. A focused Profile context includes that Profile's domain suffix, geosite, or geoip values so the operating AI can inspect and modify its routing intent. Focused context for unrelated Servers, Routes, Providers, and ClientTargets does not expand those rules.

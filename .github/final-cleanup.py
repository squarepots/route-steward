from pathlib import Path

ROOT = Path('.')

def read(path):
    return (ROOT / path).read_text(encoding='utf-8')

def write(path, text):
    (ROOT / path).write_text(text, encoding='utf-8', newline='\n')

def replace(path, old, new, count=1):
    text = read(path)
    if text.count(old) < count:
        raise RuntimeError(f'{path}: expected text not found: {old[:120]!r}')
    write(path, text.replace(old, new, count))

# Keep default agent context factually aligned with the implementation. This is a local fact correction,
# not a projection of general Playbook guidance.
replace('AGENTS.md',
'''Host preparation changes UFW, swap/fstab, SMTP egress, SSH, sysctl, journald, packages, unattended upgrades, and vnstat. Deployment supports dedicated, rebuildable Ubuntu 24.04 amd64 hosts. RST deployment and uninstall operate on RST-owned resources and named policy files; earlier global host settings remain in place.''',
'''Host preparation installs the RST-required package set, SSH key-only drop-in, and UFW baseline. Deployment supports dedicated, rebuildable Ubuntu 24.04 amd64 hosts. RST deployment and uninstall operate on RST-owned resources and named policy files; settings left by older Route Steward releases remain in place unless the operator changes them separately.''')

replace('.agents/skills/route-steward/SKILL.md',
'Host preparation changes UFW, RST-required packages, SSH key-only policy, and UFW baseline. Use a dedicated, rebuildable host.',
'Host preparation installs the RST-required package set, SSH key-only policy, and UFW baseline. Use a dedicated, rebuildable host.')
replace('.agents/skills/route-steward/SKILL.md',
'Subscription-token rotation changes one Shadowrocket ClientTarget credential.',
'Subscription-token rotation changes one subscription-backed ClientTarget credential.')

replace('ARCHITECTURE.md',
'- **Policy** — legacy schema-1 routing input retained for compatibility.\n',
'')
replace('ARCHITECTURE.md',
'- **Private subscription** — delivery state for one Shadowrocket ClientTarget.',
'- **Private subscription** — delivery state for one Mihomo or Shadowrocket ClientTarget.')
replace('ARCHITECTURE.md',
'''  → subscription body + token hash as Worker secrets
  → isolated token-protected HTTPS endpoint
  → Shadowrocket refresh''',
'''  → bounded subscription body chunks + token hash as Worker secrets
  → isolated token-protected HTTPS endpoint
  → Clash Verge-compatible or Shadowrocket subscription refresh''')
replace('ARCHITECTURE.md',
'Token rotation is recoverable and changes one ClientTarget. The Worker stores the subscription body and token hash as secrets and serves the configuration from a non-cacheable HTTPS endpoint.',
'Token rotation is recoverable and changes one ClientTarget. The Worker stores bounded subscription-body chunks and the token hash as secrets and serves the configuration from a non-cacheable HTTPS endpoint.')

replace('OPERATIONS.md',
'''RST owns its `/usr/local/lib/route-steward`, `/etc/route-steward`, `/var/lib/route-steward`, `route-steward-*` systemd units, `route-steward-hysteria` runtime user, `wg-rst*` Link interfaces, generated files, and individually named policy files. The initial host preparation also changes global UFW defaults, swap/fstab, SMTP egress, SSH/sysctl/journald, package, unattended-upgrades, and vnstat state.

Use a dedicated, rebuildable Ubuntu 24.04 amd64 host. Deployment and uninstall leave unrelated Xray, Hysteria, WireGuard, service, package, and firewall state in place. Uninstall removes RST-owned artifacts and named policy files. It cannot reconstruct earlier UFW defaults, swap/fstab, packages, or global host settings.''',
'''RST owns its `/usr/local/lib/route-steward`, `/etc/route-steward`, `/var/lib/route-steward`, `route-steward-*` systemd units, `route-steward-hysteria` runtime user, `wg-rst*` Link interfaces, generated files, and individually named policy files. Initial host preparation installs the RST-required package set, an RST-named SSH key-only drop-in, and a UFW baseline. A host marker makes that preparation one-time for current releases.

Use a dedicated, rebuildable Ubuntu 24.04 amd64 host. Deployment and uninstall leave unrelated Xray, Hysteria, WireGuard, service, package, and firewall state in place. Older Route Steward releases may already have changed swap/fstab, SMTP egress, sysctl/BBR, journald, unattended-upgrades, or vnstat; current deployment does not recreate those settings and does not automatically reverse them.''')
replace('OPERATIONS.md',
'- `mihomo` — private Hysteria2 Routes plus zero or more explicitly included generic Providers, with optional target-scoped process-name rules;',
'- `mihomo` — private Hysteria2 Routes plus zero or more explicitly included generic Providers, with file or optional private-subscription delivery and optional target-scoped process-name rules;')
replace('OPERATIONS.md',
'''Subscription state belongs to one ClientTarget. Publication resolves one Shadowrocket target, uses its Worker/host identity and bearer token, exports the current node list, validates and deploys the Worker, verifies the endpoint, and rebuilds local render state.''',
'''Subscription state belongs to one ClientTarget. Publication accepts a Mihomo or Shadowrocket target, uses its Worker/host identity and bearer token, exports the renderer-specific configuration, deploys the Worker, and verifies that the endpoint returns the exact generated body. Mihomo publication also writes a private subscription-reference artifact for one-time import into a Clash Verge-compatible client; later publication updates the same URL.''')
replace('OPERATIONS.md',
'Backup creates an encrypted archive containing canonical schema-2 inventory, active migration checkpoints, required SSH material, and auxiliary private state.',
'Backup creates an encrypted archive containing canonical schema-2 inventory, secrets, active migration checkpoints, and required SSH material. Regenerable observed evidence is not included in new archives.')

replace('SECURITY.md',
'''The supported host is a dedicated, rebuildable Ubuntu 24.04 amd64 server. Initial preparation changes host-wide UFW, SMTP egress, swap/fstab, SSH/sysctl/journald/BBR, packages, unattended-upgrades, and vnstat.''',
'''The supported host is a dedicated, rebuildable Ubuntu 24.04 amd64 server. Current initial preparation installs the RST-required package set, an RST-named SSH key-only drop-in, and the UFW baseline. Older Route Steward releases may have left additional host-wide tuning or policy changes; current releases do not silently reverse them.''')
replace('SECURITY.md',
'Subscription state belongs to one Shadowrocket ClientTarget.',
'Subscription state belongs to one Mihomo or Shadowrocket ClientTarget.')

replace('docs/FAQ.md',
'''Initial setup prepares the whole host. It changes UFW defaults, swap/fstab, SSH, sysctl, journald, packages, unattended-upgrades, SMTP egress, and vnstat state. A fresh dedicated host makes those effects explicit and keeps unrelated production workloads outside the change boundary.''',
'''Current initial setup installs the RST-required package set, an SSH key-only drop-in, and the UFW baseline. RST still supports dedicated, rebuildable hosts so proxy deployment and host ownership stay unambiguous while broader shared-host support remains unproven. Older releases may have left additional host-wide settings in place.''')
replace('docs/FAQ.md',
'''It delivers one private Shadowrocket ClientTarget configuration from an isolated Cloudflare Worker endpoint. The bearer token is target-scoped and the UTF-8 configuration body is limited to 5120 bytes. Cloudflare remains inside that delivery path's privacy boundary.''',
'''It delivers one private Mihomo or Shadowrocket ClientTarget from an isolated Cloudflare Worker endpoint. The bearer token is target-scoped. Route Steward splits the private body into bounded Worker-secret chunks and currently limits the complete UTF-8 payload to 240000 bytes. Cloudflare remains inside that delivery path's privacy boundary.''')

# Add direct coverage for the Mihomo subscription path without contacting Cloudflare.
write('internal/steward/subscription_mihomo_test.go', r'''package steward

import (
	"strings"
	"testing"
)

func TestMihomoPrivateSubscriptionLifecycleWithoutPublication(t *testing.T) {
	state, route := healthFixture(t, "direct", false)
	if _, err := AddProfile(state, map[string]any{
		"profile_id":     "desktop-profile",
		"include_routes": []any{route.ID},
		"routing": map[string]any{"rules": []any{
			map[string]any{
				"match":  map[string]any{"type": "geoip", "value": "US"},
				"action": map[string]any{"type": "route", "route": route.ID},
			},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := AddClientTarget(state, map[string]any{
		"target_id":  "desktop",
		"profile_id": "desktop-profile",
		"renderer":   "mihomo",
	}); err != nil {
		t.Fatal(err)
	}

	preflight, err := NewPreflight("publish-subscription", "desktop", state, map[string]any{
		"worker_name": "synthetic-worker",
		"host":        "subscription.example.invalid",
	}, false)
	if err != nil || !preflight.Ready {
		t.Fatalf("Mihomo subscription preflight failed: %#v err=%v", preflight, err)
	}

	subscription, err := initializeSubscriptionState(state, "desktop", "synthetic-worker", "subscription.example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	target := findClientTarget(state.Inventory, "desktop")
	if target == nil || target.Delivery != "subscription" || target.SubscriptionSecretRef == "" {
		t.Fatalf("Mihomo target did not enter subscription delivery: %#v", target)
	}
	if subscription.Host != "subscription.example.invalid" || subscription.Token == "" {
		t.Fatalf("subscription state is incomplete: %#v", subscription)
	}

	body, count, err := ExportSubscriptionBody(state, "desktop")
	if err != nil {
		t.Fatal(err)
	}
	if count == 0 || !strings.Contains(body, "proxies:") || !strings.Contains(body, "GEOIP,US,RST-Route-") {
		t.Fatalf("Mihomo subscription body is incomplete: count=%d body=%s", count, body)
	}
	chunks, err := subscriptionBodyChunks(body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(chunks, "") != body {
		t.Fatal("subscription chunks did not reconstruct the exact Mihomo body")
	}
	for _, chunk := range chunks {
		if len([]byte(chunk)) > subscriptionSecretChunkBytes {
			t.Fatalf("subscription chunk exceeds the per-secret bound: %d", len([]byte(chunk)))
		}
	}
}

func TestSubscriptionChunksPreserveUTF8(t *testing.T) {
	body := strings.Repeat("节点-route-", 700)
	chunks, err := subscriptionBodyChunks(body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(chunks, "") != body {
		t.Fatal("UTF-8 subscription body changed while chunking")
	}
	for _, chunk := range chunks {
		if len([]byte(chunk)) > subscriptionSecretChunkBytes {
			t.Fatalf("UTF-8 chunk exceeds bound: %d", len([]byte(chunk)))
		}
	}
}
''')

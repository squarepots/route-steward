package steward

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenericProfileRoutingRendersDeclaredOrder(t *testing.T) {
	state, route := healthFixture(t, "direct", true)
	context := map[string]any{
		"profile_id":     "explicit",
		"include_routes": []any{route.ID},
		"routing": map[string]any{"rules": []any{
			map[string]any{"match": map[string]any{"type": "domain_suffix", "value": "example.net"}, "action": map[string]any{"type": "direct"}},
			map[string]any{"match": map[string]any{"type": "geosite", "value": "example-category"}, "action": map[string]any{"type": "route", "route": route.ID}},
			map[string]any{"match": map[string]any{"type": "geoip", "value": "US"}, "action": map[string]any{"type": "route", "route": route.ID}},
		}},
	}
	if _, err := AddProfile(state, context); err != nil {
		t.Fatal(err)
	}
	if _, err := AddClientTarget(state, map[string]any{"target_id": "desktop", "profile_id": "explicit", "renderer": "mihomo", "mihomo_process_names": []any{"launcher.exe"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := RenderClients(state, "desktop", true); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(state.PrivateDir, "delivery", "desktop.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	yaml := string(b)
	for _, required := range []string{
		"  - 'DOMAIN-SUFFIX,example.net,DIRECT'\n",
		"  - 'GEOSITE,example-category,RST-Route-route-a'\n",
		"  - 'GEOIP,US,RST-Route-route-a,no-resolve'\n",
		"  - 'PROCESS-NAME,launcher.exe,Applications'\n",
		"  - MATCH,Private Routes\n",
	} {
		if !strings.Contains(yaml, required) {
			t.Fatalf("rendered routing missed %q:\n%s", required, yaml)
		}
	}
	privateRule := strings.Index(yaml, "  - IP-CIDR6,fe80::/10,DIRECT,no-resolve\n")
	processRule := strings.Index(yaml, "  - 'PROCESS-NAME,launcher.exe,Applications'\n")
	domainRule := strings.Index(yaml, "  - 'DOMAIN-SUFFIX,example.net,DIRECT'\n")
	geositeRule := strings.Index(yaml, "  - 'GEOSITE,example-category,RST-Route-route-a'\n")
	geoipRule := strings.Index(yaml, "  - 'GEOIP,US,RST-Route-route-a,no-resolve'\n")
	finalRule := strings.Index(yaml, "  - MATCH,Private Routes\n")
	if !(privateRule < processRule && processRule < domainRule && domainRule < geositeRule && geositeRule < geoipRule && geoipRule < finalRule) {
		t.Fatalf("routing order changed:\n%s", yaml)
	}
	if strings.Count(yaml, "  - MATCH,") != 1 {
		t.Fatalf("generated YAML must have exactly one final MATCH rule:\n%s", yaml)
	}

	sanitized, _ := json.Marshal(SanitizedContext(state.Inventory))
	text := string(sanitized)
	for _, forbidden := range []string{"china_direct", "service_routes", `"policy"`, "launcher.exe"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("sanitized context retained retired/private value %q: %s", forbidden, text)
		}
	}
	for _, required := range []string{`"rules"`, `"domain_suffix"`, `"example.net"`} {
		if !strings.Contains(text, required) {
			t.Fatalf("sanitized context missed %q: %s", required, text)
		}
	}
}

func TestProfileRoutingRejectsInvalidRules(t *testing.T) {
	state, route := healthFixture(t, "direct", false)
	base := func(rule map[string]any) map[string]any {
		return map[string]any{"profile_id": "candidate", "include_routes": []any{"*"}, "routing": map[string]any{"rules": []any{rule}}}
	}
	cases := []map[string]any{
		base(map[string]any{"match": map[string]any{"type": "domain_suffix", "value": "bad,token"}, "action": map[string]any{"type": "direct"}}),
		base(map[string]any{"match": map[string]any{"type": "unknown", "value": "x"}, "action": map[string]any{"type": "direct"}}),
		base(map[string]any{"match": map[string]any{"type": "geosite", "value": "x"}, "action": map[string]any{"type": "route", "route": "missing"}}),
		base(map[string]any{"match": map[string]any{"type": "geoip", "value": "US"}, "action": map[string]any{"type": "direct", "route": route.ID}}),
	}
	for _, context := range cases {
		if _, err := AddProfile(state, context); err == nil {
			t.Fatalf("invalid routing rule was accepted: %#v", context)
		}
		preflight, err := NewPreflight("add-profile", "", state, context, false)
		if err != nil || preflight.Ready || !contains(preflight.Conflicts, "profile-routing-invalid") {
			t.Fatalf("invalid routing rule passed preflight: %#v err=%v", preflight, err)
		}
	}
	policy := map[string]any{"profile_id": "legacy", "policy": "privacy"}
	if _, err := AddProfile(state, policy); err != nil {
		t.Fatalf("legacy policy compatibility input was not translated: %v", err)
	}
	if profile := findProfile(state.Inventory, "legacy"); profile == nil || profile.Routing == nil || len(profile.Routing.Rules) != 0 {
		t.Fatalf("legacy privacy policy did not translate to empty schema-2 routing: %#v", profile)
	}
}

func TestProfileRoutingReferencesFailClosed(t *testing.T) {
	state, route := healthFixture(t, "direct", false)
	makeContext := func(include []any, target string) map[string]any {
		return map[string]any{"profile_id": "candidate", "include_routes": include, "routing": map[string]any{"rules": []any{map[string]any{"match": map[string]any{"type": "geosite", "value": "example-category"}, "action": map[string]any{"type": "route", "route": target}}}}}
	}
	for _, context := range []map[string]any{makeContext([]any{"*"}, "missing-route"), makeContext([]any{}, route.ID)} {
		if _, err := AddProfile(state, context); err == nil {
			t.Fatal("invalid Route reference was accepted")
		}
	}
	findRoute(state.Inventory, route.ID).Enabled = false
	if _, err := AddProfile(state, makeContext([]any{"*"}, route.ID)); err == nil || !strings.Contains(err.Error(), "disabled Route") {
		t.Fatalf("disabled Route was not rejected: %v", err)
	}
}

func TestSchemaOneInventoryUpgradesToGenericRules(t *testing.T) {
	var inv Inventory
	raw := []byte(`{"schema":1,"policies":[{"id":"balanced-cn"}],"profiles":[{"id":"legacy","policy":"balanced-cn"},{"id":"explicit","routing":{"china_direct":true,"service_routes":[{"service":"openai","route":"route-a"}]}}]}`)
	if err := json.Unmarshal(raw, &inv); err != nil {
		t.Fatal(err)
	}
	if inv.Schema != InventorySchema {
		t.Fatalf("legacy inventory upgraded to schema %d, want %d", inv.Schema, InventorySchema)
	}
	if len(inv.Profiles) != 2 {
		t.Fatalf("legacy profiles were lost: %#v", inv.Profiles)
	}
	legacy := effectiveProfileRouting(inv.Profiles[0])
	if len(legacy.Rules) != 3 || legacy.Rules[0].Match.Type != "domain_suffix" || legacy.Rules[0].Action.Type != "direct" {
		t.Fatalf("balanced-cn compatibility was not translated: %#v", legacy)
	}
	explicit := effectiveProfileRouting(inv.Profiles[1])
	if len(explicit.Rules) != 4 || explicit.Rules[0].Match.Type != "geosite" || explicit.Rules[0].Match.Value != "openai" || explicit.Rules[0].Action.Route != "route-a" {
		t.Fatalf("legacy explicit routing was not translated: %#v", explicit)
	}
	encoded, err := json.Marshal(inv)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, retired := range []string{"policies", "policy", "china_direct", "service_routes"} {
		if strings.Contains(text, retired) {
			t.Fatalf("schema-2 inventory retained %q: %s", retired, text)
		}
	}
	if !strings.Contains(text, `"schema":2`) || !strings.Contains(text, `"rules"`) {
		t.Fatalf("schema-2 inventory was not serialized canonically: %s", text)
	}
}

func TestSchemaOneExplicitEmptyRouteSelectionStaysEmpty(t *testing.T) {
	var inv Inventory
	raw := []byte(`{"schema":1,"profiles":[{"id":"empty","include_routes":[],"include_providers":[]}]}`)
	if err := json.Unmarshal(raw, &inv); err != nil {
		t.Fatal(err)
	}
	if len(inv.Profiles) != 1 || inv.Profiles[0].IncludeRoutes == nil || len(inv.Profiles[0].IncludeRoutes) != 0 {
		t.Fatalf("explicit empty route selection changed during upgrade: %#v", inv.Profiles)
	}
}

func TestSchemaTwoRoutingRejectsLegacyOrUnknownFields(t *testing.T) {
	for _, raw := range []string{
		`{"rules":[],"china_direct":true}`,
		`{"rules":[{"match":{"type":"geoip","value":"US","extra":true},"action":{"type":"direct"}}]}`,
	} {
		var routing ProfileRouting
		if err := json.Unmarshal([]byte(raw), &routing); err == nil {
			t.Fatalf("schema-2 routing accepted unknown field: %s", raw)
		}
	}
}

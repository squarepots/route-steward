package steward

import (
	"context"
	"testing"
)

func TestFocusedCapabilitiesReturnOneOperation(t *testing.T) {
	envelope, exit := RunRequest(context.Background(), Request{Command: "capabilities", Operation: "add-server"})
	if exit != 0 || !envelope.Success {
		t.Fatalf("focused capabilities failed: exit=%d envelope=%#v", exit, envelope)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("focused capabilities returned unexpected data: %#v", envelope.Data)
	}
	capability, ok := data["capability"].(Capability)
	if !ok || capability.ID != "add-server" {
		t.Fatalf("focused capabilities returned the wrong operation: %#v", data)
	}
	if _, exists := data["capabilities"]; exists {
		t.Fatal("focused capabilities returned the full operation list")
	}
	if _, exists := data["drivers"]; exists {
		t.Fatal("focused capabilities returned the full driver matrix")
	}

	missing, exit := RunRequest(context.Background(), Request{Command: "capabilities", Operation: "does-not-exist"})
	if exit == 0 || missing.Success || missing.Code != "capability-not-found" {
		t.Fatalf("unknown capability did not fail clearly: exit=%d envelope=%#v", exit, missing)
	}
}

func TestTargetedContextProjectsEachObjectKind(t *testing.T) {
	state, route := healthFixture(t, "relay", false)
	if _, err := AddProvider(state, map[string]any{"provider_id": "optional-a", "url": "https://provider.example.invalid/list.yaml"}); err != nil {
		t.Fatal(err)
	}
	if _, err := AddProfile(state, map[string]any{"profile_id": "primary", "include_routes": []any{route.ID}, "include_providers": []any{"optional-a"}, "routing": map[string]any{"rules": []any{map[string]any{"match": map[string]any{"type": "domain_suffix", "value": "example.invalid"}, "action": map[string]any{"type": "direct"}}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := AddProfile(state, map[string]any{"profile_id": "unrelated", "include_routes": []any{}, "routing": map[string]any{"rules": []any{map[string]any{"match": map[string]any{"type": "domain_suffix", "value": "unrelated.example.invalid"}, "action": map[string]any{"type": "direct"}}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := AddClientTarget(state, map[string]any{"target_id": "desktop", "profile_id": "primary", "renderer": "mihomo"}); err != nil {
		t.Fatal(err)
	}

	serverResult := mustTargetContext(t, state.Inventory, "entry-a", "server")
	assertStringSet(t, serverResult["links"], "link-a")
	assertStringSet(t, serverResult["routes"], route.ID)

	linkResult := mustTargetContext(t, state.Inventory, "link-a", "link")
	assertStringSet(t, linkResult["routes"], route.ID)
	link := linkResult["link"].(map[string]any)
	if link["entry_server"] != "entry-a" || link["exit_server"] != "exit-b" {
		t.Fatalf("link context lost its server relationships: %#v", link)
	}

	routeResult := mustTargetContext(t, state.Inventory, route.ID, "route")
	assertStringSet(t, routeResult["profiles"], "primary")
	assertStringSet(t, routeResult["client_targets"], "desktop")
	if _, exists := routeResult["routing"]; exists {
		t.Fatal("route context expanded profile routing")
	}
	if _, exists := routeResult["counts"]; exists {
		t.Fatal("route context included the full project summary")
	}

	providerResult := mustTargetContext(t, state.Inventory, "optional-a", "provider")
	assertStringSet(t, providerResult["profiles"], "primary")

	profileResult := mustTargetContext(t, state.Inventory, "primary", "profile")
	profile := profileResult["profile"].(map[string]any)
	routing := profile["routing"].(map[string]any)
	rules := routing["rules"].([]map[string]any)
	if len(rules) != 1 {
		t.Fatalf("profile context did not include its own routing intent: %#v", profile)
	}
	assertStringSet(t, profileResult["client_targets"], "desktop")

	clientResult := mustTargetContext(t, state.Inventory, "desktop", "client_target")
	client := clientResult["client_target"].(map[string]any)
	if client["profile"] != "primary" || client["renderer"] != "mihomo" {
		t.Fatalf("client target context lost its direct configuration: %#v", client)
	}
	if _, exists := clientResult["routing"]; exists {
		t.Fatal("client target context expanded profile routing")
	}
}

func TestTargetedContextFailsClosedOnAmbiguousIDs(t *testing.T) {
	state, route := healthFixture(t, "direct", false)
	if _, err := AddProfile(state, map[string]any{"profile_id": route.ID, "include_routes": []any{route.ID}}); err != nil {
		t.Fatal(err)
	}
	result, code := SanitizedTargetContext(state.Inventory, route.ID)
	if code != "context-target-ambiguous" {
		t.Fatalf("ambiguous context returned %q: %#v", code, result)
	}
	assertStringSet(t, result["candidates"], "profile", "route")

	missing, code := SanitizedTargetContext(state.Inventory, "missing")
	if code != "context-target-not-found" || missing["target"] != "missing" {
		t.Fatalf("missing context target did not fail clearly: %q %#v", code, missing)
	}
}

func mustTargetContext(t *testing.T, inv *Inventory, target, kind string) map[string]any {
	t.Helper()
	result, code := SanitizedTargetContext(inv, target)
	if code != "" {
		t.Fatalf("target context %s failed: %s %#v", target, code, result)
	}
	if result["kind"] != kind {
		t.Fatalf("target context %s has kind %#v, want %s", target, result["kind"], kind)
	}
	return result
}

func assertStringSet(t *testing.T, value any, wanted ...string) {
	t.Helper()
	values, ok := value.([]string)
	if !ok {
		t.Fatalf("value has unexpected string-set shape: %#v", value)
	}
	if len(values) != len(wanted) {
		t.Fatalf("string set %#v has length %d, want %#v", values, len(values), wanted)
	}
	for i := range wanted {
		if values[i] != wanted[i] {
			t.Fatalf("string set %#v, want %#v", values, wanted)
		}
	}
}

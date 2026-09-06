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

func TestTargetedContextReturnsOnlyRelevantRelationships(t *testing.T) {
	state, route := healthFixture(t, "direct", false)
	if _, err := AddProfile(state, map[string]any{
		"profile_id":     "primary",
		"include_routes": []any{route.ID},
		"routing": map[string]any{"rules": []any{
			map[string]any{
				"match":  map[string]any{"type": "domain_suffix", "value": "example.invalid"},
				"action": map[string]any{"type": "direct"},
			},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := AddProfile(state, map[string]any{
		"profile_id":     "unrelated",
		"include_routes": []any{},
		"routing": map[string]any{"rules": []any{
			map[string]any{
				"match":  map[string]any{"type": "domain_suffix", "value": "unrelated.example.invalid"},
				"action": map[string]any{"type": "direct"},
			},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := AddClientTarget(state, map[string]any{"target_id": "desktop", "profile_id": "primary", "renderer": "mihomo"}); err != nil {
		t.Fatal(err)
	}

	result, code := SanitizedTargetContext(state.Inventory, route.ID)
	if code != "" {
		t.Fatalf("route context failed: %s %#v", code, result)
	}
	if result["kind"] != "route" {
		t.Fatalf("route context has wrong kind: %#v", result)
	}
	profiles := result["profiles"].([]string)
	if len(profiles) != 1 || profiles[0] != "primary" {
		t.Fatalf("route context exposed the wrong profiles: %#v", profiles)
	}
	targets := result["client_targets"].([]string)
	if len(targets) != 1 || targets[0] != "desktop" {
		t.Fatalf("route context exposed the wrong client targets: %#v", targets)
	}
	if _, exists := result["routing"]; exists {
		t.Fatal("route context expanded profile routing")
	}

	profileResult, code := SanitizedTargetContext(state.Inventory, "primary")
	if code != "" {
		t.Fatalf("profile context failed: %s %#v", code, profileResult)
	}
	profile, ok := profileResult["profile"].(map[string]any)
	if !ok {
		t.Fatalf("profile context has unexpected shape: %#v", profileResult)
	}
	routing := profile["routing"].(map[string]any)
	rules := routing["rules"].([]map[string]any)
	if len(rules) != 1 {
		t.Fatalf("profile context did not include its routing intent: %#v", profile)
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
	candidates := result["candidates"].([]string)
	if len(candidates) != 2 || candidates[0] != "profile" || candidates[1] != "route" {
		t.Fatalf("ambiguous context returned unexpected candidates: %#v", candidates)
	}

	missing, code := SanitizedTargetContext(state.Inventory, "missing")
	if code != "context-target-not-found" || missing["target"] != "missing" {
		t.Fatalf("missing context target did not fail clearly: %q %#v", code, missing)
	}
}

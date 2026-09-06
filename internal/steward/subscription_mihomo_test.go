package steward

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

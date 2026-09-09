package steward

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVerifySubscriptionEndpointAcceptsSupportedLargeBodies(t *testing.T) {
	for _, size := range []int{8192, 8193, subscriptionSecretChunkBytes*subscriptionMaxChunks} {
		t.Run(strings.Repeat("x", min(size, 32)), func(t *testing.T) {
			body := strings.Repeat("x", size)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Cache-Control", "private, no-store")
				w.Header().Set("Content-Type", "text/yaml")
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			if err := verifySubscriptionEndpoint(server.URL, "mihomo", body); err != nil {
				t.Fatalf("supported body size %d was rejected: %v", size, err)
			}
		})
	}
}

func TestVerifySubscriptionEndpointRejectsMismatchedLargeBody(t *testing.T) {
	expected := strings.Repeat("x", 8193)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write([]byte(strings.Repeat("x", 8192) + "y"))
	}))
	defer server.Close()
	if err := verifySubscriptionEndpoint(server.URL, "mihomo", expected); err == nil {
		t.Fatal("mismatched body was accepted")
	}
}

func TestSubscriptionPublicationDriftTracksVerifiedInputs(t *testing.T) {
	state, route := healthFixture(t, "direct", false)
	if _, err := AddProfile(state, map[string]any{
		"profile_id":     "desktop-profile",
		"include_routes": []any{route.ID},
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
	subscription, err := initializeSubscriptionState(state, "desktop", "synthetic-worker", "subscription.example.invalid")
	if err != nil {
		t.Fatal(err)
	}

	items, err := subscriptionPublicationDrift(state)
	if err != nil || len(items) != 1 || items[0]["category"] != "subscription-publication-unknown" {
		t.Fatalf("legacy publication state was not unknown: %#v err=%v", items, err)
	}

	fingerprint, err := subscriptionInputFingerprint(state, "desktop")
	if err != nil {
		t.Fatal(err)
	}
	body, _, err := ExportSubscriptionBody(state, "desktop")
	if err != nil {
		t.Fatal(err)
	}
	now := utcNow()
	subscription.LastPublishedAt = &now
	subscription.PublishedInputFingerprint = fingerprint
	subscription.PublishedBodySHA256 = subscriptionBodySHA256(body)
	if err := writeJSONAtomic(subscription.Path, subscription); err != nil {
		t.Fatal(err)
	}

	items, err = subscriptionPublicationDrift(state)
	if err != nil || len(items) != 1 || items[0]["category"] != "subscription-publication-current" {
		t.Fatalf("verified publication was not current: %#v err=%v", items, err)
	}

	findRoute(state.Inventory, route.ID).DisplayName = "changed-route"
	items, err = subscriptionPublicationDrift(state)
	if err != nil || len(items) != 1 || items[0]["category"] != "subscription-publication-stale" {
		t.Fatalf("changed publication inputs were not stale: %#v err=%v", items, err)
	}
}

func TestSubscriptionArtifactRetryDoesNotRotateTokenAgain(t *testing.T) {
	if retry := subscriptionArtifactRetry(&ClientTarget{Renderer: "mihomo"}); retry != "publish-subscription" {
		t.Fatalf("Mihomo artifact retry would repeat token rotation: %q", retry)
	}
	if retry := subscriptionArtifactRetry(&ClientTarget{Renderer: "shadowrocket"}); retry != "render-client" {
		t.Fatalf("Shadowrocket artifact retry is wrong: %q", retry)
	}
}

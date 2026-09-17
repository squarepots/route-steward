package steward

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVerifySubscriptionEndpointAcceptsSupportedLargeBodies(t *testing.T) {
	for _, size := range []int{8192, 8193, subscriptionSecretChunkBytes * subscriptionMaxChunks} {
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

func TestSubscriptionPublicationDriftSeparatesPublicationAndClientRefresh(t *testing.T) {
	state, route, subscription := subscriptionPublicationFixture(t)

	items, err := subscriptionPublicationDrift(state)
	if err != nil {
		t.Fatal(err)
	}
	if driftCategory(items, "publication") != "subscription-publication-unknown" || driftCategory(items, "client_refresh") != "subscription-client-refresh-stale" {
		t.Fatalf("initial subscription state was not separated correctly: %#v", items)
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
	if _, err := writeSubscriptionReference(state, "desktop", subscription); err != nil {
		t.Fatal(err)
	}

	items, err = subscriptionPublicationDrift(state)
	if err != nil {
		t.Fatal(err)
	}
	if driftCategory(items, "publication") != "subscription-publication-current" || driftCategory(items, "client_refresh") != "subscription-client-refresh-current" {
		t.Fatalf("verified publication and client refresh were not current: %#v", items)
	}

	findRoute(state.Inventory, route.ID).DisplayName = "changed-route"
	items, err = subscriptionPublicationDrift(state)
	if err != nil {
		t.Fatal(err)
	}
	if driftCategory(items, "publication") != "subscription-publication-stale" || driftCategory(items, "client_refresh") != "subscription-client-refresh-current" {
		t.Fatalf("publication drift was not independent from the client reference: %#v", items)
	}
}

func TestPublishSubscriptionRetrySkipsCompletedRemoteMutation(t *testing.T) {
	state, _, _ := subscriptionPublicationFixture(t)
	remoteCurrent := false
	deployCalls := 0
	stateWrites := 0
	deps := defaultSubscriptionDependencies()
	deps.Verify = func(string, string, string) error {
		if remoteCurrent {
			return nil
		}
		return errors.New("remote subscription is not current")
	}
	deps.Deploy = func(string, string, string, string, string) error {
		deployCalls++
		remoteCurrent = true
		return nil
	}
	deps.WriteState = func(path string, value any) error {
		stateWrites++
		if stateWrites == 1 {
			return errors.New("synthetic local state failure")
		}
		return writeJSONAtomic(path, value)
	}

	_, err := publishSubscriptionWith(state, "desktop", nil, deps)
	var staged *operationStageError
	if !errors.As(err, &staged) || staged.StateChanged != "subscription-published-verified" || staged.PublicationState != "current" || staged.ClientRefreshState != "pending" {
		t.Fatalf("verified remote publication was not reported as a partial local failure: %#v err=%v", staged, err)
	}

	result, err := publishSubscriptionWith(state, "desktop", nil, deps)
	if err != nil {
		t.Fatal(err)
	}
	if deployCalls != 1 || result["remote_mutation_performed"] != false {
		t.Fatalf("retry repeated a completed Worker mutation: calls=%d result=%#v", deployCalls, result)
	}
}

func TestRotateSubscriptionTokenRetryReusesPendingToken(t *testing.T) {
	state, _, subscription := subscriptionPublicationFixture(t)
	proposed := strings.Repeat("A", 43)
	remoteToken := subscription.Token
	deployCalls := 0
	newTokenCalls := 0
	stateWrites := 0
	deps := defaultSubscriptionDependencies()
	deps.NewToken = func() (string, error) {
		newTokenCalls++
		return proposed, nil
	}
	deps.Verify = func(endpoint, _ string, _ string) error {
		if strings.HasSuffix(endpoint, "/"+remoteToken) {
			return nil
		}
		return errors.New("token is not active")
	}
	deps.Deploy = func(_, _, token, _, _ string) error {
		deployCalls++
		remoteToken = token
		return nil
	}
	deps.WriteState = func(path string, value any) error {
		stateWrites++
		if stateWrites == 2 {
			return errors.New("synthetic token commit failure")
		}
		return writeJSONAtomic(path, value)
	}

	_, err := rotateSubscriptionTokenWith(state, "desktop", deps)
	var staged *operationStageError
	if !errors.As(err, &staged) || staged.StateChanged != "new-token-active-at-worker" {
		t.Fatalf("active pending token was not reported after commit failure: %#v err=%v", staged, err)
	}
	pending, err := readSubscriptionState(state, "desktop")
	if err != nil || pending.PendingToken == nil || *pending.PendingToken != proposed {
		t.Fatalf("pending token was not preserved for retry: %#v err=%v", pending, err)
	}

	result, err := rotateSubscriptionTokenWith(state, "desktop", deps)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := readSubscriptionState(state, "desktop")
	if err != nil {
		t.Fatal(err)
	}
	if newTokenCalls != 1 || deployCalls != 1 || fresh.Token != proposed || fresh.PendingToken != nil || result["token_rotated"] != true {
		t.Fatalf("rotation retry did not resume the pending token: tokenCalls=%d deployCalls=%d state=%#v result=%#v", newTokenCalls, deployCalls, fresh, result)
	}
}

func TestRotationArtifactFailureRecoversWithoutAnotherRotation(t *testing.T) {
	state, _, subscription := subscriptionPublicationFixture(t)
	proposed := strings.Repeat("B", 43)
	remoteToken := subscription.Token
	deployCalls := 0
	newTokenCalls := 0
	referenceWrites := 0
	deps := defaultSubscriptionDependencies()
	deps.NewToken = func() (string, error) {
		newTokenCalls++
		return proposed, nil
	}
	deps.Verify = func(endpoint, _ string, _ string) error {
		if strings.HasSuffix(endpoint, "/"+remoteToken) {
			return nil
		}
		return errors.New("token is not active")
	}
	deps.Deploy = func(_, _, token, _, _ string) error {
		deployCalls++
		remoteToken = token
		return nil
	}
	deps.WriteReference = func(state *State, targetID string, current *subscriptionState) (*Artifact, error) {
		referenceWrites++
		if referenceWrites == 1 {
			return nil, errors.New("synthetic reference failure")
		}
		return writeSubscriptionReference(state, targetID, current)
	}

	_, err := rotateSubscriptionTokenWith(state, "desktop", deps)
	var staged *operationStageError
	if !errors.As(err, &staged) || staged.StateChanged != "subscription-token-rotated" || staged.Retry != "publish-subscription" {
		t.Fatalf("artifact failure did not expose the non-rotation recovery action: %#v err=%v", staged, err)
	}
	committed, err := readSubscriptionState(state, "desktop")
	if err != nil || committed.Token != proposed || committed.PendingToken != nil {
		t.Fatalf("rotation was not committed before the artifact failure: %#v err=%v", committed, err)
	}

	result, err := publishSubscriptionWith(state, "desktop", nil, deps)
	if err != nil {
		t.Fatal(err)
	}
	if newTokenCalls != 1 || deployCalls != 1 || referenceWrites != 2 || result["remote_mutation_performed"] != false {
		t.Fatalf("artifact recovery repeated credential or remote work: tokenCalls=%d deployCalls=%d referenceWrites=%d result=%#v", newTokenCalls, deployCalls, referenceWrites, result)
	}
}

func subscriptionPublicationFixture(t *testing.T) (*State, Route, *subscriptionState) {
	t.Helper()
	state, route := healthFixture(t, "direct", false)
	if _, err := AddProfile(state, map[string]any{"profile_id": "desktop-profile", "include_routes": []any{route.ID}}); err != nil {
		t.Fatal(err)
	}
	if _, err := AddClientTarget(state, map[string]any{"target_id": "desktop", "profile_id": "desktop-profile", "renderer": "mihomo"}); err != nil {
		t.Fatal(err)
	}
	subscription, err := initializeSubscriptionState(state, "desktop", "synthetic-worker", "subscription.example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	return state, route, subscription
}

func driftCategory(items []map[string]any, domain string) string {
	for _, item := range items {
		if item["state_domain"] == domain {
			value, _ := item["category"].(string)
			return value
		}
	}
	return ""
}

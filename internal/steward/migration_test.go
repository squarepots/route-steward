package steward

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMigrationCreatesHealthyReplacementBeforeClientSwitch(t *testing.T) {
	state, source, input := migrationFixture(t, "direct", false)
	input["replacement_server_id"] = "Replacement-C"
	preflight, err := NewPreflight("migrate-route", source.ID, state, input, false)
	if err != nil || !preflight.Ready || preflight.Executor != "workflow" {
		t.Fatalf("valid migration preflight failed: %#v err=%v", preflight, err)
	}
	deps := migrationTestDependencies(nil, "healthy")
	result, err := migrateRouteWith(context.Background(), state, source.ID, input, deps)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "complete" || result.Phase != "complete" || result.OldCapacityRetired || !result.RetirementRequiresAction {
		t.Fatalf("successful migration result is incomplete: %#v", result)
	}
	oldRoute, replacement := findRoute(state.Inventory, source.ID), findRoute(state.Inventory, result.ReplacementRoute)
	if oldRoute == nil || oldRoute.Enabled || replacement == nil || !replacement.Enabled || replacement.State != "deployed" {
		t.Fatalf("migration did not switch desired client selection safely: old=%#v replacement=%#v", oldRoute, replacement)
	}
	if len(state.Inventory.Servers) != 2 || len(state.Inventory.Routes) != 2 {
		t.Fatal("migration created duplicate desired capacity")
	}
	artifact, err := os.ReadFile(filepath.Join(state.Inventory.Delivery.Directory, "desktop.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(artifact), "192.0.2.10") || !strings.Contains(string(artifact), "203.0.113.30") {
		t.Fatal("client output was not switched from the old Route to the proven replacement")
	}
	status, err := MigrationStatus(state, source.DisplayName)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(status)
	if !strings.Contains(string(encoded), `"status":"complete"`) || strings.Contains(string(encoded), "203.0.113.30") || strings.Contains(string(encoded), state.Inventory.Servers[0].SSH.KeyPath) {
		t.Fatalf("sanitized migration status is incomplete or leaked private context: %s", encoded)
	}
	checkpoint, err := os.ReadFile(filepath.Join(state.PrivateDir, migrationStateFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(checkpoint), "api_token") || strings.Contains(string(checkpoint), "must-not-be-persisted") {
		t.Fatal("migration checkpoint persisted an unsupported caller-supplied field")
	}
	second, err := migrateRouteWith(context.Background(), state, source.ID, input, migrationDependencies{
		Deploy: func(context.Context, *State, string) (map[string]any, error) {
			t.Fatal("completed retry redeployed")
			return nil, nil
		},
		Health: func(context.Context, *State, string, bool) (HealthResult, error) {
			t.Fatal("completed retry rechecked health")
			return HealthResult{}, nil
		},
		Render: deps.Render, Publish: deps.Publish, Now: deps.Now,
	})
	if err != nil || second.Status != "complete" || len(state.Inventory.Routes) != 2 {
		t.Fatalf("completed migration was not idempotent: %#v err=%v", second, err)
	}
}

func TestMigrationPreservesPortHoppingClientContract(t *testing.T) {
	state, source, input := migrationFixture(t, "direct", false)
	current := findRoute(state.Inventory, source.ID)
	current.ListenPort = 20000
	current.PortHopping = &PortHopping{StartPort: 20000, EndPort: 20003}
	entry := findServer(state.Inventory, current.EntryServer)
	for i := range entry.Firewall.Rules {
		if entry.Firewall.Rules[i].Protocol == "udp" && entry.Firewall.Rules[i].Port == 443 {
			entry.Firewall.Rules[i].Port, entry.Firewall.Rules[i].EndPort = 20000, 20003
		}
	}
	if err := state.Save(false); err != nil {
		t.Fatal(err)
	}
	result, err := migrateRouteWith(context.Background(), state, source.ID, input, migrationTestDependencies(nil, "healthy"))
	if err != nil || result.Status != "complete" {
		t.Fatalf("port-hopping migration did not complete: %#v err=%v", result, err)
	}
	replacement := findRoute(state.Inventory, result.ReplacementRoute)
	if replacement == nil || !samePortHopping(replacement.PortHopping, &PortHopping{StartPort: 20000, EndPort: 20003}) || !samePortHopping(result.PortHopping, replacement.PortHopping) {
		t.Fatalf("replacement did not retain the source port-hopping contract: route=%#v result=%#v", replacement, result)
	}
	checkpoint, err := readMigrationStateFile(filepath.Join(state.PrivateDir, migrationStateFile))
	if err != nil || len(checkpoint.Transactions) != 1 || !samePortHopping(checkpoint.Transactions[0].PortHopping, replacement.PortHopping) {
		t.Fatalf("migration checkpoint did not preserve port hopping: checkpoint=%#v err=%v", checkpoint, err)
	}
	artifact, err := os.ReadFile(filepath.Join(state.Inventory.Delivery.Directory, "desktop.yaml"))
	if err != nil || !strings.Contains(string(artifact), "ports: '20000-20003'") {
		t.Fatalf("migrated client artifact omitted port hopping: err=%v artifact=%q", err, artifact)
	}
}

func TestMigrationRetriesDeploymentWithoutDuplicatingState(t *testing.T) {
	state, source, input := migrationFixture(t, "direct", false)
	failed := migrationTestDependencies(errors.New("synthetic SSH failure"), "healthy")
	result, err := migrateRouteWith(context.Background(), state, source.ID, input, failed)
	if err != nil || result.Status != "blocked" || result.Phase != "replacement-prepared" || result.LastFailure != "replacement-deployment-failed" {
		t.Fatalf("deployment failure was not checkpointed safely: %#v err=%v", result, err)
	}
	if !findRoute(state.Inventory, source.ID).Enabled || findRoute(state.Inventory, result.ReplacementRoute).Enabled {
		t.Fatal("failed deployment changed the active client Route")
	}
	retried, err := migrateRouteWith(context.Background(), state, source.ID, input, migrationTestDependencies(nil, "healthy"))
	if err != nil || retried.Status != "complete" || len(state.Inventory.Servers) != 2 || len(state.Inventory.Routes) != 2 {
		t.Fatalf("deployment retry was not deterministic: %#v err=%v", retried, err)
	}
}

func TestMigrationFailsClosedWhenPreparedTopologyChanges(t *testing.T) {
	state, source, input := migrationFixture(t, "direct", false)
	result, err := migrateRouteWith(context.Background(), state, source.ID, input, migrationTestDependencies(errors.New("synthetic SSH failure"), "healthy"))
	if err != nil || result.Phase != "replacement-prepared" {
		t.Fatalf("fixture migration did not reach prepared state: %#v err=%v", result, err)
	}
	findRoute(state.Inventory, result.ReplacementRoute).ListenPort++
	if err := state.Save(false); err != nil {
		t.Fatal(err)
	}
	deployCalls := 0
	deps := migrationTestDependencies(nil, "healthy")
	deps.Deploy = func(context.Context, *State, string) (map[string]any, error) {
		deployCalls++
		return nil, nil
	}
	blocked, err := migrateRouteWith(context.Background(), state, source.ID, input, deps)
	if err != nil || blocked.Status != "blocked" || blocked.LastFailure != "migration-state-conflict" || deployCalls != 0 {
		t.Fatalf("changed prepared topology was not rejected before deployment: %#v calls=%d err=%v", blocked, deployCalls, err)
	}
}

func TestMigrationRequiresHealthyTrafficBeforeSwitch(t *testing.T) {
	state, source, input := migrationFixture(t, "direct", false)
	result, err := migrateRouteWith(context.Background(), state, source.ID, input, migrationTestDependencies(nil, "unhealthy"))
	if err != nil || result.Status != "blocked" || result.Phase != "replacement-deployed" || result.LastFailure != "replacement-health-not-healthy" {
		t.Fatalf("unhealthy replacement was not blocked: %#v err=%v", result, err)
	}
	if !findRoute(state.Inventory, source.ID).Enabled {
		t.Fatal("unhealthy replacement disabled the old Route")
	}
	if _, err := os.Stat(filepath.Join(state.Inventory.Delivery.Directory, "desktop.yaml")); !os.IsNotExist(err) {
		t.Fatal("client output changed before replacement traffic was proven healthy")
	}
	retried, err := migrateRouteWith(context.Background(), state, source.ID, input, migrationTestDependencies(nil, "healthy"))
	if err != nil || retried.Status != "complete" {
		t.Fatalf("healthy retry did not finish migration: %#v err=%v", retried, err)
	}
}

func TestMigrationRollsBackFailedRenderAndRechecksHealthOnRetry(t *testing.T) {
	state, source, input := migrationFixture(t, "direct", false)
	deps := migrationTestDependencies(nil, "healthy")
	renderCalls := 0
	deps.Render = func(state *State, target string, skip bool) (RenderResult, error) {
		renderCalls++
		if renderCalls == 1 {
			return RenderResult{}, errors.New("synthetic render failure")
		}
		return RenderClients(state, target, skip)
	}
	result, err := migrateRouteWith(context.Background(), state, source.ID, input, deps)
	if err != nil || result.Status != "blocked" || result.Phase != "replacement-deployed" || result.LastFailure != "client-render-failed" {
		t.Fatalf("render failure was not rolled back: %#v err=%v", result, err)
	}
	if !findRoute(state.Inventory, source.ID).Enabled || findRoute(state.Inventory, result.ReplacementRoute).Enabled {
		t.Fatal("render rollback did not restore old desired selection")
	}
	healthCalls := 0
	retry := migrationTestDependencies(nil, "healthy")
	originalHealth := retry.Health
	retry.Health = func(ctx context.Context, state *State, route string, include bool) (HealthResult, error) {
		healthCalls++
		return originalHealth(ctx, state, route, include)
	}
	completed, err := migrateRouteWith(context.Background(), state, source.ID, input, retry)
	if err != nil || completed.Status != "complete" || healthCalls != 1 {
		t.Fatalf("retry did not recheck health before switching: %#v health=%d err=%v", completed, healthCalls, err)
	}
}

func TestMigrationSwitchesAndRollsBackHeadlessRouteSelection(t *testing.T) {
	state, source, input := migrationFixture(t, "direct", false)
	if _, err := AddClientTarget(state, map[string]any{"target_id": "backend", "profile_id": "primary", "renderer": "hysteria2", "route_id": source.ID, "listen": "127.0.0.1:18080"}); err != nil {
		t.Fatal(err)
	}
	deps := migrationTestDependencies(nil, "healthy")
	renderCalls := 0
	deps.Render = func(state *State, target string, skip bool) (RenderResult, error) {
		renderCalls++
		if renderCalls == 1 {
			return RenderResult{}, errors.New("synthetic headless render failure")
		}
		return RenderClients(state, target, skip)
	}
	blocked, err := migrateRouteWith(context.Background(), state, source.ID, input, deps)
	if err != nil || blocked.Status != "blocked" || findClientTarget(state.Inventory, "backend").Route != source.ID {
		t.Fatalf("failed headless switch did not restore its explicit Route: %#v err=%v", blocked, err)
	}
	completed, err := migrateRouteWith(context.Background(), state, source.ID, input, migrationTestDependencies(nil, "healthy"))
	headless := findClientTarget(state.Inventory, "backend")
	if err != nil || completed.Status != "complete" || headless == nil || headless.Route != completed.ReplacementRoute {
		t.Fatalf("headless target did not switch to the healthy replacement: %#v target=%#v err=%v", completed, headless, err)
	}
	artifact, err := os.ReadFile(filepath.Join(state.Inventory.Delivery.Directory, "backend.json"))
	if err != nil || strings.Contains(string(artifact), "192.0.2.10") || !strings.Contains(string(artifact), "203.0.113.30") {
		t.Fatalf("headless artifact did not follow the replacement Route: err=%v", err)
	}
}

func TestMigrationUpdatesExplicitProfileRoutingRules(t *testing.T) {
	state, source, input := migrationFixture(t, "direct", false)
	profile := findProfile(state.Inventory, "primary")
	profile.Routing = &ProfileRouting{Rules: []ProfileRoutingRule{{Match: ProfileRoutingMatch{Type: "geosite", Value: "example-category"}, Action: ProfileRoutingAction{Type: "route", Route: source.ID}}}}
	if err := state.Save(false); err != nil {
		t.Fatal(err)
	}
	result, err := migrateRouteWith(context.Background(), state, source.ID, input, migrationTestDependencies(nil, "healthy"))
	if err != nil || result.Status != "complete" {
		t.Fatalf("migration did not complete: %#v err=%v", result, err)
	}
	profile = findProfile(state.Inventory, "primary")
	if profile == nil || profile.Routing == nil || len(profile.Routing.Rules) != 1 || profile.Routing.Rules[0].Action.Route != result.ReplacementRoute {
		t.Fatalf("explicit routing rule did not follow replacement Route: %#v", profile)
	}
	artifact, err := os.ReadFile(filepath.Join(state.Inventory.Delivery.Directory, "desktop.yaml"))
	if err != nil || !strings.Contains(string(artifact), "'GEOSITE,example-category,RST-Route-"+result.ReplacementRoute+"'") {
		t.Fatalf("migrated artifact did not use the replacement route selector: err=%v artifact=%s", err, artifact)
	}
}

func TestMigrationRollsBackFailedSubscriptionPublication(t *testing.T) {
	state, source, input := migrationFixture(t, "direct", true)
	deps := migrationTestDependencies(nil, "healthy")
	publishCalls := 0
	deps.Publish = func(*State, string, map[string]any) (map[string]any, error) {
		publishCalls++
		if publishCalls == 1 {
			return nil, errors.New("synthetic publication failure")
		}
		return map[string]any{"published": true}, nil
	}
	result, err := migrateRouteWith(context.Background(), state, source.ID, input, deps)
	if err != nil || result.Status != "blocked" || result.Phase != "replacement-deployed" || result.LastFailure != "subscription-publication-failed" {
		t.Fatalf("publication failure was not rolled back: %#v err=%v", result, err)
	}
	if publishCalls != 2 || !findRoute(state.Inventory, source.ID).Enabled || findRoute(state.Inventory, result.ReplacementRoute).Enabled {
		t.Fatalf("publication rollback did not restore the old output: calls=%d", publishCalls)
	}
	completed, err := migrateRouteWith(context.Background(), state, source.ID, input, migrationTestDependencies(nil, "healthy"))
	if err != nil || completed.Status != "complete" {
		t.Fatalf("publication retry did not complete: %#v err=%v", completed, err)
	}
}

func TestMigrationRecordsRollbackPendingUntilPublicationCanBeRestored(t *testing.T) {
	state, source, input := migrationFixture(t, "direct", true)
	deps := migrationTestDependencies(nil, "healthy")
	deps.Publish = func(*State, string, map[string]any) (map[string]any, error) {
		return nil, errors.New("synthetic publication outage")
	}
	result, err := migrateRouteWith(context.Background(), state, source.ID, input, deps)
	if err != nil || result.Phase != "rollback-pending" || result.LastFailure != "client-switch-rollback-failed" {
		t.Fatalf("unconfirmed publication rollback was not recorded: %#v err=%v", result, err)
	}
	if result.Working["old_route"] != "desired-selection-restored-output-unconfirmed" || !findRoute(state.Inventory, source.ID).Enabled || findRoute(state.Inventory, result.ReplacementRoute).Enabled {
		t.Fatalf("rollback-pending result overstated client state: %#v", result)
	}
	completed, err := migrateRouteWith(context.Background(), state, source.ID, input, migrationTestDependencies(nil, "healthy"))
	if err != nil || completed.Status != "complete" {
		t.Fatalf("rollback-pending retry did not restore and resume deterministically: %#v err=%v", completed, err)
	}
}

func TestMigrationRecoversAnInterruptedClientSwitch(t *testing.T) {
	state, source, input := migrationFixture(t, "direct", false)
	result, err := migrateRouteWith(context.Background(), state, source.ID, input, migrationTestDependencies(nil, "unhealthy"))
	if err != nil || result.Phase != "replacement-deployed" {
		t.Fatalf("fixture migration did not reach deployed state: %#v err=%v", result, err)
	}
	store, err := readMigrationState(state.PrivateDir)
	if err != nil {
		t.Fatal(err)
	}
	txn := findMigration(store, source.ID)
	txn.Phase = "switching"
	if err := saveMigrationState(state.PrivateDir, store, txn, migrationTestDependencies(nil, "healthy").Now); err != nil {
		t.Fatal(err)
	}
	if err := setMigrationSelection(state, txn, false); err != nil {
		t.Fatal(err)
	}
	completed, err := migrateRouteWith(context.Background(), state, source.ID, input, migrationTestDependencies(nil, "healthy"))
	if err != nil || completed.Status != "complete" || findRoute(state.Inventory, source.ID).Enabled || !findRoute(state.Inventory, completed.ReplacementRoute).Enabled {
		t.Fatalf("interrupted switch was not rolled back and resumed: %#v err=%v", completed, err)
	}
}

func TestRelayMigrationCreatesMatchingReplacementLink(t *testing.T) {
	state, source, input := migrationFixture(t, "relay", false)
	result, err := migrateRouteWith(context.Background(), state, source.ID, input, migrationTestDependencies(nil, "healthy"))
	if err != nil || result.Status != "complete" || result.ReplacementLink == nil {
		t.Fatalf("relay migration failed: %#v err=%v", result, err)
	}
	link := findLink(state.Inventory, *result.ReplacementLink)
	replacement := findRoute(state.Inventory, result.ReplacementRoute)
	if link == nil || replacement == nil || link.EntryServer != "replacement-c" || link.ExitServer != source.ExitServer || deref(replacement.Link) != link.ID {
		t.Fatalf("relay replacement path is incorrect: link=%#v route=%#v", link, replacement)
	}
}

func TestRelayExitMigrationAllocatesNonOverlappingPortHoppingRange(t *testing.T) {
	state, source, input := migrationFixture(t, "relay", false)
	current := findRoute(state.Inventory, source.ID)
	oldPort := current.ListenPort
	current.ListenPort = 20000
	current.PortHopping = &PortHopping{StartPort: 20000, EndPort: 20003}
	entry := findServer(state.Inventory, current.EntryServer)
	for i := range entry.Firewall.Rules {
		if entry.Firewall.Rules[i].Protocol == "udp" && entry.Firewall.Rules[i].Port == oldPort {
			entry.Firewall.Rules[i].Port, entry.Firewall.Rules[i].EndPort = 20000, 20003
		}
	}
	if err := state.Save(false); err != nil {
		t.Fatal(err)
	}
	input["replace_server_id"] = current.ExitServer
	result, err := migrateRouteWith(context.Background(), state, source.ID, input, migrationTestDependencies(nil, "healthy"))
	if err != nil || result.Status != "complete" {
		t.Fatalf("relay exit port-hopping migration did not complete: %#v err=%v", result, err)
	}
	replacement := findRoute(state.Inventory, result.ReplacementRoute)
	expected := &PortHopping{StartPort: 20004, EndPort: 20007}
	if replacement == nil || !samePortHopping(replacement.PortHopping, expected) || !samePortHopping(result.PortHopping, expected) {
		t.Fatalf("relay exit migration did not reserve a same-width non-overlapping range: route=%#v result=%#v", replacement, result)
	}
	artifact, err := os.ReadFile(filepath.Join(state.Inventory.Delivery.Directory, "desktop.yaml"))
	if err != nil || !strings.Contains(string(artifact), "ports: '20004-20007'") {
		t.Fatalf("relay exit migration did not publish its replacement range: err=%v artifact=%q", err, artifact)
	}
}

func migrationFixture(t *testing.T, kind string, subscription bool) (*State, Route, map[string]any) {
	t.Helper()
	state, source := healthFixture(t, kind, false)
	if _, err := AddProfile(state, map[string]any{"profile_id": "primary", "policy": "privacy", "include_routes": []any{source.ID}}); err != nil {
		t.Fatal(err)
	}
	renderer, delivery := "mihomo", ""
	if subscription {
		renderer, delivery = "shadowrocket", "nodes"
	}
	context := map[string]any{"target_id": "desktop", "profile_id": "primary", "renderer": renderer}
	if delivery != "" {
		context["delivery"] = delivery
	}
	if _, err := AddClientTarget(state, context); err != nil {
		t.Fatal(err)
	}
	if subscription {
		if _, err := initializeSubscriptionState(state, "desktop", "synthetic-worker", "subscription.example.invalid"); err != nil {
			t.Fatal(err)
		}
	}
	key := state.Inventory.Servers[0].SSH.KeyPath
	input := map[string]any{
		"replacement_server_id": "replacement-c",
		"reason":                "server-failure",
		"replacement_server": map[string]any{
			"public_ipv4": "203.0.113.30", "ssh_user": "ubuntu", "ssh_key_path": key, "host_ownership": "dedicated", "region": "replacement-region", "api_token": "must-not-be-persisted",
		},
	}
	if kind == "relay" {
		input["replace_server_id"] = source.EntryServer
	}
	return state, source, input
}

func migrationTestDependencies(deployFailure error, healthStatus string) migrationDependencies {
	return migrationDependencies{
		Deploy: func(_ context.Context, state *State, routeID string) (map[string]any, error) {
			if deployFailure != nil {
				return nil, deployFailure
			}
			candidate := cloneInventory(state.Inventory)
			route := findRoute(candidate, routeID)
			if route == nil {
				return nil, errors.New("replacement Route missing")
			}
			route.Enabled, route.State = true, "deployed"
			if err := saveCandidate(state, candidate, false); err != nil {
				return nil, err
			}
			return map[string]any{"route": routeID, "state": "deployed"}, nil
		},
		Health: func(_ context.Context, _ *State, routeID string, _ bool) (HealthResult, error) {
			return HealthResult{SchemaVersion: 1, Route: routeID, Status: healthStatus}, nil
		},
		// Migration unit tests exercise desired-state switching; the pinned-core
		// compatibility suite owns runtime config validation separately.
		Render: func(state *State, target string, _ bool) (RenderResult, error) {
			return RenderClients(state, target, true)
		},
		Publish: func(*State, string, map[string]any) (map[string]any, error) {
			return map[string]any{"published": true}, nil
		},
		Now: func() time.Time { return time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC) },
	}
}

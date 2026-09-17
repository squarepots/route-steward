package steward

import (
	"context"
	"errors"
	"testing"
)

func TestDeploymentAuditDecisionUsesVerifiedRemoteState(t *testing.T) {
	route := &Route{State: "pending"}

	decision, err := deploymentAuditDecision(route, AuditEvidence{Category: "in-sync"})
	if err != nil || decision != "deploy" {
		t.Fatalf("fresh pending Route should still perform its initial deployment: decision=%q err=%v", decision, err)
	}

	route.State = "deploying"
	decision, err = deploymentAuditDecision(route, AuditEvidence{Category: "in-sync"})
	if err != nil || decision != "adopt" {
		t.Fatalf("verified in-sync retry should be adopted without remote mutation: decision=%q err=%v", decision, err)
	}

	route.State = "deployed"
	decision, err = deploymentAuditDecision(route, AuditEvidence{Category: "in-sync"})
	if err != nil || decision != "adopt" {
		t.Fatalf("already deployed in-sync Route should be adopted without remote mutation: decision=%q err=%v", decision, err)
	}

	route.State = "pending"
	decision, err = deploymentAuditDecision(route, AuditEvidence{Category: "service-missing"})
	if err != nil || decision != "deploy" {
		t.Fatalf("known missing service should allow deployment: decision=%q err=%v", decision, err)
	}

	if decision, err = deploymentAuditDecision(route, AuditEvidence{Category: "undetermined"}); err == nil || decision != "" {
		t.Fatalf("undetermined remote state should block mutation: decision=%q err=%v", decision, err)
	}

	route.State = "deployed"
	if decision, err = deploymentAuditDecision(route, AuditEvidence{Category: "service-missing"}); err == nil || decision != "" {
		t.Fatalf("deployed Route with missing service should block mutation: decision=%q err=%v", decision, err)
	}
}

func TestAdoptVerifiedRouteRestoresLocalState(t *testing.T) {
	state, route := healthFixture(t, "direct", false)
	current := findRoute(state.Inventory, route.ID)
	current.Enabled = false
	current.State = "deploying"
	if err := state.Save(false); err != nil {
		t.Fatal(err)
	}

	evidence := AuditEvidence{
		Route:                     route.ID,
		Status:                    "healthy",
		Category:                  "in-sync",
		ActualEgressIPv4:          stringPointer(findServer(state.Inventory, route.ExitServer).Network.ExpectedEgressIPv4),
		EgressMatchesDeclaredExit: true,
	}
	result, err := adoptVerifiedRoute(state, current, evidence, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if result["state"] != "deployed" || result["enabled"] != true {
		t.Fatalf("verified Route was not adopted: %#v", result)
	}
	reloaded, err := LoadState(state.PrivateDir)
	if err != nil {
		t.Fatal(err)
	}
	adopted := findRoute(reloaded.Inventory, route.ID)
	if adopted == nil || adopted.State != "deployed" || !adopted.Enabled {
		t.Fatalf("verified Route was not persisted locally: %#v", adopted)
	}
}

func TestMarkRouteDeployingPersistsRetryCheckpoint(t *testing.T) {
	state, route := healthFixture(t, "direct", false)
	current := findRoute(state.Inventory, route.ID)
	current.Enabled = false
	current.State = "pending"
	if err := state.Save(false); err != nil {
		t.Fatal(err)
	}
	if err := markRouteDeploying(state, route.ID); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadState(state.PrivateDir)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := findRoute(reloaded.Inventory, route.ID)
	if checkpoint == nil || checkpoint.State != "deploying" || checkpoint.Enabled {
		t.Fatalf("deployment retry checkpoint was not persisted: %#v", checkpoint)
	}
}

func TestMigrationRetriesAfterUndeterminedDeploymentWithoutDuplicatingState(t *testing.T) {
	state, source, input := migrationFixture(t, "direct", false)
	deps := migrationTestDependencies(nil, "healthy")
	originalDeploy := deps.Deploy
	calls := 0
	deps.Deploy = func(ctx context.Context, state *State, routeID string) (map[string]any, error) {
		calls++
		if calls == 1 {
			return nil, &operationStageError{
				Stage:        "remote-deployment",
				StateChanged: "remote-state-undetermined",
				Retry:        "deploy-route",
				Err:          errors.New("synthetic interrupted deployment"),
			}
		}
		return originalDeploy(ctx, state, routeID)
	}

	blocked, err := migrateRouteWith(context.Background(), state, source.ID, input, deps)
	if err != nil || blocked.Status != "blocked" || blocked.Phase != "replacement-prepared" || blocked.LastFailure != "replacement-deployment-failed" {
		t.Fatalf("partial deployment was not checkpointed for retry: %#v err=%v", blocked, err)
	}
	completed, err := migrateRouteWith(context.Background(), state, source.ID, input, deps)
	if err != nil || completed.Status != "complete" || calls != 2 {
		t.Fatalf("migration did not resume through the deployment contract: %#v calls=%d err=%v", completed, calls, err)
	}
	if len(state.Inventory.Routes) != 2 || len(state.Inventory.Servers) != 2 {
		t.Fatalf("migration retry duplicated desired state: routes=%d servers=%d", len(state.Inventory.Routes), len(state.Inventory.Servers))
	}
}

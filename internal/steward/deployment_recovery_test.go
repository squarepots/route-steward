package steward

import (
	"context"
	"errors"
	"testing"
)

func TestDeploymentAuditAllowsOnlyKnownRemoteStates(t *testing.T) {
	route := &Route{State: "pending"}
	if err := deploymentAuditAllowsMutation(route, AuditEvidence{Category: "service-missing"}); err != nil {
		t.Fatalf("fresh pending Route rejected service-missing state: %v", err)
	}
	if err := deploymentAuditAllowsMutation(route, AuditEvidence{Category: "in-sync"}); err != nil {
		t.Fatalf("pending Route rejected verified in-sync state: %v", err)
	}
	if err := deploymentAuditAllowsMutation(route, AuditEvidence{Category: "undetermined"}); err == nil {
		t.Fatal("pending Route accepted undetermined remote state")
	}

	route.State = "deploying"
	if err := deploymentAuditAllowsMutation(route, AuditEvidence{Category: "service-missing"}); err != nil {
		t.Fatalf("retry checkpoint rejected known missing service: %v", err)
	}

	route.State = "deployed"
	if err := deploymentAuditAllowsMutation(route, AuditEvidence{Category: "service-missing"}); err == nil {
		t.Fatal("deployed Route accepted a missing remote service")
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

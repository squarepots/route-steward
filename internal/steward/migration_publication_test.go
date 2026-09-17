package steward

import (
	"context"
	"errors"
	"testing"
)

func TestMigrationPublicationProgressDistinguishesFailureStages(t *testing.T) {
	cases := []struct {
		name       string
		publishErr error
		wantState  string
		wantStage  string
	}{
		{name: "before external mutation", publishErr: errors.New("synthetic pre-publication failure"), wantState: "intent"},
		{name: "after external mutation", publishErr: &operationStageError{Stage: "subscription-verification", StateChanged: "subscription-published-unverified", Retry: "publish-subscription", Err: errors.New("synthetic verification failure")}, wantState: "published-unverified", wantStage: "subscription-verification"},
		{name: "after verified publication", publishErr: &operationStageError{Stage: "client-import-artifact", StateChanged: "subscription-published-verified", Retry: "render-client", Err: errors.New("synthetic client refresh failure")}, wantState: "verified", wantStage: "client-import-artifact"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state, source, input := migrationFixture(t, "direct", true)
			store, txn := preparedMigrationSwitch(t, state, source, input)
			deps := migrationTestDependencies(nil, "healthy")
			deps.Publish = func(*State, string, map[string]any) (map[string]any, error) {
				return nil, tc.publishErr
			}
			if _, err := switchMigrationClients(state, store, txn, deps); err == nil {
				t.Fatal("publication failure unexpectedly succeeded")
			}
			progress := migrationPublicationFor(txn, "desktop")
			if progress == nil || progress.Direction != "forward" || progress.State != tc.wantState || progress.FailureStage != tc.wantStage || progress.InputFingerprint == "" {
				t.Fatalf("publication failure stage was not checkpointed: %#v", progress)
			}
		})
	}
}

func TestMigrationPersistsRollbackIntentBeforePublicationRollback(t *testing.T) {
	state, source, input := migrationFixture(t, "direct", true)
	deps := migrationTestDependencies(nil, "healthy")
	publishCalls := 0
	observedRollbackPending := false
	deps.Publish = func(state *State, _ string, _ map[string]any) (map[string]any, error) {
		publishCalls++
		if publishCalls == 1 {
			return nil, &operationStageError{Stage: "client-import-artifact", StateChanged: "subscription-published-verified", Retry: "render-client", Err: errors.New("synthetic forward failure")}
		}
		store, err := readMigrationState(state.PrivateDir)
		if err != nil {
			return nil, err
		}
		txn := findMigration(store, source.ID)
		observedRollbackPending = txn != nil && txn.Phase == "rollback-pending"
		return nil, errors.New("synthetic rollback interruption")
	}
	result, err := migrateRouteWith(context.Background(), state, source.ID, input, deps)
	if err != nil || result.Phase != "rollback-pending" || result.LastFailure != "client-switch-rollback-failed" {
		t.Fatalf("interrupted rollback was not checkpointed: %#v err=%v", result, err)
	}
	if !observedRollbackPending {
		t.Fatal("rollback publication began before rollback-pending was durable")
	}
}

func TestMigrationRollbackResumesAtIncompletePublicationTarget(t *testing.T) {
	state, source, input := migrationFixture(t, "direct", true)
	if _, err := AddClientTarget(state, map[string]any{"target_id": "tablet", "profile_id": "primary", "renderer": "shadowrocket", "delivery": "nodes"}); err != nil {
		t.Fatal(err)
	}
	if _, err := initializeSubscriptionState(state, "tablet", "synthetic-worker-tablet", "tablet.example.invalid"); err != nil {
		t.Fatal(err)
	}
	store, txn := preparedMigrationSwitch(t, state, source, input)
	if err := setMigrationSelection(state, txn, false); err != nil {
		t.Fatal(err)
	}
	for _, targetID := range txn.AffectedClientTargets {
		fingerprint, err := subscriptionInputFingerprint(state, targetID)
		if err != nil {
			t.Fatal(err)
		}
		setMigrationPublicationProgress(txn, targetID, "forward", fingerprint, "complete", "")
	}
	txn.Phase = "rollback-pending"
	if err := saveMigrationState(state.PrivateDir, store, txn, migrationTestDependencies(nil, "healthy").Now); err != nil {
		t.Fatal(err)
	}

	deps := migrationTestDependencies(nil, "healthy")
	firstCalls := []string{}
	deps.Publish = func(_ *State, targetID string, _ map[string]any) (map[string]any, error) {
		firstCalls = append(firstCalls, targetID)
		if targetID == "tablet" {
			return nil, errors.New("synthetic process interruption")
		}
		return map[string]any{"published": true}, nil
	}
	if err := rollbackMigrationSwitch(state, store, txn, deps); err == nil {
		t.Fatal("rollback interruption unexpectedly succeeded")
	}
	if len(firstCalls) != 2 || migrationPublicationFor(txn, "desktop").State != "complete" || migrationPublicationFor(txn, "desktop").Direction != "rollback" || migrationPublicationFor(txn, "tablet").State != "intent" {
		t.Fatalf("first rollback did not persist per-target progress: calls=%v progress=%#v", firstCalls, txn.PublicationProgress)
	}

	reloaded, err := readMigrationState(state.PrivateDir)
	if err != nil {
		t.Fatal(err)
	}
	reloadedTxn := findMigration(reloaded, source.ID)
	secondCalls := []string{}
	retry := migrationTestDependencies(nil, "healthy")
	retry.Publish = func(_ *State, targetID string, _ map[string]any) (map[string]any, error) {
		secondCalls = append(secondCalls, targetID)
		return map[string]any{"published": true}, nil
	}
	if err := rollbackMigrationSwitch(state, reloaded, reloadedTxn, retry); err != nil {
		t.Fatal(err)
	}
	if len(secondCalls) != 1 || secondCalls[0] != "tablet" {
		t.Fatalf("reloaded rollback repeated completed publication work: %v", secondCalls)
	}
	if !findRoute(state.Inventory, source.ID).Enabled || findRoute(state.Inventory, reloadedTxn.ReplacementRoute).Enabled {
		t.Fatal("rollback did not restore the old desired Route")
	}
}

func TestMigrationReadsLegacyPublicationAttemptCheckpoint(t *testing.T) {
	state, source, input := migrationFixture(t, "direct", true)
	store, txn := preparedMigrationSwitch(t, state, source, input)
	txn.PublicationAttempted = []string{"desktop"}
	txn.PublicationProgress = nil
	txn.Phase = "rollback-pending"
	if err := setMigrationSelection(state, txn, false); err != nil {
		t.Fatal(err)
	}
	if err := saveMigrationState(state.PrivateDir, store, txn, migrationTestDependencies(nil, "healthy").Now); err != nil {
		t.Fatal(err)
	}

	reloaded, err := readMigrationState(state.PrivateDir)
	if err != nil {
		t.Fatal(err)
	}
	reloadedTxn := findMigration(reloaded, source.ID)
	publishCalls := 0
	deps := migrationTestDependencies(nil, "healthy")
	deps.Publish = func(*State, string, map[string]any) (map[string]any, error) {
		publishCalls++
		return map[string]any{"published": true}, nil
	}
	if err := rollbackMigrationSwitch(state, reloaded, reloadedTxn, deps); err != nil {
		t.Fatal(err)
	}
	if publishCalls != 1 || migrationPublicationFor(reloadedTxn, "desktop") == nil || migrationPublicationFor(reloadedTxn, "desktop").State != "complete" {
		t.Fatalf("legacy publication attempt did not recover through the new checkpoint model: calls=%d progress=%#v", publishCalls, reloadedTxn.PublicationProgress)
	}
}

func preparedMigrationSwitch(t *testing.T, state *State, source Route, input map[string]any) (*migrationState, *migrationTransaction) {
	t.Helper()
	deps := migrationTestDependencies(nil, "healthy")
	txn, err := newMigrationTransaction(state, source.ID, input, deps.Now)
	if err != nil {
		t.Fatal(err)
	}
	store := &migrationState{Schema: 1, Transactions: []migrationTransaction{txn}}
	current := &store.Transactions[0]
	if err := prepareMigration(state, current); err != nil {
		t.Fatal(err)
	}
	current.AffectedClientTargets = affectedMigrationTargets(state.Inventory, source.ID)
	current.Phase = "switching"
	if err := saveMigrationState(state.PrivateDir, store, current, deps.Now); err != nil {
		t.Fatal(err)
	}
	return store, current
}

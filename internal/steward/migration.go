package steward

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

const migrationStateFile = "migrations.json"

type migrationState struct {
	Schema       int                    `json:"schema"`
	Transactions []migrationTransaction `json:"transactions"`
}

type migrationPublicationProgress struct {
	TargetID         string `json:"target_id"`
	Direction        string `json:"direction"`
	InputFingerprint string `json:"input_fingerprint,omitempty"`
	State            string `json:"state"`
	FailureStage     string `json:"failure_stage,omitempty"`
}

type migrationTransaction struct {
	ID                       string                         `json:"id"`
	SourceRoute              string                         `json:"source_route"`
	ReplacedServer           string                         `json:"replaced_server"`
	ReplacementServer        string                         `json:"replacement_server"`
	ReplacementRoute         string                         `json:"replacement_route"`
	ReplacementLink          *string                        `json:"replacement_link,omitempty"`
	ReplacementServerContext map[string]any                 `json:"replacement_server_context,omitempty"`
	Reason                   string                         `json:"reason"`
	Phase                    string                         `json:"phase"`
	Attempt                  int                            `json:"attempt"`
	CreatedAt                string                         `json:"created_at"`
	UpdatedAt                string                         `json:"updated_at"`
	AffectedClientTargets    []string                       `json:"affected_client_targets"`
	PublicationAttempted     []string                       `json:"publication_attempted"`
	PublicationProgress      []migrationPublicationProgress `json:"publication_progress,omitempty"`
	CreatedReplacementServer bool                           `json:"created_replacement_server"`
	CreatedReplacementLink   bool                           `json:"created_replacement_link"`
	CreatedReplacementRoute  bool                           `json:"created_replacement_route"`
	LastFailure              string                         `json:"last_failure,omitempty"`
	OldCapacityRetired       bool                           `json:"old_capacity_retired"`
	ListenPort               int                            `json:"listen_port"`
	PortHopping              *PortHopping                   `json:"port_hopping,omitempty"`
	DisplayName              string                         `json:"display_name"`
}

type MigrationResult struct {
	SchemaVersion            int                 `json:"schema_version"`
	MigrationID              string              `json:"migration_id"`
	SourceRoute              string              `json:"source_route"`
	ReplacementServer        string              `json:"replacement_server"`
	ReplacementRoute         string              `json:"replacement_route"`
	ReplacementLink          *string             `json:"replacement_link,omitempty"`
	Phase                    string              `json:"phase"`
	Status                   string              `json:"status"`
	Summary                  string              `json:"summary"`
	AffectedClientTargets    []string            `json:"affected_client_targets"`
	Publication              []map[string]string `json:"publication,omitempty"`
	Working                  map[string]string   `json:"working"`
	Changed                  []string            `json:"changed"`
	LastFailure              string              `json:"last_failure,omitempty"`
	PortHopping              *PortHopping        `json:"port_hopping,omitempty"`
	Next                     []string            `json:"next"`
	OldCapacityRetired       bool                `json:"old_capacity_retired"`
	RetirementRequiresAction bool                `json:"retirement_requires_explicit_action"`
}

type migrationDependencies struct {
	Deploy  func(context.Context, *State, string) (map[string]any, error)
	Health  func(context.Context, *State, string, bool) (HealthResult, error)
	Render  func(*State, string, bool) (RenderResult, error)
	Publish func(*State, string, map[string]any) (map[string]any, error)
	Now     func() time.Time
}

func MigrateRoute(ctx context.Context, state *State, sourceRoute string, input map[string]any) (MigrationResult, error) {
	deps := migrationDependencies{
		Deploy:  deployRouteWithoutRender,
		Health:  HealthRoute,
		Render:  RenderClients,
		Publish: PublishSubscription,
		Now:     time.Now,
	}
	return migrateRouteWith(ctx, state, sourceRoute, input, deps)
}

func MigrationStatus(state *State, sourceRoute string) (map[string]any, error) {
	store, err := readMigrationState(state.PrivateDir)
	if err != nil {
		return nil, err
	}
	canonical := sourceRoute
	if route := findRoute(state.Inventory, sourceRoute); route != nil {
		canonical = route.ID
	}
	items := []MigrationResult{}
	for _, txn := range store.Transactions {
		if sourceRoute == "" || txn.SourceRoute == canonical {
			items = append(items, migrationResult(txn))
		}
	}
	if sourceRoute != "" && len(items) == 0 {
		return nil, fmt.Errorf("no migration exists for Route %q", sourceRoute)
	}
	return map[string]any{"schema_version": 1, "count": len(items), "migrations": items}, nil
}

func migrateRouteWith(ctx context.Context, state *State, sourceRoute string, input map[string]any, deps migrationDependencies) (MigrationResult, error) {
	if deps.Deploy == nil || deps.Health == nil || deps.Render == nil || deps.Publish == nil || deps.Now == nil {
		return MigrationResult{}, errors.New("migration dependencies are incomplete")
	}
	store, err := readMigrationState(state.PrivateDir)
	if err != nil {
		return MigrationResult{}, err
	}
	canonicalSource := sourceRoute
	if route := findRoute(state.Inventory, sourceRoute); route != nil {
		canonicalSource = route.ID
	}
	txn := findMigration(store, canonicalSource)
	if txn == nil {
		created, err := newMigrationTransaction(state, canonicalSource, input, deps.Now)
		if err != nil {
			return MigrationResult{}, err
		}
		store.Transactions = append(store.Transactions, created)
		txn = &store.Transactions[len(store.Transactions)-1]
		if err := saveMigrationState(state.PrivateDir, store, txn, deps.Now); err != nil {
			return MigrationResult{}, err
		}
	} else if replacement := stringField(input, "replacement_server_id"); replacement != "" {
		normalized, normalizeErr := convertID(replacement)
		if normalizeErr != nil || normalized != txn.ReplacementServer {
			return MigrationResult{}, errors.New("an existing migration cannot be retargeted to another replacement Server")
		}
	}
	txn.Attempt++
	txn.LastFailure = ""
	if err := saveMigrationState(state.PrivateDir, store, txn, deps.Now); err != nil {
		return MigrationResult{}, err
	}

	if txn.Phase == "complete" {
		return migrationResult(*txn), nil
	}
	if txn.Phase != "planned" && txn.Phase != "preparing" {
		if err := verifyPreparedMigration(state, txn); err != nil {
			txn.LastFailure = "migration-state-conflict"
			if saveErr := saveMigrationState(state.PrivateDir, store, txn, deps.Now); saveErr != nil {
				return MigrationResult{}, errors.Join(err, saveErr)
			}
			return migrationResult(*txn), nil
		}
	}
	if txn.Phase == "switching" {
		txn.Phase = "rollback-pending"
		if err := saveMigrationState(state.PrivateDir, store, txn, deps.Now); err != nil {
			return MigrationResult{}, err
		}
	}
	if txn.Phase == "rollback-pending" {
		if err := rollbackMigrationSwitch(state, store, txn, deps); err != nil {
			txn.LastFailure = "client-switch-rollback-failed"
			if saveErr := saveMigrationState(state.PrivateDir, store, txn, deps.Now); saveErr != nil {
				return MigrationResult{}, errors.Join(err, saveErr)
			}
			return migrationResult(*txn), nil
		}
		txn.Phase = "replacement-deployed"
		txn.PublicationAttempted = []string{}
		txn.PublicationProgress = []migrationPublicationProgress{}
		if err := saveMigrationState(state.PrivateDir, store, txn, deps.Now); err != nil {
			return MigrationResult{}, err
		}
	}

	if txn.Phase == "planned" || txn.Phase == "preparing" {
		txn.Phase = "preparing"
		if err := saveMigrationState(state.PrivateDir, store, txn, deps.Now); err != nil {
			return MigrationResult{}, err
		}
		if err := prepareMigration(state, txn); err != nil {
			txn.LastFailure = "replacement-preparation-failed"
			if saveErr := saveMigrationState(state.PrivateDir, store, txn, deps.Now); saveErr != nil {
				return MigrationResult{}, errors.Join(err, saveErr)
			}
			return migrationResult(*txn), nil
		}
		txn.Phase = "replacement-prepared"
		txn.AffectedClientTargets = affectedMigrationTargets(state.Inventory, txn.SourceRoute)
		if err := saveMigrationState(state.PrivateDir, store, txn, deps.Now); err != nil {
			return MigrationResult{}, err
		}
	}

	if txn.Phase == "replacement-prepared" {
		if _, err := deps.Deploy(ctx, state, txn.ReplacementRoute); err != nil {
			txn.LastFailure = "replacement-deployment-failed"
			if saveErr := saveMigrationState(state.PrivateDir, store, txn, deps.Now); saveErr != nil {
				return MigrationResult{}, errors.Join(err, saveErr)
			}
			return migrationResult(*txn), nil
		}
		txn.Phase = "replacement-deployed"
		if err := saveMigrationState(state.PrivateDir, store, txn, deps.Now); err != nil {
			return MigrationResult{}, err
		}
	}

	if txn.Phase == "replacement-deployed" {
		health, err := deps.Health(ctx, state, txn.ReplacementRoute, false)
		if err != nil || health.Status != "healthy" {
			txn.LastFailure = "replacement-health-not-healthy"
			if saveErr := saveMigrationState(state.PrivateDir, store, txn, deps.Now); saveErr != nil {
				if err != nil {
					return MigrationResult{}, errors.Join(err, saveErr)
				}
				return MigrationResult{}, saveErr
			}
			return migrationResult(*txn), nil
		}
		txn.Phase = "replacement-healthy"
		if err := saveMigrationState(state.PrivateDir, store, txn, deps.Now); err != nil {
			return MigrationResult{}, err
		}
	}

	if txn.Phase == "replacement-healthy" {
		if current := affectedMigrationTargets(state.Inventory, txn.SourceRoute); !slices.Equal(current, txn.AffectedClientTargets) {
			txn.LastFailure = "affected-client-targets-changed"
			if err := saveMigrationState(state.PrivateDir, store, txn, deps.Now); err != nil {
				return MigrationResult{}, err
			}
			return migrationResult(*txn), nil
		}
		txn.Phase = "switching"
		txn.PublicationAttempted = []string{}
		txn.PublicationProgress = []migrationPublicationProgress{}
		if err := saveMigrationState(state.PrivateDir, store, txn, deps.Now); err != nil {
			return MigrationResult{}, err
		}
		failure, err := switchMigrationClients(state, store, txn, deps)
		if err != nil {
			txn.Phase = "rollback-pending"
			txn.LastFailure = failure
			if saveErr := saveMigrationState(state.PrivateDir, store, txn, deps.Now); saveErr != nil {
				return MigrationResult{}, errors.Join(err, saveErr)
			}
			if rollbackErr := rollbackMigrationSwitch(state, store, txn, deps); rollbackErr != nil {
				txn.LastFailure = "client-switch-rollback-failed"
			} else {
				txn.Phase = "replacement-deployed"
				txn.PublicationAttempted = []string{}
				txn.PublicationProgress = []migrationPublicationProgress{}
				txn.LastFailure = failure
			}
			if saveErr := saveMigrationState(state.PrivateDir, store, txn, deps.Now); saveErr != nil {
				return MigrationResult{}, errors.Join(err, saveErr)
			}
			return migrationResult(*txn), nil
		}
		txn.Phase = "complete"
		txn.PublicationAttempted = []string{}
		txn.LastFailure = ""
		if err := saveMigrationState(state.PrivateDir, store, txn, deps.Now); err != nil {
			return MigrationResult{}, err
		}
	}
	return migrationResult(*txn), nil
}

func newMigrationTransaction(state *State, sourceRoute string, input map[string]any, now func() time.Time) (migrationTransaction, error) {
	source := findRoute(state.Inventory, sourceRoute)
	if source == nil {
		return migrationTransaction{}, fmt.Errorf("unknown Route %q", sourceRoute)
	}
	replacementServerRaw := stringField(input, "replacement_server_id")
	if replacementServerRaw == "" {
		return migrationTransaction{}, errors.New("replacement_server_id is required")
	}
	replacementServer, err := convertID(replacementServerRaw)
	if err != nil {
		return migrationTransaction{}, fmt.Errorf("replacement_server_id: %w", err)
	}
	replacedServer := source.EntryServer
	if source.Kind == "relay" {
		replacedServer, err = convertID(stringField(input, "replace_server_id"))
		if err != nil {
			return migrationTransaction{}, fmt.Errorf("replace_server_id: %w", err)
		}
		if replacedServer != source.EntryServer && replacedServer != source.ExitServer {
			return migrationTransaction{}, errors.New("replace_server_id must identify one endpoint of the relay Route")
		}
	}
	if replacementServer == replacedServer {
		return migrationTransaction{}, errors.New("replacement Server must differ from the Server being replaced")
	}
	replacementRoute := stringField(input, "replacement_route_id")
	if replacementRoute == "" {
		replacementRoute = derivedMigrationID(source.ID, replacementServer, "route")
	}
	replacementRoute, err = convertID(replacementRoute)
	if err != nil {
		return migrationTransaction{}, fmt.Errorf("replacement_route_id: %w", err)
	}
	var replacementLink *string
	if source.Kind == "relay" {
		value := stringField(input, "replacement_link_id")
		if value == "" {
			value = derivedMigrationID(source.ID, replacementServer, "link")
		}
		value, err = convertID(value)
		if err != nil {
			return migrationTransaction{}, fmt.Errorf("replacement_link_id: %w", err)
		}
		replacementLink = stringPointer(value)
	}
	serverContext := migrationServerContext(objectField(input, "replacement_server"))
	if existing := findServer(state.Inventory, replacementServer); existing == nil {
		if serverContext == nil {
			return migrationTransaction{}, errors.New("replacement_server details are required when the Server is not already in inventory")
		}
		serverContext = cloneObject(serverContext)
		serverContext["server_id"] = replacementServer
	} else if serverContext != nil {
		serverContext = cloneObject(serverContext)
		serverContext["server_id"] = replacementServer
	}
	port := intField(input, "listen_port", 0)
	if port == 0 {
		port = migrationListenPort(state.Inventory, *source, replacedServer)
	}
	if port < 1 || port > 65535 {
		return migrationTransaction{}, errors.New("migration listen_port is invalid")
	}
	portHopping := (*PortHopping)(nil)
	if source.PortHopping != nil {
		portHopping = &PortHopping{StartPort: port, EndPort: port + source.PortHopping.EndPort - source.PortHopping.StartPort}
		if err := validatePortHopping(port, portHopping); err != nil {
			return migrationTransaction{}, fmt.Errorf("migration port_hopping is invalid: %w", err)
		}
	}
	displayName := defaultString(stringField(input, "display_name"), source.DisplayName+"-replacement")
	if !displayNamePattern.MatchString(displayName) {
		return migrationTransaction{}, errors.New("migration display_name is invalid")
	}
	timestamp := now().UTC().Format(time.RFC3339Nano)
	return migrationTransaction{
		ID: derivedMigrationID(source.ID, replacementServer, "migration"), SourceRoute: source.ID,
		ReplacedServer: replacedServer, ReplacementServer: replacementServer, ReplacementRoute: replacementRoute,
		ReplacementLink: replacementLink, ReplacementServerContext: serverContext,
		Reason: defaultString(stringField(input, "reason"), "planned-replacement"), Phase: "planned",
		CreatedAt: timestamp, UpdatedAt: timestamp, AffectedClientTargets: []string{}, PublicationAttempted: []string{}, PublicationProgress: []migrationPublicationProgress{},
		ListenPort: port, PortHopping: portHopping, DisplayName: displayName,
	}, nil
}

func prepareMigration(state *State, txn *migrationTransaction) error {
	source := findRoute(state.Inventory, txn.SourceRoute)
	if source == nil {
		return errors.New("source Route disappeared during migration")
	}
	replacement := findServer(state.Inventory, txn.ReplacementServer)
	if replacement == nil {
		if txn.ReplacementServerContext == nil {
			return errors.New("replacement Server context is unavailable")
		}
		if _, err := AddServer(state, cloneObject(txn.ReplacementServerContext)); err != nil {
			return err
		}
		txn.CreatedReplacementServer = true
		replacement = findServer(state.Inventory, txn.ReplacementServer)
	} else if err := verifyReplacementServer(replacement, txn.ReplacementServerContext); err != nil {
		return err
	}

	entryID, exitID := migrationEndpoints(*source, txn)
	linkID := ""
	if source.Kind == "relay" {
		linkID = deref(txn.ReplacementLink)
		if existing := findLink(state.Inventory, linkID); existing == nil {
			if _, err := AddLink(state, map[string]any{"link_id": linkID, "entry_server": entryID, "exit_server": exitID}); err != nil {
				return err
			}
			txn.CreatedReplacementLink = true
		} else if existing.EntryServer != entryID || existing.ExitServer != exitID || existing.Driver != "wireguard" {
			return errors.New("existing replacement Link does not match the migration")
		}
	}
	if existing := findRoute(state.Inventory, txn.ReplacementRoute); existing == nil {
		context := map[string]any{"route_id": txn.ReplacementRoute, "display_name": txn.DisplayName, "kind": source.Kind, "entry_server": entryID, "listen_port": txn.ListenPort}
		if txn.PortHopping != nil {
			context["port_hopping"] = portHoppingText(txn.PortHopping)
		}
		if source.Kind == "relay" {
			context["exit_server"], context["link_id"] = exitID, linkID
		}
		if _, err := AddRoute(state, context); err != nil {
			return err
		}
		txn.CreatedReplacementRoute = true
	} else if existing.Kind != source.Kind || existing.EntryServer != entryID || existing.ExitServer != exitID || deref(existing.Link) != linkID || existing.ListenPort != txn.ListenPort || !samePortHopping(existing.PortHopping, txn.PortHopping) {
		return errors.New("existing replacement Route does not match the migration")
	}
	txn.ReplacementServerContext = nil
	return nil
}

func verifyPreparedMigration(state *State, txn *migrationTransaction) error {
	source := findRoute(state.Inventory, txn.SourceRoute)
	if source == nil || findServer(state.Inventory, txn.ReplacementServer) == nil {
		return errors.New("migration desired state is incomplete")
	}
	entryID, exitID := migrationEndpoints(*source, txn)
	linkID := ""
	if source.Kind == "relay" {
		linkID = deref(txn.ReplacementLink)
		link := findLink(state.Inventory, linkID)
		if link == nil || link.EntryServer != entryID || link.ExitServer != exitID || link.Driver != "wireguard" {
			return errors.New("migration replacement Link no longer matches its checkpoint")
		}
	}
	replacement := findRoute(state.Inventory, txn.ReplacementRoute)
	if replacement == nil || replacement.Kind != source.Kind || replacement.EntryServer != entryID || replacement.ExitServer != exitID || deref(replacement.Link) != linkID || replacement.ListenPort != txn.ListenPort || !samePortHopping(replacement.PortHopping, txn.PortHopping) {
		return errors.New("migration replacement Route no longer matches its checkpoint")
	}
	for _, reference := range []string{replacement.PayloadSecretRef, replacement.CredentialSecretRef} {
		if _, err := ResolveSecret(reference, state.PrivateDir, nil); err != nil {
			return errors.New("migration replacement Route secret state is incomplete")
		}
	}
	return nil
}

func migrationEndpoints(source Route, txn *migrationTransaction) (string, string) {
	entryID, exitID := source.EntryServer, source.ExitServer
	if txn.ReplacedServer == source.EntryServer {
		entryID = txn.ReplacementServer
	}
	if txn.ReplacedServer == source.ExitServer {
		exitID = txn.ReplacementServer
	}
	if source.Kind == "direct" {
		entryID, exitID = txn.ReplacementServer, txn.ReplacementServer
	}
	return entryID, exitID
}

func switchMigrationClients(state *State, store *migrationState, txn *migrationTransaction, deps migrationDependencies) (string, error) {
	if err := setMigrationSelection(state, txn, false); err != nil {
		return "client-selection-write-failed", err
	}
	for _, targetID := range txn.AffectedClientTargets {
		if _, err := deps.Render(state, targetID, false); err != nil {
			return "client-render-failed", err
		}
		target := findClientTarget(state.Inventory, targetID)
		if target != nil && target.Delivery == "subscription" {
			fingerprint, err := subscriptionInputFingerprint(state, targetID)
			if err != nil {
				return "subscription-publication-fingerprint-failed", err
			}
			setMigrationPublicationProgress(txn, targetID, "forward", fingerprint, "intent", "")
			if err := saveMigrationState(state.PrivateDir, store, txn, deps.Now); err != nil {
				return "migration-checkpoint-failed", err
			}
			if _, err := deps.Publish(state, targetID, nil); err != nil {
				publicationState, failureStage := migrationPublicationFailureState(err)
				setMigrationPublicationProgress(txn, targetID, "forward", fingerprint, publicationState, failureStage)
				if saveErr := saveMigrationState(state.PrivateDir, store, txn, deps.Now); saveErr != nil {
					return "migration-checkpoint-failed", errors.Join(err, saveErr)
				}
				return "subscription-publication-failed", err
			}
			setMigrationPublicationProgress(txn, targetID, "forward", fingerprint, "complete", "")
			if err := saveMigrationState(state.PrivateDir, store, txn, deps.Now); err != nil {
				return "migration-checkpoint-failed", err
			}
		}
	}
	return "", nil
}

func rollbackMigrationSwitch(state *State, store *migrationState, txn *migrationTransaction, deps migrationDependencies) error {
	rollbackTargets := migrationPublicationTargets(txn)
	if err := setMigrationSelection(state, txn, true); err != nil {
		return err
	}
	var failures []error
	for _, targetID := range txn.AffectedClientTargets {
		if _, err := deps.Render(state, targetID, false); err != nil {
			failures = append(failures, err)
		}
	}
	for _, targetID := range rollbackTargets {
		target := findClientTarget(state.Inventory, targetID)
		if target == nil || target.Delivery != "subscription" {
			continue
		}
		fingerprint, err := subscriptionInputFingerprint(state, targetID)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if progress := migrationPublicationFor(txn, targetID); progress != nil && progress.Direction == "rollback" && progress.State == "complete" && progress.InputFingerprint == fingerprint {
			continue
		}
		setMigrationPublicationProgress(txn, targetID, "rollback", fingerprint, "intent", "")
		if err := saveMigrationState(state.PrivateDir, store, txn, deps.Now); err != nil {
			failures = append(failures, err)
			continue
		}
		if _, err := deps.Publish(state, targetID, nil); err != nil {
			publicationState, failureStage := migrationPublicationFailureState(err)
			setMigrationPublicationProgress(txn, targetID, "rollback", fingerprint, publicationState, failureStage)
			if saveErr := saveMigrationState(state.PrivateDir, store, txn, deps.Now); saveErr != nil {
				failures = append(failures, errors.Join(err, saveErr))
			} else {
				failures = append(failures, err)
			}
			continue
		}
		setMigrationPublicationProgress(txn, targetID, "rollback", fingerprint, "complete", "")
		if err := saveMigrationState(state.PrivateDir, store, txn, deps.Now); err != nil {
			failures = append(failures, err)
		}
	}
	if len(failures) > 0 {
		return errors.Join(failures...)
	}
	return nil
}

func migrationPublicationFailureState(err error) (string, string) {
	var staged *operationStageError
	if !errors.As(err, &staged) {
		return "intent", ""
	}
	switch staged.StateChanged {
	case "subscription-published-unverified", "new-token-active-at-worker":
		return "published-unverified", staged.Stage
	case "subscription-published-verified", "subscription-token-rotated":
		return "verified", staged.Stage
	default:
		return "intent", staged.Stage
	}
}

func setMigrationPublicationProgress(txn *migrationTransaction, targetID, direction, fingerprint, state, failureStage string) {
	for i := range txn.PublicationProgress {
		if txn.PublicationProgress[i].TargetID == targetID {
			txn.PublicationProgress[i] = migrationPublicationProgress{TargetID: targetID, Direction: direction, InputFingerprint: fingerprint, State: state, FailureStage: failureStage}
			return
		}
	}
	txn.PublicationProgress = append(txn.PublicationProgress, migrationPublicationProgress{TargetID: targetID, Direction: direction, InputFingerprint: fingerprint, State: state, FailureStage: failureStage})
	sort.Slice(txn.PublicationProgress, func(i, j int) bool { return txn.PublicationProgress[i].TargetID < txn.PublicationProgress[j].TargetID })
}

func migrationPublicationFor(txn *migrationTransaction, targetID string) *migrationPublicationProgress {
	for i := range txn.PublicationProgress {
		if txn.PublicationProgress[i].TargetID == targetID {
			return &txn.PublicationProgress[i]
		}
	}
	return nil
}

func migrationPublicationTargets(txn *migrationTransaction) []string {
	targets := append([]string(nil), txn.PublicationAttempted...)
	for _, progress := range txn.PublicationProgress {
		targets = append(targets, progress.TargetID)
	}
	return sortedUnique(targets)
}

func validMigrationPublicationProgress(progress migrationPublicationProgress) bool {
	if !stableIDPattern.MatchString(progress.TargetID) || (progress.Direction != "forward" && progress.Direction != "rollback") {
		return false
	}
	switch progress.State {
	case "intent", "published-unverified", "verified", "complete":
	default:
		return false
	}
	if progress.InputFingerprint != "" {
		decoded, err := hex.DecodeString(progress.InputFingerprint)
		if err != nil || len(decoded) != sha256.Size {
			return false
		}
	}
	return true
}

func setMigrationSelection(state *State, txn *migrationTransaction, restoreOld bool) error {
	candidate := cloneInventory(state.Inventory)
	if err := applyMigrationSelection(candidate, txn, restoreOld); err != nil {
		return err
	}
	return saveCandidate(state, candidate, false)
}

func applyMigrationSelection(candidate *Inventory, txn *migrationTransaction, restoreOld bool) error {
	oldRoute := findRoute(candidate, txn.SourceRoute)
	newRoute := findRoute(candidate, txn.ReplacementRoute)
	if oldRoute == nil || newRoute == nil {
		return errors.New("migration Route state is incomplete")
	}
	oldRoute.Enabled = restoreOld
	newRoute.Enabled = !restoreOld
	from, to := txn.SourceRoute, txn.ReplacementRoute
	if restoreOld {
		from, to = txn.ReplacementRoute, txn.SourceRoute
	}
	for i := range candidate.Profiles {
		profile := &candidate.Profiles[i]
		if !contains(profile.IncludeRoutes, "*") {
			for index, routeID := range profile.IncludeRoutes {
				if routeID == from {
					profile.IncludeRoutes[index] = to
				}
			}
			profile.IncludeRoutes = sortedUnique(profile.IncludeRoutes)
		}
		if profile.Routing != nil {
			for index := range profile.Routing.Rules {
				rule := &profile.Routing.Rules[index]
				if rule.Action.Type == "route" && rule.Action.Route == from {
					rule.Action.Route = to
				}
			}
		}
	}
	for i := range candidate.ClientTargets {
		target := &candidate.ClientTargets[i]
		if target.Renderer == "hysteria2" && target.Route == from {
			target.Route = to
		}
	}
	return nil
}

func affectedMigrationTargets(inv *Inventory, sourceRoute string) []string {
	profiles := map[string]bool{}
	for _, profile := range inv.Profiles {
		if contains(profile.IncludeRoutes, "*") || contains(profile.IncludeRoutes, sourceRoute) {
			profiles[profile.ID] = true
		}
	}
	targets := []string{}
	for _, target := range inv.ClientTargets {
		if target.Renderer == "hysteria2" {
			if target.Route == sourceRoute {
				targets = append(targets, target.ID)
			}
		} else if profiles[target.Profile] {
			targets = append(targets, target.ID)
		}
	}
	sort.Strings(targets)
	return targets
}

func migrationResult(txn migrationTransaction) MigrationResult {
	status := "in-progress"
	summary := "Replacement capacity is being prepared while the old Route remains selected."
	next := []string{"retry migrate-route with the same target and replacement Server"}
	working := map[string]string{"old_route": "preserved-in-client-output", "replacement_route": "not-yet-proven"}
	if txn.LastFailure != "" {
		status = "blocked"
		summary = "Migration stopped safely. The old Route remains selected and the recorded step can be retried."
	}
	if txn.Phase == "replacement-deployed" {
		working["replacement_route"] = "deployed-awaiting-healthy-traffic-proof"
	}
	if txn.Phase == "replacement-healthy" || txn.Phase == "switching" || txn.Phase == "rollback-pending" {
		working["replacement_route"] = "healthy"
	}
	if txn.Phase == "rollback-pending" {
		status = "blocked"
		summary = "Client switching did not finish and rollback still needs a deterministic retry; recorded state is not being treated as complete."
		next = []string{"retry migrate-route to finish rollback before another client switch"}
		working["old_route"] = "desired-selection-restored-output-unconfirmed"
		working["replacement_route"] = "healthy-not-selected-in-desired-state"
	}
	if txn.Phase == "complete" {
		status = "complete"
		summary = "Replacement traffic is healthy and affected client outputs now select it. Old remote capacity is preserved."
		next = []string{"confirm clients have refreshed the generated artifact or subscription", "retire old external capacity only under a separate explicit destructive request"}
		working["old_route"] = "remote-capacity-preserved-client-disabled"
		working["replacement_route"] = "healthy-and-selected"
	}
	changed := []string{}
	if txn.CreatedReplacementServer {
		changed = append(changed, "replacement-server-added")
	}
	if txn.CreatedReplacementLink {
		changed = append(changed, "replacement-link-added")
	}
	if txn.CreatedReplacementRoute {
		changed = append(changed, "replacement-route-added")
	}
	if txn.Phase == "complete" {
		changed = append(changed, "affected-client-outputs-switched")
	}
	publication := make([]map[string]string, 0, len(txn.PublicationProgress))
	for _, progress := range txn.PublicationProgress {
		item := map[string]string{"target": progress.TargetID, "direction": progress.Direction, "state": progress.State}
		if progress.FailureStage != "" {
			item["failure_stage"] = progress.FailureStage
		}
		publication = append(publication, item)
	}
	return MigrationResult{
		SchemaVersion: 1, MigrationID: txn.ID, SourceRoute: txn.SourceRoute, ReplacementServer: txn.ReplacementServer,
		ReplacementRoute: txn.ReplacementRoute, ReplacementLink: txn.ReplacementLink, Phase: txn.Phase, Status: status,
		Summary: summary, AffectedClientTargets: append([]string(nil), txn.AffectedClientTargets...), Publication: publication, Working: working,
		Changed: changed, LastFailure: txn.LastFailure, PortHopping: clonePortHopping(txn.PortHopping), Next: next, OldCapacityRetired: txn.OldCapacityRetired,
		RetirementRequiresAction: true,
	}
}

func readMigrationState(privateDir string) (*migrationState, error) {
	path := filepath.Join(privateDir, migrationStateFile)
	if !regularFile(path) {
		return &migrationState{Schema: 1, Transactions: []migrationTransaction{}}, nil
	}
	return readMigrationStateFile(path)
}

func readMigrationStateFile(path string) (*migrationState, error) {
	var state migrationState
	if err := readJSON(path, &state); err != nil {
		return nil, errors.New("private migration state is invalid")
	}
	if state.Schema != 1 || state.Transactions == nil {
		return nil, errors.New("private migration state schema must be 1")
	}
	seen := map[string]bool{}
	for _, txn := range state.Transactions {
		idsValid := stableIDPattern.MatchString(txn.SourceRoute) && stableIDPattern.MatchString(txn.ReplacedServer) && stableIDPattern.MatchString(txn.ReplacementServer) && stableIDPattern.MatchString(txn.ReplacementRoute)
		if txn.ReplacementLink != nil {
			idsValid = idsValid && stableIDPattern.MatchString(*txn.ReplacementLink)
		}
		progressValid := true
		progressTargets := map[string]bool{}
		for _, progress := range txn.PublicationProgress {
			if progressTargets[progress.TargetID] || !validMigrationPublicationProgress(progress) {
				progressValid = false
				break
			}
			progressTargets[progress.TargetID] = true
		}
		if txn.ID == "" || seen[txn.ID] || !idsValid || !validMigrationPhase(txn.Phase) || !progressValid || txn.ListenPort < 1 || txn.ListenPort > 65535 || validatePortHopping(txn.ListenPort, txn.PortHopping) != nil {
			return nil, errors.New("private migration state contains an invalid transaction")
		}
		seen[txn.ID] = true
	}
	return &state, nil
}

func saveMigrationState(privateDir string, state *migrationState, txn *migrationTransaction, now func() time.Time) error {
	txn.UpdatedAt = now().UTC().Format(time.RFC3339Nano)
	return writeJSONAtomic(filepath.Join(privateDir, migrationStateFile), state)
}

func findMigration(state *migrationState, sourceRoute string) *migrationTransaction {
	for i := range state.Transactions {
		if state.Transactions[i].SourceRoute == sourceRoute && state.Transactions[i].Phase != "abandoned" {
			return &state.Transactions[i]
		}
	}
	return nil
}

func validMigrationPhase(phase string) bool {
	switch phase {
	case "planned", "preparing", "replacement-prepared", "replacement-deployed", "replacement-healthy", "switching", "rollback-pending", "complete", "abandoned":
		return true
	default:
		return false
	}
}

func derivedMigrationID(source, replacement, suffix string) string {
	base := strings.ToLower(source + "-to-" + replacement + "-" + suffix)
	if len(base) <= 63 && stableIDPattern.MatchString(base) {
		return base
	}
	sum := sha256.Sum256([]byte(base))
	tail := "-" + hex.EncodeToString(sum[:4])
	limit := 63 - len(tail)
	if limit < 1 {
		limit = 1
	}
	prefix := strings.Trim(base[:limit], "-")
	return prefix + tail
}

func migrationListenPort(inv *Inventory, source Route, replacedServer string) int {
	width := 1
	if source.PortHopping != nil {
		width = source.PortHopping.EndPort - source.PortHopping.StartPort + 1
	}
	if source.Kind == "direct" || replacedServer == source.EntryServer {
		return source.ListenPort
	}
	port := source.ListenPort + 1
	for port+width-1 <= 65535 && portRangeUsedByEntry(inv, source.EntryServer, port, port+width-1) {
		port++
	}
	if port+width-1 > 65535 {
		return 0
	}
	return port
}

func verifyReplacementServer(server *Server, context map[string]any) error {
	if context == nil {
		return nil
	}
	checks := map[string]string{
		"public_ipv4": server.Network.PublicIPv4,
		"ssh_user":    server.SSH.User,
	}
	for key, actual := range checks {
		if expected := stringField(context, key); expected != "" && expected != actual {
			return errors.New("existing replacement Server does not match supplied migration context")
		}
	}
	if expected := stringField(context, "ssh_key_path"); expected != "" {
		absolute, err := filepath.Abs(expected)
		if err != nil || absolute != server.SSH.KeyPath {
			return errors.New("existing replacement Server does not match supplied migration context")
		}
	}
	return nil
}

func objectField(input map[string]any, name string) map[string]any {
	if input == nil {
		return nil
	}
	value, ok := input[name]
	if !ok {
		return nil
	}
	object, _ := value.(map[string]any)
	return object
}

func cloneObject(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func migrationServerContext(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	allowed := []string{
		"server_id", "provider", "account_label", "instance_name", "region", "zone", "os", "architecture", "roles",
		"public_ipv4", "public_ipv6", "private_ipv4", "expected_egress_ipv4", "expected_egress_ipv6",
		"ssh_user", "ssh_key_path", "ssh_allowed_sources", "host_ownership",
	}
	out := map[string]any{}
	for _, key := range allowed {
		if value, ok := input[key]; ok {
			out[key] = value
		}
	}
	return out
}

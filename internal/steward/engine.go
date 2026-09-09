package steward

import (
	"context"
	"errors"
	"fmt"
)

const safeFailureSummary = "The operation failed locally. No secret-bearing diagnostic was returned through the agent surface."
const stagedFailureSummary = "The operation stopped after a partial state change. Use stage, state_changed, and retry to continue safely."

type operationStageError struct {
	Stage        string
	StateChanged string
	Retry        string
	Err          error
}

func (e *operationStageError) Error() string { return e.Err.Error() }
func (e *operationStageError) Unwrap() error { return e.Err }

type Request struct {
	Command    string
	Operation  string
	Target     string
	Context    map[string]any
	PrivateDir string
	Approved   bool
}

func RunRequest(ctx context.Context, request Request) (Envelope, int) {
	success := func(command string, data any) (Envelope, int) {
		return Envelope{SchemaVersion: 1, Command: command, Success: true, Code: "ok", Data: data}, 0
	}
	failure := func(command, code string, data any, exit int) (Envelope, int) {
		return Envelope{SchemaVersion: 1, Command: command, Success: false, Code: code, Data: data}, exit
	}
	if request.Command == "capabilities" {
		if request.Operation != "" {
			capability, err := CapabilityByID(request.Operation)
			if err != nil {
				return failure("capabilities", "capability-not-found", map[string]any{"operation": request.Operation}, 2)
			}
			return success("capabilities", map[string]any{"product": "route-steward", "interface": "agent-machine-surface", "capability": capability})
		}
		return success("capabilities", map[string]any{"product": "route-steward", "interface": "agent-machine-surface", "capabilities": Capabilities(), "drivers": DriverCapabilities()})
	}
	if request.Command == "bootstrap" {
		state, created, err := Bootstrap(request.PrivateDir)
		if err != nil {
			return SanitizedFailure("bootstrap", err)
		}
		return success("bootstrap", map[string]any{"created": created, "context": SanitizedContext(state.Inventory)})
	}
	if request.Operation == "recover" && (request.Command == "preflight" || request.Command == "execute") {
		preflight := NewRecoveryPreflight(request.PrivateDir, request.Context)
		if request.Command == "preflight" {
			return success("preflight", preflight)
		}
		if !preflight.Ready {
			return failure("execute", "context-gate-blocked", preflight, 2)
		}
		return failure("execute", "local-assistance-required", map[string]any{"preflight": preflight, "result": localAssistanceResult("recover", "route-steward recover --archive <path> --private-dir <directory>")}, 3)
	}
	if request.Command == "execute" && request.Operation == "bootstrap" {
		state, created, err := Bootstrap(request.PrivateDir)
		if err != nil {
			return SanitizedFailure("execute", err)
		}
		return success("execute", map[string]any{"operation": "bootstrap", "created": created, "context": SanitizedContext(state.Inventory)})
	}
	state, err := LoadState(request.PrivateDir)
	if err != nil {
		return SanitizedFailure(request.Command, err)
	}
	switch request.Command {
	case "context":
		if request.Target != "" {
			data, code := SanitizedTargetContext(state.Inventory, request.Target)
			if code != "" {
				return failure("context", code, data, 2)
			}
			return success("context", data)
		}
		return success("context", SanitizedContext(state.Inventory))
	case "drift":
		report, err := DriftReport(state)
		if err != nil {
			return SanitizedFailure("drift", err)
		}
		return success("drift", report)
	case "migrations":
		result, err := MigrationStatus(state, request.Target)
		if err != nil {
			return SanitizedFailure("migrations", err)
		}
		return success("migrations", result)
	case "health":
		preflight, err := NewPreflight("health", request.Target, state, request.Context, false)
		if err != nil {
			return SanitizedFailure("health", err)
		}
		if !preflight.Ready {
			return failure("health", "context-gate-blocked", preflight, 2)
		}
		result, err := HealthRoute(ctx, state, request.Target, boolField(request.Context, "include_public_ip", false))
		if err != nil {
			return SanitizedFailure("health", err)
		}
		return success("health", result)
	case "preflight":
		if request.Operation == "" {
			return SanitizedFailure("preflight", errors.New("operation is required"))
		}
		preflight, err := NewPreflight(request.Operation, request.Target, state, request.Context, request.Approved)
		if err != nil {
			return SanitizedFailure("preflight", err)
		}
		return success("preflight", preflight)
	case "execute":
		if request.Operation == "" {
			return SanitizedFailure("execute", errors.New("operation is required"))
		}
		preflight, err := NewPreflight(request.Operation, request.Target, state, request.Context, request.Approved)
		if err != nil {
			return SanitizedFailure("execute", err)
		}
		if !preflight.Ready {
			return failure("execute", "context-gate-blocked", preflight, 2)
		}
		result, code, exit := executeReady(ctx, state, request)
		if exit != 0 {
			return failure("execute", code, result, exit)
		}
		return success("execute", map[string]any{"preflight": preflight, "result": result})
	default:
		return SanitizedFailure(request.Command, fmt.Errorf("unsupported command %q", request.Command))
	}
}

func executeReady(ctx context.Context, state *State, request Request) (any, string, int) {
	fail := func(err error) (any, string, int) {
		code := "operation-failed"
		if errors.Is(err, errSubscriptionPayloadTooLarge) {
			code = "subscription-payload-too-large"
		}
		data := map[string]any{"summary": safeFailureSummary, "operation": request.Operation}
		var staged *operationStageError
		if errors.As(err, &staged) {
			data["summary"] = stagedFailureSummary
			data["stage"] = staged.Stage
			data["state_changed"] = staged.StateChanged
			retry := staged.Retry
			if request.Operation == "rotate-subscription-token" && staged.StateChanged == "subscription-published-unverified" {
				retry = "rotate-subscription-token"
			}
			data["retry"] = retry
		}
		return data, code, 1
	}
	var result any
	var err error
	switch request.Operation {
	case "status":
		result = SanitizedContext(state.Inventory)
	case "audit":
		evidence := AuditRoute(ctx, state, request.Target)
		_, err = SetObservedRoute(state, stateRouteID(state, request.Target), evidence.Status, evidence.Category, deref(evidence.ActualEgressIPv4), deref(evidence.HysteriaVersion), deref(evidence.WireGuardVersion))
		result = evidence.Sanitized()
	case "health":
		result, err = HealthRoute(ctx, state, request.Target, boolField(request.Context, "include_public_ip", false))
	case "add-server":
		result, err = AddServer(state, request.Context)
	case "add-link":
		result, err = AddLink(state, request.Context)
	case "add-route":
		result, err = AddRoute(state, request.Context)
	case "add-provider":
		result, err = AddProvider(state, request.Context)
	case "update-provider":
		result, err = UpdateProvider(state, request.Target, request.Context)
	case "remove-provider":
		result, err = RemoveProvider(state, request.Target)
	case "add-profile":
		result, err = AddProfile(state, request.Context)
	case "update-profile":
		result, err = UpdateProfile(state, request.Target, request.Context)
	case "remove-profile":
		result, err = RemoveProfile(state, request.Target)
	case "add-client-target":
		result, err = AddClientTarget(state, request.Context)
	case "update-client-target":
		result, err = UpdateClientTarget(state, request.Target, request.Context)
	case "remove-client-target":
		result, err = RemoveClientTarget(state, request.Target)
	case "deploy-route":
		result, err = DeployRoute(ctx, state, request.Target, true)
	case "render-client":
		var render RenderResult
		render, err = RenderClients(state, request.Target, false)
		if err == nil {
			result = SanitizedRender(render)
		}
	case "publish-subscription":
		result, err = PublishSubscription(state, request.Target, request.Context)
	case "rotate-subscription-token":
		result, err = RotateSubscriptionToken(state, request.Target)
	case "backup":
		return localAssistanceResult("backup", "route-steward backup --private-dir <directory>"), "local-assistance-required", 3
	case "migrate-route":
		migration, migrationErr := MigrateRoute(ctx, state, request.Target, request.Context)
		if migrationErr != nil {
			err = migrationErr
		} else if migration.Status == "blocked" {
			return migration, "workflow-blocked", 4
		} else {
			result = migration
		}
	default:
		return map[string]string{"summary": safeFailureSummary}, "operation-failed", 1
	}
	if err != nil {
		return fail(err)
	}
	return result, "ok", 0
}

func SanitizedFailure(command string, err error) (Envelope, int) {
	code := "operation-failed"
	if errors.Is(err, errSubscriptionPayloadTooLarge) {
		code = "subscription-payload-too-large"
	}
	return Envelope{SchemaVersion: 1, Command: command, Success: false, Code: code, Data: map[string]string{"summary": safeFailureSummary}}, 1
}

func localAssistanceResult(operation, command string) map[string]any {
	return map[string]any{
		"operation":                    operation,
		"executor":                     "local-assisted",
		"command":                      command,
		"requires_local_secret_prompt": true,
		"secret_prompt_rule":           "The password must stay in the local 7-Zip prompt and must not enter the model, MCP stream, process arguments, repository files, or logs.",
		"remote_changed":               false,
	}
}

func stateRouteID(state *State, target string) string {
	if route := findRoute(state.Inventory, target); route != nil {
		return route.ID
	}
	return target
}

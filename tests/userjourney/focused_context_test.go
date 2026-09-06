package userjourney_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPFocusedCapabilityAndContext(t *testing.T) {
	root := repositoryRoot(t)
	temp := t.TempDir()
	binary := testedBinary(t, root, temp)
	privateDir := filepath.Join(temp, "private")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "focused-context-acceptance", Version: "1"}, nil)
	transport := &mcp.CommandTransport{Command: exec.Command(binary, "mcp", "--private-dir", privateDir)}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	bootstrap, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "route_steward_bootstrap", Arguments: map[string]any{}})
	if err != nil || bootstrap.IsError {
		t.Fatalf("MCP bootstrap failed: result=%#v err=%v", bootstrap, err)
	}

	focusedCapability, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "route_steward_capabilities",
		Arguments: map[string]any{"operation": "add-server"},
	})
	if err != nil || focusedCapability.IsError {
		t.Fatalf("focused MCP capability lookup failed: result=%#v err=%v", focusedCapability, err)
	}
	capEnvelope := decodeMCPEnvelope(t, focusedCapability)
	if !capEnvelope.Success || !jsonContains(capEnvelope.Data, `"capability":{"id":"add-server"}`) {
		t.Fatalf("focused MCP capability lookup returned unexpected data: %s", capEnvelope.Data)
	}
	if jsonContains(capEnvelope.Data, `"capabilities"`) || jsonContains(capEnvelope.Data, `"drivers"`) {
		t.Fatalf("focused MCP capability lookup returned full discovery data: %s", capEnvelope.Data)
	}

	keyPath := filepath.Join(privateDir, "fixture.pem")
	if err := os.WriteFile(keyPath, []byte("synthetic-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	callMCPExecute(t, ctx, session, "add-server", "", map[string]any{
		"server_id": "entry-a", "public_ipv4": "192.0.2.10", "ssh_user": "ubuntu", "ssh_key_path": keyPath, "host_ownership": "dedicated",
	})
	callMCPExecute(t, ctx, session, "add-route", "", map[string]any{
		"route_id": "direct-a", "display_name": "Direct-A", "kind": "direct", "entry_server": "entry-a", "listen_port": 443,
	})
	callMCPExecute(t, ctx, session, "add-profile", "", map[string]any{
		"profile_id": "primary", "include_routes": []any{"direct-a"}, "include_providers": []any{}, "routing": map[string]any{"rules": []any{}},
	})
	callMCPExecute(t, ctx, session, "add-client-target", "", map[string]any{
		"target_id": "desktop", "profile_id": "primary", "renderer": "mihomo",
	})

	focusedContext, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "route_steward_context",
		Arguments: map[string]any{"target": "direct-a"},
	})
	if err != nil || focusedContext.IsError {
		t.Fatalf("focused MCP context failed: result=%#v err=%v", focusedContext, err)
	}
	contextEnvelope := decodeMCPEnvelope(t, focusedContext)
	if !contextEnvelope.Success || !jsonContains(contextEnvelope.Data, `"kind":"route"`) || !jsonContains(contextEnvelope.Data, `"id":"direct-a"`) {
		t.Fatalf("focused MCP context returned unexpected data: %s", contextEnvelope.Data)
	}
	if !jsonContains(contextEnvelope.Data, `"profiles":["primary"]`) || !jsonContains(contextEnvelope.Data, `"client_targets":["desktop"]`) {
		t.Fatalf("focused MCP context lost direct consumers: %s", contextEnvelope.Data)
	}
	if jsonContains(contextEnvelope.Data, `"counts"`) || jsonContains(contextEnvelope.Data, `"supported_operations"`) {
		t.Fatalf("focused MCP context returned full-project context: %s", contextEnvelope.Data)
	}
	for _, privateValue := range []string{"192.0.2.10", keyPath} {
		if jsonContains(contextEnvelope.Data, privateValue) {
			t.Fatalf("focused MCP context exposed private value %q: %s", privateValue, contextEnvelope.Data)
		}
	}
}

func callMCPExecute(t *testing.T, ctx context.Context, session *mcp.ClientSession, operation, target string, input map[string]any) {
	t.Helper()
	arguments := map[string]any{"operation": operation, "context": input}
	if target != "" {
		arguments["target"] = target
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "route_steward_execute", Arguments: arguments})
	if err != nil || result.IsError {
		t.Fatalf("MCP execute %s failed: result=%#v err=%v", operation, result, err)
	}
	envelope := decodeMCPEnvelope(t, result)
	if !envelope.Success {
		t.Fatalf("MCP execute %s returned failure: %#v", operation, envelope)
	}
}

func decodeMCPEnvelope(t *testing.T, result *mcp.CallToolResult) envelope {
	t.Helper()
	body, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var decoded envelope
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("MCP structured content is not an envelope: %v\n%s", err, body)
	}
	return decoded
}

func jsonContains(data json.RawMessage, fragment string) bool {
	return stringContains(string(data), fragment)
}

func stringContains(value, fragment string) bool {
	return len(fragment) == 0 || (len(value) >= len(fragment) && indexString(value, fragment) >= 0)
}

func indexString(value, fragment string) int {
	for i := 0; i+len(fragment) <= len(value); i++ {
		if value[i:i+len(fragment)] == fragment {
			return i
		}
	}
	return -1
}

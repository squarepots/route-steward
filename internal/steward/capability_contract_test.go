package steward

import "testing"

func TestCapabilityMetadataIsComplete(t *testing.T) {
	capabilities := Capabilities()
	if len(capabilities) == 0 {
		t.Fatal("capability list is empty")
	}
	seen := map[string]bool{}
	for _, capability := range capabilities {
		if capability.ID == "" || seen[capability.ID] {
			t.Fatalf("capability ID is empty or duplicated: %q", capability.ID)
		}
		seen[capability.ID] = true
		if capability.State != "supported" {
			t.Fatalf("capability %s has unexpected state %q", capability.ID, capability.State)
		}
		if capability.RequiredContext == nil || capability.Effects == nil {
			t.Fatalf("capability %s omits required_context or effects", capability.ID)
		}
	}

	addServer, err := CapabilityByID("add-server")
	if err != nil {
		t.Fatal(err)
	}
	if addServer.AuthorizationClass != "local-write" || !addServer.Mutation {
		t.Fatalf("add-server contract changed unexpectedly: %#v", addServer)
	}
	foundOwnership := false
	for _, field := range addServer.RequiredContext {
		if field.Name == "host_ownership" {
			foundOwnership = field.Required && field.Type == "dedicated"
		}
	}
	if !foundOwnership {
		t.Fatalf("add-server does not require dedicated host ownership: %#v", addServer.RequiredContext)
	}

	migration, err := CapabilityByID("migrate-route")
	if err != nil {
		t.Fatal(err)
	}
	if migration.Executor != "workflow" || !containsString(migration.Effects, "persist-resumable-migration-transaction") {
		t.Fatalf("migration contract lost resumable workflow semantics: %#v", migration)
	}

	rotation, err := CapabilityByID("rotate-subscription-token")
	if err != nil {
		t.Fatal(err)
	}
	if rotation.AuthorizationClass != "credential-change" {
		t.Fatalf("subscription token rotation is not guarded as a credential change: %#v", rotation)
	}

	if _, err := CapabilityByID("does-not-exist"); err == nil {
		t.Fatal("unknown capability was accepted")
	}
}

func TestDriverCapabilityProjectionHasSupportedFamilies(t *testing.T) {
	drivers := DriverCapabilities()
	for _, key := range []string{"compute", "ingress", "links", "providers", "health_checks", "renderers", "subscription_delivery"} {
		if _, ok := drivers[key]; !ok {
			t.Fatalf("driver capability projection is missing %s", key)
		}
	}

	if firstCapabilityID(t, drivers["compute"]) != "byo-ssh-ubuntu-24.04-amd64" {
		t.Fatalf("unexpected compute capability: %#v", drivers["compute"])
	}
	if firstCapabilityID(t, drivers["ingress"]) != "hysteria2" {
		t.Fatalf("unexpected ingress capability: %#v", drivers["ingress"])
	}
	if firstCapabilityID(t, drivers["links"]) != "wireguard-single-hop" {
		t.Fatalf("unexpected link capability: %#v", drivers["links"])
	}
	if firstCapabilityID(t, drivers["health_checks"]) != "hysteria2-client-traffic" {
		t.Fatalf("unexpected health capability: %#v", drivers["health_checks"])
	}

	renderers, ok := drivers["renderers"].([]any)
	if !ok {
		t.Fatalf("renderer capability projection has unexpected shape: %#v", drivers["renderers"])
	}
	seen := map[string]bool{}
	for _, raw := range renderers {
		item, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("renderer capability has unexpected shape: %#v", raw)
		}
		id, _ := item["id"].(string)
		seen[id] = true
	}
	for _, id := range []string{"mihomo", "karing", "shadowrocket", "hysteria2"} {
		if !seen[id] {
			t.Fatalf("renderer capability projection is missing %s", id)
		}
	}

	routing, ok := drivers["profile_routing"].(map[string]any)
	if !ok || routing["ordered"] != true {
		t.Fatalf("profile routing capability projection has unexpected shape: %#v", drivers["profile_routing"])
	}
	if got := stringSliceFromAny(routing["match_types"]); len(got) != 3 || !containsString(got, "domain_suffix") || !containsString(got, "geosite") || !containsString(got, "geoip") {
		t.Fatalf("profile routing match types are incomplete: %#v", routing["match_types"])
	}
}

func firstCapabilityID(t *testing.T, value any) string {
	t.Helper()
	items, ok := value.([]any)
	if !ok || len(items) == 0 {
		t.Fatalf("capability list has unexpected shape: %#v", value)
	}
	item, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("capability item has unexpected shape: %#v", items[0])
	}
	id, _ := item["id"].(string)
	return id
}

func stringSliceFromAny(value any) []string {
	switch items := value.(type) {
	case []string:
		return append([]string(nil), items...)
	case []any:
		result := make([]string, 0, len(items))
		for _, raw := range items {
			if item, ok := raw.(string); ok {
				result = append(result, item)
			}
		}
		return result
	default:
		return nil
	}
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

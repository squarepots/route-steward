package steward

import (
	"fmt"
	"sort"
	"strings"

	routesteward "github.com/squarepots/route-steward"
)

func Capabilities() []Capability {
	field := func(name, kind string, required bool) ContextField {
		return ContextField{Name: name, Type: kind, Required: required}
	}
	target := func(kind string, required bool) ContextField {
		return ContextField{Name: "target", Type: kind, Required: required, Source: "argument"}
	}
	return []Capability{
		capability("status", "agent", false, "read-only", "Read sanitized local state.", nil, "read-sanitized-local-state"),
		capability("drift", "agent", false, "read-only", "Compare desired routes and ClientTarget renders with sanitized observed state.", nil, "read-sanitized-local-and-observed-state"),
		capability("migrations", "agent", false, "read-only", "Read sanitized resumable migration checkpoints.", []ContextField{target("route-id", false)}, "read-sanitized-migration-state"),
		capability("audit", "core", false, "read-only", "Compare one supported remote Route with desired state without changing it.", []ContextField{target("route-id", true)}, "read-remote-supported-state"),
		capability("health", "core", false, "read-only", "Run server audit and a real Hysteria2 client traffic check for one deployed Route.", []ContextField{target("route-id", true), field("include_public_ip", "boolean", false)}, "read-remote-supported-state", "download-verified-local-health-helper-if-absent", "contact-public-health-endpoints-through-route", "write-sanitized-observed-health"),
		capability("bootstrap", "agent", true, "local-write", "Create clean local private state.", nil, "create-local-private-state"),
		capability("add-server", "agent", true, "local-write", "Add a BYO SSH Server to desired state without connecting to it.", []ContextField{field("server_id", "stable-id", true), field("public_ipv4", "ipv4", true), field("ssh_user", "unix-user", true), field("ssh_key_path", "local-file-path", true), field("host_ownership", "dedicated", true)}, "update-local-desired-state"),
		capability("add-link", "agent", true, "local-write", "Allocate one WireGuard Link and generate its keys locally.", []ContextField{field("link_id", "stable-id", true), field("entry_server", "server-id", true), field("exit_server", "server-id", true)}, "allocate-local-link-and-keys"),
		capability("add-route", "agent", true, "local-write", "Add a Hysteria2 direct or relay Route and generate its credentials locally.", []ContextField{field("route_id", "stable-id", true), field("kind", "direct|relay", true), field("entry_server", "server-id", true), field("port_hopping", "udp-port-range-2-to-8", false)}, "allocate-local-route-and-credentials"),
		capability("add-provider", "agent", true, "local-write", "Add an optional generic upstream Provider and keep its URL in local secret storage.", []ContextField{field("provider_id", "stable-id", true), field("url", "http-url", true)}, "store-provider-url-as-local-secret"),
		capability("update-provider", "agent", true, "local-write", "Update an existing optional Provider without exposing its URL through agent status.", []ContextField{target("provider-id", true)}, "update-local-provider"),
		capability("remove-provider", "agent", true, "local-write", "Remove an unreferenced Provider and its local URL secret.", []ContextField{target("provider-id", true)}, "remove-local-provider-and-secret"),
		capability("add-profile", "agent", true, "local-write", "Add a reusable Profile with ordered generic routing rules.", []ContextField{field("profile_id", "stable-id", true), field("routing", "ordered-profile-routing-rules", false), field("include_routes", "route-id-list", false), field("include_providers", "provider-id-list", false)}, "update-local-profile"),
		capability("update-profile", "agent", true, "local-write", "Update a Profile routing and provider selection without changing renderer identity.", []ContextField{target("profile-id", true), field("routing", "ordered-profile-routing-rules", false), field("include_routes", "route-id-list", false), field("include_providers", "provider-id-list", false)}, "update-local-profile"),
		capability("remove-profile", "agent", true, "local-write", "Remove a Profile only when no ClientTarget references it.", []ContextField{target("profile-id", true)}, "remove-local-profile"),
		capability("add-client-target", "agent", true, "local-write", "Add a renderer-backed ClientTarget that references a reusable Profile.", []ContextField{field("target_id", "stable-id", true), field("profile_id", "profile-id", true), field("renderer", "mihomo|karing|shadowrocket|hysteria2", true), {Name: "mihomo_process_names", Type: "process-name-list-0-to-32", Required: false, When: "renderer is mihomo"}, {Name: "route_id", Type: "route-id", Required: false, When: "renderer is hysteria2"}, {Name: "listen", Type: "loopback-listener", Required: false, When: "renderer is hysteria2"}, {Name: "ingress_family", Type: "auto|ipv4|ipv6", Required: false, When: "renderer is hysteria2"}}, "update-local-client-target"),
		capability("update-client-target", "agent", true, "local-write", "Update a ClientTarget's Profile, delivery, or renderer-specific settings.", []ContextField{target("client-target-id", true), field("profile_id", "profile-id", false), field("delivery", "file|subscription", false), {Name: "mihomo_process_names", Type: "process-name-list-0-to-32", Required: false, When: "current renderer is mihomo"}, {Name: "route_id", Type: "route-id", Required: false, When: "current renderer is hysteria2"}, {Name: "listen", Type: "loopback-listener", Required: false, When: "current renderer is hysteria2"}, {Name: "ingress_family", Type: "auto|ipv4|ipv6", Required: false, When: "current renderer is hysteria2"}}, "update-local-client-target"),
		capability("remove-client-target", "agent", true, "local-write", "Remove a ClientTarget after any target-scoped subscription state is revoked.", []ContextField{target("client-target-id", true)}, "remove-local-client-target"),
		capability("deploy-route", "core", true, "remote-write", "Deploy one existing desired Route.", []ContextField{target("route-id", true)}, "mutate-supported-dedicated-hosts", "render-private-client-artifacts"),
		capability("render-client", "core", true, "local-write", "Render supported client files from inventory and secrets.", []ContextField{target("client-target-id", false)}, "write-private-client-artifacts"),
		capability("publish-subscription", "core", true, "external-publication", "Publish one private Mihomo or Shadowrocket ClientTarget subscription payload.", []ContextField{target("client-target-id", true), {Name: "worker_name", Type: "worker-name", Required: false, When: "subscription state is absent"}, {Name: "host", Type: "hostname", Required: false, When: "subscription state is absent"}}, "publish-private-subscription-payload"),
		capability("rotate-subscription-token", "agent", true, "credential-change", "Rotate only one ClientTarget subscription bearer token after explicit current authorization.", []ContextField{target("client-target-id", true)}, "rotate-target-subscription-token"),
		capability("migrate-route", "workflow", true, "remote-write", "Create, test, and switch to replacement capacity through a resumable transaction. Keep old capacity available.", []ContextField{target("route-id", true), field("replacement_server_id", "server-id", true), {Name: "replacement_server", Type: "server-context", Required: false, When: "replacement Server is absent from inventory"}, {Name: "replace_server_id", Type: "server-id", Required: false, When: "the source Route is a relay"}}, "persist-resumable-migration-transaction", "create-and-validate-overlap", "switch-client-output-after-healthy-traffic-proof", "keep-old-remote-capacity-unretired"),
		secretPromptCapability("backup", "Create an encrypted recovery archive using a local 7-Zip password prompt.", nil, "write-encrypted-local-recovery-archive"),
		secretPromptCapability("recover", "Restore private state using the local recovery workflow and 7-Zip password prompt.", []ContextField{field("archive_path", "local-file-path", true)}, "restore-local-canonical-state"),
	}
}

func capability(id, executor string, mutation bool, authorization, description string, required []ContextField, effects ...string) Capability {
	if required == nil {
		required = []ContextField{}
	}
	return Capability{ID: id, State: "supported", Executor: executor, Mutation: mutation, AuthorizationClass: authorization, Description: description, RequiredContext: required, Effects: effects}
}
func secretPromptCapability(id, description string, required []ContextField, effects ...string) Capability {
	c := capability(id, "local-assisted", true, "local-write", description, required, effects...)
	c.RequiresLocalSecretPrompt = true
	return c
}
func CapabilityByID(id string) (Capability, error) {
	for _, c := range Capabilities() {
		if c.ID == id {
			return c, nil
		}
	}
	return Capability{}, fmt.Errorf("unsupported Route Steward operation %q", id)
}

func DriverCapabilities() map[string]any {
	return map[string]any{
		"schema_version":  1,
		"product_version": routesteward.Version(),
		"compute":         []any{map[string]any{"id": "byo-ssh-ubuntu-24.04-amd64", "state": "supported", "provisioning": "bring-your-own", "transport": "ssh", "package_manager": "apt", "architecture": "amd64", "operating_system": "ubuntu-24.04", "host_ownership": "dedicated"}},
		"ingress":         []any{map[string]any{"id": "hysteria2", "state": "supported", "version": hysteriaProtocolVersion, "transport": "udp", "address_families": []string{"ipv4", "ipv6"}, "credential_model": "local-canonical-pinned-tls", "reliability": []string{"salamander-obfuscation", "optional-port-hopping"}}},
		"links":           []any{map[string]any{"id": "wireguard-single-hop", "state": "supported", "hops": 1, "address_family": "ipv4"}},
		"providers":       []any{map[string]any{"id": "mihomo-http-provider", "state": "supported", "optional": true, "schemes": []string{"https", "http"}, "health_check": false}},
		"profile_routing": map[string]any{"state": "supported", "ordered": true, "match_types": []string{"domain_suffix", "geosite", "geoip"}, "action_types": []string{"direct", "route"}, "route_target": "enabled-included-route-id", "fallback": "Private Routes", "renderers": []string{"mihomo", "karing"}},
		"health_checks":   []any{map[string]any{"id": "hysteria2-client-traffic", "state": "supported", "routes": []string{"direct", "relay"}, "on_demand": true, "external_endpoints": []string{"cloudflare-trace", "ipify"}, "packet_loss": "unsupported"}},
		"renderers": []any{
			map[string]any{"id": "mihomo", "state": "supported", "compatibility_baseline": mihomoCompatibilityBaseline, "clients": []string{"Clash Verge-compatible Mihomo clients"}, "delivery": []string{"private-file", "private-subscription"}, "global_selector": "GLOBAL", "provider_group_semantics": []string{"proxies", "use"}, "process_routing": map[string]any{"field": "mihomo_process_names", "rule": "PROCESS-NAME", "mode": "find-process-mode strict", "policy_group": "Applications", "process_name_limit": maxMihomoProcessNames, "rule_position": "after-private-direct-before-profile-routing"}},
			map[string]any{"id": "karing", "state": "supported", "compatibility_baseline": karingCompatibilityBaseline, "delivery": []string{"private-clash-yaml"}, "platforms": []string{"windows", "macos", "linux", "ios", "android", "tvos"}, "tls_identity": "sha256-certificate-pinning"},
			map[string]any{"id": "shadowrocket", "state": "supported", "delivery": []string{"node-import", "private-subscription"}},
			map[string]any{"id": "hysteria2", "state": "supported", "delivery": []string{"private-json"}, "local_modes": []string{"http", "socks5"}, "runtime": "verified-official-client"},
		},
		"client_proxy":          []any{map[string]any{"id": "hysteria2-loopback", "state": "supported", "command": "proxy", "check": "real-http-exit-identity", "listen_scope": "loopback-only"}},
		"subscription_delivery": []any{map[string]any{"id": "cloudflare-worker", "state": "supported", "optional": true, "role": "private-client-config-delivery-only"}},
	}
}

func SanitizedContext(inv *Inventory) map[string]any {
	enabledRoutes, enabledProviders := 0, 0
	servers := make([]map[string]any, 0, len(inv.Servers))
	for _, server := range inv.Servers {
		servers = append(servers, map[string]any{"id": server.ID, "roles": append([]string(nil), server.Roles...)})
	}
	links := make([]map[string]any, 0, len(inv.Links))
	for _, link := range inv.Links {
		links = append(links, map[string]any{"id": link.ID, "entry_server": link.EntryServer, "exit_server": link.ExitServer, "enabled": link.Enabled})
	}
	routes := make([]map[string]any, 0, len(inv.Routes))
	for _, route := range inv.Routes {
		if route.Enabled {
			enabledRoutes++
		}
		item := map[string]any{"id": route.ID, "kind": route.Kind, "entry_server": route.EntryServer, "exit_server": route.ExitServer, "enabled": route.Enabled, "state": route.State}
		if route.Link != nil {
			item["link"] = *route.Link
		}
		routes = append(routes, item)
	}
	providers := make([]map[string]any, 0, len(inv.Providers))
	for _, provider := range inv.Providers {
		if provider.Enabled {
			enabledProviders++
		}
		providers = append(providers, map[string]any{"id": provider.ID, "enabled": provider.Enabled})
	}
	profiles := make([]map[string]any, 0, len(inv.Profiles))
	for _, profile := range inv.Profiles {
		routing := effectiveProfileRouting(profile)
		rules := make([]map[string]any, 0, len(routing.Rules))
		for _, rule := range routing.Rules {
			match := map[string]string{"type": rule.Match.Type, "value": rule.Match.Value}
			action := map[string]string{"type": rule.Action.Type}
			if rule.Action.Route != "" {
				action["route"] = rule.Action.Route
			}
			rules = append(rules, map[string]any{"match": match, "action": action})
		}
		profiles = append(profiles, map[string]any{"id": profile.ID, "include_routes": append([]string(nil), profile.IncludeRoutes...), "include_providers": append([]string(nil), profile.IncludeProviders...), "routing": map[string]any{"rules": rules}})
	}
	targets := make([]map[string]any, 0, len(inv.ClientTargets))
	processNameCount := 0
	for _, target := range inv.ClientTargets {
		processNameCount += len(target.MihomoProcessNames)
		item := map[string]any{"id": target.ID, "profile": target.Profile, "renderer": target.Renderer, "delivery": target.Delivery, "subscription_initialized": target.SubscriptionSecretRef != ""}
		if target.Renderer == "mihomo" {
			item["mihomo_process_name_count"] = len(target.MihomoProcessNames)
		}
		if target.Renderer == "hysteria2" {
			item["route"] = target.Route
		}
		targets = append(targets, item)
	}
	operations := make([]map[string]string, 0, len(Capabilities()))
	for _, capability := range Capabilities() {
		operations = append(operations, map[string]string{"id": capability.ID, "state": capability.State, "authorization_class": capability.AuthorizationClass})
	}
	return map[string]any{
		"schema_version":   1,
		"inventory_schema": inv.Schema,
		"counts":           map[string]int{"servers": len(inv.Servers), "links": len(inv.Links), "routes": len(inv.Routes), "enabled_routes": enabledRoutes, "providers": len(inv.Providers), "enabled_providers": enabledProviders, "profiles": len(inv.Profiles), "client_targets": len(inv.ClientTargets), "mihomo_process_names": processNameCount},
		"servers":          servers, "links": links, "routes": routes, "providers": providers, "profiles": profiles, "client_targets": targets,
		"supported_operations": operations,
	}
}

func sortedUnique(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}
func stringField(context map[string]any, name string) string {
	if context == nil {
		return ""
	}
	value, ok := context[name]
	if !ok || value == nil {
		return ""
	}
	s, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}
func hasField(context map[string]any, name string) bool {
	if context == nil {
		return false
	}
	_, ok := context[name]
	return ok
}
func stringSliceField(context map[string]any, name string, fallback []string) []string {
	if context == nil {
		return append([]string(nil), fallback...)
	}
	raw, ok := context[name]
	if !ok {
		return append([]string(nil), fallback...)
	}
	values, ok := raw.([]any)
	if !ok {
		if strs, ok := raw.([]string); ok {
			return append([]string(nil), strs...)
		}
		return []string{}
	}
	out := make([]string, 0, len(values))
	for _, item := range values {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
func boolField(context map[string]any, name string, fallback bool) bool {
	if context == nil {
		return fallback
	}
	raw, ok := context[name]
	if !ok {
		return fallback
	}
	value, ok := raw.(bool)
	if !ok {
		return fallback
	}
	return value
}
func intField(context map[string]any, name string, fallback int) int {
	if context == nil {
		return fallback
	}
	raw, ok := context[name]
	if !ok {
		return fallback
	}
	switch value := raw.(type) {
	case float64:
		return int(value)
	case int:
		return value
	case jsonNumber:
		var n int
		_, _ = fmt.Sscanf(string(value), "%d", &n)
		return n
	}
	return fallback
}

type jsonNumber string

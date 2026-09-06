from pathlib import Path
import re

ROOT = Path('.')

def read(path):
    return (ROOT / path).read_text(encoding='utf-8')

def write(path, text):
    (ROOT / path).write_text(text, encoding='utf-8', newline='\n')

def replace(path, old, new, count=1):
    text = read(path)
    actual = text.count(old)
    if actual < count:
        raise RuntimeError(f'{path}: expected at least {count} copies, found {actual}: {old[:80]!r}')
    write(path, text.replace(old, new, count))

# Current schema owns only current concepts. Legacy policy fields stay in the v1 decoder.
replace('internal/steward/types.go', '\tProviders     []Provider     `json:"providers"`\n\tPolicies      []Policy       `json:"-"`\n\tProfiles      []Profile      `json:"profiles"`\n', '\tProviders     []Provider     `json:"providers"`\n\tProfiles      []Profile      `json:"profiles"`\n')
replace('internal/steward/types.go', 'type Policy struct {\n\tID          string `json:"-"`\n\tDescription string `json:"-"`\n\tDNSMode     string `json:"-"`\n}\n\n', '')
replace('internal/steward/types.go', 'type Profile struct {\n\tID               string          `json:"id"`\n\tPolicy           string          `json:"-"`\n\tIncludeRoutes    []string        `json:"include_routes"`\n', 'type Profile struct {\n\tID               string          `json:"id"`\n\tIncludeRoutes    []string        `json:"include_routes"`\n')

# Make auxiliary schemas independent instead of projecting inventory schema into them.
replace('internal/steward/state.go', '\t\tServers: []Server{}, Links: []Link{}, Routes: []Route{}, Providers: []Provider{},\n\t\tPolicies: []Policy{},\n\t\tProfiles: []Profile{}, ClientTargets: []ClientTarget{},\n', '\t\tServers: []Server{}, Links: []Link{}, Routes: []Route{}, Providers: []Provider{},\n\t\tProfiles: []Profile{}, ClientTargets: []ClientTarget{},\n')
replace('internal/steward/state.go', 'index := SecretIndex{Schema: InventorySchema, Refs: map[string]SecretRef{}}', 'index := SecretIndex{Schema: SecretIndexSchema, Refs: map[string]SecretRef{}}')
replace('internal/steward/state.go', 'if index.Schema != InventorySchema || index.Refs == nil {', 'if index.Schema != SecretIndexSchema || index.Refs == nil {')
replace('internal/steward/state.go', 'failures = append(failures, "inventory schema must be 1")', 'failures = append(failures, "inventory schema must be 2")')
replace('internal/steward/state.go', '\tpolicyIDs := make([]string, 0, len(inv.Policies))\n\tpolicySet := map[string]bool{}\n\tfor _, policy := range inv.Policies {\n\t\tpolicyIDs = append(policyIDs, policy.ID)\n\t\tpolicySet[policy.ID] = true\n\t}\n', '')
replace('internal/steward/state.go', '\tcheckIDs("Provider", providerIDs)\n\tcheckIDs("Policy", policyIDs)\n\tcheckIDs("Profile", profileIDs)\n', '\tcheckIDs("Provider", providerIDs)\n\tcheckIDs("Profile", profileIDs)\n')
replace('internal/steward/state.go', '\tfor _, p := range inv.Profiles {\n\t\tif p.Policy != "" && !policySet[p.Policy] && !legacyPolicyID(p.Policy) {\n\t\t\tfailures = append(failures, fmt.Sprintf("Profile %q references an unknown Policy", p.ID))\n\t\t}\n', '\tfor _, p := range inv.Profiles {\n')
replace('internal/steward/state.go', 'return ObservedState{Schema: InventorySchema, GeneratedAt: nil, Servers: []ObservedObject{}, Links: []ObservedObject{}, Routes: []ObservedRoute{}}', 'return ObservedState{Schema: ObservedSchema, GeneratedAt: nil, Servers: []ObservedObject{}, Links: []ObservedObject{}, Routes: []ObservedRoute{}}')
replace('internal/steward/schema_compat.go', '\tindex.Schema = InventorySchema\n', '\tindex.Schema = SecretIndexSchema\n')

# Schema-2 routing is one authority. Legacy input converts at the boundary only.
write('internal/steward/profile_routing.go', r'''package steward

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
)

type ProfileRouting struct {
	Rules []ProfileRoutingRule `json:"rules,omitempty"`
}

type ProfileRoutingRule struct {
	Match  ProfileRoutingMatch  `json:"match"`
	Action ProfileRoutingAction `json:"action"`
}

type ProfileRoutingMatch struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type ProfileRoutingAction struct {
	Type  string `json:"type"`
	Route string `json:"route,omitempty"`
}

func (routing *ProfileRouting) UnmarshalJSON(data []byte) error {
	type disk struct {
		Rules []ProfileRoutingRule `json:"rules,omitempty"`
	}
	var decoded disk
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return errors.New("routing must contain one JSON object")
		}
		return err
	}
	routing.Rules = decoded.Rules
	return nil
}

func (routing ProfileRouting) MarshalJSON() ([]byte, error) {
	normalized, err := normalizeProfileRouting(&routing)
	if err != nil {
		return nil, err
	}
	type disk struct {
		Rules []ProfileRoutingRule `json:"rules,omitempty"`
	}
	return json.Marshal(disk{Rules: normalized.Rules})
}

func defaultProfileRouting() *ProfileRouting {
	return &ProfileRouting{Rules: []ProfileRoutingRule{}}
}

func cloneProfileRouting(routing *ProfileRouting) *ProfileRouting {
	if routing == nil {
		return nil
	}
	out := &ProfileRouting{Rules: make([]ProfileRoutingRule, len(routing.Rules))}
	copy(out.Rules, routing.Rules)
	return out
}

func normalizeProfileRouting(routing *ProfileRouting) (*ProfileRouting, error) {
	if routing == nil {
		return nil, errors.New("routing must be an object")
	}
	out := cloneProfileRouting(routing)
	seen := map[string]bool{}
	for i := range out.Rules {
		rule := &out.Rules[i]
		rule.Match.Type = strings.ToLower(strings.TrimSpace(rule.Match.Type))
		rule.Match.Value = strings.TrimSpace(rule.Match.Value)
		rule.Action.Type = strings.ToLower(strings.TrimSpace(rule.Action.Type))
		rule.Action.Route = strings.TrimSpace(rule.Action.Route)
		switch rule.Match.Type {
		case "domain_suffix", "geosite", "geoip":
		default:
			return nil, fmt.Errorf("unsupported routing match type %q", rule.Match.Type)
		}
		if !validRoutingValue(rule.Match.Value) {
			return nil, errors.New("routing match value must be non-empty and contain no comma, line break, or control character")
		}
		switch rule.Action.Type {
		case "direct":
			if rule.Action.Route != "" {
				return nil, errors.New("direct routing action cannot include a Route ID")
			}
		case "route":
			if rule.Action.Route == "" {
				return nil, errors.New("route routing action requires a Route ID")
			}
		default:
			return nil, fmt.Errorf("unsupported routing action type %q", rule.Action.Type)
		}
		key := rule.Match.Type + "\x00" + strings.ToLower(rule.Match.Value)
		if seen[key] {
			return nil, fmt.Errorf("routing matcher %q is duplicated", rule.Match.Value)
		}
		seen[key] = true
	}
	return out, nil
}

func validRoutingValue(value string) bool {
	if value == "" || strings.ContainsAny(value, ",\r\n") {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func profileRoutingFromContext(context map[string]any) (*ProfileRouting, bool, error) {
	if hasField(context, "routing") {
		raw := context["routing"]
		if raw == nil {
			return nil, true, errors.New("routing must be an object")
		}
		b, err := json.Marshal(raw)
		if err != nil {
			return nil, true, fmt.Errorf("decode routing: %w", err)
		}
		var routing ProfileRouting
		if err := json.Unmarshal(b, &routing); err != nil {
			return nil, true, fmt.Errorf("decode routing: %w", err)
		}
		normalized, err := normalizeProfileRouting(&routing)
		if err != nil {
			return nil, true, err
		}
		return normalized, true, nil
	}
	if hasField(context, "policy") {
		switch strings.ToLower(strings.TrimSpace(stringField(context, "policy"))) {
		case "privacy", "":
			return defaultProfileRouting(), true, nil
		case "balanced-cn":
			normalized, err := normalizeProfileRouting(&ProfileRouting{Rules: legacyChinaDirectRules()})
			return normalized, true, err
		default:
			return nil, true, errors.New("legacy policy is unsupported; use routing.rules")
		}
	}
	return nil, false, nil
}

func effectiveProfileRouting(profile Profile) ProfileRouting {
	if profile.Routing == nil {
		return ProfileRouting{Rules: []ProfileRoutingRule{}}
	}
	routing, err := normalizeProfileRouting(profile.Routing)
	if err == nil {
		return *routing
	}
	return ProfileRouting{Rules: append([]ProfileRoutingRule(nil), profile.Routing.Rules...)}
}

func profileRouteIncluded(profile Profile, routeID string) bool {
	return contains(profile.IncludeRoutes, "*") || contains(profile.IncludeRoutes, routeID)
}

func validateProfileRouting(inv *Inventory, profile Profile) error {
	if profile.Routing == nil {
		return nil
	}
	routing, err := normalizeProfileRouting(profile.Routing)
	if err != nil {
		return err
	}
	for _, rule := range routing.Rules {
		if rule.Action.Type != "route" {
			continue
		}
		route := findRoute(inv, rule.Action.Route)
		if route == nil {
			return fmt.Errorf("routing rule references unknown Route %q", rule.Action.Route)
		}
		if !route.Enabled {
			return fmt.Errorf("routing rule references disabled Route %q", rule.Action.Route)
		}
		if !profileRouteIncluded(profile, rule.Action.Route) {
			return fmt.Errorf("routing rule references Route %q outside Profile", rule.Action.Route)
		}
	}
	return nil
}
''')

# Preserve explicit empty v1 route/provider selections during upgrade.
replace('internal/steward/inventory_v1.go', '''\t\tincludeRoutes := append([]string(nil), profile.IncludeRoutes...)\n\t\tif includeRoutes == nil {\n\t\t\tincludeRoutes = []string{"*"}\n\t\t}\n\t\tincludeProviders := append([]string(nil), profile.IncludeProviders...)\n\t\tif includeProviders == nil {\n\t\t\tincludeProviders = []string{}\n\t\t}\n''', '''\t\tvar includeRoutes []string\n\t\tif profile.IncludeRoutes == nil {\n\t\t\tincludeRoutes = []string{"*"}\n\t\t} else {\n\t\t\tincludeRoutes = append([]string{}, profile.IncludeRoutes...)\n\t\t}\n\t\tvar includeProviders []string\n\t\tif profile.IncludeProviders == nil {\n\t\t\tincludeProviders = []string{}\n\t\t} else {\n\t\t\tincludeProviders = append([]string{}, profile.IncludeProviders...)\n\t\t}\n''')
replace('internal/steward/inventory_v1.go', '\treturn &Inventory{Schema: InventorySchema, Metadata: old.Metadata, Delivery: old.Delivery, Servers: old.Servers, Links: old.Links, Routes: old.Routes, Providers: old.Providers, Profiles: profiles, ClientTargets: old.ClientTargets}, nil\n', '\treturn &Inventory{Schema: InventorySchema, Metadata: old.Metadata, Delivery: old.Delivery, Servers: old.Servers, Links: old.Links, Routes: old.Routes, Providers: old.Providers, Profiles: profiles, ClientTargets: old.ClientTargets}, nil\n')
replace('internal/steward/inventory_v1.go', '\nfunc legacyPolicyID(policy string) bool { return legacyPolicyIDV1(policy) }\n', '\n')

# Do not store legacy policy in current profiles; translate it immediately.
replace('internal/steward/operations.go', 'profile := Profile{ID: id, Policy: stringField(context, "policy"), IncludeRoutes: stringSliceField(context, "include_routes", []string{"*"}), IncludeProviders: stringSliceField(context, "include_providers", []string{})}', 'profile := Profile{ID: id, IncludeRoutes: stringSliceField(context, "include_routes", []string{"*"}), IncludeProviders: stringSliceField(context, "include_providers", []string{})}')
replace('internal/steward/operations.go', '''\tif hasField(context, "policy") {\n\t\tprofile.Policy = stringField(context, "policy")\n\t}\n\tif hasField(context, "include_routes") {''', '''\tif hasField(context, "include_routes") {''')
replace('internal/steward/operations.go', '''\tif hasField(context, "routing") {\n\t\trouting, _, err := profileRoutingFromContext(context)\n\t\tif err != nil {\n\t\t\treturn nil, err\n\t\t}\n\t\tprofile.Routing = routing\n\t}\n''', '''\tif hasField(context, "routing") || hasField(context, "policy") {\n\t\trouting, _, err := profileRoutingFromContext(context)\n\t\tif err != nil {\n\t\t\treturn nil, err\n\t\t}\n\t\tprofile.Routing = routing\n\t}\n''')

replace('internal/steward/preflight.go', 'candidate := Profile{ID: stringField(context, "profile_id"), Policy: stringField(context, "policy"), IncludeRoutes: stringSliceField(context, "include_routes", []string{"*"}), IncludeProviders: stringSliceField(context, "include_providers", []string{})}', 'candidate := Profile{ID: stringField(context, "profile_id"), IncludeRoutes: stringSliceField(context, "include_routes", []string{"*"}), IncludeProviders: stringSliceField(context, "include_providers", []string{})}')
replace('internal/steward/preflight.go', '''\t\t\tif hasField(context, "policy") {\n\t\t\t\tcandidate.Policy = stringField(context, "policy")\n\t\t\t}\n''', '')
replace('internal/steward/preflight.go', '''\t\t\tif hasField(context, "routing") {\n\t\t\t\tif routing, _, err := profileRoutingFromContext(context); err != nil {\n\t\t\t\t\tconflicts = append(conflicts, "profile-routing-invalid")\n\t\t\t\t} else {\n\t\t\t\t\tcandidate.Routing = routing\n\t\t\t\t}\n\t\t\t}\n''', '''\t\t\tif hasField(context, "routing") || hasField(context, "policy") {\n\t\t\t\tif routing, _, err := profileRoutingFromContext(context); err != nil {\n\t\t\t\t\tconflicts = append(conflicts, "profile-routing-invalid")\n\t\t\t\t} else {\n\t\t\t\t\tcandidate.Routing = routing\n\t\t\t\t}\n\t\t\t}\n''')

# Migration updates canonical routing rules directly.
replace('internal/steward/migration.go', '''\t\tif profile.Routing != nil {\n\t\t\tfor index, binding := range profile.Routing.ServiceRoutes {\n\t\t\t\tif binding.Route == from {\n\t\t\t\t\tprofile.Routing.ServiceRoutes[index].Route = to\n\t\t\t\t}\n\t\t\t}\n\t\t}\n''', '''\t\tif profile.Routing != nil {\n\t\t\tfor index := range profile.Routing.Rules {\n\t\t\t\trule := &profile.Routing.Rules[index]\n\t\t\t\tif rule.Action.Type == "route" && rule.Action.Route == from {\n\t\t\t\t\trule.Action.Route = to\n\t\t\t\t}\n\t\t\t}\n\t\t}\n''')

# Render into an isolated staging directory, validate there, then atomically replace live files.
text = read('internal/steward/render.go')
start = text.index('func RenderClients(')
end = text.index('\nfunc renderHysteria2', start)
new_render = r'''func RenderClients(state *State, targetID string, skipValidation bool) (RenderResult, error) {
	targetIDs := []string{}
	if targetID != "" {
		if findClientTarget(state.Inventory, targetID) == nil {
			return RenderResult{}, fmt.Errorf("unknown ClientTarget %q", targetID)
		}
		targetIDs = append(targetIDs, targetID)
	} else {
		for _, target := range state.Inventory.ClientTargets {
			targetIDs = append(targetIDs, target.ID)
		}
	}
	return RenderClientTargets(state, targetIDs, skipValidation)
}

func RenderClientTargets(state *State, targetIDs []string, skipValidation bool) (RenderResult, error) {
	outputDir := state.Inventory.Delivery.Directory
	if outputDir == "" {
		outputDir = filepath.Join(state.PrivateDir, "delivery")
	}
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return RenderResult{}, err
	}
	if err := protectPath(outputDir, true); err != nil {
		return RenderResult{}, err
	}
	if len(targetIDs) == 0 {
		return RenderResult{SchemaVersion: 1, Command: "render-client-targets", Success: true, Outputs: []RenderOutput{}}, nil
	}
	stageDir, err := os.MkdirTemp(outputDir, ".rst-render-*")
	if err != nil {
		return RenderResult{}, err
	}
	defer os.RemoveAll(stageDir)
	type staged struct {
		output RenderOutput
		stage  string
		final  string
	}
	stagedOutputs := make([]staged, 0, len(targetIDs))
	seen := map[string]bool{}
	for _, targetID := range targetIDs {
		if seen[targetID] {
			continue
		}
		seen[targetID] = true
		target := findClientTarget(state.Inventory, targetID)
		if target == nil {
			return RenderResult{}, fmt.Errorf("unknown ClientTarget %q", targetID)
		}
		profile := findProfile(state.Inventory, target.Profile)
		if profile == nil {
			return RenderResult{}, fmt.Errorf("unknown Profile %q", target.Profile)
		}
		extension := ".yaml"
		switch target.Renderer {
		case "shadowrocket":
			extension = ".html"
		case "hysteria2":
			extension = ".json"
		}
		stagePath := filepath.Join(stageDir, target.ID+extension)
		finalPath := filepath.Join(outputDir, target.ID+extension)
		var output RenderOutput
		var renderErr error
		switch target.Renderer {
		case "mihomo":
			output, renderErr = renderClash(state, *profile, *target, stagePath, "mihomo", skipValidation)
		case "karing":
			output, renderErr = renderClash(state, *profile, *target, stagePath, "karing", skipValidation)
		case "shadowrocket":
			output, renderErr = renderShadowrocket(state, *profile, *target, stagePath)
		case "hysteria2":
			output, renderErr = renderHysteria2(state, *profile, *target, stagePath)
		default:
			renderErr = fmt.Errorf("ClientTarget %q uses unsupported renderer %q", target.ID, target.Renderer)
		}
		if renderErr != nil {
			return RenderResult{}, renderErr
		}
		output.Path = finalPath
		stagedOutputs = append(stagedOutputs, staged{output: output, stage: stagePath, final: finalPath})
	}
	outputs := make([]RenderOutput, 0, len(stagedOutputs))
	for _, item := range stagedOutputs {
		data, err := os.ReadFile(item.stage)
		if err != nil {
			return RenderResult{}, err
		}
		if err := writeFileAtomic(item.final, data, 0o600); err != nil {
			return RenderResult{}, err
		}
		outputs = append(outputs, item.output)
	}
	if err := updateRenderManifest(state, outputs); err != nil {
		return RenderResult{}, err
	}
	return RenderResult{SchemaVersion: 1, Command: "render-client-targets", Success: true, Outputs: outputs}, nil
}
'''
write('internal/steward/render.go', text[:start] + new_render + text[end:])

# Quote complete Mihomo rule scalars so generic matcher values cannot change YAML structure.
replace('internal/steward/render.go', 'fmt.Fprintf(&processRules, "  - PROCESS-NAME,%s,Applications\\n", name)', 'fmt.Fprintf(&processRules, "  - %s\\n", yamlQuote("PROCESS-NAME,"+name+",Applications"))')
replace('internal/steward/render.go', 'fmt.Fprintf(&profileRules, "  - DOMAIN-SUFFIX,%s,%s\\n", rule.Match.Value, targetName)', 'fmt.Fprintf(&profileRules, "  - %s\\n", yamlQuote("DOMAIN-SUFFIX,"+rule.Match.Value+","+targetName))')
replace('internal/steward/render.go', 'fmt.Fprintf(&profileRules, "  - GEOSITE,%s,%s\\n", rule.Match.Value, targetName)', 'fmt.Fprintf(&profileRules, "  - %s\\n", yamlQuote("GEOSITE,"+rule.Match.Value+","+targetName))')
replace('internal/steward/render.go', 'fmt.Fprintf(&profileRules, "  - GEOIP,%s,%s,no-resolve\\n", rule.Match.Value, targetName)', 'fmt.Fprintf(&profileRules, "  - %s\\n", yamlQuote("GEOIP,"+rule.Match.Value+","+targetName+",no-resolve"))')

# Per-target freshness: only inputs consumed by that target invalidate its artifact.
text = read('internal/steward/render.go')
start = text.index('func canonicalFingerprint(')
end = text.index('\nfunc updateRenderManifest', start)
new_fp = r'''func clientTargetFingerprint(state *State, targetID string) (string, error) {
	target := findClientTarget(state.Inventory, targetID)
	if target == nil {
		return "", fmt.Errorf("unknown ClientTarget %q", targetID)
	}
	profile := findProfile(state.Inventory, target.Profile)
	if profile == nil {
		return "", fmt.Errorf("unknown Profile %q", target.Profile)
	}
	hash := sha256.New()
	add := func(name string, value any) error {
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		hash.Write([]byte(name))
		hash.Write([]byte{0})
		hash.Write(data)
		hash.Write([]byte{0})
		return nil
	}
	addBytes := func(name string, data []byte) {
		hash.Write([]byte(name))
		hash.Write([]byte{0})
		hash.Write(data)
		hash.Write([]byte{0})
	}
	if err := add("target", target); err != nil {
		return "", err
	}
	if err := add("profile", profile); err != nil {
		return "", err
	}
	allRoutes := contains(profile.IncludeRoutes, "*")
	for _, route := range state.Inventory.Routes {
		if !route.Enabled || (!allRoutes && !contains(profile.IncludeRoutes, route.ID)) {
			continue
		}
		if err := add("route:"+route.ID, route); err != nil {
			return "", err
		}
		path, err := ResolveSecret(route.PayloadSecretRef, state.PrivateDir, nil)
		if err != nil {
			return "", err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		addBytes("route-payload:"+route.ID, data)
	}
	allProviders := contains(profile.IncludeProviders, "*")
	for _, provider := range state.Inventory.Providers {
		if !provider.Enabled || (!allProviders && !contains(profile.IncludeProviders, provider.ID)) {
			continue
		}
		if err := add("provider:"+provider.ID, provider); err != nil {
			return "", err
		}
		path, err := ResolveSecret(provider.SourceSecretRef, state.PrivateDir, nil)
		if err != nil {
			return "", err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		addBytes("provider-source:"+provider.ID, data)
	}
	if target.Renderer == "shadowrocket" && target.SubscriptionSecretRef != "" {
		path, err := ResolveSecret(target.SubscriptionSecretRef, state.PrivateDir, nil)
		if err != nil {
			return "", err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		addBytes("subscription", data)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
'''
write('internal/steward/render.go', text[:start] + new_fp + text[end:])
replace('internal/steward/render.go', '''\tfingerprint, err := canonicalFingerprint(state)\n\tif err != nil {\n\t\treturn err\n\t}\n\tentries := map[string]renderManifestEntry{}\n''', '''\tentries := map[string]renderManifestEntry{}\n''')
replace('internal/steward/render.go', '''\tfor _, output := range outputs {\n\t\tsum, err := sha256File(output.Path)\n''', '''\tfor _, output := range outputs {\n\t\tfingerprint, err := clientTargetFingerprint(state, output.ClientTarget)\n\t\tif err != nil {\n\t\t\treturn err\n\t\t}\n\t\tsum, err := sha256File(output.Path)\n''')
# encoding/binary is no longer needed.
replace('internal/steward/render.go', '\t"encoding/binary"\n', '')

# Deploy only client targets affected by this route.
replace('internal/steward/execution.go', '''\tif renderClients {\n\t\trender, err := RenderClients(state, "", skipClientValidation)\n\t\tif err != nil {\n\t\t\treturn nil, fmt.Errorf("Route deployed, but client rendering failed: %w", err)\n\t\t}\n\t\tresult["render"] = SanitizedRender(render)\n\t}\n''', '''\tif renderClients {\n\t\ttargets := affectedMigrationTargets(state.Inventory, route.ID)\n\t\trender, err := RenderClientTargets(state, targets, skipClientValidation)\n\t\tif err != nil {\n\t\t\treturn nil, fmt.Errorf("Route deployed, but affected client rendering failed: %w", err)\n\t\t}\n\t\tresult["render"] = SanitizedRender(render)\n\t}\n''')

# Context exposes safe object relationships so a fresh agent can continue without raw private state.
text = read('internal/steward/capabilities.go')
start = text.index('func SanitizedContext(')
end = text.index('\nfunc sortedUnique', start)
new_context = r'''func SanitizedContext(inv *Inventory) map[string]any {
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
		"schema_version": 1,
		"inventory_schema": inv.Schema,
		"counts": map[string]int{"servers": len(inv.Servers), "links": len(inv.Links), "routes": len(inv.Routes), "enabled_routes": enabledRoutes, "providers": len(inv.Providers), "enabled_providers": enabledProviders, "profiles": len(inv.Profiles), "client_targets": len(inv.ClientTargets), "mihomo_process_names": processNameCount},
		"servers": servers, "links": links, "routes": routes, "providers": providers, "profiles": profiles, "client_targets": targets,
		"supported_operations": operations,
	}
}
'''
write('internal/steward/capabilities.go', text[:start] + new_context + text[end:])

# Drift is explicitly historical evidence; client freshness is target scoped.
text = read('internal/steward/observed.go')
text = text.replace('items = append(items, map[string]any{"id": route.ID, "category": category, "severity": driftSeverity(category), "desired": "enabled", "observed": observedValue})', 'items = append(items, map[string]any{"id": route.ID, "category": category, "severity": driftSeverity(category), "desired": "enabled", "observed": observedValue, "observed_at": value.AuditedAt})')
old = '''\tfingerprint, err := canonicalFingerprint(state)\n\tif err != nil {\n\t\treturn nil, err\n\t}\n\tbyID := map[string]renderManifestEntry{}\n'''
if old not in text:
    raise RuntimeError('observed.go: canonical fingerprint block not found')
text = text.replace(old, '\tbyID := map[string]renderManifestEntry{}\n', 1)
old = '''\tfor _, target := range targets {\n\t\tcategory := "in-sync"\n\t\tentry, ok := byID[target.ID]\n\t\tif !ok || entry.InputFingerprint != fingerprint || filepath.Base(entry.FileName) != entry.FileName {\n'''
new = '''\tfor _, target := range targets {\n\t\tfingerprint, err := clientTargetFingerprint(state, target.ID)\n\t\tif err != nil {\n\t\t\treturn nil, err\n\t\t}\n\t\tcategory := "in-sync"\n\t\tentry, ok := byID[target.ID]\n\t\tif !ok || entry.InputFingerprint != fingerprint || filepath.Base(entry.FileName) != entry.FileName {\n'''
if old not in text:
    raise RuntimeError('observed.go: client loop not found')
text = text.replace(old, new, 1)
write('internal/steward/observed.go', text)

# New recovery archives omit regenerable observation cache; legacy archives remain readable.
replace('internal/steward/recovery.go', '''\tobserved := filepath.Join(state.PrivateDir, "observed.json")\n\tif regularFile(observed) {\n\t\tif err := copyOne(observed, "private/observed.json"); err != nil {\n\t\t\treturn "", err\n\t\t}\n\t}\n''', '')

# Host setup becomes a narrow one-time RST baseline instead of a general VPS tuning bundle.
write('server/base-setup.sh', r'''#!/usr/bin/env bash
set -euo pipefail

SOURCE_DIR=${1:-/tmp/route-steward/config}
MARKER=/var/lib/route-steward/host-prepared-v1

if [[ -f "$MARKER" ]]; then
  printf 'BASE_SETUP_OK\n'
  printf 'HOST_PREPARATION=existing\n'
  exit 0
fi

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends \
  ca-certificates \
  curl \
  jq \
  openssl \
  ufw \
  nftables

install -D -m 0644 "$SOURCE_DIR/99-route-steward-ssh.conf" /etc/ssh/sshd_config.d/99-route-steward-ssh.conf
install -d -m 0755 /run/sshd
sshd -t
systemctl reload ssh.service

sed -i 's/^IPV6=.*/IPV6=yes/' /etc/default/ufw
ufw default deny incoming >/dev/null
ufw default allow outgoing >/dev/null
ufw limit 22/tcp comment 'RST SSH key-only' >/dev/null
ufw --force enable >/dev/null

install -d -m 0755 /var/lib/route-steward
touch "$MARKER"
chmod 0644 "$MARKER"

printf 'BASE_SETUP_OK\n'
printf 'HOST_PREPARATION=created\n'
ufw status verbose
''')
# Remove no-longer-owned general host tuning files.
for path in [
    'server/config/99-route-steward-sysctl.conf',
    'server/config/bbr.conf',
    'server/config/99-limits.conf',
    'server/config/52unattended-upgrades-local',
]:
    p = ROOT / path
    if p.exists():
        p.unlink()

# Uninstall no longer pretends to own removed global tuning files.
text = read('server/uninstall.sh')
text = re.sub(r'\n# Remove only host policy files installed under RST-owned names\.[\s\S]*?rm -f /etc/apt/apt\.conf\.d/52-route-steward-unattended-upgrades\n', '\n', text, count=1)
write('server/uninstall.sh', text)

# Static tests keep independent safety properties, not deleted implementation shape.
text = read('scripts/Test-Templates.ps1')
for line in [
    "Require-Text 'server/base-setup.sh' '/etc/modules-load.d/route-steward-bbr\\.conf' 'Modules-load policy is not installed under a RST-owned name.'\n",
    "Require-Text 'server/base-setup.sh' '/etc/systemd/journald\\.conf\\.d/99-route-steward\\.conf' 'Journald policy is not installed under a RST-owned name.'\n",
    "Require-Text 'server/base-setup.sh' '/etc/apt/apt\\.conf\\.d/52-route-steward-unattended-upgrades' 'Unattended-upgrades policy is not installed under a RST-owned name.'\n",
    "Require-Text 'server/uninstall.sh' '/etc/modules-load.d/route-steward-bbr\\.conf' 'Uninstall does not target the RST-owned modules-load policy.'\n",
    "Require-Text 'server/uninstall.sh' '/etc/systemd/journald\\.conf\\.d/99-route-steward\\.conf' 'Uninstall does not target the RST-owned journald policy.'\n",
    "Require-Text 'server/uninstall.sh' '/etc/apt/apt\\.conf\\.d/52-route-steward-unattended-upgrades' 'Uninstall does not target the RST-owned unattended-upgrades policy.'\n",
]:
    text = text.replace(line, '')
write('scripts/Test-Templates.ps1', text)

# Targeted regressions for the state boundary and staging behavior.
path = 'internal/steward/profile_routing_test.go'
text = read(path)
insert = r'''
func TestSchemaOneExplicitEmptyRouteSelectionStaysEmpty(t *testing.T) {
	var inv Inventory
	raw := []byte(`{"schema":1,"profiles":[{"id":"empty","include_routes":[],"include_providers":[]}]}`)
	if err := json.Unmarshal(raw, &inv); err != nil {
		t.Fatal(err)
	}
	if len(inv.Profiles) != 1 || inv.Profiles[0].IncludeRoutes == nil || len(inv.Profiles[0].IncludeRoutes) != 0 {
		t.Fatalf("explicit empty route selection changed during upgrade: %#v", inv.Profiles)
	}
}

func TestSchemaTwoRoutingRejectsLegacyOrUnknownFields(t *testing.T) {
	for _, raw := range []string{
		`{"rules":[],"china_direct":true}`,
		`{"rules":[{"match":{"type":"geoip","value":"US","extra":true},"action":{"type":"direct"}}]}`,
	} {
		var routing ProfileRouting
		if err := json.Unmarshal([]byte(raw), &routing); err == nil {
			t.Fatalf("schema-2 routing accepted unknown field: %s", raw)
		}
	}
}
'''
text += insert
write(path, text)

# Documentation follows the product boundary without changing AGENTS.md.
for path in ['ARCHITECTURE.md', 'OPERATIONS.md', 'docs/QUICKSTART.md', '.agents/skills/route-steward/SKILL.md']:
    text = read(path)
    text = text.replace('China/service routing', 'ordered generic routing')
    text = text.replace('swap/fstab, SMTP egress, SSH, sysctl, journald, packages, unattended upgrades, and vnstat', 'RST-required packages, SSH key-only policy, and UFW baseline')
    text = text.replace('UFW, swap/fstab, SMTP egress, SSH, sysctl, journald, packages, unattended upgrades, and vnstat', 'RST-required packages, SSH key-only policy, and UFW baseline')
    text = text.replace('Initial host preparation also has the global effects documented in [Operations](OPERATIONS.md#remote-ownership), so supported hosts are dedicated and rebuildable.', 'Initial host preparation installs only the RST-required package, SSH, and firewall baseline documented in [Operations](OPERATIONS.md#remote-ownership). Supported hosts remain dedicated and rebuildable until broader host sharing is proven.')
    text = text.replace('Bootstrap creates empty schema-1 inventory', 'Bootstrap creates empty schema-2 inventory')
    write(path, text)

# Architecture state compatibility text was stale after schema 2.
text = read('ARCHITECTURE.md')
text = text.replace('Inventory schema `1` stores Servers, Links, Routes, Providers, Profiles, and ClientTargets. It also accepts legacy Profile policy fields from earlier schema-1 releases.', 'Inventory schema `2` stores Servers, Links, Routes, Providers, Profiles, and ClientTargets. Schema-1 state is translated at load time; legacy policy and China/service fields are not current state.')
text = text.replace('validates schema-1 state', 'validates current state')
write('ARCHITECTURE.md', text)

# Recovery docs should not claim observation cache is part of new archives.
text = read('.agents/skills/route-steward/references/migration-recovery.md')
text = text.replace('resets observed evidence', 'recreates empty observed evidence')
write('.agents/skills/route-steward/references/migration-recovery.md', text)

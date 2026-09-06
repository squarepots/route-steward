package steward

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

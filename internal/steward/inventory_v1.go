package steward

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type inventoryV1 struct {
	Schema        int            `json:"schema"`
	Metadata      Metadata       `json:"metadata"`
	Delivery      Delivery       `json:"delivery"`
	Servers       []Server       `json:"servers"`
	Links         []Link         `json:"links"`
	Routes        []Route        `json:"routes"`
	Providers     []Provider     `json:"providers"`
	Policies      []policyV1     `json:"policies"`
	Profiles      []profileV1    `json:"profiles"`
	ClientTargets []ClientTarget `json:"client_targets"`
}

type policyV1 struct {
	ID string `json:"id"`
}
type profileV1 struct {
	ID               string            `json:"id"`
	Policy           string            `json:"policy,omitempty"`
	IncludeRoutes    []string          `json:"include_routes"`
	IncludeProviders []string          `json:"include_providers"`
	Routing          *profileRoutingV1 `json:"routing,omitempty"`
}
type profileRoutingV1 struct {
	ChinaDirect   bool                    `json:"china_direct"`
	ServiceRoutes []profileServiceRouteV1 `json:"service_routes,omitempty"`
}
type profileServiceRouteV1 struct {
	Service string `json:"service"`
	Route   string `json:"route"`
}

func (inv *Inventory) UnmarshalJSON(data []byte) error {
	decoded, err := decodeInventory(data)
	if err != nil {
		return err
	}
	*inv = *decoded
	return nil
}

func decodeInventory(data []byte) (*Inventory, error) {
	var header struct {
		Schema int `json:"schema"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return nil, err
	}
	switch header.Schema {
	case InventorySchema:
		type current Inventory
		var inv current
		if err := json.Unmarshal(data, &inv); err != nil {
			return nil, err
		}
		return (*Inventory)(&inv), nil
	case 1:
		return upgradeInventoryV1(data)
	default:
		return nil, fmt.Errorf("inventory schema %d is unsupported", header.Schema)
	}
}

func upgradeInventoryV1(data []byte) (*Inventory, error) {
	var old inventoryV1
	if err := json.Unmarshal(data, &old); err != nil {
		return nil, err
	}
	if old.Schema != 1 {
		return nil, errors.New("legacy inventory schema must be 1")
	}
	policySet := map[string]bool{}
	for _, policy := range old.Policies {
		if policy.ID != "" {
			policySet[policy.ID] = true
		}
	}
	profiles := make([]Profile, 0, len(old.Profiles))
	for _, profile := range old.Profiles {
		if profile.Policy != "" && !policySet[profile.Policy] && !legacyPolicyIDV1(profile.Policy) {
			return nil, fmt.Errorf("Profile %q references an unknown Policy", profile.ID)
		}
		routing, err := upgradeProfileRoutingV1(profile)
		if err != nil {
			return nil, fmt.Errorf("Profile %q has invalid legacy routing: %w", profile.ID, err)
		}
		var includeRoutes []string
		if profile.IncludeRoutes == nil {
			includeRoutes = []string{"*"}
		} else {
			includeRoutes = append([]string{}, profile.IncludeRoutes...)
		}
		var includeProviders []string
		if profile.IncludeProviders == nil {
			includeProviders = []string{}
		} else {
			includeProviders = append([]string{}, profile.IncludeProviders...)
		}
		profiles = append(profiles, Profile{ID: profile.ID, IncludeRoutes: includeRoutes, IncludeProviders: includeProviders, Routing: routing})
	}
	return &Inventory{Schema: InventorySchema, Metadata: old.Metadata, Delivery: old.Delivery, Servers: old.Servers, Links: old.Links, Routes: old.Routes, Providers: old.Providers, Profiles: profiles, ClientTargets: old.ClientTargets}, nil
}

func upgradeProfileRoutingV1(profile profileV1) (*ProfileRouting, error) {
	routing := &ProfileRouting{Rules: []ProfileRoutingRule{}}
	if profile.Routing != nil {
		for _, binding := range profile.Routing.ServiceRoutes {
			service := strings.ToLower(strings.TrimSpace(binding.Service))
			if service != "openai" && service != "youtube" {
				return nil, fmt.Errorf("unsupported legacy service %q", binding.Service)
			}
			routing.Rules = append(routing.Rules, ProfileRoutingRule{Match: ProfileRoutingMatch{Type: "geosite", Value: service}, Action: ProfileRoutingAction{Type: "route", Route: strings.TrimSpace(binding.Route)}})
		}
		if profile.Routing.ChinaDirect {
			routing.Rules = append(routing.Rules, legacyChinaDirectRules()...)
		}
		return normalizeProfileRouting(routing)
	}
	if strings.EqualFold(strings.TrimSpace(profile.Policy), "balanced-cn") {
		routing.Rules = append(routing.Rules, legacyChinaDirectRules()...)
	}
	return normalizeProfileRouting(routing)
}

func legacyChinaDirectRules() []ProfileRoutingRule {
	return []ProfileRoutingRule{
		{Match: ProfileRoutingMatch{Type: "domain_suffix", Value: "cn"}, Action: ProfileRoutingAction{Type: "direct"}},
		{Match: ProfileRoutingMatch{Type: "geosite", Value: "CN"}, Action: ProfileRoutingAction{Type: "direct"}},
		{Match: ProfileRoutingMatch{Type: "geoip", Value: "CN"}, Action: ProfileRoutingAction{Type: "direct"}},
	}
}
func legacyPolicyIDV1(policy string) bool {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case "privacy", "balanced-cn":
		return true
	default:
		return false
	}
}

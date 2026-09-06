package steward

func SanitizedTargetContext(inv *Inventory, target string) (map[string]any, string) {
	full := SanitizedContext(inv)
	type match struct {
		kind string
		key  string
		item map[string]any
	}
	matches := []match{}
	for _, candidate := range []struct {
		kind string
		key  string
	}{
		{"server", "servers"},
		{"link", "links"},
		{"route", "routes"},
		{"provider", "providers"},
		{"profile", "profiles"},
		{"client_target", "client_targets"},
	} {
		if item := sanitizedItemByID(full[candidate.key], target); item != nil {
			matches = append(matches, match{kind: candidate.kind, key: candidate.key, item: item})
		}
	}
	if len(matches) == 0 {
		return map[string]any{"target": target}, "context-target-not-found"
	}
	if len(matches) > 1 {
		kinds := make([]string, 0, len(matches))
		for _, item := range matches {
			kinds = append(kinds, item.kind)
		}
		return map[string]any{"target": target, "candidates": sortedUnique(kinds)}, "context-target-ambiguous"
	}

	selected := matches[0]
	result := map[string]any{"schema_version": 1, "kind": selected.kind, selected.kind: selected.item}
	switch selected.kind {
	case "server":
		links, routes := []string{}, []string{}
		for _, link := range inv.Links {
			if link.EntryServer == target || link.ExitServer == target {
				links = append(links, link.ID)
			}
		}
		for _, route := range inv.Routes {
			if route.EntryServer == target || route.ExitServer == target {
				routes = append(routes, route.ID)
			}
		}
		result["links"] = sortedUnique(links)
		result["routes"] = sortedUnique(routes)
	case "link":
		routes := []string{}
		for _, route := range inv.Routes {
			if route.Link != nil && *route.Link == target {
				routes = append(routes, route.ID)
			}
		}
		result["routes"] = sortedUnique(routes)
	case "route":
		profiles, clientTargets := []string{}, []string{}
		for _, profile := range inv.Profiles {
			if selectionIncludes(profile.IncludeRoutes, target) {
				profiles = append(profiles, profile.ID)
			}
		}
		for _, clientTarget := range inv.ClientTargets {
			if clientTarget.Renderer == "hysteria2" && clientTarget.Route == target {
				clientTargets = append(clientTargets, clientTarget.ID)
				continue
			}
			profile := findProfile(inv, clientTarget.Profile)
			if profile != nil && selectionIncludes(profile.IncludeRoutes, target) {
				clientTargets = append(clientTargets, clientTarget.ID)
			}
		}
		result["profiles"] = sortedUnique(profiles)
		result["client_targets"] = sortedUnique(clientTargets)
	case "provider":
		profiles := []string{}
		for _, profile := range inv.Profiles {
			if selectionIncludes(profile.IncludeProviders, target) {
				profiles = append(profiles, profile.ID)
			}
		}
		result["profiles"] = sortedUnique(profiles)
	case "profile":
		clientTargets := []string{}
		for _, clientTarget := range inv.ClientTargets {
			if clientTarget.Profile == target {
				clientTargets = append(clientTargets, clientTarget.ID)
			}
		}
		result["client_targets"] = sortedUnique(clientTargets)
	}
	return result, ""
}

func sanitizedItemByID(value any, id string) map[string]any {
	items, ok := value.([]map[string]any)
	if !ok {
		return nil
	}
	for _, item := range items {
		if itemID, _ := item["id"].(string); itemID == id {
			return item
		}
	}
	return nil
}

func selectionIncludes(values []string, id string) bool {
	for _, value := range values {
		if value == "*" || value == id {
			return true
		}
	}
	return false
}

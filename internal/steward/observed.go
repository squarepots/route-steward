package steward

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

var driftCategories = map[string]bool{"in-sync": true, "service-missing": true, "remote-config-mismatch": true, "firewall-network-mismatch": true, "wireguard-link-mismatch": true, "hysteria-listener-mismatch": true, "certificate-mismatch": true, "egress-mismatch": true, "undetermined": true}

func ReadObserved(privateDir string, allowMissing bool) (*ObservedState, error) {
	path := filepath.Join(privateDir, "observed.json")
	if !regularFile(path) {
		if allowMissing {
			value := emptyObserved()
			return &value, nil
		}
		return nil, errors.New("observed state was not found")
	}
	var observed ObservedState
	if err := readJSON(path, &observed); err != nil {
		return nil, err
	}
	if observed.Schema != 1 {
		return nil, errors.New("observed state schema must be 1")
	}
	return &observed, nil
}

func SetObservedRoute(state *State, routeID, status, category, actualIPv4, hysteriaVersion, wireguardVersion string) (ObservedRoute, error) {
	route := findRoute(state.Inventory, routeID)
	if route == nil {
		return ObservedRoute{}, fmt.Errorf("unknown Route %q", routeID)
	}
	if category == "" {
		if status == "healthy" {
			category = "in-sync"
		} else if status == "mismatch" {
			category = "egress-mismatch"
		} else {
			category = "undetermined"
		}
	}
	if !driftCategories[category] {
		category = "undetermined"
	}
	observed, err := ReadObserved(state.PrivateDir, true)
	if err != nil {
		return ObservedRoute{}, err
	}
	routeMap := map[string]ObservedRoute{}
	for _, item := range observed.Routes {
		routeMap[item.ID] = item
	}
	entry := ObservedRoute{ID: route.ID, AuditStatus: status, Category: category, AuditedAt: utcNow(), ActualEgressIPv4: stringPointer(actualIPv4), HysteriaVersion: stringPointer(hysteriaVersion), WireGuardVersion: stringPointer(wireguardVersion)}
	if previous, ok := routeMap[route.ID]; ok {
		entry.Health = previous.Health
	}
	routeMap[route.ID] = entry
	observed.Routes = []ObservedRoute{}
	ids := []string{}
	for _, item := range state.Inventory.Routes {
		if _, ok := routeMap[item.ID]; ok {
			ids = append(ids, item.ID)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		observed.Routes = append(observed.Routes, routeMap[id])
	}
	serverMap := map[string]ObservedObject{}
	for _, item := range observed.Servers {
		serverMap[item.ID] = item
	}
	serverStatus := "unknown"
	if status == "healthy" {
		serverStatus = "reachable"
	} else if category == "service-missing" {
		serverStatus = "unhealthy"
	}
	for _, id := range []string{route.EntryServer, route.ExitServer} {
		serverMap[id] = ObservedObject{ID: id, AuditStatus: serverStatus, AuditedAt: utcNow()}
	}
	observed.Servers = []ObservedObject{}
	for _, server := range state.Inventory.Servers {
		if item, ok := serverMap[server.ID]; ok {
			observed.Servers = append(observed.Servers, item)
		}
	}
	if route.Kind == "relay" && route.Link != nil {
		linkMap := map[string]ObservedObject{}
		for _, item := range observed.Links {
			linkMap[item.ID] = item
		}
		linkStatus := "unknown"
		if status == "healthy" {
			linkStatus = "healthy"
		} else if category == "wireguard-link-mismatch" {
			linkStatus = "mismatch"
		}
		linkMap[*route.Link] = ObservedObject{ID: *route.Link, AuditStatus: linkStatus, AuditedAt: utcNow()}
		observed.Links = []ObservedObject{}
		for _, link := range state.Inventory.Links {
			if item, ok := linkMap[link.ID]; ok {
				observed.Links = append(observed.Links, item)
			}
		}
	}
	now := utcNow()
	observed.GeneratedAt = &now
	if err := writeJSONAtomic(filepath.Join(state.PrivateDir, "observed.json"), observed); err != nil {
		return ObservedRoute{}, err
	}
	return entry, nil
}

func SetObservedHealth(state *State, result HealthResult) (ObservedHealth, error) {
	route := findRoute(state.Inventory, result.Route)
	if route == nil {
		return ObservedHealth{}, fmt.Errorf("unknown Route %q", result.Route)
	}
	observed, err := ReadObserved(state.PrivateDir, true)
	if err != nil {
		return ObservedHealth{}, err
	}
	checks := map[string]string{}
	for _, check := range result.Checks {
		checks[check.Name] = check.Status
	}
	health := ObservedHealth{Status: result.Status, CheckedAt: result.CheckedAt, LatencyMS: result.LatencyMS, Checks: checks}
	routeMap := map[string]ObservedRoute{}
	for _, item := range observed.Routes {
		routeMap[item.ID] = item
	}
	entry, ok := routeMap[route.ID]
	if !ok {
		entry = ObservedRoute{ID: route.ID, AuditStatus: "undetermined", Category: "undetermined", AuditedAt: result.CheckedAt}
	}
	entry.Health = &health
	routeMap[route.ID] = entry
	observed.Routes = []ObservedRoute{}
	ids := []string{}
	for _, item := range state.Inventory.Routes {
		if _, ok := routeMap[item.ID]; ok {
			ids = append(ids, item.ID)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		observed.Routes = append(observed.Routes, routeMap[id])
	}
	now := utcNow()
	observed.GeneratedAt = &now
	if err := writeJSONAtomic(filepath.Join(state.PrivateDir, "observed.json"), observed); err != nil {
		return ObservedHealth{}, err
	}
	return health, nil
}

func DriftReport(state *State) (map[string]any, error) {
	observed, err := ReadObserved(state.PrivateDir, true)
	if err != nil {
		return nil, err
	}
	byRoute := map[string]ObservedRoute{}
	for _, item := range observed.Routes {
		byRoute[item.ID] = item
	}
	routes := append([]Route(nil), state.Inventory.Routes...)
	sort.Slice(routes, func(i, j int) bool { return routes[i].Order < routes[j].Order })
	items := []map[string]any{}
	for _, route := range routes {
		if !route.Enabled {
			observedValue := "not-observed"
			if value, ok := byRoute[route.ID]; ok {
				observedValue = value.AuditStatus
			}
			items = append(items, map[string]any{"id": route.ID, "category": "disabled", "severity": "info", "desired": "disabled", "observed": observedValue})
			continue
		}
		value, ok := byRoute[route.ID]
		if !ok {
			items = append(items, map[string]any{"id": route.ID, "category": "never-audited", "severity": "warning", "desired": "enabled", "observed": "not-observed"})
			continue
		}
		category := value.Category
		if category == "" {
			if value.AuditStatus == "healthy" {
				category = "in-sync"
			} else if value.AuditStatus == "mismatch" {
				category = "egress-mismatch"
			} else {
				category = "undetermined"
			}
		}
		if !driftCategories[category] {
			category = "undetermined"
		}
		observedValue := "drifted"
		if category == "in-sync" {
			observedValue = "healthy"
		} else if category == "undetermined" {
			observedValue = "undetermined"
		}
		items = append(items, map[string]any{"id": route.ID, "category": category, "severity": driftSeverity(category), "desired": "enabled", "observed": observedValue, "observed_at": value.AuditedAt})
	}
	clientItems, err := clientRenderDrift(state)
	if err != nil {
		return nil, err
	}
	items = append(items, clientItems...)
	publicationItems, err := subscriptionPublicationDrift(state)
	if err != nil {
		return nil, err
	}
	items = append(items, publicationItems...)
	errorsCount, warnings := 0, 0
	for _, item := range items {
		if item["severity"] == "error" {
			errorsCount++
		} else if item["severity"] == "warning" {
			warnings++
		}
	}
	return map[string]any{"schema_version": 1, "generated_at": utcNow(), "drifted": errorsCount > 0 || warnings > 0, "summary": map[string]int{"errors": errorsCount, "warnings": warnings, "routes": len(state.Inventory.Routes), "client_targets": len(state.Inventory.ClientTargets)}, "items": items}, nil
}

func clientRenderDrift(state *State) ([]map[string]any, error) {
	manifest := renderManifest{Schema: 1, Targets: []renderManifestEntry{}}
	manifestPath := filepath.Join(state.Inventory.Delivery.Directory, "client-render-manifest.json")
	if regularFile(manifestPath) {
		if err := readJSON(manifestPath, &manifest); err != nil {
			return nil, err
		}
	}
	byID := map[string]renderManifestEntry{}
	for _, entry := range manifest.Targets {
		byID[entry.ID] = entry
	}
	targets := append([]ClientTarget(nil), state.Inventory.ClientTargets...)
	sort.Slice(targets, func(i, j int) bool { return targets[i].ID < targets[j].ID })
	out := []map[string]any{}
	for _, target := range targets {
		fingerprint, err := clientTargetFingerprint(state, target.ID)
		if err != nil {
			return nil, err
		}
		category := "in-sync"
		entry, ok := byID[target.ID]
		if !ok || entry.InputFingerprint != fingerprint || filepath.Base(entry.FileName) != entry.FileName {
			category = "client-render-stale"
		} else {
			path := filepath.Join(state.Inventory.Delivery.Directory, entry.FileName)
			if _, err := os.Stat(path); err != nil {
				category = "client-render-stale"
			} else if sum, err := sha256File(path); err != nil || sum != entry.OutputSHA256 {
				category = "client-render-stale"
			}
		}
		severity, observedValue := "info", "current"
		if category != "in-sync" {
			severity, observedValue = "warning", "stale-or-missing"
		}
		out = append(out, map[string]any{"id": "client-target:" + target.ID, "target": target.ID, "category": category, "severity": severity, "desired": "current-with-canonical-state", "observed": observedValue})
	}
	return out, nil
}

func driftSeverity(category string) string {
	if category == "in-sync" || category == "disabled" {
		return "info"
	}
	if category == "never-audited" || category == "undetermined" || category == "client-render-stale" {
		return "warning"
	}
	return "error"
}

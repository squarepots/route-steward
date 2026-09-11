package steward

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

func subscriptionInputFingerprint(state *State, targetID string) (string, error) {
	target := findClientTarget(state.Inventory, targetID)
	if !subscriptionCapableTarget(target) {
		return "", fmt.Errorf("ClientTarget %q does not support private subscription delivery", targetID)
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

	return hex.EncodeToString(hash.Sum(nil)), nil
}

func subscriptionBodySHA256(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

func subscriptionPublicationDrift(state *State) ([]map[string]any, error) {
	targets := append([]ClientTarget(nil), state.Inventory.ClientTargets...)
	sort.Slice(targets, func(i, j int) bool { return targets[i].ID < targets[j].ID })
	out := []map[string]any{}
	for _, target := range targets {
		if target.Delivery != "subscription" || !subscriptionCapableTarget(&target) {
			continue
		}
		subscription, err := readSubscriptionState(state, target.ID)
		if err != nil {
			return nil, err
		}
		fingerprint, err := subscriptionInputFingerprint(state, target.ID)
		if err != nil {
			return nil, err
		}

		category, severity, observed := "subscription-publication-current", "info", "current"
		if subscription.PublishedInputFingerprint == "" || subscription.LastPublishedAt == nil {
			category, severity, observed = "subscription-publication-unknown", "warning", "unknown"
		} else if subscription.PublishedInputFingerprint != fingerprint {
			category, severity, observed = "subscription-publication-stale", "warning", "stale"
		}
		item := map[string]any{
			"id":           "subscription:" + target.ID,
			"target":       target.ID,
			"state_domain": "publication",
			"category":     category,
			"severity":     severity,
			"desired":      "current-with-canonical-state",
			"observed":     observed,
		}
		if subscription.LastPublishedAt != nil {
			item["observed_at"] = *subscription.LastPublishedAt
		}
		out = append(out, item)

		refreshCurrent, err := subscriptionClientRefreshCurrent(state, target, subscription)
		if err != nil {
			return nil, err
		}
		refreshCategory, refreshSeverity, refreshObserved := "subscription-client-refresh-current", "info", "current"
		if !refreshCurrent {
			refreshCategory, refreshSeverity, refreshObserved = "subscription-client-refresh-stale", "warning", "stale-or-missing"
		}
		out = append(out, map[string]any{
			"id":           "subscription-client-refresh:" + target.ID,
			"target":       target.ID,
			"state_domain": "client_refresh",
			"category":     refreshCategory,
			"severity":     refreshSeverity,
			"desired":      "current-with-subscription-state",
			"observed":     refreshObserved,
		})
	}
	return out, nil
}

func subscriptionClientRefreshCurrent(state *State, target ClientTarget, subscription *subscriptionState) (bool, error) {
	url := subscriptionEndpoint(subscription, subscription.Token)
	if target.Renderer == "mihomo" {
		data, err := os.ReadFile(filepath.Join(state.Inventory.Delivery.Directory, target.ID+".subscription.txt"))
		if os.IsNotExist(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return string(data) == url+"\n", nil
	}
	data, err := os.ReadFile(filepath.Join(state.Inventory.Delivery.Directory, target.ID+".html"))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	encodedURL := base64.RawURLEncoding.EncodeToString([]byte(url))
	encodedImport := base64.StdEncoding.EncodeToString([]byte("sub://" + encodedURL))
	return bytes.Contains(data, []byte(encodedImport)), nil
}

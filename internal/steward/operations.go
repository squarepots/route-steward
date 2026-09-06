package steward

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var displayNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func AddServer(state *State, context map[string]any) (map[string]any, error) {
	id, err := convertID(stringField(context, "server_id"))
	if err != nil {
		return nil, err
	}
	if findServer(state.Inventory, id) != nil {
		return nil, fmt.Errorf("Server %q already exists", id)
	}
	publicV4 := stringField(context, "public_ipv4")
	if ip := net.ParseIP(publicV4); ip == nil || ip.To4() == nil {
		return nil, errors.New("public_ipv4 is invalid")
	}
	publicV6 := stringField(context, "public_ipv6")
	if publicV6 != "" {
		if ip := net.ParseIP(publicV6); ip == nil || ip.To4() != nil {
			return nil, errors.New("public_ipv6 is invalid")
		}
	}
	privateV4 := stringField(context, "private_ipv4")
	if privateV4 != "" {
		if ip := net.ParseIP(privateV4); ip == nil || ip.To4() == nil {
			return nil, errors.New("private_ipv4 is invalid")
		}
	}
	sshUser := stringField(context, "ssh_user")
	if !unixUserPattern.MatchString(sshUser) {
		return nil, errors.New("ssh_user must be a lowercase Unix account name with at most 32 characters")
	}
	if stringField(context, "host_ownership") != "dedicated" {
		return nil, errors.New("host_ownership must be dedicated")
	}
	keyPath, err := filepath.Abs(stringField(context, "ssh_key_path"))
	if err != nil {
		return nil, err
	}
	expected4 := stringField(context, "expected_egress_ipv4")
	if expected4 == "" {
		expected4 = publicV4
	}
	server := Server{
		ID: id, Provider: defaultString(stringField(context, "provider"), "byo"), AccountLabel: defaultString(stringField(context, "account_label"), "personal"), InstanceName: defaultString(stringField(context, "instance_name"), id), Region: defaultString(stringField(context, "region"), "unknown"), Zone: stringField(context, "zone"), OS: defaultString(stringField(context, "os"), "ubuntu-24.04"), Architecture: defaultString(stringField(context, "architecture"), "x86_64"), Roles: stringSliceField(context, "roles", []string{"entry", "exit"}),
		Compute:  Compute{Driver: "byo-ssh", HostOwnership: "dedicated"},
		Network:  Network{PublicIPv4: publicV4, IPv4Type: "static", PrivateIPv4: stringPointer(privateV4), PublicIPv6: stringPointer(publicV6), ExpectedEgressIPv4: expected4, ExpectedEgressIPv6: stringPointer(stringField(context, "expected_egress_ipv6"))},
		SSH:      SSH{User: sshUser, KeyPath: keyPath, AllowedSources: stringSliceField(context, "ssh_allowed_sources", []string{"trusted"})},
		Firewall: Firewall{Profile: "pending", Rules: []FirewallRule{{Family: "dual", Protocol: "tcp", Port: 22, Source: "trusted"}}},
	}
	candidate := cloneInventory(state.Inventory)
	candidate.Servers = append(candidate.Servers, server)
	if err := saveCandidate(state, candidate, true); err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "compute_driver": "byo-ssh", "host_ownership": "dedicated", "state": "desired-only", "remote_changed": false}, nil
}

func AddLink(state *State, context map[string]any) (map[string]any, error) {
	id, err := convertID(stringField(context, "link_id"))
	if err != nil {
		return nil, err
	}
	entryID, exitID := stringField(context, "entry_server"), stringField(context, "exit_server")
	if findServer(state.Inventory, entryID) == nil || findServer(state.Inventory, exitID) == nil {
		return nil, errors.New("Link endpoints must reference existing Servers")
	}
	if entryID == exitID {
		return nil, errors.New("a Link requires different entry and exit Servers")
	}
	if findLink(state.Inventory, id) != nil {
		return nil, fmt.Errorf("Link %q already exists", id)
	}
	slot, err := nextLinkSlot(state.Inventory)
	if err != nil {
		return nil, err
	}
	link := Link{ID: id, Type: "wireguard", Driver: "wireguard", EntryServer: entryID, ExitServer: exitID, Slot: slot, Interface: fmt.Sprintf("wg-rst%02d", slot), ListenPort: 51819 + slot, Subnet: fmt.Sprintf("10.77.%d.0/30", slot), EntryAddress: fmt.Sprintf("10.77.%d.1/30", slot), ExitAddress: fmt.Sprintf("10.77.%d.2/30", slot), EndpointFamily: "ipv4", SecretRef: "link-key:" + id, Enabled: true}
	candidate := cloneInventory(state.Inventory)
	candidate.Links = append(candidate.Links, link)
	for i := range candidate.Servers {
		if candidate.Servers[i].ID == exitID {
			candidate.Servers[i].Firewall.Rules = append(candidate.Servers[i].Firewall.Rules, FirewallRule{Family: "ipv4", Protocol: "udp", Port: link.ListenPort, SourceServer: entryID})
		}
	}
	secret, err := newLinkSecret(id)
	if err != nil {
		return nil, err
	}
	relative := filepath.ToSlash(filepath.Join("managed-links", id, "keys.json"))
	secretPath := filepath.Join(state.PrivateDir, "secrets", filepath.FromSlash(relative))
	if err := createRegisteredSecret(state, candidate, link.SecretRef, "managed-link-key", relative, secretPath, secret); err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "driver": "wireguard", "state": "desired-only", "remote_changed": false}, nil
}

func AddRoute(state *State, context map[string]any) (map[string]any, error) {
	id, err := convertID(stringField(context, "route_id"))
	if err != nil {
		return nil, err
	}
	if findRoute(state.Inventory, id) != nil {
		return nil, fmt.Errorf("Route %q already exists", id)
	}
	kind := strings.ToLower(stringField(context, "kind"))
	if kind != "direct" && kind != "relay" {
		return nil, errors.New("Route kind must be direct or relay")
	}
	entryID := stringField(context, "entry_server")
	entry := findServer(state.Inventory, entryID)
	if entry == nil {
		return nil, fmt.Errorf("unknown entry Server %q", entryID)
	}
	exitID := entryID
	var linkID *string
	if kind == "relay" {
		exitID = stringField(context, "exit_server")
		if findServer(state.Inventory, exitID) == nil {
			return nil, fmt.Errorf("unknown exit Server %q", exitID)
		}
		value := stringField(context, "link_id")
		link := findLink(state.Inventory, value)
		if link == nil || link.EntryServer != entryID || link.ExitServer != exitID {
			return nil, errors.New("the Link does not match Route endpoints")
		}
		linkID = stringPointer(value)
	}
	defaultPort := 443
	if kind == "relay" {
		defaultPort = 8443
		for portUsedByEntry(state.Inventory, entryID, defaultPort) {
			defaultPort++
		}
	}
	hopping, err := parsePortHoppingRange(stringField(context, "port_hopping"))
	if err != nil {
		return nil, err
	}
	port := intField(context, "listen_port", defaultPort)
	if hopping != nil && !hasField(context, "listen_port") {
		port = hopping.StartPort
	}
	if port < 1 || port > 65535 {
		return nil, errors.New("listen_port is invalid")
	}
	if err := validatePortHopping(port, hopping); err != nil {
		return nil, err
	}
	portStart, portEnd := port, port
	if hopping != nil {
		portStart, portEnd = hopping.StartPort, hopping.EndPort
	}
	if portRangeUsedByEntry(state.Inventory, entryID, portStart, portEnd) {
		return nil, fmt.Errorf("Route %q reuses a Hysteria2 listener or port-hopping range on Server %q", id, entryID)
	}
	displayName := defaultString(stringField(context, "display_name"), id)
	if !displayNamePattern.MatchString(displayName) {
		return nil, errors.New("display_name must use ASCII letters, digits, dot, underscore, or dash")
	}
	route := Route{ID: id, DisplayName: displayName, Kind: kind, Ingress: Ingress{Driver: "hysteria2"}, EntryServer: entryID, ExitServer: exitID, Link: linkID, ListenPort: port, PortHopping: hopping, Enabled: false, Order: len(state.Inventory.Routes) + 1, AddressFamilies: []string{"ipv6", "ipv4"}, PayloadSecretRef: "route-payload:" + id, CredentialSecretRef: "route-credential:" + id, CredentialMode: "personal-pinned", State: "pending"}
	candidate := cloneInventory(state.Inventory)
	candidate.Routes = append(candidate.Routes, route)
	for i := range candidate.Servers {
		if candidate.Servers[i].ID == entryID {
			candidate.Servers[i].Firewall.Rules = append(candidate.Servers[i].Firewall.Rules, FirewallRule{Family: "dual", Protocol: "udp", Port: portStart, EndPort: portEnd, Source: "any"})
		}
	}
	bundle, err := newRouteBundle(id, displayName, entry.Network.PublicIPv4, deref(entry.Network.PublicIPv6), port, portHoppingText(hopping))
	if err != nil {
		return nil, err
	}
	managedRelative := filepath.ToSlash(filepath.Join("managed-routes", id))
	managedDirectory := filepath.Join(state.PrivateDir, "secrets", filepath.FromSlash(managedRelative))
	if err := createRouteSecrets(state, candidate, route, managedRelative, managedDirectory, bundle); err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "ingress_driver": "hysteria2", "state": "pending", "enabled": false, "remote_changed": false}, nil
}

func AddProvider(state *State, context map[string]any) (map[string]any, error) {
	id, err := convertID(stringField(context, "provider_id"))
	if err != nil {
		return nil, err
	}
	if findProvider(state.Inventory, id) != nil {
		return nil, fmt.Errorf("Provider %q already exists", id)
	}
	providerURL := stringField(context, "url")
	if !ValidateProviderURL(providerURL) {
		return nil, errors.New("Provider URL must be an absolute HTTP(S) URL without line breaks")
	}
	interval := intField(context, "interval_seconds", 86400)
	if interval < 3600 {
		return nil, errors.New("Provider refresh interval must be at least one hour")
	}
	provider := Provider{ID: id, DisplayName: defaultString(stringField(context, "display_name"), id), SourceType: "mihomo-http", SourceSecretRef: "provider:" + id, IntervalSeconds: interval, HealthCheck: false, Enabled: boolField(context, "enabled", true)}
	candidate := cloneInventory(state.Inventory)
	candidate.Providers = append(candidate.Providers, provider)
	relative := filepath.ToSlash(filepath.Join("providers", id+".url"))
	secretPath := filepath.Join(state.PrivateDir, "secrets", filepath.FromSlash(relative))
	if err := createRegisteredSecret(state, candidate, provider.SourceSecretRef, "url", relative, secretPath, []byte(providerURL+"\n")); err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "source_type": "mihomo-http", "enabled": provider.Enabled, "url_stored_as_secret": true, "remote_changed": false}, nil
}

func UpdateProvider(state *State, target string, context map[string]any) (map[string]any, error) {
	candidate := cloneInventory(state.Inventory)
	provider := findProvider(candidate, target)
	if provider == nil {
		return nil, fmt.Errorf("unknown Provider %q", target)
	}
	var oldURL []byte
	var secretPath string
	if hasField(context, "url") {
		value := stringField(context, "url")
		if !ValidateProviderURL(value) {
			return nil, errors.New("Provider URL must be an absolute HTTP(S) URL without line breaks")
		}
		secretPath, _ = ResolveSecret(provider.SourceSecretRef, state.PrivateDir, nil)
		oldURL, _ = os.ReadFile(secretPath)
		if err := writeFileAtomic(secretPath, []byte(value+"\n"), 0o600); err != nil {
			return nil, err
		}
	}
	if hasField(context, "display_name") {
		provider.DisplayName = stringField(context, "display_name")
	}
	if hasField(context, "interval_seconds") {
		interval := intField(context, "interval_seconds", provider.IntervalSeconds)
		if interval < 3600 {
			if secretPath != "" {
				_ = writeFileAtomic(secretPath, oldURL, 0o600)
			}
			return nil, errors.New("Provider refresh interval must be at least one hour")
		}
		provider.IntervalSeconds = interval
	}
	if hasField(context, "enabled") {
		provider.Enabled = boolField(context, "enabled", provider.Enabled)
	}
	provider.HealthCheck = false
	if err := saveCandidate(state, candidate, false); err != nil {
		if secretPath != "" {
			_ = writeFileAtomic(secretPath, oldURL, 0o600)
		}
		return nil, err
	}
	return map[string]any{"id": target, "enabled": provider.Enabled, "url_stored_as_secret": true, "remote_changed": false}, nil
}

func RemoveProvider(state *State, target string) (map[string]any, error) {
	oldInventory := cloneInventory(state.Inventory)
	provider := findProvider(state.Inventory, target)
	if provider == nil {
		return nil, fmt.Errorf("unknown Provider %q", target)
	}
	for _, p := range state.Inventory.Profiles {
		if contains(p.IncludeProviders, target) {
			return nil, fmt.Errorf("Provider %q is still referenced by a Profile", target)
		}
	}
	secretPath, err := ResolveSecret(provider.SourceSecretRef, state.PrivateDir, nil)
	if err != nil {
		return nil, err
	}
	secret, err := os.ReadFile(secretPath)
	if err != nil {
		return nil, err
	}
	index, err := ReadSecretIndex(state.PrivateDir)
	if err != nil {
		return nil, err
	}
	oldIndex := cloneIndex(index)
	delete(index.Refs, provider.SourceSecretRef)
	if err := writeJSONAtomic(filepath.Join(state.PrivateDir, "secrets", "index.json"), index); err != nil {
		return nil, err
	}
	candidate := cloneInventory(state.Inventory)
	next := candidate.Providers[:0]
	for _, p := range candidate.Providers {
		if p.ID != target {
			next = append(next, p)
		}
	}
	candidate.Providers = next
	if err := saveCandidate(state, candidate, true); err != nil {
		_ = writeJSONAtomic(filepath.Join(state.PrivateDir, "secrets", "index.json"), oldIndex)
		return nil, err
	}
	if err := os.Remove(secretPath); err != nil {
		rollbackErrors := []error{err}
		if restoreErr := writeFileAtomic(secretPath, secret, 0o600); restoreErr != nil {
			rollbackErrors = append(rollbackErrors, restoreErr)
		}
		if restoreErr := writeJSONAtomic(filepath.Join(state.PrivateDir, "secrets", "index.json"), oldIndex); restoreErr != nil {
			rollbackErrors = append(rollbackErrors, restoreErr)
		}
		if restoreErr := writeJSONAtomic(state.InventoryPath, oldInventory); restoreErr != nil {
			rollbackErrors = append(rollbackErrors, restoreErr)
		}
		state.Inventory = oldInventory
		return nil, errors.Join(rollbackErrors...)
	}
	return map[string]any{"id": target, "removed": true}, nil
}

func AddProfile(state *State, context map[string]any) (map[string]any, error) {
	id, err := convertID(stringField(context, "profile_id"))
	if err != nil {
		return nil, err
	}
	if findProfile(state.Inventory, id) != nil {
		return nil, fmt.Errorf("Profile %q already exists", id)
	}
	routing, hasRouting, err := profileRoutingFromContext(context)
	if err != nil {
		return nil, err
	}
	profile := Profile{ID: id, IncludeRoutes: stringSliceField(context, "include_routes", []string{"*"}), IncludeProviders: stringSliceField(context, "include_providers", []string{})}
	if hasRouting {
		profile.Routing = routing
	} else if !hasField(context, "policy") {
		profile.Routing = defaultProfileRouting()
	}
	candidate := cloneInventory(state.Inventory)
	candidate.Profiles = append(candidate.Profiles, profile)
	if err := saveCandidate(state, candidate, false); err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "role": "route-provider-routing-selection", "remote_changed": false}, nil
}

func UpdateProfile(state *State, target string, context map[string]any) (map[string]any, error) {
	candidate := cloneInventory(state.Inventory)
	profile := findProfile(candidate, target)
	if profile == nil {
		return nil, fmt.Errorf("unknown Profile %q", target)
	}
	if hasField(context, "include_routes") {
		profile.IncludeRoutes = stringSliceField(context, "include_routes", nil)
	}
	if hasField(context, "include_providers") {
		profile.IncludeProviders = stringSliceField(context, "include_providers", nil)
	}
	if hasField(context, "routing") || hasField(context, "policy") {
		routing, _, err := profileRoutingFromContext(context)
		if err != nil {
			return nil, err
		}
		profile.Routing = routing
	}
	if err := saveCandidate(state, candidate, false); err != nil {
		return nil, err
	}
	return map[string]any{"id": target, "remote_changed": false}, nil
}

func RemoveProfile(state *State, target string) (map[string]any, error) {
	if findProfile(state.Inventory, target) == nil {
		return nil, fmt.Errorf("unknown Profile %q", target)
	}
	for _, t := range state.Inventory.ClientTargets {
		if t.Profile == target {
			return nil, fmt.Errorf("Profile %q is still referenced by a ClientTarget", target)
		}
	}
	candidate := cloneInventory(state.Inventory)
	next := candidate.Profiles[:0]
	for _, p := range candidate.Profiles {
		if p.ID != target {
			next = append(next, p)
		}
	}
	candidate.Profiles = next
	if err := saveCandidate(state, candidate, false); err != nil {
		return nil, err
	}
	return map[string]any{"id": target, "removed": true}, nil
}

func AddClientTarget(state *State, context map[string]any) (map[string]any, error) {
	id, err := convertID(stringField(context, "target_id"))
	if err != nil {
		return nil, err
	}
	if findClientTarget(state.Inventory, id) != nil {
		return nil, fmt.Errorf("ClientTarget %q already exists", id)
	}
	profileID := stringField(context, "profile_id")
	if findProfile(state.Inventory, profileID) == nil {
		return nil, fmt.Errorf("unknown Profile %q", profileID)
	}
	renderer := stringField(context, "renderer")
	if renderer != "mihomo" && renderer != "karing" && renderer != "shadowrocket" && renderer != "hysteria2" {
		return nil, fmt.Errorf("unsupported ClientTarget renderer %q", renderer)
	}
	if hasField(context, "mihomo_process_names") && renderer != "mihomo" {
		return nil, errors.New("mihomo_process_names requires the mihomo renderer")
	}
	processNames, err := mihomoProcessNamesFromContext(context)
	if err != nil {
		return nil, err
	}
	delivery := "file"
	var qr json.RawMessage
	if renderer == "shadowrocket" {
		delivery = defaultString(stringField(context, "delivery"), "nodes")
		raw, ok := context["qr"]
		if ok {
			qr, _ = json.Marshal(raw)
		} else {
			qr = []byte(`{"default_mode":"batch","target_utf8_bytes":2400,"size_px":220,"max_viewport_height":40}`)
		}
	}
	target := ClientTarget{ID: id, Profile: profileID, Renderer: renderer, Delivery: delivery, QR: qr}
	if renderer == "mihomo" {
		target.MihomoProcessNames = processNames
	}
	if renderer == "hysteria2" {
		target.Route = stringField(context, "route_id")
		target.Listen = defaultString(stringField(context, "listen"), "127.0.0.1:1080")
		target.IngressFamily = defaultString(stringField(context, "ingress_family"), "auto")
	}
	candidate := cloneInventory(state.Inventory)
	candidate.ClientTargets = append(candidate.ClientTargets, target)
	if err := saveCandidate(state, candidate, false); err != nil {
		return nil, err
	}
	result := map[string]any{"id": id, "profile": profileID, "renderer": renderer, "delivery": delivery, "remote_changed": false}
	if renderer == "mihomo" {
		result["mihomo_process_name_count"] = len(target.MihomoProcessNames)
	}
	if renderer == "hysteria2" {
		result["route"] = target.Route
		result["listen"] = target.Listen
		result["ingress_family"] = target.IngressFamily
	}
	return result, nil
}

func UpdateClientTarget(state *State, target string, context map[string]any) (map[string]any, error) {
	candidate := cloneInventory(state.Inventory)
	t := findClientTarget(candidate, target)
	if t == nil {
		return nil, fmt.Errorf("unknown ClientTarget %q", target)
	}
	if hasField(context, "profile_id") {
		t.Profile = stringField(context, "profile_id")
	}
	if hasField(context, "delivery") {
		delivery := stringField(context, "delivery")
		if t.SubscriptionSecretRef != "" && delivery != "subscription" {
			return nil, errors.New("a subscription-backed ClientTarget must revoke its subscription state before changing delivery mode")
		}
		t.Delivery = delivery
	}
	if hasField(context, "mihomo_process_names") {
		if t.Renderer != "mihomo" {
			return nil, errors.New("mihomo_process_names requires the mihomo renderer")
		}
		processNames, err := mihomoProcessNamesFromContext(context)
		if err != nil {
			return nil, err
		}
		t.MihomoProcessNames = processNames
	}
	if t.Renderer == "hysteria2" {
		if hasField(context, "route_id") {
			t.Route = stringField(context, "route_id")
		}
		if hasField(context, "listen") {
			t.Listen = stringField(context, "listen")
		}
		if hasField(context, "ingress_family") {
			t.IngressFamily = stringField(context, "ingress_family")
		}
	}
	if err := saveCandidate(state, candidate, false); err != nil {
		return nil, err
	}
	result := map[string]any{"id": t.ID, "profile": t.Profile, "renderer": t.Renderer, "delivery": t.Delivery, "remote_changed": false}
	if t.Renderer == "mihomo" {
		result["mihomo_process_name_count"] = len(t.MihomoProcessNames)
	}
	if t.Renderer == "hysteria2" {
		result["route"] = t.Route
		result["listen"] = t.Listen
		result["ingress_family"] = t.IngressFamily
	}
	return result, nil
}

func RemoveClientTarget(state *State, target string) (map[string]any, error) {
	t := findClientTarget(state.Inventory, target)
	if t == nil {
		return nil, fmt.Errorf("unknown ClientTarget %q", target)
	}
	if t.SubscriptionSecretRef != "" {
		return nil, errors.New("revoke the ClientTarget subscription state before removing it")
	}
	candidate := cloneInventory(state.Inventory)
	next := candidate.ClientTargets[:0]
	for _, item := range candidate.ClientTargets {
		if item.ID != target {
			next = append(next, item)
		}
	}
	candidate.ClientTargets = next
	if err := saveCandidate(state, candidate, false); err != nil {
		return nil, err
	}
	return map[string]any{"id": target, "removed": true}, nil
}

type routeBundle struct {
	Credential  []byte
	Certificate []byte
	PrivateKey  []byte
	Payload     []byte
}

func newLinkSecret(id string) ([]byte, error) {
	curve := ecdh.X25519()
	makePair := func() (map[string]string, error) {
		key, err := curve.GenerateKey(rand.Reader)
		if err != nil {
			return nil, err
		}
		return map[string]string{"private_key": base64.StdEncoding.EncodeToString(key.Bytes()), "public_key": base64.StdEncoding.EncodeToString(key.PublicKey().Bytes())}, nil
	}
	entry, err := makePair()
	if err != nil {
		return nil, err
	}
	exit, err := makePair()
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(map[string]any{"schema": 1, "link_id": id, "created_at": utcNow(), "entry": entry, "exit": exit}, "", "  ")
}

func newRouteBundle(id, displayName, entryIPv4, entryIPv6 string, port int, portHopping string) (routeBundle, error) {
	authBytes := make([]byte, 32)
	obfsBytes := make([]byte, 32)
	if _, err := rand.Read(authBytes); err != nil {
		return routeBundle{}, err
	}
	if _, err := rand.Read(obfsBytes); err != nil {
		return routeBundle{}, err
	}
	auth := hex.EncodeToString(authBytes)
	obfs := hex.EncodeToString(obfsBytes)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return routeBundle{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return routeBundle{}, err
	}
	now := time.Now().UTC()
	notAfter := now.AddDate(10, 0, 0)
	ips := []net.IP{net.ParseIP(entryIPv4)}
	if entryIPv6 != "" {
		ips = append(ips, net.ParseIP(entryIPv6))
	}
	template := x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: entryIPv4}, NotBefore: now.Add(-5 * time.Minute), NotAfter: notAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IPAddresses: ips, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return routeBundle{}, err
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return routeBundle{}, err
	}
	fingerprint := sha256.Sum256(der)
	credential, err := json.MarshalIndent(map[string]any{"schema": 1, "hysteria": map[string]string{"auth": auth, "obfs": obfs}, "created_at": utcNow(), "certificate_not_after": notAfter.Format(time.RFC3339Nano)}, "", "  ")
	if err != nil {
		return routeBundle{}, err
	}
	var nodes strings.Builder
	writeNode := func(suffix, address string) {
		ports := ""
		if portHopping != "" {
			ports = fmt.Sprintf("    ports: '%s'\n", portHopping)
		}
		fmt.Fprintf(&nodes, "  - name: %s-HY2-%s\n    type: hysteria2\n    server: '%s'\n    port: %d\n%s    password: '%s'\n    sni: '%s'\n    skip-cert-verify: true\n    fingerprint: '%s'\n    alpn: [h3]\n    obfs: salamander\n    obfs-password: '%s'\n", displayName, suffix, address, port, ports, auth, entryIPv4, hex.EncodeToString(fingerprint[:]), obfs)
	}
	if entryIPv6 != "" {
		writeNode("v6", entryIPv6)
	}
	writeNode("v4", entryIPv4)
	payload := fmt.Sprintf("schema: 1\nname: '%s'\nproxies:\n%s", displayName, nodes.String())
	return routeBundle{Credential: append(credential, '\n'), Certificate: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), PrivateKey: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}), Payload: []byte(payload)}, nil
}

func createRegisteredSecret(state *State, candidate *Inventory, reference, kind, relative, path string, data []byte) error {
	if regularFile(path) {
		return fmt.Errorf("secret for %q already exists", reference)
	}
	index, err := ReadSecretIndex(state.PrivateDir)
	if err != nil {
		return err
	}
	if _, ok := index.Refs[reference]; ok {
		return fmt.Errorf("secret reference %q already exists", reference)
	}
	oldIndex := cloneIndex(index)
	if err := writeFileAtomic(path, data, 0o600); err != nil {
		return err
	}
	index.Refs[reference] = SecretRef{Type: kind, Path: filepath.ToSlash(relative)}
	if err := writeJSONAtomic(filepath.Join(state.PrivateDir, "secrets", "index.json"), index); err != nil {
		_ = os.Remove(path)
		return err
	}
	if err := saveCandidate(state, candidate, false); err != nil {
		_ = writeJSONAtomic(filepath.Join(state.PrivateDir, "secrets", "index.json"), oldIndex)
		_ = os.Remove(path)
		return err
	}
	return nil
}

func createRouteSecrets(state *State, candidate *Inventory, route Route, managedRelative, managedDirectory string, bundle routeBundle) error {
	if _, err := os.Stat(managedDirectory); err == nil {
		return fmt.Errorf("managed secret directory for %q already exists", route.ID)
	}
	parent := filepath.Dir(managedDirectory)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".rst-route-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for name, data := range map[string][]byte{"credentials.json": bundle.Credential, "server.crt": bundle.Certificate, "server.key": bundle.PrivateKey, "client-payload.yaml": bundle.Payload} {
		if err := writeFileAtomic(filepath.Join(stage, name), data, 0o600); err != nil {
			return err
		}
	}
	if err := protectPath(stage, true); err != nil {
		return err
	}
	index, err := ReadSecretIndex(state.PrivateDir)
	if err != nil {
		return err
	}
	if _, ok := index.Refs[route.PayloadSecretRef]; ok {
		return fmt.Errorf("secret reference %q already exists", route.PayloadSecretRef)
	}
	if _, ok := index.Refs[route.CredentialSecretRef]; ok {
		return fmt.Errorf("secret reference %q already exists", route.CredentialSecretRef)
	}
	oldIndex := cloneIndex(index)
	if err := os.Rename(stage, managedDirectory); err != nil {
		return err
	}
	index.Refs[route.PayloadSecretRef] = SecretRef{Type: "client-payload", Path: filepath.ToSlash(filepath.Join(managedRelative, "client-payload.yaml"))}
	index.Refs[route.CredentialSecretRef] = SecretRef{Type: "managed-route-credential", Path: filepath.ToSlash(filepath.Join(managedRelative, "credentials.json"))}
	if err := writeJSONAtomic(filepath.Join(state.PrivateDir, "secrets", "index.json"), index); err != nil {
		_ = os.RemoveAll(managedDirectory)
		return err
	}
	if err := saveCandidate(state, candidate, false); err != nil {
		_ = writeJSONAtomic(filepath.Join(state.PrivateDir, "secrets", "index.json"), oldIndex)
		_ = os.RemoveAll(managedDirectory)
		return err
	}
	return nil
}

func saveCandidate(state *State, candidate *Inventory, skipSecrets bool) error {
	candidate.Metadata.UpdatedAt = utcNow()
	if err := ValidateInventory(candidate, state.PrivateDir, skipSecrets); err != nil {
		return err
	}
	if err := writeJSONAtomic(state.InventoryPath, candidate); err != nil {
		return err
	}
	state.Inventory = candidate
	return nil
}
func cloneInventory(inv *Inventory) *Inventory {
	b, _ := json.Marshal(inv)
	var out Inventory
	_ = json.Unmarshal(b, &out)
	return &out
}
func cloneIndex(index *SecretIndex) *SecretIndex {
	out := &SecretIndex{Schema: index.Schema, Refs: map[string]SecretRef{}}
	for k, v := range index.Refs {
		out.Refs[k] = v
	}
	return out
}
func findServer(inv *Inventory, id string) *Server {
	for i := range inv.Servers {
		if inv.Servers[i].ID == id {
			return &inv.Servers[i]
		}
	}
	return nil
}
func findLink(inv *Inventory, id string) *Link {
	for i := range inv.Links {
		if inv.Links[i].ID == id {
			return &inv.Links[i]
		}
	}
	return nil
}
func findRoute(inv *Inventory, id string) *Route {
	for i := range inv.Routes {
		if inv.Routes[i].ID == id || inv.Routes[i].DisplayName == id {
			return &inv.Routes[i]
		}
	}
	return nil
}
func findProvider(inv *Inventory, id string) *Provider {
	for i := range inv.Providers {
		if inv.Providers[i].ID == id {
			return &inv.Providers[i]
		}
	}
	return nil
}
func findProfile(inv *Inventory, id string) *Profile {
	for i := range inv.Profiles {
		if inv.Profiles[i].ID == id {
			return &inv.Profiles[i]
		}
	}
	return nil
}
func findClientTarget(inv *Inventory, id string) *ClientTarget {
	for i := range inv.ClientTargets {
		if inv.ClientTargets[i].ID == id {
			return &inv.ClientTargets[i]
		}
	}
	return nil
}
func nextLinkSlot(inv *Inventory) (int, error) {
	for slot := 1; slot <= 99; slot++ {
		iface := fmt.Sprintf("wg-rst%02d", slot)
		port := 51819 + slot
		subnet := fmt.Sprintf("10.77.%d.0/30", slot)
		used := false
		for _, l := range inv.Links {
			if l.Interface == iface || l.ListenPort == port || l.Subnet == subnet {
				used = true
				break
			}
		}
		if !used {
			return slot, nil
		}
	}
	return 0, errors.New("no free relay slot remains in the supported 1..99 range")
}
func portUsedByEntry(inv *Inventory, entry string, port int) bool {
	return portRangeUsedByEntry(inv, entry, port, port)
}

func portRangeUsedByEntry(inv *Inventory, entry string, start, end int) bool {
	for _, r := range inv.Routes {
		if r.EntryServer != entry {
			continue
		}
		routeStart, routeEnd, err := routePortRange(r)
		if err == nil && portRangesOverlap(start, end, routeStart, routeEnd) {
			return true
		}
	}
	return false
}
func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

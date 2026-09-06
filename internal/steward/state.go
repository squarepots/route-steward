package steward

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	stableIDPattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	unixUserPattern      = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	linkInterfacePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,14}$`)
	linkSubnetPattern    = regexp.MustCompile(`^10\.77\.(?:[0-9]|[1-9][0-9]|[12][0-9]{2})\.0/30$`)
)

type State struct {
	PrivateDir    string
	InventoryPath string
	Inventory     *Inventory
}

func utcNow() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func NewCleanInventory(privateDir string) *Inventory {
	now := utcNow()
	return &Inventory{
		Schema:   InventorySchema,
		Metadata: Metadata{ID: "route-steward", CreatedAt: now, UpdatedAt: now},
		Delivery: Delivery{
			Directory:         filepath.Join(privateDir, "delivery"),
			RecoveryDirectory: filepath.Join(privateDir, "recovery"),
		},
		Servers: []Server{}, Links: []Link{}, Routes: []Route{}, Providers: []Provider{},
		Profiles: []Profile{}, ClientTargets: []ClientTarget{},
	}
}

func Bootstrap(privateDir string) (*State, bool, error) {
	root, err := filepath.Abs(privateDir)
	if err != nil {
		return nil, false, fmt.Errorf("resolve private directory: %w", err)
	}
	inventoryPath := filepath.Join(root, "inventory.json")
	indexPath := filepath.Join(root, "secrets", "index.json")
	observedPath := filepath.Join(root, "observed.json")
	invExists := regularFile(inventoryPath)
	indexExists := regularFile(indexPath)
	observedExists := regularFile(observedPath)
	if invExists || indexExists || observedExists {
		if !invExists || !indexExists {
			return nil, false, errors.New("private state is partially initialized; refusing to overwrite or infer a repair")
		}
		if !observedExists {
			if err := writeJSONAtomic(observedPath, emptyObserved()); err != nil {
				return nil, false, err
			}
		}
		state, err := LoadState(root)
		return state, false, err
	}
	if err := os.MkdirAll(filepath.Join(root, "secrets"), 0o700); err != nil {
		return nil, false, fmt.Errorf("create secret directory: %w", err)
	}
	if err := protectPath(root, true); err != nil {
		return nil, false, err
	}
	if err := protectPath(filepath.Join(root, "secrets"), true); err != nil {
		return nil, false, err
	}
	index := SecretIndex{Schema: SecretIndexSchema, Refs: map[string]SecretRef{}}
	inv := NewCleanInventory(root)
	if err := writeJSONAtomic(indexPath, index); err != nil {
		return nil, false, err
	}
	if err := writeJSONAtomic(inventoryPath, inv); err != nil {
		return nil, false, err
	}
	if err := writeJSONAtomic(observedPath, emptyObserved()); err != nil {
		return nil, false, err
	}
	state, err := LoadState(root)
	return state, true, err
}

func LoadState(privateDir string) (*State, error) {
	root, err := filepath.Abs(privateDir)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(root, "inventory.json")
	var inv Inventory
	if err := readJSON(path, &inv); err != nil {
		return nil, fmt.Errorf("read private inventory: %w", err)
	}
	if err := ValidateInventory(&inv, root, false); err != nil {
		return nil, err
	}
	return &State{PrivateDir: root, InventoryPath: path, Inventory: &inv}, nil
}

func (s *State) Save(skipSecrets bool) error {
	s.Inventory.Metadata.UpdatedAt = utcNow()
	if err := ValidateInventory(s.Inventory, s.PrivateDir, skipSecrets); err != nil {
		return err
	}
	return writeJSONAtomic(s.InventoryPath, s.Inventory)
}

func ReadSecretIndex(privateDir string) (*SecretIndex, error) {
	var index SecretIndex
	if err := readJSON(filepath.Join(privateDir, "secrets", "index.json"), &index); err != nil {
		return nil, fmt.Errorf("read secret index: %w", err)
	}
	if index.Schema != SecretIndexSchema || index.Refs == nil {
		return nil, errors.New("secret index schema must be 1")
	}
	return &index, nil
}

func ResolveSecret(reference, privateDir string, index *SecretIndex) (string, error) {
	if index == nil {
		var err error
		index, err = ReadSecretIndex(privateDir)
		if err != nil {
			return "", err
		}
	}
	entry, ok := index.Refs[reference]
	if !ok {
		return "", fmt.Errorf("secret reference %q is not registered", reference)
	}
	if filepath.IsAbs(entry.Path) || entry.Path == "" {
		return "", fmt.Errorf("secret reference %q has an invalid path", reference)
	}
	root, err := filepath.Abs(filepath.Join(privateDir, "secrets"))
	if err != nil {
		return "", err
	}
	resolved, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(entry.Path)))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("secret reference %q leaves the private secret directory", reference)
	}
	if !regularFile(resolved) {
		return "", fmt.Errorf("secret reference %q is missing", reference)
	}
	return resolved, nil
}

func RegisterSecret(privateDir, reference, kind, relativePath string) error {
	index, err := ReadSecretIndex(privateDir)
	if err != nil {
		return err
	}
	if _, exists := index.Refs[reference]; exists {
		return fmt.Errorf("secret reference %q already exists", reference)
	}
	index.Refs[reference] = SecretRef{Type: kind, Path: filepath.ToSlash(relativePath)}
	return writeJSONAtomic(filepath.Join(privateDir, "secrets", "index.json"), index)
}

func ValidateInventory(inv *Inventory, privateDir string, skipSecrets bool) error {
	var failures []string
	if inv.Schema != InventorySchema {
		failures = append(failures, "inventory schema must be 2")
	}
	checkIDs := func(kind string, ids []string) {
		seen := map[string]bool{}
		for _, id := range ids {
			if !stableIDPattern.MatchString(id) {
				failures = append(failures, fmt.Sprintf("%s %q has an invalid stable id", kind, id))
			}
			if seen[id] {
				failures = append(failures, fmt.Sprintf("%s id %q is duplicated", kind, id))
			}
			seen[id] = true
		}
	}
	serverIDs := make([]string, 0, len(inv.Servers))
	serverSet := map[string]bool{}
	for _, server := range inv.Servers {
		serverIDs = append(serverIDs, server.ID)
		serverSet[server.ID] = true
	}
	linkIDs := make([]string, 0, len(inv.Links))
	linkSet := map[string]bool{}
	for _, link := range inv.Links {
		linkIDs = append(linkIDs, link.ID)
		linkSet[link.ID] = true
	}
	routeIDs := make([]string, 0, len(inv.Routes))
	routeSet := map[string]bool{}
	for _, route := range inv.Routes {
		routeIDs = append(routeIDs, route.ID)
		routeSet[route.ID] = true
	}
	providerIDs := make([]string, 0, len(inv.Providers))
	providerSet := map[string]bool{}
	for _, provider := range inv.Providers {
		providerIDs = append(providerIDs, provider.ID)
		providerSet[provider.ID] = true
	}
	profileIDs := make([]string, 0, len(inv.Profiles))
	profileSet := map[string]bool{}
	for _, profile := range inv.Profiles {
		profileIDs = append(profileIDs, profile.ID)
		profileSet[profile.ID] = true
	}
	targetIDs := make([]string, 0, len(inv.ClientTargets))
	for _, target := range inv.ClientTargets {
		targetIDs = append(targetIDs, target.ID)
	}
	checkIDs("Server", serverIDs)
	checkIDs("Link", linkIDs)
	checkIDs("Route", routeIDs)
	checkIDs("Provider", providerIDs)
	checkIDs("Profile", profileIDs)
	checkIDs("ClientTarget", targetIDs)

	for _, s := range inv.Servers {
		if s.Compute.Driver != "byo-ssh" {
			failures = append(failures, fmt.Sprintf("Server %q uses unsupported compute driver", s.ID))
		}
		if s.Compute.HostOwnership != "dedicated" {
			failures = append(failures, fmt.Sprintf("Server %q must confirm dedicated host ownership", s.ID))
		}
		if s.Network.PublicIPv4 != "" && (net.ParseIP(s.Network.PublicIPv4) == nil || strings.Contains(s.Network.PublicIPv4, ":")) {
			failures = append(failures, fmt.Sprintf("Server %q has invalid public IPv4", s.ID))
		}
		if s.Network.PrivateIPv4 != nil && *s.Network.PrivateIPv4 != "" && (net.ParseIP(*s.Network.PrivateIPv4) == nil || strings.Contains(*s.Network.PrivateIPv4, ":")) {
			failures = append(failures, fmt.Sprintf("Server %q has invalid private IPv4", s.ID))
		}
		if s.Network.PublicIPv6 != nil && *s.Network.PublicIPv6 != "" && (net.ParseIP(*s.Network.PublicIPv6) == nil || !strings.Contains(*s.Network.PublicIPv6, ":")) {
			failures = append(failures, fmt.Sprintf("Server %q has invalid public IPv6", s.ID))
		}
		if s.Network.ExpectedEgressIPv4 != "" && (net.ParseIP(s.Network.ExpectedEgressIPv4) == nil || strings.Contains(s.Network.ExpectedEgressIPv4, ":")) {
			failures = append(failures, fmt.Sprintf("Server %q has invalid expected egress IPv4", s.ID))
		}
		if !unixUserPattern.MatchString(s.SSH.User) {
			failures = append(failures, fmt.Sprintf("Server %q has invalid SSH user", s.ID))
		}
		if s.SSH.KeyPath == "" {
			failures = append(failures, fmt.Sprintf("Server %q has no SSH key path", s.ID))
		}
		firewallRules := map[string]bool{}
		for _, rule := range s.Firewall.Rules {
			if rule.Family != "ipv4" && rule.Family != "ipv6" && rule.Family != "dual" {
				failures = append(failures, fmt.Sprintf("Server %q has invalid firewall family", s.ID))
			}
			if rule.Protocol != "tcp" && rule.Protocol != "udp" {
				failures = append(failures, fmt.Sprintf("Server %q has invalid firewall protocol", s.ID))
			}
			endPort := rule.EndPort
			if endPort == 0 {
				endPort = rule.Port
			}
			if rule.Port < 1 || rule.Port > 65535 || endPort < rule.Port || endPort > 65535 {
				failures = append(failures, fmt.Sprintf("Server %q has invalid firewall port", s.ID))
			}
			if rule.Source == "" && rule.SourceServer == "" {
				failures = append(failures, fmt.Sprintf("Server %q has a firewall rule without a source", s.ID))
			}
			if rule.SourceServer != "" && !serverSet[rule.SourceServer] {
				failures = append(failures, fmt.Sprintf("Server %q firewall references unknown Server %q", s.ID, rule.SourceServer))
			}
			source := rule.Source
			if rule.SourceServer != "" {
				source = "server:" + rule.SourceServer
			}
			key := fmt.Sprintf("%s:%s:%d-%d:%s", rule.Family, rule.Protocol, rule.Port, endPort, source)
			if firewallRules[key] {
				failures = append(failures, fmt.Sprintf("Server %q has duplicate firewall rule %q", s.ID, key))
			}
			firewallRules[key] = true
		}
	}
	interfaces, ports, subnets := map[string]bool{}, map[int]bool{}, map[string]bool{}
	for _, l := range inv.Links {
		if l.Driver != "wireguard" || l.Type != "wireguard" {
			failures = append(failures, fmt.Sprintf("Link %q has unsupported state", l.ID))
		}
		if !serverSet[l.EntryServer] || !serverSet[l.ExitServer] {
			failures = append(failures, fmt.Sprintf("Link %q has invalid endpoints", l.ID))
		}
		if !linkInterfacePattern.MatchString(l.Interface) {
			failures = append(failures, fmt.Sprintf("Link %q has invalid interface", l.ID))
		}
		if l.ListenPort < 1 || l.ListenPort > 65535 {
			failures = append(failures, fmt.Sprintf("Link %q has invalid UDP port", l.ID))
		}
		if !linkSubnetPattern.MatchString(l.Subnet) {
			failures = append(failures, fmt.Sprintf("Link %q has invalid /30 subnet", l.ID))
		}
		if interfaces[l.Interface] || ports[l.ListenPort] || subnets[l.Subnet] {
			failures = append(failures, fmt.Sprintf("Link %q reuses an allocation", l.ID))
		}
		interfaces[l.Interface], ports[l.ListenPort], subnets[l.Subnet] = true, true, true
	}
	routeListeners, directEntries := map[string][][2]int{}, map[string]bool{}
	for _, r := range inv.Routes {
		if r.Kind != "direct" && r.Kind != "relay" {
			failures = append(failures, fmt.Sprintf("Route %q has unsupported kind", r.ID))
		}
		if r.Ingress.Driver != "hysteria2" || r.ListenPort < 1 || r.ListenPort > 65535 {
			failures = append(failures, fmt.Sprintf("Route %q has invalid ingress", r.ID))
		}
		startPort, endPort, rangeErr := routePortRange(r)
		if rangeErr != nil {
			failures = append(failures, fmt.Sprintf("Route %q has invalid port hopping: %v", r.ID, rangeErr))
			startPort, endPort = r.ListenPort, r.ListenPort
		}
		if !serverSet[r.EntryServer] || !serverSet[r.ExitServer] {
			failures = append(failures, fmt.Sprintf("Route %q references an unknown server", r.ID))
		}
		for _, existing := range routeListeners[r.EntryServer] {
			if portRangesOverlap(startPort, endPort, existing[0], existing[1]) {
				failures = append(failures, fmt.Sprintf("Route %q reuses Hysteria2 listener range on Server %q", r.ID, r.EntryServer))
				break
			}
		}
		routeListeners[r.EntryServer] = append(routeListeners[r.EntryServer], [2]int{startPort, endPort})
		for _, link := range inv.Links {
			if link.ExitServer == r.EntryServer && link.ListenPort >= startPort && link.ListenPort <= endPort {
				failures = append(failures, fmt.Sprintf("Route %q port range overlaps WireGuard Link %q on Server %q", r.ID, link.ID, r.EntryServer))
			}
		}
		if r.Kind == "direct" {
			if directEntries[r.EntryServer] {
				failures = append(failures, fmt.Sprintf("Direct Route %q reuses one entry Server service", r.ID))
			}
			directEntries[r.EntryServer] = true
		}
		if r.Kind == "relay" {
			if r.Link == nil || !linkSet[*r.Link] {
				failures = append(failures, fmt.Sprintf("Relay Route %q references an unknown Link", r.ID))
			} else if link := findLink(inv, *r.Link); link.EntryServer != r.EntryServer || link.ExitServer != r.ExitServer {
				failures = append(failures, fmt.Sprintf("Relay Route %q does not match its Link endpoints", r.ID))
			}
		}
		if r.PayloadSecretRef == "" {
			failures = append(failures, fmt.Sprintf("Route %q has no payload secret reference", r.ID))
		}
	}
	for _, p := range inv.Providers {
		if p.SourceType != "mihomo-http" || p.SourceSecretRef == "" || p.IntervalSeconds < 3600 || p.HealthCheck {
			failures = append(failures, fmt.Sprintf("Provider %q is invalid", p.ID))
		}
	}
	for _, p := range inv.Profiles {
		for _, id := range p.IncludeRoutes {
			if id != "*" && !routeSet[id] {
				failures = append(failures, fmt.Sprintf("Profile %q references unknown Route %q", p.ID, id))
			}
		}
		for _, id := range p.IncludeProviders {
			if id != "*" && !providerSet[id] {
				failures = append(failures, fmt.Sprintf("Profile %q references unknown Provider %q", p.ID, id))
			}
		}
		if err := validateProfileRouting(inv, p); err != nil {
			failures = append(failures, fmt.Sprintf("Profile %q has invalid routing: %v", p.ID, err))
		}
	}
	for _, t := range inv.ClientTargets {
		if !profileSet[t.Profile] {
			failures = append(failures, fmt.Sprintf("ClientTarget %q references an unknown Profile", t.ID))
		}
		if len(t.MihomoProcessNames) > 0 {
			if t.Renderer != "mihomo" {
				failures = append(failures, fmt.Sprintf("ClientTarget %q has Mihomo process names but is not a Mihomo renderer", t.ID))
			} else if _, err := validateMihomoProcessNames(t.MihomoProcessNames); err != nil {
				failures = append(failures, fmt.Sprintf("ClientTarget %q has invalid Mihomo process names", t.ID))
			}
		}
		switch t.Renderer {
		case "mihomo", "karing":
			if t.Delivery != "file" || t.SubscriptionSecretRef != "" {
				failures = append(failures, fmt.Sprintf("Clash-file ClientTarget %q has invalid delivery", t.ID))
			}
		case "shadowrocket":
			if t.Delivery != "nodes" && t.Delivery != "subscription" {
				failures = append(failures, fmt.Sprintf("Shadowrocket ClientTarget %q has invalid delivery", t.ID))
			}
			if (t.Delivery == "subscription") != (t.SubscriptionSecretRef != "") {
				failures = append(failures, fmt.Sprintf("Shadowrocket ClientTarget %q has inconsistent subscription state", t.ID))
			}
		case "hysteria2":
			if t.Delivery != "file" || t.SubscriptionSecretRef != "" {
				failures = append(failures, fmt.Sprintf("Hysteria2 ClientTarget %q has invalid delivery", t.ID))
			}
			profile := findProfile(inv, t.Profile)
			route := findRoute(inv, t.Route)
			if route == nil || !route.Enabled || profile == nil || (!contains(profile.IncludeRoutes, "*") && !contains(profile.IncludeRoutes, t.Route)) {
				failures = append(failures, fmt.Sprintf("Hysteria2 ClientTarget %q has invalid Route selection", t.ID))
			}
			if _, err := normalizeProxyListen(t.Listen); err != nil {
				failures = append(failures, fmt.Sprintf("Hysteria2 ClientTarget %q has invalid loopback listener", t.ID))
			}
			if t.IngressFamily != "auto" && t.IngressFamily != "ipv4" && t.IngressFamily != "ipv6" {
				failures = append(failures, fmt.Sprintf("Hysteria2 ClientTarget %q has invalid ingress family", t.ID))
			}
		default:
			failures = append(failures, fmt.Sprintf("ClientTarget %q has unsupported renderer", t.ID))
		}
	}
	if !skipSecrets {
		index, err := ReadSecretIndex(privateDir)
		if err != nil {
			failures = append(failures, err.Error())
		} else {
			refs := map[string]bool{}
			for _, l := range inv.Links {
				if l.Enabled {
					refs[l.SecretRef] = true
				}
			}
			for _, r := range inv.Routes {
				if r.Enabled {
					refs[r.PayloadSecretRef] = true
					refs[r.CredentialSecretRef] = true
				}
			}
			for _, p := range inv.Providers {
				if p.Enabled {
					refs[p.SourceSecretRef] = true
				}
			}
			for _, t := range inv.ClientTargets {
				if t.SubscriptionSecretRef != "" {
					refs[t.SubscriptionSecretRef] = true
				}
			}
			for ref := range refs {
				if _, err := ResolveSecret(ref, privateDir, index); err != nil {
					failures = append(failures, err.Error())
				}
			}
		}
	}
	if len(failures) > 0 {
		sort.Strings(failures)
		return errors.New(strings.Join(failures, "\n"))
	}
	return nil
}

func ValidateProviderURL(value string) bool {
	if strings.ContainsAny(value, "\r\n") {
		return false
	}
	u, err := url.Parse(value)
	return err == nil && u.IsAbs() && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func emptyObserved() ObservedState {
	return ObservedState{Schema: ObservedSchema, GeneratedAt: nil, Servers: []ObservedObject{}, Links: []ObservedObject{}, Routes: []ObservedRoute{}}
}

func readJSON(path string, dst any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, dst); err != nil {
		return err
	}
	return nil
}

func writeJSONAtomic(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return writeFileAtomic(path, b, 0o600)
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := protectPath(dir, true); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".rst-write-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmp)
		}
	}()
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := protectPath(tmp, false); err != nil {
		return err
	}
	if err := atomicReplace(tmp, path); err != nil {
		return err
	}
	if err := protectPath(path, false); err != nil {
		return err
	}
	ok = true
	return nil
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func sha256File(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func convertID(value string) (string, error) {
	id := strings.ToLower(strings.TrimSpace(value))
	if !stableIDPattern.MatchString(id) {
		return "", errors.New("id must start with a lowercase letter or digit and contain only lowercase letters, digits, and dashes")
	}
	return id, nil
}

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	v := value
	return &v
}

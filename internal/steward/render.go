package steward

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	clientassets "github.com/squarepots/route-steward/client"
)

type routeNode struct {
	Name    string
	RouteID string
	Values  map[string]string
	Body    string
}

type renderManifest struct {
	Schema  int                   `json:"schema"`
	Targets []renderManifestEntry `json:"targets"`
}

type renderManifestEntry struct {
	ID               string `json:"id"`
	Renderer         string `json:"renderer"`
	FileName         string `json:"file_name"`
	OutputSHA256     string `json:"output_sha256"`
	InputFingerprint string `json:"input_fingerprint"`
	RenderedAt       string `json:"rendered_at"`
}

func RenderClients(state *State, targetID string, skipValidation bool) (RenderResult, error) {
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

func renderHysteria2(state *State, profile Profile, target ClientTarget, outputPath string) (RenderOutput, error) {
	_, node, err := headlessRouteNode(state, profile, target)
	if err != nil {
		return RenderOutput{}, err
	}
	config, err := hysteriaClientConfig(node, target.Listen, true)
	if err != nil {
		return RenderOutput{}, err
	}
	if err := writeFileAtomic(outputPath, config, 0o600); err != nil {
		return RenderOutput{}, err
	}
	return RenderOutput{ClientTarget: target.ID, Profile: profile.ID, Renderer: "hysteria2", Path: outputPath, NodeCount: 1, Validation: "official-json-structure-checked"}, nil
}

func headlessRouteNode(state *State, profile Profile, target ClientTarget) (Route, routeNode, error) {
	route := findRoute(state.Inventory, target.Route)
	if route == nil || !route.Enabled {
		return Route{}, routeNode{}, fmt.Errorf("Hysteria2 ClientTarget %q does not select an enabled Route", target.ID)
	}
	if !contains(profile.IncludeRoutes, "*") && !contains(profile.IncludeRoutes, route.ID) {
		return Route{}, routeNode{}, fmt.Errorf("Hysteria2 ClientTarget %q selects a Route outside its Profile", target.ID)
	}
	nodes, err := healthRouteNodes(state, *route)
	if err != nil {
		return Route{}, routeNode{}, err
	}
	family := target.IngressFamily
	if family == "auto" {
		family = "ipv4"
	}
	for _, node := range nodes {
		if addressFamily(node.Values["server"]) == family {
			return *route, node, nil
		}
	}
	if target.IngressFamily == "auto" && family == "ipv4" {
		for _, node := range nodes {
			if addressFamily(node.Values["server"]) == "ipv6" {
				return *route, node, nil
			}
		}
	}
	return Route{}, routeNode{}, fmt.Errorf("Hysteria2 ClientTarget %q has no %s ingress", target.ID, target.IngressFamily)
}

func SanitizedRender(result RenderResult) RenderResult {
	out := result
	out.Outputs = make([]RenderOutput, len(result.Outputs))
	for i, item := range result.Outputs {
		fileName := filepath.Base(item.Path)
		item.Path = ""
		item.Artifact = &Artifact{ID: item.ClientTarget, FileName: fileName, RelativePath: "<private>/delivery/" + fileName}
		out.Outputs[i] = item
	}
	return out
}

func renderClash(state *State, profile Profile, target ClientTarget, outputPath, renderer string, skipValidation bool) (RenderOutput, error) {
	if err := validateProfileRouting(state.Inventory, profile); err != nil {
		return RenderOutput{}, fmt.Errorf("Profile %q has invalid routing: %w", profile.ID, err)
	}
	nodes, bodies, err := profileNodes(state, profile)
	if err != nil {
		return RenderOutput{}, err
	}
	if renderer == "karing" {
		for _, node := range nodes {
			if err := validateKaringClashNode(node); err != nil {
				return RenderOutput{}, err
			}
		}
	}
	processNames := []string{}
	if renderer == "mihomo" {
		processNames, err = validateMihomoProcessNames(target.MihomoProcessNames)
		if err != nil {
			return RenderOutput{}, err
		}
	}
	providers, err := profileProviders(state, profile)
	if err != nil {
		return RenderOutput{}, err
	}
	routing := effectiveProfileRouting(profile)
	var providerBlock, providerUse strings.Builder
	if len(providers) > 0 {
		providerBlock.WriteString("proxy-providers:\n")
		for _, provider := range providers {
			fmt.Fprintf(&providerBlock, "  %s:\n    type: http\n    url: %s\n    path: ./proxy_providers/%s.yaml\n    interval: %d\n    proxy: DIRECT\n    health-check:\n      enable: false\n", provider.ID, yamlQuote(provider.URL), provider.ID, provider.Interval)
		}
		providerBlock.WriteByte('\n')
		providerUse.WriteString("    use:\n")
		for _, provider := range providers {
			fmt.Fprintf(&providerUse, "      - %s\n", yamlQuote(provider.ID))
		}
	}
	var globalGroup strings.Builder
	if renderer == "mihomo" {
		globalGroup.WriteString("  - name: GLOBAL\n    type: select\n    proxies:\n      - Private Routes\n")
		for _, node := range nodes {
			fmt.Fprintf(&globalGroup, "      - %s\n", yamlQuote(node.Name))
		}
		globalGroup.WriteString("      - DIRECT\n      - REJECT\n")
		if len(providers) > 0 {
			globalGroup.WriteString("    use:\n")
			for _, provider := range providers {
				fmt.Fprintf(&globalGroup, "      - %s\n", yamlQuote(provider.ID))
			}
		}
	}
	var nodeLines strings.Builder
	for _, node := range nodes {
		fmt.Fprintf(&nodeLines, "      - %s\n", yamlQuote(node.Name))
	}
	processMode := ""
	var processGroup, processRules strings.Builder
	if len(processNames) > 0 {
		processMode = "find-process-mode: strict\n"
		processGroup.WriteString("  - name: Applications\n    type: select\n    proxies:\n      - DIRECT\n      - Private Routes\n")
		for _, name := range processNames {
			fmt.Fprintf(&processRules, "  - %s\n", yamlQuote("PROCESS-NAME,"+name+",Applications"))
		}
	}
	dns := "dns:\n  enable: true\n  ipv6: true\n  enhanced-mode: fake-ip\n  fake-ip-range: 198.18.0.1/16\n  use-system-hosts: false\n  respect-rules: true\n  default-nameserver:\n    - https://1.1.1.1/dns-query\n    - https://8.8.8.8/dns-query\n  proxy-server-nameserver:\n    - https://1.1.1.1/dns-query\n    - https://8.8.8.8/dns-query\n  nameserver:\n    - 'https://1.1.1.1/dns-query#Private Routes'\n    - 'https://8.8.8.8/dns-query#Private Routes'\n"
	var routeGroups, profileRules strings.Builder
	createdGroups := map[string]bool{}
	for _, rule := range routing.Rules {
		targetName := "DIRECT"
		if rule.Action.Type == "route" {
			targetName = profileRouteGroupName(rule.Action.Route)
			if !createdGroups[targetName] {
				routeGroups.WriteString("  - name: " + targetName + "\n    type: select\n    proxies:\n")
				for _, node := range nodes {
					if node.RouteID == rule.Action.Route {
						fmt.Fprintf(&routeGroups, "      - %s\n", yamlQuote(node.Name))
					}
				}
				createdGroups[targetName] = true
			}
		}
		switch rule.Match.Type {
		case "domain_suffix":
			fmt.Fprintf(&profileRules, "  - %s\n", yamlQuote("DOMAIN-SUFFIX,"+rule.Match.Value+","+targetName))
		case "geosite":
			fmt.Fprintf(&profileRules, "  - %s\n", yamlQuote("GEOSITE,"+rule.Match.Value+","+targetName))
		case "geoip":
			fmt.Fprintf(&profileRules, "  - %s\n", yamlQuote("GEOIP,"+rule.Match.Value+","+targetName+",no-resolve"))
		}
	}
	text := "# route-steward: agent-native\n# Private file: contains live proxy credentials.\nmode: rule\n" + processMode + "ipv6: true\nprofile:\n  store-selected: true\n  store-fake-ip: true\n\n" + dns + "\nproxies:\n" + strings.Join(bodies, "\n") + "\n\n" + providerBlock.String() + "proxy-groups:\n  - name: Private Routes\n    type: select\n    proxies:\n" + nodeLines.String() + providerUse.String() + globalGroup.String() + routeGroups.String() + processGroup.String() + "rules:\n  - DOMAIN,localhost,DIRECT\n  - DOMAIN-SUFFIX,local,DIRECT\n  - IP-CIDR,10.0.0.0/8,DIRECT,no-resolve\n  - IP-CIDR,100.64.0.0/10,DIRECT,no-resolve\n  - IP-CIDR,127.0.0.0/8,DIRECT,no-resolve\n  - IP-CIDR,169.254.0.0/16,DIRECT,no-resolve\n  - IP-CIDR,172.16.0.0/12,DIRECT,no-resolve\n  - IP-CIDR,192.168.0.0/16,DIRECT,no-resolve\n  - IP-CIDR6,::1/128,DIRECT,no-resolve\n  - IP-CIDR6,fc00::/7,DIRECT,no-resolve\n  - IP-CIDR6,fe80::/10,DIRECT,no-resolve\n" + processRules.String() + profileRules.String() + "  - MATCH,Private Routes\n"
	if err := writeFileAtomic(outputPath, []byte(text), 0o600); err != nil {
		return RenderOutput{}, err
	}
	validation := "skipped"
	if !skipValidation {
		core := findMihomo()
		if core == "" {
			validation = "unavailable"
		} else {
			home, err := os.MkdirTemp("", "rst-mihomo-*")
			if err != nil {
				return RenderOutput{}, err
			}
			defer os.RemoveAll(home)
			cmd := exec.Command(core, "-t", "-d", home, "-f", outputPath)
			if output, err := cmd.CombinedOutput(); err != nil {
				return RenderOutput{}, fmt.Errorf("Clash validation core rejected generated ClientTarget: %w: %s", err, string(output))
			}
			validation = "passed"
		}
	}
	return RenderOutput{ClientTarget: target.ID, Profile: profile.ID, Renderer: renderer, Path: outputPath, NodeCount: len(nodes), ProviderCount: len(providers), Validation: validation}, nil
}

func validateKaringClashNode(node routeNode) error {
	if _, err := validateManagedHysteria2Node(node); err != nil {
		return fmt.Errorf("Karing ClientTarget node %q does not satisfy the pinned Hysteria2 import contract: %w", node.Name, err)
	}
	if node.Values["skip-cert-verify"] != "true" || node.Values["alpn"] != "[h3]" {
		return fmt.Errorf("Karing ClientTarget node %q must retain pinned self-signed TLS and Hysteria2 ALPN", node.Name)
	}
	return nil
}

func renderShadowrocket(state *State, profile Profile, target ClientTarget, outputPath string) (RenderOutput, error) {
	nodes, _, err := profileNodes(state, profile)
	if err != nil {
		return RenderOutput{}, err
	}
	items := make([]map[string]string, 0, len(nodes))
	subscriptionURL, err := subscriptionURLForTarget(state, target)
	if err != nil {
		return RenderOutput{}, err
	}
	if subscriptionURL != "" {
		encoded := base64.RawURLEncoding.EncodeToString([]byte(subscriptionURL))
		items = append(items, map[string]string{"name": "Private subscription", "encoded": base64.StdEncoding.EncodeToString([]byte("sub://" + encoded))})
	} else {
		for _, node := range nodes {
			uri, err := shadowrocketURI(node)
			if err != nil {
				return RenderOutput{}, err
			}
			items = append(items, map[string]string{"name": node.Name, "encoded": base64.StdEncoding.EncodeToString([]byte(uri))})
		}
	}
	data, _ := json.Marshal(items)
	vendor, err := fs.ReadFile(clientassets.Files, "vendor/qrcode-generator-1.4.4.js")
	if err != nil {
		return RenderOutput{}, err
	}
	vendor = bytes.ReplaceAll(vendor, []byte("</script"), []byte("<\\/script"))
	app := fmt.Sprintf(";(() => {\n  'use strict';\n  const items = %s;\n  const decode = (v) => new TextDecoder().decode(Uint8Array.from(atob(v), c => c.charCodeAt(0)));\n  const root = document.getElementById('items');\n  for (const item of items) {\n    const card = document.createElement('section');\n    const title = document.createElement('h2'); title.textContent = item.name;\n    const box = document.createElement('div'); box.className = 'qr';\n    const qr = qrcode(0, 'L'); qr.addData(decode(item.encoded), 'Byte'); qr.make(); box.innerHTML = qr.createSvgTag({ scalable: true, margin: 4 });\n    card.append(title, box); root.append(card);\n  }\n})();\n", data)
	script := string(vendor) + "\n" + app
	digest := sha256.Sum256([]byte(script))
	hash := base64.StdEncoding.EncodeToString(digest[:])
	html := fmt.Sprintf("<!doctype html><html><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><meta http-equiv=\"Content-Security-Policy\" content=\"default-src 'none'; img-src data:; style-src 'unsafe-inline'; script-src 'sha256-%s'; base-uri 'none'; connect-src 'none'\"><title>Route Steward · Shadowrocket</title><style>body{font-family:system-ui,sans-serif;max-width:720px;margin:0 auto;padding:24px}h1{font-size:1.35rem}#items{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:16px}section{border:1px solid #ccc;border-radius:14px;padding:14px;text-align:center}.qr{width:min(220px,100%%);margin:auto;background:#fff}.qr svg{width:100%%;height:auto}h2{font-size:.9rem;overflow-wrap:anywhere}p{color:#666;font-size:.85rem}</style></head><body><h1>Shadowrocket import</h1><p>Offline private import page. Keep this file private.</p><main id=\"items\"></main><script>%s</script></body></html>\n", hash, script)
	lower := strings.ToLower(html)
	if strings.Contains(lower, "src=\"http") || strings.Contains(lower, "href=\"http") || strings.Contains(lower, "fetch(") {
		return RenderOutput{}, errors.New("Shadowrocket output contains an external request")
	}
	if err := writeFileAtomic(outputPath, []byte(html), 0o600); err != nil {
		return RenderOutput{}, err
	}
	return RenderOutput{ClientTarget: target.ID, Profile: profile.ID, Renderer: "shadowrocket", Path: outputPath, NodeCount: len(nodes), Subscription: subscriptionURL != "", Validation: "offline-structure-checked"}, nil
}

type providerRender struct {
	ID, URL  string
	Interval int
}

func profileProviders(state *State, profile Profile) ([]providerRender, error) {
	if len(profile.IncludeProviders) == 0 {
		return []providerRender{}, nil
	}
	all := contains(profile.IncludeProviders, "*")
	out := []providerRender{}
	for _, provider := range state.Inventory.Providers {
		if !provider.Enabled || (!all && !contains(profile.IncludeProviders, provider.ID)) {
			continue
		}
		if provider.SourceType != "mihomo-http" {
			return nil, fmt.Errorf("Provider %q source type is unsupported", provider.ID)
		}
		path, err := ResolveSecret(provider.SourceSecretRef, state.PrivateDir, nil)
		if err != nil {
			return nil, err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		value := strings.TrimSpace(string(body))
		if !ValidateProviderURL(value) {
			return nil, fmt.Errorf("Provider %q URL is invalid", provider.ID)
		}
		out = append(out, providerRender{ID: provider.ID, URL: value, Interval: provider.IntervalSeconds})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func profileRouteGroupName(routeID string) string {
	return "RST-Route-" + routeID
}

func profileNodes(state *State, profile Profile) ([]routeNode, []string, error) {
	all := contains(profile.IncludeRoutes, "*")
	routes := append([]Route(nil), state.Inventory.Routes...)
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Order == routes[j].Order {
			return routes[i].ID < routes[j].ID
		}
		return routes[i].Order < routes[j].Order
	})
	nodes := []routeNode{}
	bodies := []string{}
	for _, route := range routes {
		if !route.Enabled || (!all && !contains(profile.IncludeRoutes, route.ID)) {
			continue
		}
		if route.Ingress.Driver != "hysteria2" {
			return nil, nil, fmt.Errorf("Route %q uses unsupported ingress driver", route.ID)
		}
		path, err := ResolveSecret(route.PayloadSecretRef, state.PrivateDir, nil)
		if err != nil {
			return nil, nil, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, err
		}
		parsed, body, err := parseRoutePayload(string(data))
		if err != nil {
			return nil, nil, fmt.Errorf("Route %q: %w", route.ID, err)
		}
		for i := range parsed {
			parsed[i].RouteID = route.ID
		}
		nodes = append(nodes, parsed...)
		bodies = append(bodies, body)
	}
	if len(nodes) == 0 {
		return nil, nil, fmt.Errorf("Profile %q has no enabled Routes", profile.ID)
	}
	return nodes, bodies, nil
}

func parseRoutePayload(raw string) ([]routeNode, string, error) {
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	if !strings.Contains(normalized, "schema: 1\n") || !strings.Contains(normalized, "\nproxies:\n") {
		return nil, "", errors.New("client payload must use Route Steward schema 1")
	}
	parts := strings.SplitN(normalized, "\nproxies:\n", 2)
	body := strings.TrimRight(parts[1], "\r\n \t")
	lines := strings.Split(body, "\n")
	nodes := []routeNode{}
	var current *routeNode
	var currentLines []string
	flush := func() {
		if current != nil {
			current.Body = strings.Join(currentLines, "\n")
			nodes = append(nodes, *current)
		}
	}
	for _, line := range lines {
		if strings.HasPrefix(line, "  - name:") {
			flush()
			name := yamlScalar(strings.TrimSpace(strings.TrimPrefix(line, "  - name:")))
			current = &routeNode{Name: name, Values: map[string]string{}}
			currentLines = []string{line}
			continue
		}
		if current == nil {
			continue
		}
		currentLines = append(currentLines, line)
		trim := strings.TrimSpace(line)
		if idx := strings.Index(trim, ":"); idx > 0 {
			key := trim[:idx]
			value := yamlScalar(strings.TrimSpace(trim[idx+1:]))
			current.Values[key] = value
		}
	}
	flush()
	if len(nodes) == 0 {
		return nil, "", errors.New("client payload contains no proxy nodes")
	}
	for _, node := range nodes {
		if node.Values["type"] != "hysteria2" {
			return nil, "", fmt.Errorf("node %q must use Hysteria2", node.Name)
		}
	}
	return nodes, body, nil
}

func validateManagedHysteria2Node(node routeNode) (string, error) {
	required := []string{"type", "server", "port", "password", "sni", "fingerprint", "obfs", "obfs-password"}
	for _, key := range required {
		if node.Values[key] == "" {
			return "", fmt.Errorf("node %q is missing %q", node.Name, key)
		}
	}
	if port, err := strconv.Atoi(node.Values["port"]); err != nil || port < 1 || port > 65535 {
		return "", fmt.Errorf("node %q has an invalid Hysteria2 port", node.Name)
	}
	if _, err := nodePortHoppingRange(node); err != nil {
		return "", err
	}
	fingerprint := strings.ToLower(strings.NewReplacer(":", "", "-", "", " ", "").Replace(node.Values["fingerprint"]))
	if len(fingerprint) != 64 {
		return "", fmt.Errorf("node %q has an invalid SHA-256 fingerprint", node.Name)
	}
	if _, err := hex.DecodeString(fingerprint); err != nil {
		return "", fmt.Errorf("node %q has an invalid SHA-256 fingerprint", node.Name)
	}
	if node.Values["obfs"] != "salamander" {
		return "", fmt.Errorf("node %q uses unsupported Hysteria2 obfuscation", node.Name)
	}
	return fingerprint, nil
}

func nodePortHoppingRange(node routeNode) (string, error) {
	value := strings.TrimSpace(node.Values["ports"])
	if value == "" {
		return "", nil
	}
	hopping, err := parsePortHoppingRange(value)
	if err != nil {
		return "", fmt.Errorf("node %q has invalid port hopping: %w", node.Name, err)
	}
	port, err := strconv.Atoi(node.Values["port"])
	if err != nil || validatePortHopping(port, hopping) != nil {
		return "", fmt.Errorf("node %q has invalid port hopping", node.Name)
	}
	return portHoppingText(hopping), nil
}

func shadowrocketURI(node routeNode) (string, error) {
	fingerprint, err := validateManagedHysteria2Node(node)
	if err != nil {
		return "", err
	}
	endpointPort := node.Values["port"]
	if hopping, err := nodePortHoppingRange(node); err != nil {
		return "", err
	} else if hopping != "" {
		endpointPort = hopping
	}
	host := node.Values["server"]
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	escape := func(v string) string { return strings.ReplaceAll(url.QueryEscape(v), "+", "%20") }
	query := "auth=" + escape(node.Values["password"]) + "&obfs=salamander&obfs-password=" + escape(node.Values["obfs-password"]) + "&obfsParam=" + escape(node.Values["obfs-password"]) + "&sni=" + escape(node.Values["sni"]) + "&peer=" + escape(node.Values["sni"]) + "&alpn=h3&udp=1&insecure=1&pinSHA256=" + fingerprint
	return "hysteria2://" + escape(node.Values["password"]) + "@" + host + ":" + endpointPort + "/?" + query + "#" + escape(node.Name), nil
}

func yamlScalar(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		return strings.ReplaceAll(value[1:len(value)-1], "''", "'")
	}
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		if unquoted, err := strconv.Unquote(value); err == nil {
			return unquoted
		}
	}
	return value
}
func yamlQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }
func findMihomo() string {
	for _, name := range []string{"mihomo", "mihomo.exe", "verge-mihomo.exe"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	if runtime.GOOS == "windows" {
		for _, candidate := range []string{
			filepath.Join(os.Getenv("ProgramFiles"), "Clash Verge", "verge-mihomo.exe"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Clash Verge", "verge-mihomo.exe"),
		} {
			if regularFile(candidate) {
				return candidate
			}
		}
	}
	return ""
}

func clientTargetFingerprint(state *State, targetID string) (string, error) {
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

func updateRenderManifest(state *State, outputs []RenderOutput) error {
	path := filepath.Join(state.Inventory.Delivery.Directory, "client-render-manifest.json")
	manifest := renderManifest{Schema: 1, Targets: []renderManifestEntry{}}
	if regularFile(path) {
		if err := readJSON(path, &manifest); err != nil {
			return err
		}
		if manifest.Schema != 1 {
			return errors.New("Client render manifest schema must be 1")
		}
	}
	entries := map[string]renderManifestEntry{}
	for _, entry := range manifest.Targets {
		entries[entry.ID] = entry
	}
	for _, output := range outputs {
		fingerprint, err := clientTargetFingerprint(state, output.ClientTarget)
		if err != nil {
			return err
		}
		sum, err := sha256File(output.Path)
		if err != nil {
			return err
		}
		entries[output.ClientTarget] = renderManifestEntry{ID: output.ClientTarget, Renderer: output.Renderer, FileName: filepath.Base(output.Path), OutputSHA256: sum, InputFingerprint: fingerprint, RenderedAt: utcNow()}
	}
	valid := map[string]bool{}
	for _, target := range state.Inventory.ClientTargets {
		valid[target.ID] = true
	}
	ids := []string{}
	for id := range entries {
		if valid[id] {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	manifest.Targets = []renderManifestEntry{}
	for _, id := range ids {
		manifest.Targets = append(manifest.Targets, entries[id])
	}
	return writeJSONAtomic(path, manifest)
}

func subscriptionURLForTarget(state *State, target ClientTarget) (string, error) {
	if target.SubscriptionSecretRef == "" {
		return "", nil
	}
	subscription, err := readSubscriptionState(state, target.ID)
	if err != nil {
		return "", err
	}
	return "https://" + subscription.Host + "/s/" + subscription.Token, nil
}

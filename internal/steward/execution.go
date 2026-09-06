package steward

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	serverassets "github.com/squarepots/route-steward/server"
)

type AuditEvidence struct {
	Route                     string  `json:"route"`
	Status                    string  `json:"status"`
	Category                  string  `json:"category"`
	ActualEgressIPv4          *string `json:"-"`
	EgressMatchesDeclaredExit bool    `json:"egress_matches_declared_exit"`
	HysteriaVersion           *string `json:"-"`
	WireGuardVersion          *string `json:"-"`
}

func (e AuditEvidence) Sanitized() map[string]any {
	return map[string]any{"route": e.Route, "status": e.Status, "category": e.Category, "egress_matches_declared_exit": e.EgressMatchesDeclaredExit, "versions_observed": map[string]bool{"hysteria": e.HysteriaVersion != nil, "wireguard": e.WireGuardVersion != nil}}
}

type sshHost struct{ Address, User, Key string }

func AuditRoute(ctx context.Context, state *State, routeID string) AuditEvidence {
	route := findRoute(state.Inventory, routeID)
	if route == nil {
		return AuditEvidence{Route: routeID, Status: "undetermined", Category: "undetermined"}
	}
	lines, err := performRouteOperation(ctx, state, *route, true)
	if err != nil {
		return AuditEvidence{Route: route.ID, Status: "undetermined", Category: "undetermined"}
	}
	return parseAuditEvidence(state.Inventory, *route, lines)
}

func DeployRoute(ctx context.Context, state *State, routeID string, skipClientValidation bool) (map[string]any, error) {
	return deployRoute(ctx, state, routeID, skipClientValidation, true)
}

func deployRouteWithoutRender(ctx context.Context, state *State, routeID string) (map[string]any, error) {
	return deployRoute(ctx, state, routeID, true, false)
}

func deployRoute(ctx context.Context, state *State, routeID string, skipClientValidation, renderClients bool) (map[string]any, error) {
	route := findRoute(state.Inventory, routeID)
	if route == nil {
		return nil, fmt.Errorf("unknown Route %q", routeID)
	}
	if route.State == "deployed" {
		current := AuditRoute(ctx, state, routeID)
		if current.Category != "in-sync" {
			return nil, errors.New("existing deployed Route has drift; refusing to overwrite unknown remote state")
		}
	}
	lines, err := performRouteOperation(ctx, state, *route, false)
	if err != nil {
		return nil, errors.New("deterministic route operation failed")
	}
	evidence := parseAuditEvidence(state.Inventory, *route, lines)
	if evidence.Status != "healthy" {
		return nil, errors.New("route deployment completed but post-deploy audit is not healthy")
	}
	candidate := cloneInventory(state.Inventory)
	candidateRoute := findRoute(candidate, route.ID)
	candidateRoute.Enabled = true
	candidateRoute.State = "deployed"
	if err := saveCandidate(state, candidate, false); err != nil {
		return nil, err
	}
	if _, err := SetObservedRoute(state, route.ID, evidence.Status, evidence.Category, deref(evidence.ActualEgressIPv4), deref(evidence.HysteriaVersion), deref(evidence.WireGuardVersion)); err != nil {
		return nil, err
	}
	result := map[string]any{"route": route.ID, "state": "deployed", "enabled": true, "validation": evidence.Sanitized()}
	if renderClients {
		targets := affectedMigrationTargets(state.Inventory, route.ID)
		render, err := RenderClientTargets(state, targets, skipClientValidation)
		if err != nil {
			return nil, &operationStageError{Stage: "client-delivery", StateChanged: "route-deployed", Retry: "render-client", Err: err}
		}
		result["render"] = SanitizedRender(render)
	}
	return result, nil
}

func performRouteOperation(ctx context.Context, state *State, route Route, auditOnly bool) ([]string, error) {
	entry := findServer(state.Inventory, route.EntryServer)
	exit := findServer(state.Inventory, route.ExitServer)
	if entry == nil || exit == nil {
		return nil, errors.New("Route references an unknown Server")
	}
	for _, server := range []*Server{entry, exit} {
		if server.Compute.HostOwnership != "dedicated" {
			return nil, errors.New("Route deployment requires dedicated host ownership")
		}
		if !unixUserPattern.MatchString(server.SSH.User) {
			return nil, errors.New("Route deployment rejected an invalid SSH user")
		}
		if !regularFile(server.SSH.KeyPath) {
			return nil, errors.New("SSH key is missing")
		}
	}
	if _, err := exec.LookPath("ssh"); err != nil {
		return nil, errors.New("OpenSSH ssh is required")
	}
	if _, err := exec.LookPath("scp"); err != nil {
		return nil, errors.New("OpenSSH scp is required")
	}
	if route.Kind == "relay" {
		return operateRelay(ctx, state, route, *entry, *exit, auditOnly)
	}
	return operateDirect(ctx, state, route, *entry, auditOnly)
}

func operateDirect(ctx context.Context, state *State, route Route, server Server, auditOnly bool) (lines []string, err error) {
	localRoot, err := stageServerAssets()
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(localRoot)
	host := sshHost{Address: server.Network.PublicIPv4, User: server.SSH.User, Key: server.SSH.KeyPath}
	remoteRoot := "/tmp/route-steward-" + randomHex(16)
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = runSSH(cleanupCtx, host, "rm -rf "+bashQuote(remoteRoot))
	}()
	if _, err = runSSH(ctx, host, "true"); err != nil {
		return nil, err
	}
	if _, err = runSSH(ctx, host, "mkdir -m 700 "+bashQuote(remoteRoot)); err != nil {
		return nil, err
	}
	if _, err = runSCP(ctx, host, []string{"-r", filepath.Join(localRoot, "server"), host.Address + ":" + remoteRoot + "/"}); err != nil {
		return nil, err
	}
	remoteServer := remoteRoot + "/server"
	if _, err = runSSH(ctx, host, bashCommand("sudo", "bash", remoteServer+"/preflight.sh")); err != nil {
		return nil, err
	}
	credentialPath, err := ResolveSecret(route.CredentialSecretRef, state.PrivateDir, nil)
	if err != nil {
		return nil, err
	}
	credentialDir := filepath.Dir(credentialPath)
	if !auditOnly {
		if _, err = runSSH(ctx, host, bashCommand("sudo", "bash", remoteServer+"/base-setup.sh", remoteServer+"/config")); err != nil {
			return nil, err
		}
		if _, err = runSSH(ctx, host, bashCommand("sudo", "bash", remoteServer+"/install-path-components.sh", remoteServer+"/config")); err != nil {
			return nil, err
		}
		remoteCredential := remoteRoot + "/managed-credential"
		if _, err = runSCP(ctx, host, []string{"-r", credentialDir, host.Address + ":" + remoteCredential}); err != nil {
			return nil, err
		}
		configure := []string{"sudo", "bash", remoteServer + "/configure-ingress.sh", "--ipv4", server.Network.PublicIPv4, "--name", route.DisplayName, "--port", fmt.Sprint(route.ListenPort), "--output", "/var/lib/route-steward/client-payload.yaml", "--credential-dir", remoteCredential}
		if hopping := portHoppingText(route.PortHopping); hopping != "" {
			configure = append(configure, "--port-hopping-range", hopping)
		}
		if server.Network.PublicIPv6 != nil {
			configure = append(configure, "--ipv6", *server.Network.PublicIPv6)
		}
		if _, err = runSSH(ctx, host, bashCommand(configure...)); err != nil {
			return nil, err
		}
		if err = adoptRemotePayload(ctx, host, remoteRoot, "/var/lib/route-steward/client-payload.yaml", state, route); err != nil {
			return nil, err
		}
	}
	fingerprint, configHash, err := routeAuditExpectation(credentialDir, route.ListenPort, "", portHoppingText(route.PortHopping))
	if err != nil {
		return nil, err
	}
	audit := []string{"sudo", "bash", remoteServer + "/audit.sh", "--ingress-port", fmt.Sprint(route.ListenPort), "--expected-fingerprint", fingerprint, "--expected-config-hash", configHash}
	if hopping := portHoppingText(route.PortHopping); hopping != "" {
		audit = append(audit, "--port-hopping-range", hopping)
	}
	output, auditErr := runSSH(ctx, host, bashCommand(audit...))
	lines = filterAuditLines(output)
	if auditErr != nil && !auditOnly {
		return lines, errors.New("post-deploy route audit found drift")
	}
	return lines, nil
}

func operateRelay(ctx context.Context, state *State, route Route, entry, exit Server, auditOnly bool) (lines []string, err error) {
	if route.Link == nil {
		return nil, errors.New("relay Route is missing Link")
	}
	link := findLink(state.Inventory, *route.Link)
	if link == nil {
		return nil, errors.New("relay Link is missing")
	}
	secretPath, err := ResolveSecret(link.SecretRef, state.PrivateDir, nil)
	if err != nil {
		return nil, err
	}
	var keys struct {
		Entry struct {
			PrivateKey string `json:"private_key"`
			PublicKey  string `json:"public_key"`
		} `json:"entry"`
		Exit struct {
			PrivateKey string `json:"private_key"`
			PublicKey  string `json:"public_key"`
		} `json:"exit"`
	}
	if err := readJSON(secretPath, &keys); err != nil {
		return nil, err
	}
	for _, value := range []string{keys.Entry.PrivateKey, keys.Entry.PublicKey, keys.Exit.PrivateKey, keys.Exit.PublicKey} {
		decoded, err := base64Decode(value)
		if err != nil || len(decoded) != 32 {
			return nil, errors.New("managed Link key pair is invalid")
		}
	}
	localRoot, err := stageServerAssets()
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(localRoot)
	entryHost := sshHost{Address: entry.Network.PublicIPv4, User: entry.SSH.User, Key: entry.SSH.KeyPath}
	exitHost := sshHost{Address: exit.Network.PublicIPv4, User: exit.SSH.User, Key: exit.SSH.KeyPath}
	entryRoot := "/tmp/route-steward-relay-entry-" + randomHex(16)
	exitRoot := "/tmp/route-steward-relay-exit-" + randomHex(16)
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = runSSH(cleanupCtx, entryHost, "rm -rf "+bashQuote(entryRoot))
		_, _ = runSSH(cleanupCtx, exitHost, "rm -rf "+bashQuote(exitRoot))
	}()
	for _, item := range []struct {
		host sshHost
		root string
	}{{entryHost, entryRoot}, {exitHost, exitRoot}} {
		if _, err = runSSH(ctx, item.host, "true"); err != nil {
			return nil, err
		}
		if _, err = runSSH(ctx, item.host, "mkdir -m 700 "+bashQuote(item.root)); err != nil {
			return nil, err
		}
		if _, err = runSCP(ctx, item.host, []string{"-r", filepath.Join(localRoot, "server"), item.host.Address + ":" + item.root + "/"}); err != nil {
			return nil, err
		}
		if _, err = runSSH(ctx, item.host, bashCommand("sudo", "bash", item.root+"/server/preflight.sh")); err != nil {
			return nil, err
		}
	}
	entryServer, exitServer := entryRoot+"/server", exitRoot+"/server"
	credentialPath, err := ResolveSecret(route.CredentialSecretRef, state.PrivateDir, nil)
	if err != nil {
		return nil, err
	}
	credentialDir := filepath.Dir(credentialPath)
	if !auditOnly {
		for _, item := range []struct {
			host    sshHost
			server  string
			install bool
		}{{entryHost, entryServer, true}, {exitHost, exitServer, false}} {
			if _, err = runSSH(ctx, item.host, bashCommand("sudo", "bash", item.server+"/base-setup.sh", item.server+"/config")); err != nil {
				return nil, err
			}
			if item.install {
				if _, err = runSSH(ctx, item.host, bashCommand("sudo", "bash", item.server+"/install-path-components.sh", item.server+"/config")); err != nil {
					return nil, err
				}
			}
		}
		credentialRemote := entryRoot + "/managed-credential"
		if _, err = runSCP(ctx, entryHost, []string{"-r", credentialDir, entryHost.Address + ":" + credentialRemote}); err != nil {
			return nil, err
		}
		keyStage, err := os.MkdirTemp("", "rst-link-key-*")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(keyStage)
		entryKey, exitKey := filepath.Join(keyStage, "entry.key"), filepath.Join(keyStage, "exit.key")
		if err := writeFileAtomic(entryKey, []byte(keys.Entry.PrivateKey+"\n"), 0o600); err != nil {
			return nil, err
		}
		if err := writeFileAtomic(exitKey, []byte(keys.Exit.PrivateKey+"\n"), 0o600); err != nil {
			return nil, err
		}
		if _, err = runSCP(ctx, entryHost, []string{entryKey, entryHost.Address + ":" + entryRoot + "/managed-link.key"}); err != nil {
			return nil, err
		}
		if _, err = runSCP(ctx, exitHost, []string{exitKey, exitHost.Address + ":" + exitRoot + "/managed-link.key"}); err != nil {
			return nil, err
		}
		entryOutput, err := runSSH(ctx, entryHost, bashCommand("sudo", "bash", entryServer+"/prepare-relay.sh", "--interface", link.Interface, "--private-key-file", entryRoot+"/managed-link.key"))
		if err != nil {
			return nil, err
		}
		exitOutput, err := runSSH(ctx, exitHost, bashCommand("sudo", "bash", exitServer+"/prepare-relay.sh", "--interface", link.Interface, "--private-key-file", exitRoot+"/managed-link.key"))
		if err != nil {
			return nil, err
		}
		entryPublic, err := parsePublicKey(entryOutput)
		if err != nil {
			return nil, err
		}
		exitPublic, err := parsePublicKey(exitOutput)
		if err != nil {
			return nil, err
		}
		entryPeer := strings.Split(link.EntryAddress, "/")[0]
		exitPeer := strings.Split(link.ExitAddress, "/")[0]
		exitConfigure := []string{"sudo", "bash", exitServer + "/configure-relay-exit.sh", "--interface", link.Interface, "--listen-port", fmt.Sprint(link.ListenPort), "--subnet", link.Subnet, "--local-cidr", link.ExitAddress, "--peer-ip", entryPeer, "--peer-public-key", entryPublic, "--entry-public-ip", entry.Network.PublicIPv4}
		if _, err = runSSH(ctx, exitHost, bashCommand(exitConfigure...)); err != nil {
			return nil, err
		}
		entryConfigure := []string{"sudo", "bash", entryServer + "/configure-relay-entry.sh", "--interface", link.Interface, "--local-cidr", link.EntryAddress, "--peer-ip", exitPeer, "--peer-public-key", exitPublic, "--exit-endpoint", exit.Network.PublicIPv4, "--tunnel-port", fmt.Sprint(link.ListenPort), "--ingress-port", fmt.Sprint(route.ListenPort), "--entry-ipv4", entry.Network.PublicIPv4, "--exit-ipv4", exit.Network.PublicIPv4, "--name", route.DisplayName, "--via-name", entry.ID, "--output", "/var/lib/route-steward/relay-client-payload.yaml", "--unit-dir", entryServer + "/config", "--credential-dir", credentialRemote}
		if hopping := portHoppingText(route.PortHopping); hopping != "" {
			entryConfigure = append(entryConfigure, "--port-hopping-range", hopping)
		}
		if entry.Network.PublicIPv6 != nil {
			entryConfigure = append(entryConfigure, "--entry-ipv6", *entry.Network.PublicIPv6)
		}
		if _, err = runSSH(ctx, entryHost, bashCommand(entryConfigure...)); err != nil {
			return nil, err
		}
		if err = adoptRemotePayload(ctx, entryHost, entryRoot, "/var/lib/route-steward/relay-client-payload.yaml", state, route); err != nil {
			return nil, err
		}
	}
	entryPeer := strings.Split(link.EntryAddress, "/")[0]
	exitPeer := strings.Split(link.ExitAddress, "/")[0]
	exitAudit := []string{"sudo", "bash", exitServer + "/audit-relay.sh", "--role", "exit", "--interface", link.Interface, "--tunnel-port", fmt.Sprint(link.ListenPort), "--subnet", link.Subnet, "--peer-ip", entryPeer, "--expected-peer-public-key", keys.Entry.PublicKey}
	exitOutput, exitErr := runSSH(ctx, exitHost, bashCommand(exitAudit...))
	if exitErr != nil {
		lines = filterAuditLines(exitOutput)
		if !auditOnly {
			return lines, errors.New("post-deploy relay exit audit found drift")
		}
		return lines, nil
	}
	fingerprint, configHash, err := routeAuditExpectation(credentialDir, route.ListenPort, link.Interface, portHoppingText(route.PortHopping))
	if err != nil {
		return nil, err
	}
	entryAudit := []string{"sudo", "bash", entryServer + "/audit-relay.sh", "--role", "entry", "--interface", link.Interface, "--name", route.DisplayName, "--ingress-port", fmt.Sprint(route.ListenPort), "--peer-ip", exitPeer, "--expected-exit", exit.Network.PublicIPv4, "--expected-peer-public-key", keys.Exit.PublicKey, "--expected-fingerprint", fingerprint, "--expected-config-hash", configHash}
	if hopping := portHoppingText(route.PortHopping); hopping != "" {
		entryAudit = append(entryAudit, "--port-hopping-range", hopping)
	}
	entryOutput, entryErr := runSSH(ctx, entryHost, bashCommand(entryAudit...))
	lines = filterAuditLines(entryOutput)
	if entryErr != nil && !auditOnly {
		return lines, errors.New("post-deploy relay entry audit found drift")
	}
	return lines, nil
}

func stageServerAssets() (string, error) {
	root, err := os.MkdirTemp("", "rst-server-assets-*")
	if err != nil {
		return "", err
	}
	serverDir := filepath.Join(root, "server")
	err = fs.WalkDir(serverassets.Files, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == "." {
			return nil
		}
		target := filepath.Join(serverDir, filepath.FromSlash(path))
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		data, err := fs.ReadFile(serverassets.Files, path)
		if err != nil {
			return err
		}
		mode := os.FileMode(0o600)
		if strings.HasSuffix(path, ".sh") {
			mode = 0o700
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		return os.WriteFile(target, data, mode)
	})
	if err != nil {
		os.RemoveAll(root)
		return "", err
	}
	return root, nil
}

func sshTransport(host sshHost) []string {
	return []string{"-i", host.Key, "-o", "BatchMode=yes", "-o", "ConnectTimeout=15", "-o", "StrictHostKeyChecking=accept-new"}
}
func runSSH(ctx context.Context, host sshHost, remote string) (string, error) {
	args := append(sshTransport(host), "-l", host.User, host.Address, remote)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}
func runSCP(ctx context.Context, host sshHost, extra []string) (string, error) {
	args := append(sshTransport(host), "-o", "User="+host.User)
	args = append(args, extra...)
	cmd := exec.CommandContext(ctx, "scp", args...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}
func bashQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
func bashCommand(tokens ...string) string {
	quoted := make([]string, len(tokens))
	for i, token := range tokens {
		quoted[i] = bashQuote(token)
	}
	return strings.Join(quoted, " ")
}

func adoptRemotePayload(ctx context.Context, host sshHost, remoteRoot, remoteSource string, state *State, route Route) error {
	remoteExport := remoteRoot + "/client-payload.yaml"
	if _, err := runSSH(ctx, host, bashCommand("sudo", "install", "-m", "0600", "-o", host.User, remoteSource, remoteExport)); err != nil {
		return err
	}
	stage, err := os.MkdirTemp("", "rst-payload-adopt-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	download := filepath.Join(stage, "payload.yaml")
	if _, err := runSCP(ctx, host, []string{host.Address + ":" + remoteExport, download}); err != nil {
		return err
	}
	canonical, err := ResolveSecret(route.PayloadSecretRef, state.PrivateDir, nil)
	if err != nil {
		return err
	}
	expected, err := os.ReadFile(canonical)
	if err != nil {
		return err
	}
	actual, err := os.ReadFile(download)
	if err != nil {
		return err
	}
	if err := validatePayloadSemantics(expected, actual); err != nil {
		return err
	}
	return writeFileAtomic(canonical, actual, 0o600)
}

func validatePayloadSemantics(expected, actual []byte) error {
	expectedNodes, _, err := parseRoutePayload(string(expected))
	if err != nil {
		return errors.New("canonical client payload is invalid")
	}
	actualNodes, _, err := parseRoutePayload(string(actual))
	if err != nil {
		return errors.New("remote generated client payload is invalid")
	}
	if len(expectedNodes) != len(actualNodes) {
		return errors.New("remote generated client payload does not match canonical local payload")
	}
	actualByName := make(map[string]routeNode, len(actualNodes))
	for _, node := range actualNodes {
		if _, exists := actualByName[node.Name]; exists {
			return errors.New("remote generated client payload contains duplicate nodes")
		}
		actualByName[node.Name] = node
	}
	keys := []string{"type", "server", "port", "ports", "password", "sni", "skip-cert-verify", "fingerprint", "alpn", "obfs", "obfs-password"}
	for _, expectedNode := range expectedNodes {
		actualNode, exists := actualByName[expectedNode.Name]
		if !exists {
			return errors.New("remote generated client payload does not match canonical local payload")
		}
		for _, key := range keys {
			expectedValue := normalizePayloadValue(key, expectedNode.Values[key])
			actualValue := normalizePayloadValue(key, actualNode.Values[key])
			if key == "ports" && expectedValue == "" && actualValue == "" {
				continue
			}
			if expectedValue == "" || expectedValue != actualValue {
				return errors.New("remote generated client payload does not match canonical local payload")
			}
		}
	}
	return nil
}

func normalizePayloadValue(key, value string) string {
	if key == "fingerprint" {
		return strings.ToLower(strings.NewReplacer(":", "", "-", "", " ", "").Replace(value))
	}
	return value
}

func routeAuditExpectation(directory string, port int, bind, portHopping string) (string, string, error) {
	credentialData, err := os.ReadFile(filepath.Join(directory, "credentials.json"))
	if err != nil {
		return "", "", err
	}
	var credential struct {
		Hysteria struct {
			Auth string `json:"auth"`
			Obfs string `json:"obfs"`
		} `json:"hysteria"`
	}
	if err := json.Unmarshal(credentialData, &credential); err != nil {
		return "", "", err
	}
	certPEM, err := os.ReadFile(filepath.Join(directory, "server.crt"))
	if err != nil {
		return "", "", err
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return "", "", errors.New("managed certificate is invalid")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", "", err
	}
	fingerprintSum := sha256.Sum256(cert.Raw)
	fingerprint := hex.EncodeToString(fingerprintSum[:])
	material := fmt.Sprintf("hysteria2|port=%d|hop=%s|auth=%s|obfs=%s|cert=%s", port, defaultString(portHopping, "none"), credential.Hysteria.Auth, credential.Hysteria.Obfs, fingerprint)
	if bind != "" {
		material = fmt.Sprintf("hysteria2-relay|port=%d|hop=%s|auth=%s|obfs=%s|cert=%s|bind=%s", port, defaultString(portHopping, "none"), credential.Hysteria.Auth, credential.Hysteria.Obfs, fingerprint, bind)
	}
	hash := sha256.Sum256([]byte(material))
	return fingerprint, hex.EncodeToString(hash[:]), nil
}

func filterAuditLines(output string) []string {
	allowed := regexp.MustCompile(`^(?:RST_AUDIT_CATEGORY|IPV4|RELAY_EGRESS_IPV4|HYSTERIA_VERSION|WIREGUARD_VERSION|AUDIT_OK|RELAY_(?:ENTRY|EXIT)_AUDIT_OK|RELAY_EGRESS_OK)=`)
	lines := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if allowed.MatchString(line) {
			lines = append(lines, line)
		}
	}
	found := false
	for _, line := range lines {
		if strings.HasPrefix(line, "RST_AUDIT_CATEGORY=") {
			found = true
		}
	}
	if !found {
		lines = append(lines, "RST_AUDIT_CATEGORY=undetermined")
	}
	return lines
}

func parseAuditEvidence(inv *Inventory, route Route, lines []string) AuditEvidence {
	actual, hysteria, wireguard, category := "", "", "", ""
	for _, line := range lines {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		switch parts[0] {
		case "IPV4", "RELAY_EGRESS_IPV4":
			actual = strings.TrimSpace(parts[1])
		case "HYSTERIA_VERSION":
			hysteria = strings.TrimSpace(parts[1])
		case "WIREGUARD_VERSION":
			wireguard = strings.TrimSpace(parts[1])
		case "RST_AUDIT_CATEGORY":
			category = strings.TrimSpace(parts[1])
		}
	}
	if !driftCategories[category] {
		category = "undetermined"
	}
	exit := findServer(inv, route.ExitServer)
	expected := ""
	if exit != nil {
		expected = exit.Network.ExpectedEgressIPv4
	}
	if category == "in-sync" {
		if actual == "" {
			category = "undetermined"
		} else if actual != expected {
			category = "egress-mismatch"
		}
	}
	status := "mismatch"
	if category == "in-sync" {
		status = "healthy"
	} else if category == "undetermined" {
		status = "undetermined"
	}
	return AuditEvidence{Route: route.ID, Status: status, Category: category, ActualEgressIPv4: stringPointer(actual), EgressMatchesDeclaredExit: actual != "" && actual == expected, HysteriaVersion: stringPointer(hysteria), WireGuardVersion: stringPointer(wireguard)}
}

func parsePublicKey(output string) (string, error) {
	pattern := regexp.MustCompile(`(?m)^PUBLIC_KEY=([A-Za-z0-9+/]{43}=)$`)
	match := pattern.FindStringSubmatch(strings.ReplaceAll(output, "\r\n", "\n"))
	if len(match) != 2 {
		return "", errors.New("WireGuard public key was not returned")
	}
	return match[1], nil
}
func randomHex(size int) string {
	data := make([]byte, size)
	_, _ = rand.Read(data)
	return hex.EncodeToString(data)
}
func base64Decode(value string) ([]byte, error) { return base64StdDecode(value) }

// Kept in small OS-neutral helpers to make command argument tests deterministic.
func platformExecutable(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

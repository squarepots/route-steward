package steward

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	workerassets "github.com/squarepots/route-steward/worker"
)

var (
	workerNamePattern              = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	hostNamePattern                = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?$`)
	tokenPattern                   = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
	errSubscriptionPayloadTooLarge = errors.New("subscription-payload-too-large")
	subscriptionSecretChunkBytes   = 4000
	subscriptionMaxChunks          = 60
)

type subscriptionState struct {
	Schema                    int     `json:"schema"`
	WorkerName                string  `json:"worker_name"`
	Host                      string  `json:"host"`
	Token                     string  `json:"token"`
	PendingToken              *string `json:"pending_token"`
	LastPublishedAt           *string `json:"last_published_at"`
	PublishedInputFingerprint string  `json:"published_input_fingerprint,omitempty"`
	PublishedBodySHA256       string  `json:"published_body_sha256,omitempty"`
	RotationStartedAt         *string `json:"rotation_started_at,omitempty"`
	RotatedAt                 *string `json:"rotated_at,omitempty"`
	Reference                 string  `json:"-"`
	Path                      string  `json:"-"`
	TargetID                  string  `json:"-"`
}

func AssertSubscriptionBodySize(body string) (int, error) {
	size := len([]byte(body))
	if size > subscriptionSecretChunkBytes*subscriptionMaxChunks {
		return size, errSubscriptionPayloadTooLarge
	}
	return size, nil
}

func subscriptionCapableTarget(target *ClientTarget) bool {
	return target != nil && (target.Renderer == "shadowrocket" || target.Renderer == "mihomo")
}

func subscriptionBodyChunks(body string) ([]string, error) {
	if _, err := AssertSubscriptionBodySize(body); err != nil {
		return nil, err
	}
	data := []byte(body)
	chunks := make([]string, 0, (len(data)+subscriptionSecretChunkBytes-1)/subscriptionSecretChunkBytes)
	for len(data) > 0 {
		limit := subscriptionSecretChunkBytes
		if len(data) < limit {
			limit = len(data)
		}
		for limit > 0 && limit < len(data) && (data[limit]&0xc0) == 0x80 {
			limit--
		}
		if limit == 0 {
			return nil, errors.New("subscription body could not be split as UTF-8")
		}
		chunks = append(chunks, string(data[:limit]))
		data = data[limit:]
	}
	if len(chunks) == 0 {
		return nil, errors.New("subscription body is empty")
	}
	return chunks, nil
}

func newSubscriptionToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func validateWorkerIdentity(workerName, hostName string) (string, error) {
	if !workerNamePattern.MatchString(workerName) {
		return "", errors.New("worker_name must be a valid Cloudflare Worker name")
	}
	host := strings.ToLower(strings.TrimSpace(hostName))
	if !hostNamePattern.MatchString(host) || strings.Contains(host, "..") || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") {
		return "", errors.New("host must be one valid HTTPS hostname without scheme or path")
	}
	return host, nil
}

func readSubscriptionState(state *State, targetID string) (*subscriptionState, error) {
	target := findClientTarget(state.Inventory, targetID)
	if !subscriptionCapableTarget(target) {
		return nil, fmt.Errorf("ClientTarget %q does not support private subscription delivery", targetID)
	}
	if target.SubscriptionSecretRef == "" {
		return nil, fmt.Errorf("ClientTarget %q has no private subscription state", targetID)
	}
	path, err := ResolveSecret(target.SubscriptionSecretRef, state.PrivateDir, nil)
	if err != nil {
		return nil, err
	}
	var doc subscriptionState
	if err := readJSON(path, &doc); err != nil {
		return nil, err
	}
	if doc.Schema != 1 || !tokenPattern.MatchString(doc.Token) {
		return nil, errors.New("private subscription state is invalid")
	}
	if doc.PendingToken != nil && !tokenPattern.MatchString(*doc.PendingToken) {
		return nil, errors.New("private subscription pending token is invalid")
	}
	host, err := validateWorkerIdentity(doc.WorkerName, doc.Host)
	if err != nil {
		return nil, err
	}
	doc.Host = host
	doc.Reference = target.SubscriptionSecretRef
	doc.Path = path
	doc.TargetID = targetID
	return &doc, nil
}

func initializeSubscriptionState(state *State, targetID, workerName, hostName string) (*subscriptionState, error) {
	target := findClientTarget(state.Inventory, targetID)
	if !subscriptionCapableTarget(target) {
		return nil, fmt.Errorf("ClientTarget %q does not support private subscription delivery", targetID)
	}
	host, err := validateWorkerIdentity(workerName, hostName)
	if err != nil {
		return nil, err
	}
	for _, other := range state.Inventory.ClientTargets {
		if other.ID == targetID || other.SubscriptionSecretRef == "" {
			continue
		}
		otherState, err := readSubscriptionState(state, other.ID)
		if err != nil {
			return nil, fmt.Errorf("ClientTarget %q has invalid subscription state: %w", other.ID, err)
		}
		if otherState.WorkerName == workerName {
			return nil, fmt.Errorf("Worker %q is already assigned to ClientTarget %q", workerName, other.ID)
		}
		if otherState.Host == host {
			return nil, fmt.Errorf("subscription host %q is already assigned to ClientTarget %q", host, other.ID)
		}
	}
	if target.SubscriptionSecretRef != "" {
		existing, err := readSubscriptionState(state, targetID)
		if err != nil {
			return nil, err
		}
		if existing.WorkerName != workerName || existing.Host != host {
			return nil, errors.New("subscription delivery is already initialized for a different Worker or host")
		}
		return existing, nil
	}
	token, err := newSubscriptionToken()
	if err != nil {
		return nil, err
	}
	reference := "subscription:" + targetID
	relative := filepath.ToSlash(filepath.Join("subscriptions", targetID+".json"))
	path := filepath.Join(state.PrivateDir, "secrets", filepath.FromSlash(relative))
	doc := subscriptionState{Schema: 1, WorkerName: workerName, Host: host, Token: token, PendingToken: nil, LastPublishedAt: nil}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	candidate := cloneInventory(state.Inventory)
	candidateTarget := findClientTarget(candidate, targetID)
	candidateTarget.SubscriptionSecretRef = reference
	candidateTarget.Delivery = "subscription"
	if err := createRegisteredSecret(state, candidate, reference, "cloudflare-subscription", relative, path, append(data, '\n')); err != nil {
		return nil, err
	}
	return readSubscriptionState(state, targetID)
}

func ExportSubscriptionBody(state *State, targetID string) (string, int, error) {
	target := findClientTarget(state.Inventory, targetID)
	if !subscriptionCapableTarget(target) {
		return "", 0, fmt.Errorf("ClientTarget %q does not support private subscription delivery", targetID)
	}
	profile := findProfile(state.Inventory, target.Profile)
	if profile == nil {
		return "", 0, fmt.Errorf("unknown Profile %q", target.Profile)
	}
	if target.Renderer == "mihomo" {
		rendered, err := RenderClients(state, targetID, false)
		if err != nil {
			return "", 0, err
		}
		if len(rendered.Outputs) != 1 {
			return "", 0, errors.New("Mihomo subscription render did not produce one artifact")
		}
		data, err := os.ReadFile(rendered.Outputs[0].Path)
		if err != nil {
			return "", 0, err
		}
		body := string(data)
		if _, err := AssertSubscriptionBodySize(body); err != nil {
			return "", 0, err
		}
		return body, rendered.Outputs[0].NodeCount, nil
	}
	nodes, _, err := profileNodes(state, *profile)
	if err != nil {
		return "", 0, err
	}
	uris := make([]string, 0, len(nodes))
	for _, node := range nodes {
		uri, err := shadowrocketURI(node)
		if err != nil {
			return "", 0, err
		}
		uris = append(uris, uri)
	}
	if len(uris) == 0 {
		return "", 0, errors.New("no Shadowrocket nodes were produced")
	}
	body := base64.StdEncoding.EncodeToString([]byte(strings.Join(uris, "\n")))
	if _, err := AssertSubscriptionBodySize(body); err != nil {
		return "", 0, err
	}
	return body, len(uris), nil
}

func writeSubscriptionReference(state *State, targetID string, subscription *subscriptionState) (*Artifact, error) {
	path := filepath.Join(state.Inventory.Delivery.Directory, targetID+".subscription.txt")
	value := "https://" + subscription.Host + "/s/" + subscription.Token + "\n"
	if err := writeFileAtomic(path, []byte(value), 0o600); err != nil {
		return nil, err
	}
	return &Artifact{ID: targetID + "-subscription", FileName: filepath.Base(path), RelativePath: "<private>/delivery/" + filepath.Base(path)}, nil
}

func subscriptionArtifactRetry(target *ClientTarget) string {
	if target != nil && target.Renderer == "shadowrocket" {
		return "render-client"
	}
	return "publish-subscription"
}

func PublishSubscription(state *State, targetID string, context map[string]any) (map[string]any, error) {
	subscription, err := readSubscriptionState(state, targetID)
	if err != nil {
		if stringField(context, "worker_name") == "" || stringField(context, "host") == "" {
			return nil, err
		}
		subscription, err = initializeSubscriptionState(state, targetID, stringField(context, "worker_name"), stringField(context, "host"))
		if err != nil {
			return nil, err
		}
	}
	if subscription.PendingToken != nil {
		return nil, errors.New("a subscription token rotation is pending")
	}
	body, _, err := ExportSubscriptionBody(state, targetID)
	if err != nil {
		return nil, err
	}
	fingerprint, err := subscriptionInputFingerprint(state, targetID)
	if err != nil {
		return nil, err
	}
	target := findClientTarget(state.Inventory, targetID)
	if err := deployWorkerAndVerify(subscription.WorkerName, subscription.Host, subscription.Token, target.Renderer, body); err != nil {
		return nil, err
	}
	now := utcNow()
	subscription.LastPublishedAt = &now
	subscription.PublishedInputFingerprint = fingerprint
	subscription.PublishedBodySHA256 = subscriptionBodySHA256(body)
	if err := writeJSONAtomic(subscription.Path, subscription); err != nil {
		return nil, &operationStageError{Stage: "subscription-state", StateChanged: "subscription-published-verified", Retry: "publish-subscription", Err: err}
	}
	result := map[string]any{"client_target": targetID, "worker": subscription.WorkerName, "published": true, "verified": true, "publication_fingerprint": fingerprint}
	if target.Renderer == "shadowrocket" {
		if _, err := RenderClients(state, targetID, true); err != nil {
			return nil, &operationStageError{Stage: "client-import-artifact", StateChanged: "subscription-published-verified", Retry: "render-client", Err: err}
		}
	} else {
		artifact, err := writeSubscriptionReference(state, targetID, subscription)
		if err != nil {
			return nil, &operationStageError{Stage: "client-import-artifact", StateChanged: "subscription-published-verified", Retry: "publish-subscription", Err: err}
		}
		result["import_artifact"] = artifact
	}
	return result, nil
}

func RotateSubscriptionToken(state *State, targetID string) (map[string]any, error) {
	subscription, err := readSubscriptionState(state, targetID)
	if err != nil {
		return nil, err
	}
	proposed := ""
	if subscription.PendingToken != nil {
		proposed = *subscription.PendingToken
	} else {
		proposed, err = newSubscriptionToken()
		if err != nil {
			return nil, err
		}
		now := utcNow()
		subscription.PendingToken = &proposed
		subscription.RotationStartedAt = &now
		if err := writeJSONAtomic(subscription.Path, subscription); err != nil {
			return nil, err
		}
	}
	body, _, err := ExportSubscriptionBody(state, targetID)
	if err != nil {
		return nil, err
	}
	fingerprint, err := subscriptionInputFingerprint(state, targetID)
	if err != nil {
		return nil, err
	}
	target := findClientTarget(state.Inventory, targetID)
	if err := deployWorkerAndVerify(subscription.WorkerName, subscription.Host, proposed, target.Renderer, body); err != nil {
		return nil, err
	}
	var fresh subscriptionState
	if err := readJSON(subscription.Path, &fresh); err != nil {
		return nil, &operationStageError{Stage: "subscription-token-state", StateChanged: "new-token-active-at-worker", Retry: "rotate-subscription-token", Err: err}
	}
	if fresh.PendingToken == nil || *fresh.PendingToken != proposed {
		return nil, errors.New("subscription rotation intent changed during publication")
	}
	now := utcNow()
	fresh.Token = proposed
	fresh.PendingToken = nil
	fresh.RotatedAt = &now
	fresh.LastPublishedAt = &now
	fresh.PublishedInputFingerprint = fingerprint
	fresh.PublishedBodySHA256 = subscriptionBodySHA256(body)
	if err := writeJSONAtomic(subscription.Path, &fresh); err != nil {
		return nil, &operationStageError{Stage: "subscription-token-state", StateChanged: "new-token-active-at-worker", Retry: "rotate-subscription-token", Err: err}
	}
	if target.Renderer == "shadowrocket" {
		if _, err := RenderClients(state, targetID, true); err != nil {
			return nil, &operationStageError{Stage: "client-import-artifact", StateChanged: "subscription-token-rotated", Retry: subscriptionArtifactRetry(target), Err: err}
		}
	} else if _, err := writeSubscriptionReference(state, targetID, &fresh); err != nil {
		return nil, &operationStageError{Stage: "client-import-artifact", StateChanged: "subscription-token-rotated", Retry: subscriptionArtifactRetry(target), Err: err}
	}
	return map[string]any{"client_target": targetID, "token_rotated": true, "published": true, "verified": true, "publication_fingerprint": fingerprint, "old_token_revoked_at_worker": true, "unrelated_route_credentials_changed": false, "unrelated_client_credentials_changed": false}, nil
}

func deployWorkerAndVerify(workerName, host, token, format, body string) error {
	if _, err := AssertSubscriptionBodySize(body); err != nil {
		return err
	}
	stage, err := os.MkdirTemp("", "rst-subscription-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := protectPath(stage, true); err != nil {
		return err
	}
	for _, name := range []string{"package.json", "package-lock.json", "wrangler.jsonc", "tsconfig.json", "src/index.ts"} {
		data, err := fs.ReadFile(workerassets.Files, name)
		if err != nil {
			return err
		}
		path := filepath.Join(stage, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return err
		}
	}
	sum := sha256.Sum256([]byte(token))
	chunks, err := subscriptionBodyChunks(body)
	if err != nil {
		return err
	}
	secrets := map[string]string{
		"SUBSCRIPTION_TOKEN_HASH":  hex.EncodeToString(sum[:]),
		"SUBSCRIPTION_FORMAT":      format,
		"SUBSCRIPTION_CHUNK_COUNT": fmt.Sprint(len(chunks)),
	}
	for index, chunk := range chunks {
		secrets[fmt.Sprintf("SUBSCRIPTION_BODY_%02d", index)] = chunk
	}
	secretPath := filepath.Join(stage, "worker-secrets.json")
	secretData, _ := json.Marshal(secrets)
	if err := writeFileAtomic(secretPath, secretData, 0o600); err != nil {
		return err
	}
	npm, npx := "npm", "npx"
	if runtime.GOOS == "windows" {
		npm = "npm.cmd"
		npx = "npx.cmd"
	}
	if _, err := exec.LookPath(npm); err != nil {
		return errors.New("npm is unavailable for optional Cloudflare publication")
	}
	if _, err := exec.LookPath(npx); err != nil {
		return errors.New("npx is unavailable for optional Cloudflare publication")
	}
	run := func(executable string, args ...string) error {
		cmd := exec.Command(executable, args...)
		cmd.Dir = stage
		cmd.Env = append(os.Environ(), "WRANGLER_SEND_METRICS=false")
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("Cloudflare Worker command failed: %w: %s", err, string(output))
		}
		return nil
	}
	if err := run(npm, "ci", "--ignore-scripts", "--no-audit", "--no-fund"); err != nil {
		return fmt.Errorf("Worker locked install failed: %w", err)
	}
	base := []string{"--no-install", "wrangler", "deploy", "--config", "wrangler.jsonc", "--name", workerName}
	if err := run(npx, append(base, "--dry-run", "--strict")...); err != nil {
		return fmt.Errorf("Worker strict dry-run failed: %w", err)
	}
	if err := run(npx, append(base, "--secrets-file", secretPath, "--keep-vars", "--minify", "--strict")...); err != nil {
		return fmt.Errorf("Cloudflare rejected Worker deployment: %w", err)
	}
	if err := verifySubscriptionEndpoint("https://"+host+"/s/"+token, format, body); err != nil {
		return &operationStageError{Stage: "subscription-verification", StateChanged: "subscription-published-unverified", Retry: "publish-subscription", Err: err}
	}
	return nil
}

func verifySubscriptionEndpoint(endpoint, format, expected string) error {
	client := &http.Client{Timeout: 30 * time.Second}
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "RouteSteward/1")
	response, err := client.Do(request)
	if err != nil {
		return errors.New("private subscription endpoint could not be verified after publication")
	}
	defer response.Body.Close()
	limit := int64(subscriptionSecretChunkBytes*subscriptionMaxChunks + 1)
	body, err := io.ReadAll(io.LimitReader(response.Body, limit))
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK || len(body) > subscriptionSecretChunkBytes*subscriptionMaxChunks || string(body) != expected {
		return errors.New("private subscription endpoint did not return the exact locally generated body")
	}
	if !strings.Contains(strings.ToLower(response.Header.Get("Cache-Control")), "no-store") {
		return errors.New("private subscription endpoint is missing its no-store cache policy")
	}
	if format == "mihomo" && !strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "yaml") {
		return errors.New("Mihomo subscription endpoint is missing its YAML content type")
	}
	return nil
}

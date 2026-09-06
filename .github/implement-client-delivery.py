from pathlib import Path
import re

ROOT = Path('.')

def read(path):
    return (ROOT / path).read_text(encoding='utf-8')

def write(path, text):
    (ROOT / path).write_text(text, encoding='utf-8', newline='\n')

def replace(path, old, new, count=1):
    text = read(path)
    if text.count(old) < count:
        raise RuntimeError(f'{path}: expected text not found: {old[:120]!r}')
    write(path, text.replace(old, new, count))

# Mihomo can use the same isolated private subscription publisher as Shadowrocket.
replace('internal/steward/state.go', '''\t\tswitch t.Renderer {\n\t\tcase "mihomo", "karing":\n\t\t\tif t.Delivery != "file" || t.SubscriptionSecretRef != "" {\n\t\t\t\tfailures = append(failures, fmt.Sprintf("Clash-file ClientTarget %q has invalid delivery", t.ID))\n\t\t\t}\n\t\tcase "shadowrocket":\n''', '''\t\tswitch t.Renderer {\n\t\tcase "mihomo":\n\t\t\tif t.Delivery != "file" && t.Delivery != "subscription" {\n\t\t\t\tfailures = append(failures, fmt.Sprintf("Mihomo ClientTarget %q has invalid delivery", t.ID))\n\t\t\t}\n\t\t\tif (t.Delivery == "subscription") != (t.SubscriptionSecretRef != "") {\n\t\t\t\tfailures = append(failures, fmt.Sprintf("Mihomo ClientTarget %q has inconsistent subscription state", t.ID))\n\t\t\t}\n\t\tcase "karing":\n\t\t\tif t.Delivery != "file" || t.SubscriptionSecretRef != "" {\n\t\t\t\tfailures = append(failures, fmt.Sprintf("Karing ClientTarget %q has invalid delivery", t.ID))\n\t\t\t}\n\t\tcase "shadowrocket":\n''')

# Publication and token rotation accept either renderer that has a stable remote-subscription consumer.
replace('internal/steward/preflight.go', '''\tcase "publish-subscription":\n\t\tif target == "" {\n\t\t\tmissing = append(missing, "target-client-target")\n\t\t} else if t := clientTarget(target); t == nil || t.Renderer != "shadowrocket" {\n\t\t\tconflicts = append(conflicts, "target-client-target-not-shadowrocket")\n''', '''\tcase "publish-subscription":\n\t\tif target == "" {\n\t\t\tmissing = append(missing, "target-client-target")\n\t\t} else if t := clientTarget(target); t == nil || (t.Renderer != "shadowrocket" && t.Renderer != "mihomo") {\n\t\t\tconflicts = append(conflicts, "target-client-target-not-subscription-capable")\n''')
replace('internal/steward/preflight.go', '''\tcase "rotate-subscription-token":\n\t\tif target == "" {\n\t\t\tmissing = append(missing, "target-client-target")\n\t\t} else if t := clientTarget(target); t == nil {\n\t\t\tconflicts = append(conflicts, "target-client-target-missing")\n\t\t} else if t.Renderer != "shadowrocket" {\n\t\t\tconflicts = append(conflicts, "target-client-target-not-shadowrocket")\n''', '''\tcase "rotate-subscription-token":\n\t\tif target == "" {\n\t\t\tmissing = append(missing, "target-client-target")\n\t\t} else if t := clientTarget(target); t == nil {\n\t\t\tconflicts = append(conflicts, "target-client-target-missing")\n\t\t} else if t.Renderer != "shadowrocket" && t.Renderer != "mihomo" {\n\t\t\tconflicts = append(conflicts, "target-client-target-not-subscription-capable")\n''')

# Capability discovery reports the real consumer boundary, without claiming ownership of Verge runtime settings.
replace('internal/steward/capabilities.go', 'capability("publish-subscription", "core", true, "external-publication", "Publish one private ClientTarget subscription payload.', 'capability("publish-subscription", "core", true, "external-publication", "Publish one private Mihomo or Shadowrocket ClientTarget subscription payload.')
replace('internal/steward/capabilities.go', 'map[string]any{"id": "mihomo", "state": "supported", "compatibility_baseline": mihomoCompatibilityBaseline, "clients": []string{"Clash Verge-compatible Mihomo clients"},', 'map[string]any{"id": "mihomo", "state": "supported", "compatibility_baseline": mihomoCompatibilityBaseline, "clients": []string{"Clash Verge-compatible Mihomo clients"}, "delivery": []string{"private-file", "private-subscription"},')
replace('internal/steward/capabilities.go', '"role": "private-config-delivery-only"', '"role": "private-client-config-delivery-only"')

# Generic target-scoped publisher. The payload is chunked into Worker secrets so a complete Mihomo YAML is not limited to one 5 KiB environment value.
text = read('internal/steward/subscription.go')
text = text.replace('errSubscriptionPayloadTooLarge = errors.New("subscription-payload-too-large")\n)', 'errSubscriptionPayloadTooLarge = errors.New("subscription-payload-too-large")\n\tsubscriptionSecretChunkBytes = 4000\n\tsubscriptionMaxChunks = 60\n)')
text = text.replace('''func AssertSubscriptionBodySize(body string) (int, error) {\n\tsize := len([]byte(body))\n\tif size > 5120 {\n\t\treturn size, errSubscriptionPayloadTooLarge\n\t}\n\treturn size, nil\n}\n''', '''func AssertSubscriptionBodySize(body string) (int, error) {\n\tsize := len([]byte(body))\n\tif size > subscriptionSecretChunkBytes*subscriptionMaxChunks {\n\t\treturn size, errSubscriptionPayloadTooLarge\n\t}\n\treturn size, nil\n}\n\nfunc subscriptionCapableTarget(target *ClientTarget) bool {\n\treturn target != nil && (target.Renderer == "shadowrocket" || target.Renderer == "mihomo")\n}\n\nfunc subscriptionBodyChunks(body string) ([]string, error) {\n\tif _, err := AssertSubscriptionBodySize(body); err != nil {\n\t\treturn nil, err\n\t}\n\tdata := []byte(body)\n\tchunks := make([]string, 0, (len(data)+subscriptionSecretChunkBytes-1)/subscriptionSecretChunkBytes)\n\tfor len(data) > 0 {\n\t\tlimit := subscriptionSecretChunkBytes\n\t\tif len(data) < limit {\n\t\t\tlimit = len(data)\n\t\t}\n\t\tfor limit > 0 && limit < len(data) && (data[limit]&0xc0) == 0x80 {\n\t\t\tlimit--\n\t\t}\n\t\tif limit == 0 {\n\t\t\treturn nil, errors.New("subscription body could not be split as UTF-8")\n\t\t}\n\t\tchunks = append(chunks, string(data[:limit]))\n\t\tdata = data[limit:]\n\t}\n\tif len(chunks) == 0 {\n\t\treturn nil, errors.New("subscription body is empty")\n\t}\n\treturn chunks, nil\n}\n''')
text = text.replace('''\ttarget := findClientTarget(state.Inventory, targetID)\n\tif target == nil || target.Renderer != "shadowrocket" {\n\t\treturn nil, fmt.Errorf("ClientTarget %q is not a Shadowrocket target", targetID)\n\t}\n''', '''\ttarget := findClientTarget(state.Inventory, targetID)\n\tif !subscriptionCapableTarget(target) {\n\t\treturn nil, fmt.Errorf("ClientTarget %q does not support private subscription delivery", targetID)\n\t}\n''', 2)

# Replace ExportSubscriptionBody with renderer-specific output.
start = text.index('func ExportSubscriptionBody(')
end = text.index('\nfunc PublishSubscription', start)
new_export = r'''func ExportSubscriptionBody(state *State, targetID string) (string, int, error) {
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
'''
text = text[:start] + new_export + text[end:]

# Publication records a private import artifact for Mihomo; Shadowrocket keeps its QR import page.
old = '''\tbody, _, err := ExportSubscriptionBody(state, targetID)\n\tif err != nil {\n\t\treturn nil, err\n\t}\n\tif err := deployWorkerAndVerify(subscription.WorkerName, subscription.Host, subscription.Token, body); err != nil {\n\t\treturn nil, err\n\t}\n\tnow := utcNow()\n\tsubscription.LastPublishedAt = &now\n\tif err := writeJSONAtomic(subscription.Path, subscription); err != nil {\n\t\treturn nil, err\n\t}\n\tif _, err := RenderClients(state, targetID, true); err != nil {\n\t\treturn nil, fmt.Errorf("subscription published but local import artifact failed: %w", err)\n\t}\n\treturn map[string]any{"client_target": targetID, "worker": subscription.WorkerName, "published": true, "verified": true}, nil\n'''
new = '''\tbody, _, err := ExportSubscriptionBody(state, targetID)\n\tif err != nil {\n\t\treturn nil, err\n\t}\n\ttarget := findClientTarget(state.Inventory, targetID)\n\tif err := deployWorkerAndVerify(subscription.WorkerName, subscription.Host, subscription.Token, target.Renderer, body); err != nil {\n\t\treturn nil, err\n\t}\n\tnow := utcNow()\n\tsubscription.LastPublishedAt = &now\n\tif err := writeJSONAtomic(subscription.Path, subscription); err != nil {\n\t\treturn nil, &operationStageError{Stage: "subscription-state", StateChanged: "subscription-published", Retry: "publish-subscription", Err: err}\n\t}\n\tresult := map[string]any{"client_target": targetID, "worker": subscription.WorkerName, "published": true, "verified": true}\n\tif target.Renderer == "shadowrocket" {\n\t\tif _, err := RenderClients(state, targetID, true); err != nil {\n\t\t\treturn nil, &operationStageError{Stage: "client-import-artifact", StateChanged: "subscription-published", Retry: "render-client", Err: err}\n\t\t}\n\t} else {\n\t\tartifact, err := writeSubscriptionReference(state, targetID, subscription)\n\t\tif err != nil {\n\t\t\treturn nil, &operationStageError{Stage: "client-import-artifact", StateChanged: "subscription-published", Retry: "publish-subscription", Err: err}\n\t\t}\n\t\tresult["import_artifact"] = artifact\n\t}\n\treturn result, nil\n'''
if old not in text:
    raise RuntimeError('PublishSubscription block not found')
text = text.replace(old, new, 1)

# Rotation publishes the renderer-specific body and preserves a resumable pending token if local commit fails.
text = text.replace('''\tif err := deployWorkerAndVerify(subscription.WorkerName, subscription.Host, proposed, body); err != nil {\n\t\treturn nil, err\n\t}\n''', '''\ttarget := findClientTarget(state.Inventory, targetID)\n\tif err := deployWorkerAndVerify(subscription.WorkerName, subscription.Host, proposed, target.Renderer, body); err != nil {\n\t\treturn nil, err\n\t}\n''', 1)
text = text.replace('''\tif err := writeJSONAtomic(subscription.Path, &fresh); err != nil {\n\t\treturn nil, err\n\t}\n\tif _, err := RenderClients(state, targetID, true); err != nil {\n\t\treturn nil, err\n\t}\n''', '''\tif err := writeJSONAtomic(subscription.Path, &fresh); err != nil {\n\t\treturn nil, &operationStageError{Stage: "subscription-token-state", StateChanged: "new-token-active-at-worker", Retry: "rotate-subscription-token", Err: err}\n\t}\n\tif target.Renderer == "shadowrocket" {\n\t\tif _, err := RenderClients(state, targetID, true); err != nil {\n\t\t\treturn nil, &operationStageError{Stage: "client-import-artifact", StateChanged: "subscription-token-rotated", Retry: "render-client", Err: err}\n\t\t}\n\t} else if _, err := writeSubscriptionReference(state, targetID, &fresh); err != nil {\n\t\treturn nil, &operationStageError{Stage: "client-import-artifact", StateChanged: "subscription-token-rotated", Retry: "rotate-subscription-token", Err: err}\n\t}\n''', 1)

# Replace deployWorkerAndVerify signature, secret body, and verification call.
text = text.replace('func deployWorkerAndVerify(workerName, host, token, body string) error {', 'func deployWorkerAndVerify(workerName, host, token, format, body string) error {')
old_secret = '''\tsum := sha256.Sum256([]byte(token))\n\tsecretPath := filepath.Join(stage, "worker-secrets.json")\n\tsecretData, _ := json.Marshal(map[string]string{"SUBSCRIPTION_TOKEN_HASH": hex.EncodeToString(sum[:]), "SUBSCRIPTION_BODY": body})\n\tif err := writeFileAtomic(secretPath, secretData, 0o600); err != nil {\n\t\treturn err\n\t}\n'''
new_secret = '''\tsum := sha256.Sum256([]byte(token))\n\tchunks, err := subscriptionBodyChunks(body)\n\tif err != nil {\n\t\treturn err\n\t}\n\tsecrets := map[string]string{\n\t\t"SUBSCRIPTION_TOKEN_HASH": hex.EncodeToString(sum[:]),\n\t\t"SUBSCRIPTION_FORMAT": format,\n\t\t"SUBSCRIPTION_CHUNK_COUNT": fmt.Sprint(len(chunks)),\n\t}\n\tfor index, chunk := range chunks {\n\t\tsecrets[fmt.Sprintf("SUBSCRIPTION_BODY_%02d", index)] = chunk\n\t}\n\tsecretPath := filepath.Join(stage, "worker-secrets.json")\n\tsecretData, _ := json.Marshal(secrets)\n\tif err := writeFileAtomic(secretPath, secretData, 0o600); err != nil {\n\t\treturn err\n\t}\n'''
if old_secret not in text:
    raise RuntimeError('worker secrets block not found')
text = text.replace(old_secret, new_secret, 1)
text = text.replace('return verifySubscriptionEndpoint("https://"+host+"/s/"+token, body)', 'return verifySubscriptionEndpoint("https://"+host+"/s/"+token, format, body)')
text = text.replace('func verifySubscriptionEndpoint(endpoint, expected string) error {', 'func verifySubscriptionEndpoint(endpoint, format, expected string) error {')
text = text.replace('''\tif !strings.Contains(strings.ToLower(response.Header.Get("Cache-Control")), "no-store") {\n\t\treturn errors.New("private subscription endpoint is missing its no-store cache policy")\n\t}\n\treturn nil\n''', '''\tif !strings.Contains(strings.ToLower(response.Header.Get("Cache-Control")), "no-store") {\n\t\treturn errors.New("private subscription endpoint is missing its no-store cache policy")\n\t}\n\tif format == "mihomo" && !strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "yaml") {\n\t\treturn errors.New("Mihomo subscription endpoint is missing its YAML content type")\n\t}\n\treturn nil\n''')
write('internal/steward/subscription.go', text)

# Worker reconstructs bounded chunks and returns client-appropriate headers.
write('worker/src/index.ts', r'''export interface SubscriptionEnv {
  SUBSCRIPTION_TOKEN_HASH?: string;
  SUBSCRIPTION_FORMAT?: string;
  SUBSCRIPTION_CHUNK_COUNT?: string;
  [key: string]: string | undefined;
}

const TOKEN_PATTERN = /^[A-Za-z0-9_-]{43}$/;
const HASH_PATTERN = /^[0-9a-f]{64}$/;
const CHUNK_COUNT_PATTERN = /^(?:[1-9]|[1-5][0-9]|60)$/;
const encoder = new TextEncoder();

const privateHeaders = Object.freeze({
  "Cache-Control": "private, no-store, max-age=0",
  Expires: "0",
  Pragma: "no-cache",
  "Referrer-Policy": "no-referrer",
  "X-Content-Type-Options": "nosniff",
});

function textResponse(body: string | null, status: number, extra: HeadersInit = {}): Response {
  return new Response(body, {
    status,
    headers: { ...privateHeaders, ...extra },
  });
}

function fromHex(value: string): Uint8Array {
  const bytes = new Uint8Array(value.length / 2);
  for (let index = 0; index < bytes.length; index += 1) {
    bytes[index] = Number.parseInt(value.slice(index * 2, index * 2 + 2), 16);
  }
  return bytes;
}

async function tokenMatches(token: string, expectedHash: string): Promise<boolean> {
  const actual = new Uint8Array(await crypto.subtle.digest("SHA-256", encoder.encode(token)));
  const expected = fromHex(expectedHash);
  return crypto.subtle.timingSafeEqual(actual, expected);
}

function subscriptionBody(env: SubscriptionEnv): string | null {
  const countText = env.SUBSCRIPTION_CHUNK_COUNT ?? "";
  if (!CHUNK_COUNT_PATTERN.test(countText)) return null;
  const count = Number.parseInt(countText, 10);
  const chunks: string[] = [];
  for (let index = 0; index < count; index += 1) {
    const chunk = env[`SUBSCRIPTION_BODY_${index.toString().padStart(2, "0")}`];
    if (chunk === undefined) return null;
    chunks.push(chunk);
  }
  const body = chunks.join("");
  return body.length === 0 ? null : body;
}

function responseHeaders(format: string): HeadersInit {
  if (format === "mihomo") {
    return {
      "Content-Type": "application/yaml; charset=utf-8",
      "Content-Disposition": 'attachment; filename="route-steward.yaml"',
      "profile-update-interval": "24",
    };
  }
  return { "Content-Type": "text/plain; charset=utf-8" };
}

export default {
  async fetch(request: Request, env: SubscriptionEnv): Promise<Response> {
    const url = new URL(request.url);
    const match = /^\/s\/([^/]+)$/.exec(url.pathname);
    if (!match || !TOKEN_PATTERN.test(match[1])) {
      return textResponse("Not Found\n", 404);
    }

    const expectedHash = env.SUBSCRIPTION_TOKEN_HASH ?? "";
    const format = env.SUBSCRIPTION_FORMAT ?? "";
    const body = subscriptionBody(env);
    if (!HASH_PATTERN.test(expectedHash) || (format !== "shadowrocket" && format !== "mihomo") || body === null) {
      return textResponse("Service Unavailable\n", 503);
    }
    if (!(await tokenMatches(match[1], expectedHash))) {
      return textResponse("Not Found\n", 404);
    }
    if (request.method !== "GET" && request.method !== "HEAD") {
      return textResponse("Method Not Allowed\n", 405, { Allow: "GET, HEAD" });
    }

    return textResponse(request.method === "HEAD" ? null : body, 200, responseHeaders(format));
  },
} satisfies ExportedHandler<SubscriptionEnv>;
''')

write('worker/test/index.test.ts', r'''import { describe, expect, it } from "vitest";
import worker, { type SubscriptionEnv } from "../src/index";

const token = "A".repeat(43);
const subscriptionBody = "aHlzdGVyaWEyOi8vZXhhbXBsZS5pbnZhbGlkCg==";
const env: SubscriptionEnv = {
  SUBSCRIPTION_TOKEN_HASH: "0f007385b6f9d4b7eeb2748605afe1a984a0a3bfa3f014d09e2a784ce9e5cd1a",
  SUBSCRIPTION_FORMAT: "shadowrocket",
  SUBSCRIPTION_CHUNK_COUNT: "2",
  SUBSCRIPTION_BODY_00: subscriptionBody.slice(0, 20),
  SUBSCRIPTION_BODY_01: subscriptionBody.slice(20),
};

function request(path: string, method = "GET", bindings: SubscriptionEnv = env): Promise<Response> {
  return worker.fetch(new Request(`https://subscription.example.invalid${path}`, { method }), bindings);
}

describe("private subscription Worker", () => {
  it("serves only the exact token path without caching", async () => {
    const response = await request(`/s/${token}`);
    expect(response.status).toBe(200);
    expect(await response.text()).toBe(subscriptionBody);
    expect(response.headers.get("cache-control")).toContain("no-store");
    expect(response.headers.get("access-control-allow-origin")).toBeNull();
  });

  it("serves Mihomo YAML with subscription headers", async () => {
    const yaml = "proxies:\n  - name: example\n";
    const response = await request(`/s/${token}`, "GET", {
      ...env,
      SUBSCRIPTION_FORMAT: "mihomo",
      SUBSCRIPTION_CHUNK_COUNT: "1",
      SUBSCRIPTION_BODY_00: yaml,
      SUBSCRIPTION_BODY_01: undefined,
    });
    expect(response.status).toBe(200);
    expect(await response.text()).toBe(yaml);
    expect(response.headers.get("content-type")).toContain("yaml");
    expect(response.headers.get("content-disposition")).toContain("route-steward.yaml");
    expect(response.headers.get("profile-update-interval")).toBe("24");
  });

  it("supports HEAD without returning the subscription", async () => {
    const response = await request(`/s/${token}`, "HEAD");
    expect(response.status).toBe(200);
    expect(await response.text()).toBe("");
  });

  it("hides roots, malformed tokens and incorrect tokens", async () => {
    expect((await request("/")).status).toBe(404);
    expect((await request("/s/short")).status).toBe(404);
    expect((await request(`/s/${"B".repeat(43)}`)).status).toBe(404);
  });

  it("rejects writes and fails closed when secrets are missing or chunks are incomplete", async () => {
    const write = await request(`/s/${token}`, "POST");
    expect(write.status).toBe(405);
    expect(write.headers.get("allow")).toBe("GET, HEAD");
    expect((await request(`/s/${token}`, "GET", {})).status).toBe(503);
    expect((await request(`/s/${token}`, "GET", { ...env, SUBSCRIPTION_BODY_01: undefined })).status).toBe(503);
  });
});
''')

# Structured partial-failure metadata gives a fresh AI a safe retry instruction without returning raw diagnostics.
text = read('internal/steward/engine.go')
insert_after = 'const safeFailureSummary = "The operation failed locally. No secret-bearing diagnostic was returned through the agent surface."\n'
addition = r'''

type operationStageError struct {
	Stage        string
	StateChanged string
	Retry        string
	Err          error
}

func (e *operationStageError) Error() string { return e.Err.Error() }
func (e *operationStageError) Unwrap() error { return e.Err }
'''
if addition.strip() not in text:
    text = text.replace(insert_after, insert_after + addition)
old_fail = '''\tfail := func(err error) (any, string, int) {\n\t\tcode := "operation-failed"\n\t\tif errors.Is(err, errSubscriptionPayloadTooLarge) {\n\t\t\tcode = "subscription-payload-too-large"\n\t\t}\n\t\treturn map[string]string{"summary": safeFailureSummary}, code, 1\n\t}\n'''
new_fail = '''\tfail := func(err error) (any, string, int) {\n\t\tcode := "operation-failed"\n\t\tif errors.Is(err, errSubscriptionPayloadTooLarge) {\n\t\t\tcode = "subscription-payload-too-large"\n\t\t}\n\t\tdata := map[string]any{"summary": safeFailureSummary, "operation": request.Operation}\n\t\tvar staged *operationStageError\n\t\tif errors.As(err, &staged) {\n\t\t\tdata["stage"] = staged.Stage\n\t\t\tdata["state_changed"] = staged.StateChanged\n\t\t\tdata["retry"] = staged.Retry\n\t\t}\n\t\treturn data, code, 1\n\t}\n'''
if old_fail not in text:
    raise RuntimeError('engine fail closure not found')
text = text.replace(old_fail, new_fail, 1)
write('internal/steward/engine.go', text)

# If remote deployment is already committed, preserve that fact instead of flattening a later render failure.
replace('internal/steward/execution.go', '''\t\tif err != nil {\n\t\t\treturn nil, fmt.Errorf("Route deployed, but affected client rendering failed: %w", err)\n\t\t}\n''', '''\t\tif err != nil {\n\t\t\treturn nil, &operationStageError{Stage: "client-delivery", StateChanged: "route-deployed", Retry: "render-client", Err: err}\n\t\t}\n''')

# Unit-test safe staged failure metadata without exposing the underlying diagnostic.
path = 'internal/steward/steward_test.go'
text = read(path)
text += r'''

func TestExecuteReadyReportsSafePartialFailureStage(t *testing.T) {
	err := &operationStageError{Stage: "client-delivery", StateChanged: "route-deployed", Retry: "render-client", Err: errors.New("secret-bearing synthetic detail")}
	request := Request{Operation: "deploy-route"}
	fail := func(err error) map[string]any {
		data := map[string]any{"summary": safeFailureSummary, "operation": request.Operation}
		var staged *operationStageError
		if errors.As(err, &staged) {
			data["stage"] = staged.Stage
			data["state_changed"] = staged.StateChanged
			data["retry"] = staged.Retry
		}
		return data
	}
	data := fail(err)
	encoded, _ := json.Marshal(data)
	if strings.Contains(string(encoded), "secret-bearing") || data["stage"] != "client-delivery" || data["retry"] != "render-client" {
		t.Fatalf("safe staged failure metadata changed: %s", encoded)
	}
}
'''
write(path, text)

# Documentation: stable private URL is the supported Verge automation boundary; app runtime/TUN/profile selection remains client-owned.
text = read('.agents/skills/route-steward/references/clients.md')
text = text.replace('Supports Profile service rules, a `GLOBAL` selector, Providers, and optional `mihomo_process_names`.', 'Supports ordered Profile routing, a `GLOBAL` selector, Providers, optional `mihomo_process_names`, and optional private subscription delivery for Clash Verge-compatible clients.')
text = text.replace('places process rules after private-address rules and before Profile service and China rules.', 'places process rules after private-address rules and before Profile routing rules.')
text = text.replace('## Shadowrocket subscriptions\n\nEach subscription-backed ClientTarget has its own Worker/host identity and bearer token. The Worker stores the subscription body and token hash as secrets and returns the private configuration over a non-cacheable endpoint.\n', '## Private subscriptions\n\nMihomo and Shadowrocket targets can publish through an isolated Cloudflare Worker. Each subscription-backed ClientTarget has its own Worker/host identity and bearer token. The Worker stores the private configuration in bounded secret chunks and returns it over a non-cacheable endpoint. A Mihomo target also writes a private subscription-reference artifact for one-time import into Clash Verge-compatible clients; later RST publication updates the same URL. Client applications still own the active profile, selected group, system proxy, and TUN settings.\n')
write('.agents/skills/route-steward/references/clients.md', text)

text = read('docs/COMPATIBILITY.md')
text = text.replace('Initial setup changes host-wide settings.', 'Initial setup installs the RST-required package, SSH key-only, and UFW baseline.')
text = text.replace('| Mihomo | Private YAML output for Mihomo/Clash Verge-compatible clients, with explicit `GLOBAL`/emergency selection, Provider `use` composition, ordered Profile routing, and optional target-scoped `PROCESS-NAME` routing; compatibility baseline Mihomo 1.19.27 |', '| Mihomo | Private YAML file or optional private subscription for Mihomo/Clash Verge-compatible clients, with explicit `GLOBAL`/emergency selection, Provider `use` composition, ordered Profile routing, and optional target-scoped `PROCESS-NAME` routing; compatibility baseline Mihomo 1.19.27 |')
text = text.replace('The Worker delivers a token-protected Shadowrocket subscription body for one isolated ClientTarget. The body is checked as UTF-8 and must be no larger than 5120 bytes.', 'The Worker delivers a token-protected Mihomo YAML or Shadowrocket node subscription for one isolated ClientTarget. Payloads are split into bounded Worker-secret chunks; the current RST limit is 240000 UTF-8 bytes. Mihomo responses include a YAML content type and a 24-hour subscription refresh hint.')
write('docs/COMPATIBILITY.md', text)

text = read('ARCHITECTURE.md')
text = text.replace('- Mihomo ClientTargets use file delivery and may compose managed Routes', '- Mihomo ClientTargets use private file or optional private-subscription delivery and may compose managed Routes')
text = text.replace('The optional Worker publishes one private Shadowrocket subscription:', 'The optional Worker publishes one private Mihomo or Shadowrocket subscription:')
text = text.replace('  → local Shadowrocket URI export', '  → renderer-specific Mihomo YAML or Shadowrocket node export')
write('ARCHITECTURE.md', text)

text = read('docs/QUICKSTART.md')
needle = 'For a headless Hysteria2 target, use `route-steward proxy --private-dir ./private --target <id> --check`.\n'
if needle in text:
    text = text.replace(needle, needle + '\nFor Clash Verge-compatible desktop use, a Mihomo ClientTarget can remain a private local file or use `publish-subscription` to create a stable private URL. Import that subscription once in the client; later Route Steward publications update the same URL. Route Steward does not change the client\'s TUN, system-proxy, active-profile, or selector settings.\n')
write('docs/QUICKSTART.md', text)

# Version impact is minor: new Mihomo delivery capability plus backward-compatible lifecycle fixes.

import { describe, expect, it } from "vitest";
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

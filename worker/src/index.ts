export interface SubscriptionEnv {
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

// Werkbord relay: forwards opaque bytes between one runner and its phones.
// Protocol: docs/RELAY.md. The relay never sees plaintext and holds no keys; the only
// thing it stores is a hash of the room's join key.
import { DurableObject } from "cloudflare:workers";

const MAX_FRAME = 64 * 1024; // bytes per WebSocket message
const MAX_PHONES = 8; // per room
const RATE_BURST = 200; // messages
const RATE_PER_SEC = 50; // sustained

const T_DATA = 0x01;
const T_EVENT = 0x02;
const T_KICK = 0x03;
const EV_CONNECTED = 1;
const EV_DISCONNECTED = 2;

const SUBPROTOCOL = "werkbord.relay.v1";
const AUTH_PREFIX = "wb-auth.";
const ROOM_RE = /^[0-9a-f]{32}$/;
const KEY_RE = /^[A-Za-z0-9_-]{43}$/;

// A browser on another origin (the connector page) asks "why could I not connect?" with a plain request, because a failed
// WebSocket upgrade tells it nothing. The join key already gates everything and no cookie is involved, so any origin may ask.
const CORS = {
  "access-control-allow-origin": "*",
  "access-control-allow-headers": "authorization",
  "access-control-max-age": "86400",
};

const json = (status, body) =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json", ...CORS } });

function bearerKey(request) {
  const m = /^Bearer ([A-Za-z0-9_-]{43})$/.exec(request.headers.get("Authorization") || "");
  return m ? m[1] : null;
}

function parseAuth(request) {
  const offered = (request.headers.get("Sec-WebSocket-Protocol") || "").split(",").map((s) => s.trim());
  if (!offered.includes(SUBPROTOCOL)) return null;
  const a = offered.find((s) => s.startsWith(AUTH_PREFIX));
  const key = a ? a.slice(AUTH_PREFIX.length) : "";
  return KEY_RE.test(key) ? key : null;
}

async function sha256Hex(text) {
  const d = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(text));
  return [...new Uint8Array(d)].map((b) => b.toString(16).padStart(2, "0")).join("");
}

function equalHex(a, b) {
  if (a.length !== b.length) return false;
  let r = 0;
  for (let i = 0; i < a.length; i++) r |= a.charCodeAt(i) ^ b.charCodeAt(i);
  return r === 0;
}

const hex = (u8) => [...u8].map((b) => b.toString(16).padStart(2, "0")).join("");
const fromHex = (s) => Uint8Array.from(s.match(/../g).map((h) => parseInt(h, 16)));

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    if (request.method === "OPTIONS") return new Response(null, { status: 204, headers: CORS });
    if (url.pathname === "/v1/health") return json(200, { ok: true, protocol: 1, maxFrame: MAX_FRAME });
    const st = /^\/v1\/room\/([0-9a-f]{32})\/status$/.exec(url.pathname);
    if (st) {
      if (request.method !== "GET") return json(405, { error: "GET only" });
      if (!bearerKey(request)) return json(401, { error: "missing or malformed join key" });
      return env.ROOMS.get(env.ROOMS.idFromName(st[1])).fetch(request);
    }
    const m = /^\/v1\/room\/([0-9a-f]{32})$/.exec(url.pathname);
    if (!m) return json(404, { error: "not found" });
    if (request.headers.get("Upgrade") !== "websocket") return json(426, { error: "websocket required" });
    const role = url.searchParams.get("role");
    if (role !== "runner" && role !== "phone") return json(400, { error: "role must be runner or phone" });
    if (!parseAuth(request)) return json(401, { error: "missing or malformed join key" });
    const stub = env.ROOMS.get(env.ROOMS.idFromName(m[1]));
    return stub.fetch(request);
  },
};

export class Room extends DurableObject {
  constructor(ctx, env) {
    super(ctx, env);
    // Answered by the runtime without waking this object: an idle room costs no duration
    // and no request. This is what keeps the free plan's 13,000 GB-s/day out of reach.
    this.ctx.setWebSocketAutoResponse(new WebSocketRequestResponsePair("ping", "pong"));
    this.buckets = new Map();
  }

  async fetch(request) {
    if (new URL(request.url).pathname.endsWith("/status")) return this.#status(request);
    const role = new URL(request.url).searchParams.get("role");
    const key = parseAuth(request);
    if (!key) return json(401, { error: "missing or malformed join key" });
    const digest = await sha256Hex(key);
    const stored = await this.ctx.storage.get("auth");

    if (role === "runner") {
      if (stored === undefined) await this.ctx.storage.put("auth", digest); // first runner claims the room
      else if (!equalHex(stored, digest)) return json(401, { error: "wrong join key" });
    } else {
      if (stored === undefined || !equalHex(stored, digest)) return json(401, { error: "wrong join key" });
      if (this.ctx.getWebSockets("runner").length === 0) return json(503, { error: "runner offline" });
      if (this.ctx.getWebSockets("phone").length >= MAX_PHONES) return json(429, { error: "too many phones" });
    }

    const pair = new WebSocketPair();
    const [client, server] = Object.values(pair);

    if (role === "runner") {
      for (const old of this.ctx.getWebSockets("runner")) this.#close(old, 4001, "replaced by a newer runner connection");
      for (const p of this.ctx.getWebSockets("phone")) this.#close(p, 4410, "runner reconnected");
      this.ctx.acceptWebSocket(server, ["runner"]);
    } else {
      const peer = hex(crypto.getRandomValues(new Uint8Array(8)));
      this.ctx.acceptWebSocket(server, ["phone", "p:" + peer]);
      server.serializeAttachment({ peer });
      this.#toRunner(T_EVENT, peer, Uint8Array.of(EV_CONNECTED));
    }
    return new Response(null, { status: 101, webSocket: client, headers: { "Sec-WebSocket-Protocol": SUBPROTOCOL } });
  }

  // Whether a runner is connected: the one thing a phone that could not connect needs to know.
  async #status(request) {
    const key = bearerKey(request);
    const stored = await this.ctx.storage.get("auth");
    if (!key || stored === undefined || !equalHex(stored, await sha256Hex(key))) return json(401, { error: "wrong join key" });
    return json(200, { runner: this.ctx.getWebSockets("runner").length > 0, phones: this.ctx.getWebSockets("phone").length });
  }

  async webSocketMessage(ws, message) {
    if (typeof message === "string") return; // only the auto-handled "ping" is text
    if (message.byteLength > MAX_FRAME) return this.#close(ws, 1009, "frame too large");
    if (!this.#allow(ws)) return this.#close(ws, 4429, "rate limit");
    const bytes = new Uint8Array(message);
    const role = this.ctx.getTags(ws)[0];

    if (role === "phone") {
      const { peer } = ws.deserializeAttachment();
      if (!this.#toRunner(T_DATA, peer, bytes)) this.#close(ws, 4404, "runner offline");
      return;
    }
    // Runner: [type][peer 8][payload...]
    if (bytes.length < 9) return;
    const peer = hex(bytes.subarray(1, 9));
    const [phone] = this.ctx.getWebSockets("p:" + peer);
    if (!phone) return;
    if (bytes[0] === T_DATA) phone.send(bytes.subarray(9));
    else if (bytes[0] === T_KICK) this.#close(phone, 4403, "removed by the runner");
  }

  async webSocketClose(ws) {
    this.#gone(ws);
  }

  async webSocketError(ws) {
    this.#gone(ws);
  }

  #gone(ws) {
    this.buckets.delete(ws);
    const role = this.ctx.getTags(ws)[0];
    if (role === "phone") {
      const { peer } = ws.deserializeAttachment();
      this.#toRunner(T_EVENT, peer, Uint8Array.of(EV_DISCONNECTED));
    } else if (role === "runner") {
      for (const p of this.ctx.getWebSockets("phone")) this.#close(p, 4410, "runner went away");
    }
    this.#close(ws, 1000, "closed");
  }

  #toRunner(type, peer, payload) {
    const [runner] = this.ctx.getWebSockets("runner");
    if (!runner) return false;
    const out = new Uint8Array(9 + payload.length);
    out[0] = type;
    out.set(fromHex(peer), 1);
    out.set(payload, 9);
    runner.send(out);
    return true;
  }

  #close(ws, code, reason) {
    try {
      ws.close(code, reason);
    } catch {}
  }

  // Token bucket per socket. Memory only: a hibernation resets it, which is harmless.
  #allow(ws) {
    const now = Date.now();
    let b = this.buckets.get(ws);
    if (!b) b = { tokens: RATE_BURST, at: now };
    b.tokens = Math.min(RATE_BURST, b.tokens + ((now - b.at) / 1000) * RATE_PER_SEC);
    b.at = now;
    this.buckets.set(ws, b);
    if (b.tokens < 1) return false;
    b.tokens -= 1;
    return true;
  }
}

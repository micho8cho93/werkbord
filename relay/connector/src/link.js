// The phone's side of the relay: one end-to-end encrypted connection to the runner (docs/RELAY.md), and over it the
// HTTP tunnel (internal/relay/tunnel) that lets the service worker answer the app's requests from the controller.
// Plain browser APIs only (WebSocket, fetch, WebCrypto, streams), so it runs in a service worker and in Node's tests.
import { initiate } from "../../e2e/e2e.mjs";

// Why a connection failed, in the words the screens use.
//   offline      the relay is fine and the computer is not connected to it (asleep, off, no network)
//   unavailable  the relay cannot be reached or is at its limit: try again later
//   removed      the computer no longer knows this phone (signed out, or the code ran out): scan again
//   invalid      this link does not match the computer it was made for: scan again
export class LinkError extends Error {
  constructor(reason, message) {
    super(message || reason);
    this.reason = reason;
  }
}

const SUBPROTOCOL = "werkbord.relay.v1";
const AUTH_PREFIX = "wb-auth.";
const PAIRING_PREFIX = "wbr1.";
const CLOSE_KICKED = 4403;
const CLOSE_RUNNER_AWAY = 4404;
const CLOSE_RUNNER_GONE = 4410;

// Tunnel message types (internal/relay/tunnel).
const REQ_HEAD = 1, REQ_BODY = 2, REQ_END = 3, RES_HEAD = 4, RES_BODY = 5, RES_END = 6, ERR = 7, CANCEL = 8;
const CHUNK = 32 * 1024;
const PING_MS = 45_000;

const b64uToBytes = (s) => Uint8Array.from(atob(s.replace(/-/g, "+").replace(/_/g, "/")), (c) => c.charCodeAt(0));
const textEnc = new TextEncoder();
const textDec = new TextDecoder();

/** Reads the text a QR code carries after the "#". Throws LinkError("invalid"). */
export function decodePairing(text) {
  const bad = () => new LinkError("invalid", "That code is not complete. Make a new one on your computer.");
  let s = String(text || "").trim().replace(/^#/, "");
  if (!s.startsWith(PAIRING_PREFIX)) throw bad();
  let p;
  try {
    p = JSON.parse(textDec.decode(b64uToBytes(s.slice(PAIRING_PREFIX.length))));
  } catch {
    throw bad();
  }
  if (!p || p.v !== 1 || !p.u || !p.r || !p.j || !p.k || !p.s) throw bad();
  let relay;
  try {
    const u = new URL(p.u);
    if (u.protocol !== "https:" && !(u.protocol === "http:" && (u.hostname === "localhost" || u.hostname === "127.0.0.1"))) throw 0;
    relay = u;
  } catch {
    throw bad();
  }
  let runnerKey, secret;
  try {
    runnerKey = b64uToBytes(p.k);
    secret = b64uToBytes(p.s);
  } catch {
    throw bad();
  }
  if (runnerKey.length !== 32 || secret.length !== 32) throw bad();
  return { relay, room: p.r, joinKey: p.j, runnerKey, secret, code: typeof p.c === "string" ? p.c : "" };
}

function pack(type, id, payload) {
  const out = new Uint8Array(5 + payload.length);
  out[0] = type;
  new DataView(out.buffer).setUint32(1, id);
  out.set(payload, 5);
  return out;
}

function wsURL(relay, room) {
  const u = new URL(relay.href);
  u.protocol = u.protocol === "https:" ? "wss:" : "ws:";
  u.pathname = `/v1/room/${room}`;
  u.search = "?role=phone";
  u.hash = "";
  return u.href;
}

export class Link {
  /** env lets tests supply WebSocket and fetch. */
  constructor(pairing, env = {}) {
    this.p = pairing;
    this.WebSocket = env.WebSocket || globalThis.WebSocket;
    this.fetch = env.fetch || globalThis.fetch.bind(globalThis);
    this.openTimeout = env.openTimeout ?? 12_000;
    this.pingMs = env.pingMs ?? PING_MS;
    this.ws = null;
    this.session = null;
    this.calls = new Map();
    this.nextId = 1;
    this.alive = false;
    this.sendChain = Promise.resolve();
    this.recvChain = Promise.resolve();
    this.ping = null;
    this.onclose = null;
  }

  /** Connects and proves both ends. Throws LinkError. */
  async open() {
    const { relay, room, joinKey, runnerKey, secret } = this.p;
    const { hello, finish } = await initiate(runnerKey, secret);
    let ws;
    try {
      ws = new this.WebSocket(wsURL(relay, room), [SUBPROTOCOL, AUTH_PREFIX + joinKey]);
    } catch {
      throw await this.diagnose();
    }
    ws.binaryType = "arraybuffer";

    let opened = false;
    let accept;
    try {
      accept = await new Promise((resolve, reject) => {
        const timer = setTimeout(() => reject(null), this.openTimeout); // never answered: ask the relay why
        ws.onopen = () => {
          opened = true;
          ws.send(hello);
        };
        ws.onmessage = (ev) => {
          if (typeof ev.data === "string") return;
          clearTimeout(timer);
          resolve(new Uint8Array(ev.data));
        };
        ws.onclose = (ev) => {
          clearTimeout(timer);
          reject(opened ? closeError(ev.code) : null); // null: it never opened, so ask the relay why
        };
        ws.onerror = () => {
          // A refused upgrade (the relay answered 503, say) is an error before it is a close, and some runtimes never send
          // the close. Before the socket opened there is nothing more to wait for.
          if (opened) return;
          clearTimeout(timer);
          reject(null);
        };
      });
    } catch (e) {
      try {
        ws.close();
      } catch {}
      throw e instanceof LinkError ? e : await this.diagnose();
    }

    try {
      this.session = await finish(accept);
    } catch {
      try {
        ws.close();
      } catch {}
      throw new LinkError("invalid", "This link does not match your computer. Make a new code and scan it.");
    }

    this.ws = ws;
    this.alive = true;
    ws.onmessage = (ev) => this.#receive(ev);
    ws.onclose = (ev) => this.#closed(ev.code);
    ws.onerror = () => {};
    this.ping = setInterval(() => {
      try {
        ws.send("ping");
      } catch {}
    }, this.pingMs);
    this.ping.unref?.();
  }

  /**
   * Why could this phone not connect? A browser is told nothing by a failed WebSocket, so ask the relay with a plain
   * request. Only called after a failure, so it costs the relay nothing while things work.
   */
  async diagnose() {
    const { relay, room, joinKey } = this.p;
    let res;
    try {
      res = await this.fetch(new URL(`/v1/room/${room}/status`, relay).href, {
        headers: { Authorization: "Bearer " + joinKey },
        cache: "no-store",
      });
    } catch {
      return new LinkError("unavailable", "The connection service cannot be reached.");
    }
    if (res.status === 401) return new LinkError("invalid", "This link does not match your computer. Make a new code and scan it.");
    if (res.status !== 200) return new LinkError("unavailable", "The connection service is busy.");
    const body = await res.json().catch(() => ({}));
    return body.runner === false
      ? new LinkError("offline", "Your computer is offline.")
      : new LinkError("unavailable", "The connection service is busy.");
  }

  /**
   * Sends one request. Resolves with the response's status, headers and body (a stream) as soon as the head arrives.
   * body is a Uint8Array or null; signal aborts it.
   */
  async request({ method, path, headers = {}, body = null, signal }) {
    if (!this.alive) throw new LinkError("unavailable", "not connected");
    const id = this.nextId++;
    const call = { head: null, ctrl: null };
    const headed = new Promise((resolve, reject) => {
      call.resolve = resolve;
      call.reject = reject;
    });
    call.stream = new ReadableStream({
      start: (c) => {
        call.ctrl = c;
      },
      cancel: () => this.#cancel(id),
    });
    this.calls.set(id, call);
    signal?.addEventListener("abort", () => this.#cancel(id), { once: true });

    const head = textEnc.encode(JSON.stringify({ m: method, p: path, h: headers }));
    this.#send(REQ_HEAD, id, head);
    if (body) for (let o = 0; o < body.length; o += CHUNK) this.#send(REQ_BODY, id, body.subarray(o, o + CHUNK));
    this.#send(REQ_END, id, new Uint8Array(0));

    const h = await headed;
    return { status: h.s, headers: h.h || {}, body: call.stream };
  }

  close() {
    this.#closed(1000);
    try {
      this.ws?.close();
    } catch {}
  }

  #send(type, id, payload) {
    this.sendChain = this.sendChain.then(async () => {
      if (!this.alive) return;
      this.ws.send(await this.session.seal(pack(type, id, payload)));
    }).catch(() => this.#closed(1006));
    return this.sendChain;
  }

  #cancel(id) {
    if (!this.calls.has(id)) return;
    this.calls.delete(id);
    this.#send(CANCEL, id, new Uint8Array(0));
  }

  #receive(ev) {
    if (typeof ev.data === "string") return;
    const frame = new Uint8Array(ev.data);
    // Frames must be opened in the order they arrived: the counter only goes up.
    this.recvChain = this.recvChain.then(async () => {
      let msg;
      try {
        msg = await this.session.open(frame);
      } catch {
        return this.#closed(1006);
      }
      if (msg.length < 5) return;
      const type = msg[0];
      const id = new DataView(msg.buffer, msg.byteOffset).getUint32(1);
      const payload = msg.subarray(5);
      const call = this.calls.get(id);
      if (!call) return;
      switch (type) {
        case RES_HEAD:
          call.head = JSON.parse(textDec.decode(payload));
          call.resolve(call.head);
          break;
        case RES_BODY:
          call.ctrl.enqueue(payload.slice());
          break;
        case RES_END:
          this.calls.delete(id);
          call.ctrl.close();
          break;
        case ERR: {
          this.calls.delete(id);
          const f = JSON.parse(textDec.decode(payload));
          const err = new LinkError("unavailable", f.m || "the request failed");
          if (call.head) call.ctrl.error(err);
          else call.reject(err);
          break;
        }
      }
    });
  }

  #closed(code) {
    if (!this.alive) return;
    this.alive = false;
    clearInterval(this.ping);
    const err = closeError(code);
    for (const call of this.calls.values()) {
      if (call.head) call.ctrl.error(err);
      else call.reject(err);
    }
    this.calls.clear();
    this.onclose?.(err);
  }
}

function closeError(code) {
  switch (code) {
    case CLOSE_KICKED:
      return new LinkError("removed", "This phone was signed out. Scan a new code on your computer.");
    case CLOSE_RUNNER_AWAY:
    case CLOSE_RUNNER_GONE:
      return new LinkError("offline", "Your computer is offline.");
    default:
      return new LinkError("unavailable", "The connection was lost.");
  }
}

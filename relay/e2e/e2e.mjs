// Browser/Node implementation of the phone side of the relay's end-to-end encryption
// (WebCrypto only). Wire format: docs/RELAY.md. It must reproduce
// relay/conformance/e2e-vectors.json byte for byte; e2e.test.mjs checks that.
const subtle = globalThis.crypto.subtle;
const enc = new TextEncoder();

const TYPE_HELLO = 1, TYPE_ACCEPT = 2, TYPE_DATA = 3, VERSION = 1;
export const MAX_PLAINTEXT = 60 * 1024;

const cat = (...parts) => {
  const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0));
  let o = 0;
  for (const p of parts) { out.set(p, o); o += p.length; }
  return out;
};
const sha256 = async (...parts) => new Uint8Array(await subtle.digest("SHA-256", cat(...parts)));

// WebCrypto imports X25519 private keys only as PKCS#8.
const PKCS8_PREFIX = Uint8Array.from([0x30,0x2e,0x02,0x01,0x00,0x30,0x05,0x06,0x03,0x2b,0x65,0x6e,0x04,0x22,0x04,0x20]);
const b64urlToBytes = (s) => Uint8Array.from(atob(s.replace(/-/g, "+").replace(/_/g, "/")), (c) => c.charCodeAt(0));

async function keyFromSeed(seed) {
  const priv = await subtle.importKey("pkcs8", cat(PKCS8_PREFIX, seed), "X25519", true, ["deriveBits"]);
  const jwk = await subtle.exportKey("jwk", priv);
  return { priv, pub: b64urlToBytes(jwk.x) };
}
const importPub = (raw) => subtle.importKey("raw", raw, "X25519", false, []);
const dh = async (priv, pubRaw) => new Uint8Array(await subtle.deriveBits({ name: "X25519", public: await importPub(pubRaw) }, priv, 256));

export async function pskId(psk) {
  return (await sha256(enc.encode("werkbord-relay-psk-id"), psk)).slice(0, 8);
}

const nonce = (c) => { const n = new Uint8Array(12); new DataView(n.buffer).setBigUint64(4, BigInt(c)); return n; };
const aes = (k) => subtle.importKey("raw", k, "AES-GCM", false, ["encrypt", "decrypt"]);

export class Session {
  constructor(sendKey, recvKey) { this.sendKey = sendKey; this.recvKey = recvKey; this.sendN = 0n; this.recvN = -1n; }

  async seal(plaintext) {
    if (plaintext.length > MAX_PLAINTEXT) throw new Error("message too large");
    const head = new Uint8Array(9);
    head[0] = TYPE_DATA;
    new DataView(head.buffer).setBigUint64(1, this.sendN);
    const ct = new Uint8Array(await subtle.encrypt({ name: "AES-GCM", iv: nonce(this.sendN), additionalData: head }, this.sendKey, plaintext));
    this.sendN += 1n;
    return cat(head, ct);
  }

  async open(frame) {
    if (frame.length < 25 || frame[0] !== TYPE_DATA) throw new Error("malformed frame");
    const c = new DataView(frame.buffer, frame.byteOffset).getBigUint64(1);
    if (c <= this.recvN) throw new Error("replayed or reordered frame");
    let pt;
    try {
      pt = new Uint8Array(await subtle.decrypt({ name: "AES-GCM", iv: nonce(c), additionalData: frame.slice(0, 9) }, this.recvKey, frame.slice(9)));
    } catch { throw new Error("authentication failed"); }
    this.recvN = c;
    return pt;
  }
}

// Start a handshake from the phone. `ephemeralSeed` is for tests only.
export async function initiate(runnerPublic, psk, ephemeralSeed) {
  const eph = await keyFromSeed(ephemeralSeed ?? crypto.getRandomValues(new Uint8Array(32)));
  const hello = cat(Uint8Array.of(TYPE_HELLO, VERSION), await pskId(psk), eph.pub);
  return {
    hello,
    async finish(accept) {
      if (accept.length < 49 || accept[0] !== TYPE_ACCEPT) throw new Error("malformed accept");
      const re = accept.slice(1, 33);
      const h = await sha256(enc.encode("werkbord-relay-e2e-v1"), hello, re, runnerPublic);
      const ikm = cat(await dh(eph.priv, runnerPublic), await dh(eph.priv, re), psk);
      const base = await subtle.importKey("raw", ikm, "HKDF", false, ["deriveBits"]);
      const okm = new Uint8Array(await subtle.deriveBits({ name: "HKDF", hash: "SHA-256", salt: h, info: enc.encode("werkbord-relay-keys") }, base, 512));
      const s = new Session(await aes(okm.slice(0, 32)), await aes(okm.slice(32)));
      let proof;
      try {
        proof = new Uint8Array(await subtle.decrypt({ name: "AES-GCM", iv: nonce(0), additionalData: accept.slice(0, 33) }, s.recvKey, accept.slice(33)));
      } catch { throw new Error("authentication failed"); }
      if (new TextDecoder().decode(proof) !== "wb-ok") throw new Error("authentication failed");
      s.recvN = 0n;
      return s;
    },
  };
}

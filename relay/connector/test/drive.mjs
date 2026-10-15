// Drives the phone side of the relay with the same code the service worker runs (src/link.js), for the Go tests in
// internal/relay/tunnel: Node plays the browser, Go plays the relay and the runner.
//
//   node drive.mjs <mode> <pairing text>
// Prints one JSON line. Modes: tunnel (the request/response behaviour), connect (just try to connect).
import { Link, LinkError, decodePairing } from "../src/link.js";

const [mode, text] = process.argv.slice(2);
const pairing = decodePairing(text);
const link = new Link(pairing, { openTimeout: 5000, pingMs: 1000 });

const readAll = async (stream) => {
  const parts = [];
  for await (const c of stream) parts.push(c);
  const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0));
  let o = 0;
  for (const p of parts) { out.set(p, o); o += p.length; }
  return out;
};
const dec = new TextDecoder();

async function main() {
  try {
    await link.open();
  } catch (e) {
    return { connected: false, reason: e instanceof LinkError ? e.reason : "error:" + e.message };
  }
  if (mode === "connect") return { connected: true };

  const out = { connected: true };
  let r = await link.request({ method: "GET", path: "/hello?x=1", headers: { accept: "text/plain", cookie: "dropped" } });
  out.hello = { status: r.status, type: r.headers["content-type"], body: dec.decode(await readAll(r.body)) };

  const big = new Uint8Array(200 * 1024).map((_, i) => (i * 7) & 255);
  r = await link.request({ method: "POST", path: "/echo", headers: { "content-type": "application/octet-stream" }, body: big });
  const echoed = await readAll(r.body);
  out.echo = { status: r.status, same: echoed.length === big.length && echoed.every((b, i) => b === big[i]) };

  r = await link.request({ method: "GET", path: "/big" });
  const bigBody = await readAll(r.body);
  out.big = { length: bigBody.length, ok: bigBody.every((b, i) => b === ((i * 3) & 255)) };

  r = await link.request({ method: "GET", path: "/stream" });
  const reader = r.body.getReader();
  const times = [];
  const t0 = Date.now();
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    times.push([Date.now() - t0, dec.decode(value)]);
  }
  out.stream = { chunks: times.map((t) => t[1]), spread: times.at(-1)[0] - times[0][0] };

  r = await link.request({ method: "GET", path: "/forbidden" });
  out.forbidden = { status: r.status, body: dec.decode(await readAll(r.body)) };

  // A request the browser gives up on: the runner must be told to stop.
  r = await link.request({ method: "GET", path: "/hang" });
  await r.body.cancel();
  await new Promise((res) => setTimeout(res, 300));
  out.cancelled = true;

  // Many at once, answered out of order.
  const many = await Promise.all(Array.from({ length: 12 }, async (_, i) => {
    const q = await link.request({ method: "GET", path: "/n/" + i });
    return dec.decode(await readAll(q.body));
  }));
  out.many = many;

  r = await link.request({ method: "GET", path: "/enroll-check" });
  out.enrollCheck = dec.decode(await readAll(r.body));
  link.close();
  return out;
}

main().then(
  (o) => { console.log(JSON.stringify(o)); process.exit(0); },
  (e) => { console.log(JSON.stringify({ error: String(e && e.stack || e) })); process.exit(1); },
);

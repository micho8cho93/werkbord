import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { initiate } from "./e2e.mjs";

const v = JSON.parse(readFileSync(new URL("../conformance/e2e-vectors.json", import.meta.url)));
const fromHex = (s) => Uint8Array.from(s.match(/../g) ?? [], (h) => parseInt(h, 16));
const toHex = (u) => [...u].map((b) => b.toString(16).padStart(2, "0")).join("");

test("reproduces the Go vectors byte for byte", async () => {
  const { hello, finish } = await initiate(fromHex(v.RunnerPublic), fromHex(v.PSK), fromHex(v.PhoneEphemeralSeed));
  assert.equal(toHex(hello), v.Hello);
  const s = await finish(fromHex(v.Accept));
  for (const f of v.Frames) {
    if (f.From === "phone") assert.equal(toHex(await s.seal(fromHex(f.Plaintext))), f.Frame);
    else assert.equal(toHex(await s.open(fromHex(f.Frame))), f.Plaintext);
  }
});

test("rejects a tampered accept and a replayed frame", async () => {
  const { finish } = await initiate(fromHex(v.RunnerPublic), fromHex(v.PSK), fromHex(v.PhoneEphemeralSeed));
  const bad = fromHex(v.Accept);
  bad[bad.length - 1] ^= 1;
  await assert.rejects(finish(bad), /authentication/);
  const { finish: f2 } = await initiate(fromHex(v.RunnerPublic), fromHex(v.PSK), fromHex(v.PhoneEphemeralSeed));
  const s = await f2(fromHex(v.Accept));
  const frame = fromHex(v.Frames.find((x) => x.From === "runner").Frame);
  await s.open(frame);
  await assert.rejects(s.open(frame), /replayed/);
});

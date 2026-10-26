import test from 'node:test';
import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {mkdtempSync, readFileSync, rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {canonicalClaims, verifyKey} from './premium-key.mjs';

test('browser verifies actual vendor-tool keys, including Go JSON escaping and UTF-8', async () => {
  const dir = mkdtempSync(path.join(tmpdir(), 'werkbord-premium-'));
  const root = path.resolve(import.meta.dirname, '../../../..');
  const privatePath = path.join(dir, 'issuer.pem');
  const publicPath = path.join(dir, 'issuer.pub');
  const keyPath = path.join(dir, 'premium.txt');
  try {
    execFileSync('go', ['run', './cmd/werkbord-team/vendor', 'keygen', '--private-out', privatePath, '--public-out', publicPath], {cwd: root});
    execFileSync('go', ['run', './cmd/werkbord-team/vendor', 'premium', '--out', keyPath, '--id', 'pk_test', '--holder', 'Ada <&> Zoë\u2028'], {cwd: root, input: readFileSync(privatePath)});
    const key = readFileSync(keyPath, 'utf8').trim();
    const pub = readFileSync(publicPath, 'utf8').trim();
    const claims = await verifyKey(key, pub);
    assert.equal(claims.id, 'pk_test');
    assert.equal(claims.holder, 'Ada <&> Zoë\u2028');
    await assert.rejects(verifyKey(key.replace('wbpremium-', 'license-'), pub));
    const [payload, signature] = key.slice('wbpremium-'.length).split('.');
    const altered = signature[0] === 'A' ? 'B' : 'A';
    await assert.rejects(verifyKey('wbpremium-' + payload + '.' + altered + signature.slice(1), pub));
    await assert.rejects(verifyKey('wbpremium-' + Buffer.from(JSON.stringify({...claims, seats: 3})).toString('base64url') + '.' + signature, pub));
  } finally { rmSync(dir, {recursive: true, force: true}); }
});

test('rejects fields the customer app rejects', () => {
  const valid = {schema: 1, product: 'werkbord', id: 'pk_1', issuedAt: '2026-10-26T08:00:00Z'};
  for (const change of [{holder: 'é'.repeat(41)}, {holder: 'Ada\n'}, {issuedAt: '2026-02-31T08:00:00Z'}, {expiresAt: '2027-01-01'}, {product: 'werkbord-team'}, {id: '../key'}]) {
    assert.throws(() => canonicalClaims({...valid, ...change}));
  }
});

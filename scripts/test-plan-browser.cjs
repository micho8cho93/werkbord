// The plan, as a person meets it in the Team console: a real Team service built with a test issuer's public key (so unlock keys are
// really checked), a team of two that is free, a third person who needs the owner on Premium, and a member without a key who can
// read a team of three and is told why they cannot change it. No release script, installer or OS service is involved.
//
//   node scripts/test-plan-browser.cjs
const fs = require('node:fs'), os = require('node:os'), path = require('node:path'), cp = require('node:child_process'), net = require('node:net'), assert = require('node:assert/strict');
const { once } = require('node:events');
const { chromium } = require('../web/node_modules/playwright');

const root = path.resolve(__dirname, '..');
const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'plan-browser-'));
const artifacts = process.env.BROWSER_ARTIFACT_DIR || path.join(temp, 'screenshots');
fs.mkdirSync(artifacts, { recursive: true });
const children = [];
let browser;

function run(cmd, args, env = {}, input) {
  const r = cp.spawnSync(cmd, args, { cwd: root, env: { ...process.env, ...env }, encoding: 'utf8', timeout: 240000, input });
  if (r.status !== 0) throw Error(`${cmd} ${args.join(' ')}: ${r.stderr || r.stdout || r.error}`);
  return r.stdout;
}
async function port() { const s = net.createServer(); s.listen(0, '127.0.0.1'); await once(s, 'listening'); const n = s.address().port; await new Promise((r) => s.close(r)); return n; }
const delay = (ms) => new Promise((r) => setTimeout(r, ms));

async function main() {
  // A throwaway issuer: the private key never leaves this directory, and only its public half is stamped into the test build.
  const priv = path.join(temp, 'issuer.pem'), pub = path.join(temp, 'issuer.pub');
  run('go', ['run', './cmd/werkbord-team/vendor', 'keygen', '--private-out', priv, '--public-out', pub]);
  const issuer = fs.readFileSync(pub, 'utf8').trim();
  const mint = (id) => { const out = path.join(temp, id + '.key'); run('go', ['run', './cmd/werkbord-team/vendor', 'premium', '--out', out, '--id', id, '--holder', id], {}, fs.readFileSync(priv)); return fs.readFileSync(out, 'utf8').trim(); };
  const ownerKey = mint('pk_owner'), thirdKey = mint('pk_third');

  const bin = path.join(temp, 'werkbord-team');
  run('go', ['build', '-trimpath', '-ldflags', `-X werkbord/internal/entitlement.issuerPublicKey=${issuer}`, '-o', bin, './cmd/werkbord-team']);
  const addr = '127.0.0.1:' + await port(), base = 'http://' + addr;
  const env = { WERKBORD_TEAM_DATA_DIR: path.join(temp, 'data'), WERKBORD_TEAM_ADDR: addr, WERKBORD_TEAM_STORAGE: 'single-file', WERKBORD_TEAM_KEY_STORAGE: 'file', WERKBORD_TEAM_NETWORK_NODE: 'off' };
  const created = run(bin, ['workspace', 'create', '--network=false', '--name', 'Plan E2E', '--owner', 'Ada'], env);
  const ownerToken = created.match(/wbt_[a-zA-Z0-9]+/)[0];
  const server = cp.spawn(bin, ['serve'], { cwd: root, env: { ...process.env, ...env }, stdio: ['ignore', 'pipe', 'pipe'] });
  server.diagnostics = ''; server.stdout.on('data', (d) => server.diagnostics += d); server.stderr.on('data', (d) => server.diagnostics += d);
  children.push(server);
  for (let i = 0; ; i++) {
    if (server.exitCode !== null) throw Error(server.diagnostics);
    try { if ((await fetch(base + '/api/team/v1/health')).ok) break; } catch { /* not up yet */ }
    if (i > 300) throw Error('the Team service did not start: ' + server.diagnostics);
    await delay(100);
  }
  const api = async (token, method, route, body) => {
    const res = await fetch(base + '/api/team/v1' + route, { method, headers: { Authorization: 'Bearer ' + token, ...(body ? { 'Content-Type': 'application/json' } : {}) }, body: body ? JSON.stringify(body) : undefined });
    const data = await res.json().catch(() => ({}));
    return { status: res.status, data };
  };

  // A team of two: free, and the people page says how full it is.
  const partner = await api(ownerToken, 'POST', '/members', { name: 'Partner', role: 'admin' });
  assert.equal(partner.status, 201, JSON.stringify(partner.data));
  assert.equal((await api(ownerToken, 'GET', '/plan')).data.people, 2);
  const refused = await api(ownerToken, 'POST', '/members', { name: 'Third', role: 'member' });
  assert.equal(refused.status, 402, 'a third person on the Free plan');
  assert.equal(refused.data.error.code, 'premium_required');
  assert.equal((await api(ownerToken, 'PUT', '/me/premium', { key: 'wbpremium-nonsense' })).status, 400, 'a key that does not check');

  browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
  page.setDefaultTimeout(30000);
  const errors = [];
  page.on('pageerror', (e) => errors.push(e.message));

  await page.goto(`${base}/?tab=members#token=${encodeURIComponent(ownerToken)}`);
  const note = page.getByTestId('plan-note');
  await note.waitFor();
  assert.match(await note.innerText(), /2 of 2 people on the Free plan/);
  assert.equal(await page.getByTestId('plan-read-only').count(), 0, 'a team of two is never read-only');
  await page.screenshot({ path: path.join(artifacts, 'plan-members-free.png') });
  console.log('PASS a team of two is free and the people page says how full it is');

  // The owner unlocks Premium; a third person can now be added, and has no key of their own yet.
  const presented = await api(ownerToken, 'PUT', '/me/premium', { key: ownerKey });
  assert.equal(presented.status, 200, JSON.stringify(presented.data));
  assert.equal(presented.data.premium, true);
  const third = await api(ownerToken, 'POST', '/members', { name: 'Third', role: 'member' });
  assert.equal(third.status, 201, JSON.stringify(third.data));
  assert.equal((await api(ownerToken, 'PUT', '/me/premium', { key: ownerKey })).status, 200, 'presenting the same key again is fine');
  const dup = await api(third.data.token, 'PUT', '/me/premium', { key: ownerKey });
  assert.equal(dup.status, 409, 'one key cannot fill a team');

  // The partner has no key in a team of three: read-only, told why, and the refusal is a prompt, not a raw error.
  await page.goto(`${base}/?tab=members#token=${encodeURIComponent(partner.data.token)}`);
  await page.reload();
  const banner = page.getByTestId('plan-read-only');
  await banner.waitFor();
  assert.match(await banner.innerText(), /3 people, so everyone in it needs Premium/);
  assert.equal(await page.getByTestId('plan-note').count(), 0, 'the Free-plan line goes once the owner is on Premium');
  await page.screenshot({ path: path.join(artifacts, 'plan-read-only.png') });
  await page.evaluate(() => act(() => api('POST', '/projects', { name: 'Not allowed' })));
  const dialog = page.getByTestId('upgrade-dialog');
  await dialog.waitFor();
  assert.match(await dialog.innerText(), /Teams of 3 or more need Premium/);
  assert.match(await dialog.innerText(), /Premium in Settings|unlock Premium/);
  assert.equal(await page.locator('p.error').count(), 0, 'the refusal is not also an error line');
  await page.screenshot({ path: path.join(artifacts, 'plan-upgrade-prompt.png') });
  await dialog.getByRole('button', { name: 'Decline' }).click();
  await page.waitForFunction(() => !document.querySelector('[data-testid=upgrade-dialog]'));
  console.log('PASS a member without a key reads a team of three, is told why, and the refusal asks them to unlock Premium');

  // Reading is untouched, and so is what keeps the team safe: the partner can still replace a leaked credential.
  assert.equal((await api(partner.data.token, 'GET', '/projects')).status, 200);
  const reissued = await api(partner.data.token, 'POST', `/members/${partner.data.member.id}/token`);
  assert.equal(reissued.status, 200, JSON.stringify(reissued.data));

  // A key of their own ends it.
  assert.equal((await api(reissued.data.token, 'PUT', '/me/premium', { key: thirdKey })).data.readOnly, false);
  assert.equal((await api(reissued.data.token, 'POST', '/projects', { name: 'Allowed now' })).status, 201);
  assert.deepEqual(errors, [], 'no page errors');
  console.log('PASS presenting a key makes the member a full member again');
}

main().then(() => cleanup(0), (e) => { console.error(e); cleanup(1); });

async function cleanup(code) {
  try { await browser?.close(); } catch { /* already gone */ }
  for (const c of children) try { c.kill(); } catch { /* already gone */ }
  if (!process.env.KEEP_TEMP) fs.rmSync(temp, { recursive: true, force: true });
  process.exit(code);
}

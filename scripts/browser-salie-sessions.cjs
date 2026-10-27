// Salie Sessions Phase 1: real API/engine and disposable SQLite, with a scripted provider.
// Run: node scripts/browser-salie-sessions.cjs
const fs = require('node:fs'), os = require('node:os'), path = require('node:path');
const cp = require('node:child_process'), net = require('node:net');
const { once } = require('node:events');
const assert = require('node:assert/strict');
const root = path.resolve(__dirname, '..');
const { chromium } = require(path.join(root, 'web/node_modules/playwright'));
const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'werkbord-personal-'));
const artifacts = process.env.BROWSER_ARTIFACT_DIR || path.join(root, 'output/playwright/salie-sessions');
fs.mkdirSync(artifacts, { recursive: true });
const token = 'disposable-browser-credential';
const children = [];
const delay = ms => new Promise(r => setTimeout(r, ms));
let browser, lastPage;
async function freePort() {
  const server = net.createServer(); server.listen(0, '127.0.0.1');
  await once(server, 'listening'); const port = server.address().port;
  await new Promise(resolve => server.close(resolve)); return port;
}
function start(binary, args, cwd, env) {
  const child = cp.spawn(binary, args, { cwd, env: { ...process.env, ...env }, stdio: ['ignore', 'pipe', 'pipe'] });
  child.diagnostics = '';
  for (const stream of [child.stdout, child.stderr]) stream.on('data', d => child.diagnostics += d);
  children.push(child); return child;
}
async function stop(child) {
  if (child.exitCode !== null) return;
  child.kill('SIGTERM');
  await Promise.race([once(child, 'exit'), delay(5000)]);
  if (child.exitCode === null) child.kill('SIGKILL');
}
async function until(fn, message) {
  for (let i = 0; i < 120; i++) { if (await fn()) return; await delay(100); }
  throw new Error(message);
}
async function ready(url, child) {
  await until(async () => {
    if (child.exitCode !== null) throw Error(child.diagnostics);
    return fetch(url).then(r => r.ok).catch(() => false);
  }, 'Server did not start: ' + child.diagnostics);
}
async function api(base, method, url, data, status) {
  const res = await fetch(base + '/api/personal/v1' + url, {
    method, headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
    body: data === undefined ? undefined : JSON.stringify(data),
  });
  if (status) { assert.equal(res.status, status); return; }
  const raw = await res.text(); assert.ok(res.ok, `${method} ${url}: ${res.status} ${raw}`);
  return raw ? JSON.parse(raw) : undefined;
}
const ok = text => console.log('PASS ' + text);
(async () => {
  const binary = path.join(temp, 'fixture');
  cp.execFileSync('go', ['build', '-o', binary, './scripts/browser-fixture'], { cwd: root, stdio: 'inherit' });
  const apiPort = await freePort(), webPort = await freePort();
  const base = `http://127.0.0.1:${apiPort}`, web = `http://127.0.0.1:${webPort}`;
  const env = { WERKBORD_BROWSER_ADDR: `127.0.0.1:${apiPort}`, WERKBORD_BROWSER_DIR: temp, WERKBORD_BROWSER_ASSISTANT: '1' };
  let controller = start(binary, [], root, env);
  await ready(base + '/api/health', controller);
  const completed = await fetch(base + '/api/onboarding/complete', { method: 'POST', headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' }, body: '{}' });
  assert.ok(completed.ok);
  const vite = start(process.execPath, [path.join(root, 'web/node_modules/vite/bin/vite.js'), '--host', '127.0.0.1', '--port', String(webPort), '--strictPort'], path.join(root, 'web'), { WERKBORD_URL: base });
  await ready(web, vite);
  browser = await chromium.launch({ headless: true, ...(process.env.BROWSER_EXECUTABLE ? { executablePath: process.env.BROWSER_EXECUTABLE } : {}) });
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  await context.addInitScript(token => { localStorage.setItem('werkbord.token', token); localStorage.setItem('werkbord.theme', 'light'); }, token);
  const page = await context.newPage(); lastPage = page;
  const errors = [], modelRequests = [];
  page.on('pageerror', e => errors.push(e.message));
  page.on('request', r => { if (r.method() === 'POST' && /\/api\/assistant/.test(r.url())) modelRequests.push(r.url()); });
  page.setDefaultTimeout(10000);

  await page.goto(web + '/#/personal');
  await page.getByRole('button', { name: 'Create with Salie', exact: true }).click();
  const box = page.getByRole('textbox', { name: 'Message to Salie' });
  const newTab = async () => {
    await page.getByRole('button', { name: 'New tab', exact: true }).click();
    assert.equal(await page.getByRole('dialog', { name: 'New tab', exact: true }).getByText('Coding Agents', {exact:true}).count(), 0);
    await page.getByRole('button', { name: 'New Salie Conversation', exact: true }).click();
    await box.waitFor();
  };
  for (let i=0;i<5;i++) {
    if(i) await newTab();
    await box.fill('hang on session '+i);
    await page.getByRole('button', {name:'Send',exact:true}).click();
    await page.locator('article.reply').getByText('Working on it',{exact:true}).waitFor();
  }
  assert.equal(await page.getByRole('tab').count(),5);
  const tabIds = await page.getByRole('tab').evaluateAll(tabs=>tabs.map(t=>t.id));
  for(let i=0;i<5;i++) {
    await page.locator('[id="'+tabIds[i]+'"]').click();
    assert.equal(await page.locator('.user').count(),1);
    await page.getByTestId('assistant-log').getByText('hang on session '+i,{exact:true}).waitFor();
    await page.getByRole('button',{name:'Stop the reply',exact:true}).waitFor();
  }
  await page.getByRole('button',{name:'New tab',exact:true}).click();
  await page.getByRole('button',{name:'Rename current tab',exact:true}).click();
  await page.getByRole('textbox',{name:'Conversation title',exact:true}).fill('Release planning');
  await page.getByRole('button',{name:'Save',exact:true}).click();
  await page.getByRole('tab',{name:/Release planning/}).waitFor();
  await page.screenshot({path:path.join(artifacts,'sessions-desktop.png')});
  await page.setViewportSize({width:390,height:844});
  await page.screenshot({path:path.join(artifacts,'sessions-mobile.png')});
  assert.ok(await page.locator('[data-testid="assistant"]').evaluate(el=>el.scrollWidth<=el.clientWidth+1));
  await page.setViewportSize({width:1440,height:1000});
  await page.getByRole('button',{name:'Close Release planning tab',exact:true}).click();
  assert.equal(await page.getByRole('tab').count(),4);
  await page.getByRole('button',{name:'New tab',exact:true}).click();
  await page.getByRole('button',{name:/Release planning.*Running/}).click();
  await page.getByRole('button',{name:'Stop the reply',exact:true}).waitFor();
  await page.getByTestId('assistant-close').click();
  await page.getByTestId('assistant').waitFor({state:'detached'});
  assert.equal(modelRequests.filter(url=>url.endsWith('/cancel')).length,0);
  await page.reload();
  await page.getByTestId('assistant-button').first().click();
  await page.getByRole('tab',{name:/Release planning/}).waitFor();
  assert.equal(await page.getByRole('tab').count(),5);
  await page.getByRole('button',{name:'Stop the reply',exact:true}).waitFor();
  // A real provider reconfiguration changes just this session after its running turn stops.
  await page.getByRole('button',{name:'Stop the reply',exact:true}).click();
  await page.getByRole('button',{name:'Send',exact:true}).waitFor();
  await page.getByTestId('assistant-picker').click();
  await page.locator('select').filter({has:page.locator('option[value="manual"]')}).selectOption('manual');
  const providerSelect = page.locator('select').filter({has:page.locator('option[value="codex"]')});
  await providerSelect.selectOption('codex');
  await page.getByTestId('assistant-picker').click();
  await page.locator('[id="'+tabIds[0]+'"]').click();
  await page.getByRole('button',{name:'Stop the reply',exact:true}).waitFor();
  assert.equal(modelRequests.filter(url=>url.endsWith('/cancel')).length,1);
  await page.getByRole('button',{name:'New tab',exact:true}).click();
  // The disposable runner fixture registers only Codex; seed matching preferences instead
  // of the production upgrade defaults for the four built-in runtimes.
  const seeded = await fetch(base+'/api/settings/selection',{method:'PUT',headers:{Authorization:`Bearer ${token}`,'Content-Type':'application/json'},body:JSON.stringify({allowMetered:false,agents:{codex:{enabled:false,billing:'excluded'}}})});
  assert.ok(seeded.ok);
  await page.getByRole('link',{name:'Manage Coding Agents →',exact:true}).click();
  await page.getByRole('heading',{name:'Agents',exact:true}).waitFor();
  const tour = page.getByRole('button',{name:'Skip tour',exact:true});
  if(await tour.isVisible())await tour.click();
  // Direct availability is an independent preference; it cannot expose a Phase 1 launch button.
  const setup = page.locator('.catalogue li').first().locator('details');
  await setup.locator('summary').click();
  await setup.getByRole('checkbox',{name:'Allow Werkbord to start this CLI on this device',exact:true}).check();
  await setup.getByRole('checkbox',{name:'I have signed in to this CLI on this device',exact:true}).check();
  await page.getByRole('button',{name:'Save agent settings',exact:true}).click();
  await page.getByRole('status').getByText('Saved',{exact:true}).waitFor();
  await page.getByTestId('assistant-button').first().click();
  await page.getByRole('button',{name:'New tab',exact:true}).click();
  assert.equal(await page.getByRole('dialog',{name:'New tab',exact:true}).getByText('Coding Agents',{exact:true}).count(),0);
  assert.deepEqual(errors,[]);
  ok('Five independent real-engine sessions: background turns, switching, rename, close/reopen, reload, provider isolation and desktop/mobile layout');
  console.log('Screenshots: '+artifacts);
})().catch(async err => { console.error(err); if (lastPage) { console.error(lastPage.url()); console.error(await lastPage.locator('body').ariaSnapshot()); await lastPage.screenshot({ path: path.join(artifacts, 'failure.png'), fullPage: true }); } process.exitCode = 1; }).finally(async () => {
  if (browser) await browser.close();
  for (const child of children.reverse()) await stop(child);
  fs.rmSync(temp, { recursive: true, force: true });
});

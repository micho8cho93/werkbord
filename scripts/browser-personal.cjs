// Phase 2: real Svelte/OpenUI + owner API + disposable SQLite. No model provider.
// Run: node scripts/browser-personal.cjs
const fs = require('node:fs'), os = require('node:os'), path = require('node:path');
const cp = require('node:child_process'), net = require('node:net');
const { once } = require('node:events');
const assert = require('node:assert/strict');
const root = path.resolve(__dirname, '..');
const { chromium } = require(path.join(root, 'web/node_modules/playwright'));
const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'werkbord-personal-'));
const artifacts = process.env.BROWSER_ARTIFACT_DIR || path.join(root, 'output/playwright/personal');
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
  const env = { WERKBORD_BROWSER_ADDR: `127.0.0.1:${apiPort}`, WERKBORD_BROWSER_DIR: temp };
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
  await page.getByRole('button', { name: 'Save to-do example', exact: true }).click();
  await page.getByRole('heading', { name: 'To-dos', exact: true }).waitFor();
  const savedURL = page.url();
  const wid = new URLSearchParams(savedURL.split('?')[1]).get('id');
  assert.match(wid, /^pws_/);
  let workspace = await api(base, 'GET', '/workspaces/' + wid);
  const cid = workspace.collectionIds[0];
  const checklist = page.getByRole('region', { name: 'To-dos', exact: true });
  // <section aria-label> is exposed as a named region.
  await checklist.getByRole('button', { name: 'Add record', exact: true }).click();
  let editor = checklist.getByRole('form', { name: 'Add record' });
  await editor.getByLabel('To-do (required)', { exact: true }).fill('Review the saved workspace');
  await editor.getByLabel('Due date', { exact: true }).fill('2026-11-01');
  await editor.getByLabel('Priority (required)', { exact: true }).selectOption('High');
  await editor.getByLabel('Notes', { exact: true }).fill('Keep these notes through layout changes');
  await editor.getByRole('button', { name: 'Save record', exact: true }).click();
  await editor.waitFor({ state: 'detached' });
  let records = (await api(base, 'GET', `/collections/${cid}/records`)).items;
  assert.equal(records.length, 1); assert.equal(records[0].values.due, '2026-11-01');
  let record = records[0];
  ok('Saved example renders without a model; direct add persists typed fields');
  await checklist.getByRole('checkbox', { name: 'Complete Review the saved workspace', exact: true }).check();
  await until(async () => (await api(base, 'GET', `/collections/${cid}/records/${record.id}`)).values.done, 'Completion was not persisted');
  await page.getByText('1 of 1', { exact: true }).first().waitFor();
  assert.equal(await page.getByRole('cell', { name: 'Yes', exact: true }).count(), 1);
  ok('Completion updates both collection layouts and progress');

  // A layout replacement hides notes but retains collection identity and values.
  const { id, revision, createdAt, updatedAt, ...spec } = workspace;
  spec.definition.views[0].fields = ['title', 'done', 'due', 'priority'];
  workspace = await api(base, 'PUT', '/workspaces/' + wid, { revision, workspace: spec });
  await page.getByRole('button', { name: 'Reload workspace', exact: true }).click();
  await checklist.getByRole('button', { name: 'Edit Review the saved workspace', exact: true }).click();
  editor = checklist.getByRole('form', { name: 'Edit record' });
  assert.equal(await editor.getByLabel('Notes', { exact: true }).count(), 0);
  await editor.getByLabel('To-do (required)', { exact: true }).fill('Prepare a focused week');
  await editor.getByLabel('Priority (required)', { exact: true }).selectOption('Normal');
  await editor.getByLabel('Due date', { exact: true }).fill('');
  await editor.getByRole('button', { name: 'Save record', exact: true }).click();
  await editor.waitFor({ state: 'detached' });
  record = await api(base, 'GET', `/collections/${cid}/records/${record.id}`);
  assert.equal(record.values.notes, 'Keep these notes through layout changes');
  assert.equal(record.values.title, 'Prepare a focused week');
  assert.equal(record.values.priority, 'Normal');
  assert.equal(record.values.due, undefined);
  ok('Direct edits preserve hidden fields across a saved layout replacement');

  // A stale save leaves the draft visible and does not overwrite the current record.
  await checklist.getByRole('button', { name: 'Edit Prepare a focused week', exact: true }).click();
  editor = checklist.getByRole('form', { name: 'Edit record' });
  await editor.getByLabel('To-do (required)', { exact: true }).fill('Unsaved local draft');
  record = await api(base, 'PUT', `/collections/${cid}/records/${record.id}`, { revision: record.revision, values: { ...record.values, title: 'Changed elsewhere' } });
  await editor.getByRole('button', { name: 'Save record', exact: true }).click();
  await page.getByRole('alert').filter({ hasText: 'changed elsewhere' }).waitFor();
  assert.equal(await editor.getByLabel('To-do (required)', { exact: true }).inputValue(), 'Unsaved local draft');
  assert.equal((await api(base, 'GET', `/collections/${cid}/records/${record.id}`)).values.title, 'Changed elsewhere');
  await editor.getByRole('button', { name: 'Cancel', exact: true }).click();
  await page.getByRole('button', { name: 'Reload workspace', exact: true }).click();
  await checklist.getByRole('button', { name: 'Edit Changed elsewhere', exact: true }).waitFor();
  ok('Revision conflicts retain the draft and preserve the other writer’s data');

  await api(base, 'POST', `/collections/${cid}/records`, { values: { title: 'Zebra task', done: false, priority: 'Low' } });
  const extra = await api(base, 'POST', `/collections/${cid}/records`, { values: { title: 'Alpha task', done: false, priority: 'High' } });
  await page.getByRole('button', { name: 'Reload workspace', exact: true }).click();
  const tableRegion = page.getByRole('region', { name: 'All records', exact: true });
  await tableRegion.getByRole('button', { name: 'To-do', exact: true }).click();
  assert.match(await tableRegion.locator('tbody tr').first().innerText(), /Alpha task/);
  await tableRegion.getByRole('button', { name: 'To-do', exact: true }).click();
  assert.match(await tableRegion.locator('tbody tr').first().innerText(), /Zebra task/);
  await tableRegion.getByRole('combobox', { name: 'Priority', exact: true }).selectOption('High');
  assert.equal(await tableRegion.locator('tbody tr').count(), 1);
  await tableRegion.getByLabel('Filter records', { exact: true }).fill('absent');
  await tableRegion.getByText('No records match these filters.').waitFor();
  await tableRegion.getByLabel('Filter records', { exact: true }).fill('');
  await tableRegion.getByRole('combobox', { name: 'Priority', exact: true }).selectOption('');
  ok('Collection table sorts and filters without changing data');

  // Capture the actual application in both themes and a narrow layout.
  for (const [name, width, theme] of [['desktop-light', 1440, 'light'], ['desktop-dark', 1440, 'dark'], ['mobile-light', 390, 'light'], ['mobile-dark', 390, 'dark']]) {
    await page.setViewportSize({ width, height: 1000 });
    await page.evaluate(theme => document.documentElement.dataset.theme = theme, theme);
    if (width < 600) {
      assert.ok(await checklist.getByRole('searchbox').evaluate(el => parseFloat(getComputedStyle(el).fontSize) >= 16));
      const target = await checklist.locator('.done-target').first().boundingBox();
      assert.ok(target.width >= 44 && target.height >= 44);
    }
    await page.evaluate(() => { window.scrollTo(0, 0); document.querySelector('main')?.scrollTo(0, 0); });
    await page.screenshot({ path: path.join(artifacts, name + '.png'), fullPage: true });
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), `${name} has horizontal document overflow`);
  }
  ok('Light/dark and narrow layouts render without document overflow');

  await page.setViewportSize({ width: 1440, height: 1000 });
  await checklist.getByRole('button', { name: 'Delete Alpha task', exact: true }).click();
  await checklist.getByRole('button', { name: 'Delete record', exact: true }).click();
  await until(async () => (await api(base, 'GET', `/collections/${cid}/records`)).items.length === 2, 'Delete was not persisted');
  const remaining = (await api(base, 'GET', `/collections/${cid}/records`)).items;
  assert.ok(!remaining.some(r => r.id === extra.id));
  ok('Direct deletion is persisted');

  await stop(controller);
  controller = start(binary, [], root, env);
  await ready(base + '/api/health', controller);
  await page.goto(savedURL);
  await page.reload();
  await checklist.getByRole('button', { name: 'Edit Changed elsewhere', exact: true }).waitFor();
  assert.ok(await checklist.getByRole('checkbox', { name: 'Complete Changed elsewhere', exact: true }).isChecked());
  assert.equal((await api(base, 'GET', `/collections/${cid}/records/${record.id}`)).values.notes, 'Keep these notes through layout changes');
  ok('Same saved URL and SQLite records survive controller restart');

  // Backend rejects unsupported definitions. The UI also protects against a future/corrupt response.
  const current = await api(base, 'GET', '/workspaces/' + wid);
  const { id: _id, revision: currentRevision, createdAt: _created, updatedAt: _updated, ...bad } = current;
  bad.definition.layout = { type: 'Mutation', url: '/api/tasks', script: 'alert(1)' };
  await api(base, 'PUT', '/workspaces/' + wid, { revision: currentRevision, workspace: bad }, 400);
  const badRoute = '**/api/personal/v1/workspaces/' + wid;
  await page.route(badRoute, async route => {
    const response = await route.fetch();
    const raw = await response.json(); raw.definition.layout = { type: 'UnknownComponent' };
    await route.fulfill({ response, json: raw });
  });
  await page.reload();
  await page.getByRole('heading', { name: 'Workspace unavailable', exact: true }).waitFor();
  await page.unroute(badRoute);
  await page.getByRole('button', { name: 'Try again', exact: true }).click();
  await checklist.getByRole('button', { name: 'Edit Changed elsewhere', exact: true }).waitFor();
  ok('Invalid/unsupported definitions fail safely and recover after reload');
  assert.deepEqual(errors, []);
  assert.deepEqual(modelRequests, []);
  ok('No browser exceptions or model requests');
  console.log('Screenshots: ' + artifacts);
})().catch(async err => { console.error(err); if (lastPage) { console.error(lastPage.url()); console.error(await lastPage.locator('body').ariaSnapshot()); await lastPage.screenshot({ path: path.join(artifacts, 'failure.png'), fullPage: true }); } process.exitCode = 1; }).finally(async () => {
  if (browser) await browser.close();
  for (const child of children.reverse()) await stop(child);
  fs.rmSync(temp, { recursive: true, force: true });
});

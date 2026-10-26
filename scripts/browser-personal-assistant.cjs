// Phase 3: real Salie engine/protocol/approval + API + renderer + disposable SQLite; scripted provider.
// Run: node scripts/browser-personal-assistant.cjs
const fs = require('node:fs'), os = require('node:os'), path = require('node:path');
const cp = require('node:child_process'), net = require('node:net');
const { once } = require('node:events');
const assert = require('node:assert/strict');
const root = path.resolve(__dirname, '..');
const { chromium } = require(path.join(root, 'web/node_modules/playwright'));
const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'werkbord-personal-'));
const artifacts = process.env.BROWSER_ARTIFACT_DIR || path.join(root, 'output/playwright/personal-assistant');
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
  await context.addInitScript(token => { localStorage.setItem('devboard.token', token); localStorage.setItem('werkbord.theme', 'light'); }, token);
  const page = await context.newPage(); lastPage = page;
  const errors = [], modelRequests = [];
  page.on('pageerror', e => errors.push(e.message));
  page.on('request', r => { if (r.method() === 'POST' && /\/api\/assistant/.test(r.url())) modelRequests.push(r.url()); });
  page.setDefaultTimeout(10000);

  await page.goto(web + '/#/personal');
  await page.getByRole('button', { name: 'Create with Salie', exact: true }).click();
  const box = page.getByRole('textbox', { name: 'Message to the assistant' });
  const send = async text => {
    const count = await page.locator('article.reply').count();
    await box.fill(text);
    await page.getByRole('button', { name: 'Send', exact: true }).click();
    await page.waitForFunction(n => document.querySelectorAll('article.reply').length > n && [...document.querySelectorAll('article.reply')].at(-1)?.dataset.status === 'done', count);
  };
  await send('Salie, create a weekly to-do list with deadlines and priorities. Display in Universal, Individual and team:design.');
  let card = page.locator('.confirm[data-state="waiting"]');
  await card.getByRole('heading', { name: 'Weekly to-dos', exact: true }).waitFor();
  await card.getByRole('button', { name: 'Confirm', exact: true }).waitFor();
  assert.equal((await api(base, 'GET', '/workspaces')).items.length, 0);
  assert.equal((await api(base, 'GET', '/collections')).items.length, 0);
  assert.equal(await card.getByRole('button', { name: 'Add record', exact: true }).count(), 0);
  await card.screenshot({ path: path.join(artifacts, 'create-preview-desktop.png') });
  await page.setViewportSize({ width: 390, height: 844 });
  await card.locator('.workspace-preview').evaluate(el => el.scrollIntoView({ block: 'start' }));
  await page.screenshot({ path: path.join(artifacts, 'create-preview-mobile.png') });
  assert.ok(await card.evaluate(el => el.scrollWidth <= el.clientWidth + 1));
  await page.setViewportSize({ width: 1440, height: 1000 });
  await card.getByRole('button', { name: 'Confirm', exact: true }).click();
  await page.getByRole('link', { name: 'Open saved workspace', exact: true }).waitFor();
  const workspace = (await api(base, 'GET', '/workspaces')).items[0];
  const wid = workspace.id, cid = workspace.collectionIds[0];
  assert.equal(workspace.revision, 1);
  await page.getByRole('link', { name: 'Open saved workspace', exact: true }).click();
  await page.getByTestId('assistant-close').click();
  await page.getByTestId('assistant').waitFor({ state: 'detached' });
  const tasks = page.getByRole('region', { name: 'Weekly tasks', exact: true });
  await tasks.getByRole('button', { name: 'Add record', exact: true }).click();
  const editor = tasks.getByRole('form', { name: 'Add record' });
  await editor.getByLabel('Task (required)', { exact: true }).fill('Ship the weekly report');
  await editor.getByLabel('Deadline', { exact: true }).fill('2026-11-01');
  await editor.getByLabel('Priority', { exact: true }).selectOption('Urgent');
  await editor.getByRole('button', { name: 'Save record', exact: true }).click();
  await editor.waitFor({ state: 'detached' });
  const records = (await api(base, 'GET', `/collections/${cid}/records`)).items;
  assert.equal(records.length, 1);
  ok('Salie creation → registered preview → human approval → saved workspace → real task entry');
  await page.getByRole('button', { name: 'Edit with Salie', exact: true }).click();
  await page.getByTestId('assistant-picker').click();
  const picker = page.getByTestId('assistant-picker-panel');
  await picker.getByLabel('Model selection', { exact: true }).selectOption('manual');
  await picker.getByLabel('Model', { exact: true }).selectOption('sonnet');
  await page.keyboard.press('Escape');
  await send(`Reopen ${wid}. Add a section for urgent tasks and move the progress chart to the top.`);
  card = page.locator('.confirm[data-state="waiting"]');
  await card.getByRole('region', { name: 'Urgent tasks', exact: true }).getByText('Ship the weekly report', { exact: true }).waitFor();
  assert.equal((await api(base, 'GET', `/workspaces/${wid}`)).revision, 1);
  assert.equal(await card.getByRole('button', { name: /Add record|Edit Ship/ }).count(), 0);
  await card.screenshot({ path: path.join(artifacts, 'edit-preview-desktop.png') });
  await page.setViewportSize({ width: 390, height: 844 });
  await card.locator('.workspace-preview').evaluate(el => el.scrollIntoView({ block: 'start' }));
  await page.screenshot({ path: path.join(artifacts, 'edit-preview-mobile.png') });
  assert.ok(await card.evaluate(el => el.scrollWidth <= el.clientWidth + 1));
  await page.setViewportSize({ width: 1440, height: 1000 });
  await card.getByRole('button', { name: 'Confirm', exact: true }).click();
  await until(async () => (await api(base, 'GET', `/workspaces/${wid}`)).revision === 2, 'Revision not saved');
  assert.deepEqual((await api(base, 'GET', `/collections/${cid}/records`)).items, records);
  const revised = await api(base, 'GET', `/workspaces/${wid}`);
  assert.equal(revised.definition.layout.children[0].viewId, 'progress');
  assert.deepEqual(revised.collectionIds, [cid]);
  await page.getByTestId('assistant-close').click();
  await page.getByTestId('assistant').waitFor({ state: 'detached' });
  await page.getByRole('button', { name: 'Reload workspace', exact: true }).click();
  await page.getByRole('region', { name: 'Urgent tasks', exact: true }).getByText('Ship the weekly report', { exact: true }).waitFor();
  await page.screenshot({ path: path.join(artifacts, 'saved-revision-desktop.png'), fullPage: true });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({ path: path.join(artifacts, 'saved-revision-mobile.png'), fullPage: true });
  ok('Reopen after model change → revised preview with saved task → approval → same record ID, revision and values');
  await page.getByRole('button', { name: 'Edit with Salie', exact: true }).click();
  await send('Create a weekly to-do list with deadlines and priorities.');
  assert.equal((await api(base, 'GET', '/workspaces')).items.length, 1);
  assert.equal(await page.locator('.confirm[data-state="waiting"]').count(), 0);
  await page.reload();
  await page.getByRole('region', { name: 'Urgent tasks', exact: true }).getByText('Ship the weekly report', { exact: true }).waitFor();
  assert.deepEqual((await api(base, 'GET', `/collections/${cid}/records`)).items, records);
  assert.deepEqual(errors, []);
  ok('Repeated creation and page reload preserve the single saved workspace and task');

  // The same backend lifecycle operation must sit behind Salie's fixed card.
  await page.setViewportSize({width:1440,height:1000});
  await page.getByRole('button',{name:'Edit with Salie',exact:true}).click();
  await send(`Delete my weekly planner but keep the tasks. Workspace ${wid}.`);
  card = page.locator('.confirm[data-state="waiting"]');
  await card.getByRole('region',{name:'Personal workspace deletion preview',exact:true}).waitFor();
  await card.getByRole('heading',{name:'Delete workspace only: Weekly to-dos',exact:true}).waitFor();
  await until(async()=>await card.getByRole('button',{name:'Confirm',exact:true}).isEnabled(),'Deletion preview did not become ready');
  assert.equal((await api(base,'GET',`/workspaces/${wid}`)).id,wid);
  await card.screenshot({path:path.join(artifacts,'delete-keep-preview.png')});
  await card.getByRole('button',{name:'Confirm',exact:true}).click();
  await until(async()=>!(await api(base,'GET','/workspaces')).items.some(w=>w.id===wid),'Salie did not delete the workspace');
  assert.deepEqual((await api(base,'GET',`/collections/${cid}/records`)).items,records);
  ok('Natural-language keep-tasks deletion uses a live backend preview and human confirmation, retaining exact records');
  await page.getByTestId('assistant-close').click();
  await page.getByTestId('assistant').waitFor({state:'detached'});
  const collection = await api(base,'POST','/collections',{name:'Personal content',schemaVersion:1,fields:[{key:'title',name:'Title',type:'text',required:true}]});
  const contentWorkspace = await api(base,'POST','/workspaces',{name:'Content calendar',icon:'calendar',uiSchemaVersion:1,visibility:{universal:true,individual:true},collectionIds:[collection.id],readBindings:[],definition:{views:[{id:'content',name:'Content',kind:'table',collectionId:collection.id,fields:['title']}],layout:{type:'view',viewId:'content'}}});
  await api(base,'POST',`/collections/${collection.id}/records`,{values:{title:'A personal draft'}});
  await page.goto(web+`/#/personal?id=${contentWorkspace.id}`);
  await page.getByRole('heading',{name:'Content calendar',exact:true}).waitFor();
  await page.getByRole('button',{name:'Edit with Salie',exact:true}).click();
  await send(`Delete the content calendar and all of its associated personal records. Workspace ${contentWorkspace.id}.`);
  card = page.locator('.confirm[data-state="waiting"]');
  await card.getByRole('heading',{name:'Delete workspace and its data: Content calendar',exact:true}).waitFor();
  await card.getByText('Permanently delete Personal content',{exact:true}).waitFor();
  await until(async()=>await card.getByRole('button',{name:'Confirm',exact:true}).isEnabled(),'Content deletion preview did not become ready');
  assert.equal((await api(base,'GET',`/collections/${collection.id}/records`)).items.length,1);
  await card.getByRole('button',{name:'Confirm',exact:true}).click();
  await until(async()=>!(await api(base,'GET','/collections')).items.some(c=>c.id===collection.id),'Salie did not delete exclusive collection');
  assert.deepEqual((await api(base,'GET',`/collections/${cid}/records`)).items,records);
  assert.deepEqual(errors,[]);
  ok('Natural-language delete-data request requires the same explicit preview/confirmation and deletes only its exclusively owned data');
  console.log('Screenshots: ' + artifacts);
})().catch(async err => { console.error(err); if (lastPage) { console.error(lastPage.url()); console.error(await lastPage.locator('body').ariaSnapshot()); await lastPage.screenshot({ path: path.join(artifacts, 'failure.png'), fullPage: true }); } process.exitCode = 1; }).finally(async () => {
  if (browser) await browser.close();
  for (const child of children.reverse()) await stop(child);
  fs.rmSync(temp, { recursive: true, force: true });
});

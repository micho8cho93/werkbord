// Phase 5: lifecycle, reviewed deletion and recovery through the real UI/API/SQLite.
// Run: node scripts/browser-personal-lifecycle.cjs
const fs = require('node:fs'), os = require('node:os'), path = require('node:path');
const cp = require('node:child_process'), net = require('node:net');
const { once } = require('node:events');
const assert = require('node:assert/strict');
const root = path.resolve(__dirname, '..');
const { chromium } = require(path.join(root, 'web/node_modules/playwright'));
const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'werkbord-personal-'));
const artifacts = process.env.BROWSER_ARTIFACT_DIR || path.join(root, 'output/playwright/personal-lifecycle');
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
  const vite = start(process.execPath, [path.join(root, 'web/node_modules/vite/bin/vite.js'), '--host', '127.0.0.1', '--port', String(webPort), '--strictPort'], path.join(root, 'web'), { DEVBOARD_URL: base });
  await ready(web, vite);
  browser = await chromium.launch({ headless: true, ...(process.env.BROWSER_EXECUTABLE ? { executablePath: process.env.BROWSER_EXECUTABLE } : {}) });
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  await context.addInitScript(token => { localStorage.setItem('devboard.token', token); localStorage.setItem('werkbord.theme', 'light'); }, token);
  const page = await context.newPage(); lastPage = page;
  const errors = [], modelRequests = [];
  page.on('pageerror', e => errors.push(e.message));
  page.on('request', r => { if (r.method() === 'POST' && /\/api\/assistant/.test(r.url())) modelRequests.push(r.url()); });
  page.setDefaultTimeout(10000);

  const manage = async name => {
    await page.getByLabel('Manage workspace', {exact:true}).click();
    await page.getByRole('button', {name, exact:true}).click();
  };
  const confirmDeletion = async () => {
    await page.getByRole('checkbox',{name:'I confirm this deletion and the personal data choices above.',exact:true}).check();
    await page.getByRole('button',{name:'Confirm deletion',exact:true}).click();
  };
  await page.goto(web + '/#/personal');
  await page.getByRole('button',{name:'Save to-do example',exact:true}).click();
  await page.getByRole('heading',{name:'To-dos',exact:true}).waitFor();
  let workspace = (await api(base,'GET','/workspaces')).items[0];
  const wid = workspace.id, cid = workspace.collectionIds[0];
  let record = await api(base,'POST',`/collections/${cid}/records`,{values:{title:'Recover this record',done:false,priority:'High',notes:'Preserve hidden values'}});
  await page.getByRole('button',{name:'Reload workspace',exact:true}).click();
  await page.getByRole('checkbox',{name:'Complete Recover this record',exact:true}).waitFor();
  await manage('Name, icon & locations');
  await page.getByLabel('Name',{exact:true}).fill('Weekly planner');
  await page.getByRole('button',{name:'New icon',exact:true}).click();
  await page.getByRole('button',{name:'Save changes',exact:true}).click();
  await page.getByRole('heading',{name:'Weekly planner',exact:true}).waitFor();
  workspace = await api(base,'GET',`/workspaces/${wid}`);
  assert.match(workspace.icon,/^lucide-[a-z0-9-]+$/); assert.equal(workspace.revision,2);
  await manage('Definition history');
  await page.getByRole('button',{name:'Review revision',exact:true}).click();
  await page.getByRole('button',{name:'Restore this definition',exact:true}).click();
  await page.getByRole('heading',{name:'To-dos',exact:true}).waitFor();
  assert.deepEqual(await api(base,'GET',`/collections/${cid}/records/${record.id}`),record);
  ok('Name/icon changes and definition rollback create revisions without altering records');

  await manage('Name, icon & locations');
  await page.getByRole('checkbox',{name:'Universal',exact:true}).uncheck();
  await page.getByRole('checkbox',{name:'Individual',exact:true}).uncheck();
  await page.getByRole('button',{name:'Save changes',exact:true}).click();
  await until(async()=>!(await api(base,'GET',`/workspaces/${wid}`)).visibility.individual,'Hidden location not saved');
  assert.deepEqual(await api(base,'GET',`/collections/${cid}/records/${record.id}`),record);
  await stop(controller); controller = start(binary,[],root,env); await ready(base+'/api/health',controller);
  await page.reload();
  await page.getByRole('heading',{name:'To-dos',exact:true}).waitFor();
  await page.getByRole('button',{name:'Data library',exact:true}).click();
  const savedWorkspaces = page.getByRole('region',{name:'Saved workspaces',exact:true});
  await savedWorkspaces.waitFor();
  await savedWorkspaces.getByRole('listitem').filter({hasText:'Personal to-dos'}).getByRole('button',{name:'Open',exact:true}).click();
  await page.getByRole('heading',{name:'To-dos',exact:true}).waitFor();
  ok('Removing every display location keeps the definition and records; restart and library reopen persist');

  await manage('Delete workspace…');
  await page.getByRole('button',{name:'Delete workspace only',exact:true}).click();
  assert.equal(await page.getByRole('button',{name:'Confirm deletion',exact:true}).isDisabled(),true);
  // A concurrent edit invalidates approval, even though the record count is unchanged.
  record = await api(base,'PUT',`/collections/${cid}/records/${record.id}`,{revision:record.revision,values:{...record.values,title:'Retained task after concurrent edit'}});
  await confirmDeletion();
  await page.getByRole('alert').filter({hasText:'personal data or dependencies changed'}).waitFor();
  assert.equal((await api(base,'GET',`/workspaces/${wid}`)).id,wid);
  await page.getByRole('button',{name:'Delete workspace only',exact:true}).click();
  await page.getByRole('heading',{name:'Keep personal records',exact:true}).waitFor();
  await page.screenshot({path:path.join(artifacts,'delete-keep-desktop.png'),fullPage:true});
  await page.setViewportSize({width:390,height:844});
  await page.screenshot({path:path.join(artifacts,'delete-keep-mobile.png'),fullPage:true});
  assert.ok(await page.locator('body').evaluate(el=>el.scrollWidth<=innerWidth+1));
  await page.setViewportSize({width:1440,height:1000});
  await confirmDeletion();
  await page.getByRole('heading',{name:'Data library',exact:true}).waitFor();
  await page.getByText('Retained data — no workspace',{exact:false}).waitFor();
  await api(base,'GET',`/workspaces/${wid}`,undefined,404);
  assert.deepEqual(await api(base,'GET',`/collections/${cid}/records/${record.id}`),record);
  await page.getByRole('button',{name:'Inspect records',exact:true}).click();
  await page.getByRole('cell',{name:record.values.title,exact:true}).waitFor();
  await page.getByRole('button',{name:'Use in new workspace',exact:true}).click();
  await page.getByRole('heading',{name:'Personal to-dos',exact:true}).waitFor();
  let recovered = (await api(base,'GET','/workspaces')).items[0];
  assert.deepEqual(recovered.collectionIds,[cid]);
  assert.deepEqual(await api(base,'GET',`/collections/${cid}/records/${record.id}`),record);
  ok('Deletion requires confirmation; concurrent data changes block it; workspace-only deletion recovers the exact retained record');

  const {id:_id,revision:_revision,createdAt:_created,updatedAt:_updated,...sharedSpec} = recovered;
  const shared = await api(base,'POST','/workspaces',{...sharedSpec,name:'Another planner',icon:'auto'});
  await manage('Delete workspace…');
  await page.getByRole('button',{name:'Delete workspace and its data',exact:true}).click();
  await page.getByText('Protected: this collection is shared or is not owned by this workspace.',{exact:true}).waitFor();
  await page.getByText('Used by:',{exact:false}).waitFor();
  await confirmDeletion();
  await page.getByRole('heading',{name:'Data library',exact:true}).waitFor();
  assert.equal((await api(base,'GET',`/workspaces/${shared.id}`)).id,shared.id);
  assert.deepEqual(await api(base,'GET',`/collections/${cid}/records/${record.id}`),record);
  ok('Shared collections are explicitly protected while deleting a dependent workspace');

  const collection = await api(base,'POST','/collections',{name:'Content records',schemaVersion:1,fields:[{key:'title',name:'Title',type:'text',required:true}]});
  const exclusive = await api(base,'POST','/workspaces',{name:'Content calendar',icon:'calendar',uiSchemaVersion:1,collectionIds:[collection.id],readBindings:[],visibility:{universal:true,individual:true,teamWorkspaceIds:[]},definition:{views:[{id:'content',name:'Content',kind:'table',collectionId:collection.id,fields:['title'],operations:['create','update','delete']}],layout:{type:'view',viewId:'content'}}});
  const content = await api(base,'POST',`/collections/${collection.id}/records`,{values:{title:'Delete this personal draft'}});
  await page.goto(web+`/#/personal?id=${exclusive.id}`);
  await page.getByRole('heading',{name:'Content calendar',exact:true}).waitFor();
  await manage('Delete workspace…');
  await page.getByRole('button',{name:'Delete workspace and its data',exact:true}).click();
  await page.getByText('Delete Content records',{exact:true}).waitFor();
  await page.screenshot({path:path.join(artifacts,'delete-data-desktop.png'),fullPage:true});
  await confirmDeletion();
  await page.getByRole('heading',{name:'Data library',exact:true}).waitFor();
  await api(base,'GET',`/workspaces/${exclusive.id}`,undefined,404);
  await api(base,'GET',`/collections/${collection.id}`,undefined,404);
  await api(base,'GET',`/collections/${collection.id}/records/${content.id}`,undefined,404);
  assert.deepEqual(await api(base,'GET',`/collections/${cid}/records/${record.id}`),record);
  await page.screenshot({path:path.join(artifacts,'retained-library-desktop.png'),fullPage:true});
  assert.deepEqual(errors,[]); assert.deepEqual(modelRequests,[]);
  ok('Authorised deletion removes exclusively owned personal collections atomically and preserves other collections');
  console.log('Screenshots: '+artifacts);
})().catch(async err => { console.error(err); if (lastPage) { console.error(lastPage.url()); console.error(await lastPage.locator('body').ariaSnapshot()); await lastPage.screenshot({ path: path.join(artifacts, 'failure.png'), fullPage: true }); } process.exitCode = 1; }).finally(async () => {
  if (browser) await browser.close();
  for (const child of children.reverse()) await stop(child);
  fs.rmSync(temp, { recursive: true, force: true });
});

// Phase 3: real Salie engine/protocol/approval + API + renderer + disposable SQLite; scripted provider.
// Run: node scripts/browser-personal-assistant.cjs
const fs = require('node:fs'), os = require('node:os'), path = require('node:path');
const cp = require('node:child_process'), net = require('node:net');
const { once } = require('node:events');
const assert = require('node:assert/strict');
const root = path.resolve(__dirname, '..');
const { chromium } = require(path.join(root, 'web/node_modules/playwright'));
const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'werkbord-personal-'));
const artifacts = process.env.BROWSER_ARTIFACT_DIR || path.join(root, 'output/playwright/personal-engine');
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
  const vite = start(process.execPath, [path.join(root, 'web/node_modules/vite/bin/vite.js'), '--host', '127.0.0.1', '--port', String(webPort), '--strictPort'], path.join(root, 'web'), { DEVBOARD_URL: base });
  await ready(web, vite);
  browser = await chromium.launch({ headless: true, ...(process.env.BROWSER_EXECUTABLE ? { executablePath: process.env.BROWSER_EXECUTABLE } : {}) });
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, reducedMotion: 'reduce' });
  await context.addInitScript(token => { localStorage.setItem('devboard.token', token); localStorage.setItem('werkbord.theme', 'light'); }, token);
  const page = await context.newPage(); lastPage = page;
  const errors = [], modelRequests = [];
  page.on('pageerror', e => errors.push(e.message));
  page.on('request', r => { if (r.method() === 'POST' && /\/api\/assistant/.test(r.url())) modelRequests.push(r.url()); });
  page.setDefaultTimeout(10000);

  const fixtures = JSON.parse(fs.readFileSync(path.join(root,'internal/assistant/testdata/personal_apps.json'),'utf8'));
  const saved = [];
  for (const fixture of fixtures) {
    await page.goto(web + '/#/personal');
    await page.getByRole('button',{name:'Create with Salie',exact:true}).click();
    const box = page.getByRole('textbox',{name:'Message to the assistant'});
    await box.fill(fixture.prompt);
    await page.getByRole('button',{name:'Send',exact:true}).click();
    const card = page.locator('.confirm[data-state="waiting"]');
    await card.getByRole('heading',{name:fixture.name,exact:true}).waitFor();
    assert.equal((await api(base,'GET','/workspaces')).items.length,saved.length);
    await card.getByRole('button',{name:'Confirm',exact:true}).click();
    await until(async () => (await api(base,'GET','/workspaces')).items.length===saved.length+1,'Salie proposal not saved');
    const w = (await api(base,'GET','/workspaces')).items.find(w => w.name===fixture.name);
    const cid = w.collectionIds[0];
    await page.getByTestId('assistant-close').click();
    await page.getByTestId('assistant').waitFor({state:'detached'});
    await page.goto(web + '/#/personal?id=' + w.id);
    await page.getByRole('heading',{name:fixture.name,exact:true}).waitFor();
    if (fixture.name==='Sales CRM') {
      const leads = page.getByRole('region',{name:'Leads',exact:true});
      await leads.getByRole('button',{name:'Add record',exact:true}).click();
      const editor = leads.getByRole('form',{name:'Add record',exact:true});
      await editor.getByLabel('Name (required)',{exact:true}).fill('Browser lead');
      await editor.getByLabel('Stage',{exact:true}).selectOption('New');
      await editor.getByLabel('Revenue',{exact:true}).fill('1200');
      await editor.getByLabel('Probability',{exact:true}).fill('0.5');
      await editor.getByLabel('Follow Up',{exact:true}).fill('2026-11-01');
      assert.equal(await editor.getByLabel('Weighted',{exact:true}).count(),0);
      await editor.getByRole('button',{name:'Save record',exact:true}).click();
      await editor.waitFor({state:'detached'});
      await leads.getByText('600',{exact:true}).first().waitFor();
      const area = page.locator('.actions[aria-label="Leads actions"]');
      await area.locator('summary').click();
      await area.getByLabel('Browser lead',{exact:true}).check();
      await area.getByRole('button',{name:'Mark contacted',exact:true}).click();
      assert.equal((await api(base,'GET',`/collections/${cid}/records`)).items[0].values.stage,'New');
      await area.getByRole('button',{name:'Confirm Mark contacted',exact:true}).click();
      await until(async () => (await api(base,'GET',`/collections/${cid}/records`)).items[0].values.stage==='Contacted','Action not saved');
      await page.getByRole('region',{name:'Pipeline',exact:true}).getByRole('group',{name:/Contacted/}).getByText('Browser lead',{exact:true}).waitFor();
      await area.getByRole('button',{name:'Summarise pipeline',exact:true}).click();
      await page.getByRole('textbox',{name:'Message to the assistant'}).waitFor();
      assert.ok((await page.getByRole('textbox',{name:'Message to the assistant'}).inputValue()).includes('Summarise the active sales pipeline'));
      await page.getByTestId('assistant-close').click();
    await page.getByTestId('assistant').waitFor({state:'detached'});
    } else {
      await api(base,'POST',`/collections/${cid}/records`,{values:fixture.values});
      await page.getByRole('button',{name:'Reload workspace',exact:true}).click();
      await page.locator('.saved-workspace').getByText(fixture.values.title,{exact:true}).filter({visible:true}).first().waitFor();
      if (fixture.name==='Product Launch Center') {
        await page.getByRole('checkbox',{name:'Complete '+fixture.values.title,exact:true}).check();
        await until(async () => (await api(base,'GET',`/collections/${cid}/records`)).items[0].values.done===true,'Checklist not saved');
      }
      if (fixture.name==='Engineering Decision Log') {
        const decisions = page.getByRole('region',{name:'Decisions',exact:true});
        await decisions.getByRole('button',{name:'Edit '+fixture.values.title,exact:true}).click();
        const editor = decisions.getByRole('form',{name:'Edit record',exact:true});
        await editor.getByLabel('Impact',{exact:true}).fill('5');
        await editor.getByRole('button',{name:'Save record',exact:true}).click();
        await editor.waitFor({state:'detached'});
        await decisions.getByText('15',{exact:true}).first().waitFor();
      }
      if (fixture.name==='Marketing Calendar') await page.getByRole('form',{name:'Add record',exact:true}).waitFor();
    }
    const records = (await api(base,'GET',`/collections/${cid}/records`)).items;
    assert.equal(records.length,1);
    saved.push({w,records});
    for (const dark of [false,true]) {
      await page.evaluate(async dark => {const {theme}=await import('/src/lib/theme.svelte.ts');theme.set(dark?'dark':'light');await new Promise(requestAnimationFrame);},dark);
      for (const width of [1440,390]) {
        await page.setViewportSize({width,height:width===1440?1000:844});
        await page.screenshot({path:path.join(artifacts,fixture.name.replaceAll(' ','-')+'-'+(dark?'dark':'light')+'-'+width+'.png'),fullPage:true,animations:'disabled'});
        assert.ok(await page.evaluate(() => document.documentElement.scrollWidth<=innerWidth+1),'Horizontal page overflow in '+fixture.name);
      }
    }
    await page.setViewportSize({width:1440,height:1000});
    ok(fixture.name+' through Salie approval, records, rendering, themes and responsive layout');
  }
  await stop(controller);
  controller = start(binary,[],root,env); await ready(base+'/api/health',controller);
  for (const {w,records} of saved) {
    assert.deepEqual((await api(base,'GET',`/collections/${w.collectionIds[0]}/records`)).items,records);
    await page.goto(web+'/#/personal?id='+w.id);
    await page.getByRole('heading',{name:w.name,exact:true}).waitFor();
    await page.locator('.saved-workspace').waitFor();
    const preview=await api(base,'POST',`/workspaces/${w.id}/deletion-preview`,{revision:w.revision,mode:'delete_data'});
    await api(base,'DELETE',`/workspaces/${w.id}`,{revision:w.revision,mode:'delete_data',fingerprint:preview.fingerprint,confirmed:true});
  }
  assert.equal((await api(base,'GET','/workspaces')).items.length,0);
  assert.deepEqual(errors,[]);
  ok('All five apps persist across controller restart, reopen and delete with reviewed lifecycle');
  console.log('Screenshots: '+artifacts);
})().catch(async error => {
  console.error(error); if(lastPage) await lastPage.screenshot({path:path.join(artifacts,'failure.png'),fullPage:true}).catch(()=>{});process.exitCode=1;
}).finally(async () => {
  if(browser) await browser.close();for(const child of children.reverse()) await stop(child);fs.rmSync(temp,{recursive:true,force:true});
});

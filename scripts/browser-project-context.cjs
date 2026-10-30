// Isolated presentation contract tests: no services, credentials or installed workspace state.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const http = require('node:http');
const { chromium } = require('../web/node_modules/playwright');
const root = path.resolve(__dirname, '../desktop/frontend/dist/shell');
const artifacts = path.resolve(__dirname, '../output/playwright/project-context');
const workspace = (id, kind = 'personal', state = 'ready') => ({ id, kind, state, name: kind === 'personal' ? 'Personal' : id.replace('team:', ''), source: 'fixture', role: 'member' });
let items = [workspace('personal')], projects = ['one'], calls = [];
const href = (w, id) => w.kind === 'personal' ? `#/p/${id}/work` : `?tab=work&project=${id}`;
const summary = w => ({ schema: 'workspace-summary/v1', workspace: w, at: new Date().toISOString(),
  projects: projects.map(id => ({ id, name: `Project ${id}`, href: href(w, id) })),
  work: projects.map(id => ({ id: `task-${id}`, title: `${w.name} ${id}`, project: `Project ${id}`, status: 'doing', execution: 'running', href: href(w, id) })),
  queue: projects.flatMap((id, i) => [{ id: `task-${id}`, title: `Review ${w.name} ${id}`, project: `Project ${id}`, group: i ? 'next' : 'now', detail: i ? 'Ready to start' : 'Agent working', href: href(w, id) }, { id: `plan-${id}`, title: `Ship ${id}`, project: `Project ${id}`, group: 'plan', detail: '1 of 3 tasks complete', href: href(w, id) + '&plan=plan-' + id }]),
  attention: projects.map(id => ({ id, kind: 'blocked', severity: 'warning', title: `${w.name} ${id} blocked`, href: href(w, id) })), schedule: [], planned: [] });
const bridge = `window.go={main:{App:new Proxy({},{get:(_,method)=>async(...args)=>{const r=await fetch('/call',{method:'POST',body:JSON.stringify({method,args})});const d=await r.json();if(!r.ok)throw Error(d.error);return d}})}};`;
const server = http.createServer(async (req, res) => {
  if (req.url === '/call') {
    let body = ''; for await (const chunk of req) body += chunk;
    const { method, args } = JSON.parse(body); calls.push({ method, args });
    let result = null;
    if (method === 'Workspaces') result = { items, selected: 'personal', problems: [], team: { state: 'ready' } };
    else if (method === 'Overview') result = { entries: items.map(w => ({ workspace: w, ...(w.state === 'ready' ? { summary: summary(w) } : { error: 'Workspace offline' }) })), at: new Date().toISOString() };
    else if (method === 'LoadWorkspace' || method === 'OpenWorkspace') result = { id: args[0], kind: 'personal', url: `http://127.0.0.1:${server.address().port}/frame`, origin: `http://127.0.0.1:${server.address().port}` };
    else if (method === 'Premium') result = { active: false, canCheck: false, buyUrl: '' };
    else if (method === 'Analytics') result = { created: 1, completed: 0, days: [] };
    res.setHeader('Content-Type', 'application/json'); res.end(JSON.stringify(result)); return;
  }
  if (req.url.startsWith('/frame')) {
    res.setHeader('Content-Type', 'text/html');
    res.end(`<html><body>Fixture workspace<script>parent.postMessage({type:'werkbord.frame',event:'ready'},'*');addEventListener('message',e=>{if(e.data.type==='werkbord.assistant'&&e.data.action==='open'){document.body.textContent='Salie · '+(e.data.projectContext?.label??'All Projects'); parent.postMessage({type:'werkbord.frame',event:'assistant',open:true,waiting:0,running:false},'*')}});parent.postMessage({type:'werkbord.frame',event:'assistant',open:false,waiting:0,running:false},'*')</script></body></html>`); return;
  }
  const url = req.url === '/' ? '/shell/index.html' : req.url.split('?')[0];
  const file = path.join(root, url);
  if (!file.startsWith(root) || !fs.existsSync(file)) { res.statusCode = 404; res.end(); return; }
  const ext = path.extname(file);
  res.setHeader('Content-Type', ({ '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.woff2': 'font/woff2' })[ext] || 'application/octet-stream');
  const content = fs.readFileSync(file, ext === '.html' ? 'utf8' : undefined);
  res.end(ext === '.html' ? content.replace('<head>', `<head><script>${bridge}</script>`) : content);
});
(async () => {
  await new Promise(r => server.listen(0, '127.0.0.1', r));
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
    const errors = []; page.on('pageerror', e => errors.push(e.message));
    const url = `http://127.0.0.1:${server.address().port}/`;
    const picker = page.getByRole('combobox', { name: 'Project context', exact: true });
    const load = async () => { await page.goto(url); await picker.waitFor(); await page.waitForFunction(() => document.querySelector('[data-testid="project-context"]')?.textContent.includes('Project one')); };
    await load();
    assert.deepEqual((await page.locator('[data-testid="sidebar"] .destinations button').allTextContents()).map(s => s.trim()), ['Work', 'Calendar', 'Agents', 'Analytics', 'Salie']);
    assert.equal(await picker.locator('option').count(), 2);
    for (const scenario of [
      { name: 'multiple personal', items: [workspace('personal')] },
      { name: 'multiple team', items: [workspace('team:Design', 'team'), workspace('team:Platform', 'team')] },
      { name: 'mixed', items: [workspace('personal'), workspace('team:Platform', 'team')] },
    ]) {
      items = scenario.items; projects = ['one', 'two']; await load();
      assert.equal(await picker.locator('option').count(), 1 + items.length * 2);
      const selected = JSON.stringify([items.at(-1).id, 'two']);
      await picker.selectOption(selected);
      for (const destination of ['Calendar', 'Agents', 'Analytics', 'Work']) {
        await page.getByTestId('sidebar').getByRole('button', { name: destination, exact: true }).click();
        assert.equal(await picker.inputValue(), selected);
      }
      await page.getByRole('button', { name: 'Settings', exact: true }).click();
      assert.equal(await picker.inputValue(), selected);
      await page.getByTestId('sidebar').getByRole('button', { name: 'Work', exact: true }).click();
      await load(); assert.equal(await picker.inputValue(), selected);
      await page.getByRole('button', { name: 'Agents', exact: true }).click();
      assert.ok((await page.locator('main').innerText()).includes(`${items.at(-1).name} two`));
      assert.ok(!(await page.locator('main').innerText()).includes('one'));
      await picker.selectOption('');
      for (const w of items) assert.ok((await page.locator('main').innerText()).includes(`${w.name} one`));
      console.log('PASS ' + scenario.name + ': destinations, persistence, composite IDs and All Projects');
    }
    items = [workspace('personal'), workspace('team:Platform', 'team', 'offline')];
    await load();
    assert.ok((await page.locator('[aria-label="Current project"]').innerText()).includes('excluded'));
    await picker.selectOption(JSON.stringify(['personal', 'two']));
    // Permission-filtered summaries remove the project; do not retain cached rows.
    projects = ['one']; await load();
    await page.waitForFunction(() => document.querySelector('[data-testid="project-context"]').value === '');
    assert.ok(!(await picker.innerText()).includes('two'));
    console.log('PASS offline and revoked project contexts');
    items = [workspace('personal'), workspace('team:Platform', 'team')]; projects = ['one', 'two']; await load();
    await picker.selectOption(JSON.stringify(['team:Platform', 'two']));
    await page.getByRole('button', { name: 'Analytics', exact: true }).click();
    await page.waitForFunction(() => document.querySelector('#analytics-h'));
    await page.waitForTimeout(200);
    assert.deepEqual(calls.filter(c => c.method === 'Analytics').at(-1).args.slice(0, 2), ['team:Platform', 'two']);
    await page.getByTestId('sidebar').getByRole('button', { name: 'Work', exact: true }).click();
    await page.getByTestId('sidebar-salie').click();
    await page.frameLocator('[data-testid="assistant-frame"]').getByText('Salie · Project two · Platform', { exact: true }).waitFor();
    await load();
    assert.equal(await picker.inputValue(), JSON.stringify(['team:Platform', 'two']));
    console.log('PASS Salie receives the selected composite project context');
    fs.mkdirSync(artifacts, { recursive: true });
    await page.screenshot({ path: path.join(artifacts, 'desktop.png'), fullPage: true });
    await page.setViewportSize({ width: 390, height: 844 });
    await page.screenshot({ path: path.join(artifacts, 'mobile.png'), fullPage: true });
    await page.getByRole('button', { name: 'Plans', exact: false }).click();
    await page.getByRole('heading', { name: 'Plans', exact: true }).waitFor();
    assert.ok((await page.locator('main').innerText()).includes('Ship two'));
    await page.getByRole('button', { name: 'Queue', exact: false }).click();
    await picker.selectOption('');
    await page.getByText('Since you were last here', { exact: true }).waitFor();
    await page.getByRole('button', { name: /items need attention now/ }).click();
    assert.equal(await page.locator('#work-attention').getAttribute('open'), '');
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
    assert.deepEqual(errors, []);
    console.log('PASS scoped analytics, narrow shell and browser runtime');
  } finally { await browser.close(); await new Promise(r => server.close(r)); }
})().catch(error => { console.error(error); process.exitCode = 1; server.close(); });

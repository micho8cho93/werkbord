// The header and the Work overview, on isolated fixture workspaces (no services, credentials or installed state): agent rings that turn only
// when a report is fresh, popovers, people by role without any presence, Git, the frame title in the breadcrumb, and the layout at five widths.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const http = require('node:http');
const { chromium } = require('../web/node_modules/playwright');
const root = path.resolve(__dirname, '../desktop/frontend/dist/shell');
const artifacts = path.resolve(__dirname, '../output/playwright/agent-header');
const minutesAgo = m => new Date(Date.now() - m * 60_000).toISOString();
const personal = { id: 'personal', kind: 'personal', state: 'ready', name: 'Personal', source: 'fixture' };
const team = { id: 'team:Platform', kind: 'team', state: 'ready', name: 'Platform', source: 'fixture', role: 'member' };
const agent = (id, state, fresh) => ({ id, state, observedAt: minutesAgo(fresh ? 1 : 6), fresh });
const personalSummary = () => ({ schema: 'werkbord.workspace/v1', workspace: personal, at: new Date().toISOString(),
  projects: [{ id: 'one', name: 'Project one', href: '#/p/one/work' }],
  work: [
    { id: 't1', title: 'Fix login', project: 'Project one', status: 'doing', execution: 'running', href: '#/p/one/task/t1', agent: agent('codex', 'running', true) },
    { id: 't2', title: 'Write docs', project: 'Project one', status: 'doing', execution: 'starting', href: '#/p/one/task/t2', agent: agent('claude-code', 'starting', true) },
    { id: 't3', title: 'Old report', project: 'Project one', status: 'doing', execution: 'running', href: '#/p/one/task/t3', agent: agent('gemini-cli', 'running', false) },
  ],
  queue: [
    { id: 't1', title: 'Fix login', project: 'Project one', group: 'now', detail: 'Agent working', href: '#/p/one/task/t1', agent: agent('codex', 'running', true) },
    { id: 't4', title: 'Review copy', project: 'Project one', group: 'next', detail: 'Ready to start', href: '#/p/one/task/t4' },
    { id: 'pl1', title: 'Authentication overhaul', project: 'Project one', group: 'plan', detail: '1 of 3 tasks complete', href: '#/p/one/work?plan=pl1' },
  ], attention: [], schedule: [], planned: [] });
const teamSummary = () => ({ schema: 'werkbord.workspace/v1', workspace: team, at: new Date().toISOString(),
  projects: [{ id: 'two', name: 'Project two', href: '?tab=work&project=two', people: [{ id: 'm1', name: 'Maya Lindqvist', role: 'owner' }, { id: 'm2', name: 'Lucas Brandt', role: 'member' }] }],
  work: [], queue: [{ id: 'k1', title: 'Ship the API', project: 'Project two', group: 'now', href: '?tab=work&project=two&ticket=k1', owner: 'Maya Lindqvist', agent: { state: 'running', observedAt: minutesAgo(1), fresh: true } }],
  pushes: [{ id: 'p1', key: 'WB-1', project: 'Project two', branch: 'feature/api', sha: 'a1b2c3d4e5f6', href: '?tab=work&project=two&ticket=k1', at: minutesAgo(3) }],
  attention: [], schedule: [], planned: [] });
let items = [personal, team];
const bridge = `window.go={main:{App:new Proxy({},{get:(_,method)=>async(...args)=>{const r=await fetch('/call',{method:'POST',body:JSON.stringify({method,args})});const d=await r.json();if(!r.ok)throw Error(d.error);return d}})}};`;
const server = http.createServer(async (req, res) => {
  if (req.url === '/call') {
    let body = ''; for await (const chunk of req) body += chunk;
    const { method } = JSON.parse(body);
    let result = null;
    if (method === 'Workspaces') result = { items, selected: 'personal', problems: [], team: { state: 'ready' } };
    else if (method === 'Overview') result = { entries: items.map(w => ({ workspace: w, summary: w.kind === 'personal' ? personalSummary() : teamSummary() })), at: new Date().toISOString() };
    else if (method === 'LoadWorkspace' || method === 'OpenWorkspace') result = { id: 'personal', kind: 'personal', url: `http://127.0.0.1:${server.address().port}/frame`, origin: `http://127.0.0.1:${server.address().port}` };
    else if (method === 'Premium') result = { active: false, canCheck: false, buyUrl: '' };
    else if (method === 'AgentsAndConnections') result = { agents: [{ id: 'codex', name: 'Codex', signedIn: true, active: 1, limitEvents: 0 }, { id: 'claude-code', name: 'Claude Code', signedIn: false, active: 0, limitEvents: 2 }], tools: [{ id: 'c1', name: 'Notion', service: 'https://mcp.notion.com/mcp', state: 'verified' }], usage: { reported: 1.25, estimated: null, remaining: 40, resetAt: '2026-11-05' } };
    else if (method === 'ProjectHead') result = { branch: 'main', commit: 'a1b2c3d' };
    res.setHeader('Content-Type', 'application/json'); res.end(JSON.stringify(result)); return;
  }
  if (req.url.startsWith('/frame')) { res.setHeader('Content-Type', 'text/html'); res.end(`<html><body>Fixture workspace<script>parent.postMessage({type:'werkbord.frame',event:'ready'},'*')</script></body></html>`); return; }
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
  fs.mkdirSync(artifacts, { recursive: true });
  try {
    const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
    const errors = []; page.on('pageerror', e => errors.push(e.message));
    await page.goto(`http://127.0.0.1:${server.address().port}/`);
    const header = page.locator('[aria-label="Current project"]');
    await header.waitFor();

    // Counts: one confirmed running (Codex) and the Team's fresh peer report; the stale Gemini report and the starting Claude Code run are not counted.
    await page.getByTestId('header-running').waitFor();
    assert.match(await page.getByTestId('header-running').innerText(), /^2 running$/);
    assert.match(await page.getByTestId('header-needs').innerText(), /^0 need you$/);

    // Rings: only a confirmed, fresh running state turns.
    const turning = await page.locator('[data-testid="header-status"] .wb-agent').evaluateAll(marks => marks.map(m => [m.dataset.ring, getComputedStyle(m, '::before').animationName]));
    assert(turning.length >= 3, 'the header shows the agents');
    for (const [ring, animation] of turning) assert.equal(animation, ring === 'running' ? 'wb-agent-turn' : 'none', `${ring} ring`);
    const stale = await page.locator('[data-testid="header-agents"] .trigger').getAttribute('aria-label');
    assert.match(stale, /Agents on this project \(\d+\)/);

    // The popover opens, lists each agent with its state in words, and Escape closes it and returns focus.
    const trigger = page.getByTestId('header-agents').locator('.trigger');
    await trigger.click();
    assert.equal(await trigger.getAttribute('aria-expanded'), 'true');
    const text = await page.getByRole('group', { name: /Agents on this project/ }).innerText();
    assert.match(text, /Codex\s+running/); assert.match(text, /Claude Code\s+starting/); assert.match(text, /Gemini CLI\s+last reported \d+m ago/);
    await page.keyboard.press('Escape');
    assert.equal(await trigger.getAttribute('aria-expanded'), 'false');
    assert.equal(await trigger.evaluate(el => el === document.activeElement), true, 'focus returns to the button');

    // People by role, assignments and nothing about whether anyone is around, in the header and on the overview.
    await page.getByTestId('header-people').locator('.trigger').click();
    const people = await page.getByRole('group', { name: /People on this project/ }).innerText();
    assert.match(people, /Maya Lindqvist\s+owner · 1 assigned/); assert.match(people, /Lucas Brandt\s+member · nothing assigned/);
    await page.keyboard.press('Escape');
    const everything = (await header.innerText()) + (await page.getByTestId('participants').innerText()) + people;
    assert.doesNotMatch(everything, /\b(online|offline|available|last seen|working on)\b/i, 'no presence anywhere');

    // Overview: the stat cards are gone and the counts sit in the headings.
    assert.equal(await page.getByTestId('work-stats').count(), 0);
    assert.equal(await page.getByTestId('running-count').innerText(), '1');
    assert.match(await page.getByRole('heading', { name: /Up next/ }).innerText(), /^Up next\s*· 2 ready$/);
    assert.equal(await page.locator('.work-view .wb-agent[data-ring="running"]').count() > 0, true, 'running rows carry the ringed mark');

    // Git of a Personal project, and a Team's report with its provenance.
    await page.getByRole('combobox', { name: 'Project context', exact: true }).selectOption(JSON.stringify(['personal', 'one']));
    await page.getByTestId('header-git').waitFor();
    assert.match(await page.getByTestId('header-git').innerText(), /main/);
    await page.getByTestId('header-git').locator('.trigger').click();
    assert.match(await page.getByRole('group', { name: /^Git:/ }).innerText(), /main · a1b2c3d/);
    await page.keyboard.press('Escape');
    await page.getByRole('combobox', { name: 'Project context', exact: true }).selectOption(JSON.stringify(['team:Platform', 'two']));
    await page.getByTestId('header-git').waitFor();
    await page.getByTestId('header-git').locator('.trigger').click();
    assert.match(await page.getByRole('group', { name: /^Git:/ }).innerText(), /Reported \d+m ago · unverified/);
    await page.keyboard.press('Escape');
    await page.getByRole('combobox', { name: 'Project context', exact: true }).selectOption('');
    assert.equal(await page.getByTestId('header-git').count(), 0, 'several repositories invent no global branch');

    // Agents & connections: the person's own setup, only in a Personal context, with the detected state and the usage as reported.
    assert.equal(await page.getByTestId('header-tools').count(), 0, 'All Projects has no single setup to show');
    await page.getByRole('combobox', { name: 'Project context', exact: true }).selectOption(JSON.stringify(['personal', 'one']));
    await page.getByTestId('header-tools').waitFor();
    await page.getByTestId('header-tools').locator('.trigger').click();
    const tools = page.getByTestId('connected-tools');
    await tools.getByText('Coding agents').waitFor();
    const said = await tools.innerText();
    assert.match(said, /Codex[\s\S]*Signed in · 1 active · Limit unknown/);
    assert.match(said, /Claude Code[\s\S]*Installed · Limit reached 2 times this period/);
    assert.match(said, /Notion[\s\S]*Verified/);
    assert.match(await page.getByTestId('connected-usage').innerText(), /Reported API charges\s*\$1\.25[\s\S]*Estimated API equivalent\s*Not reported/);
    assert.match(said, /Balances and reset times are not available\./);
    assert.doesNotMatch(said.replace('Balances and reset times are not available.', ''), /remain|reset|allowance|balance|online|offline/i, 'no allowance and no presence');
    assert.equal(await tools.getByRole('button', { name: 'Settings' }).count(), 1);
    await page.keyboard.press('Escape');
    await page.getByRole('combobox', { name: 'Project context', exact: true }).selectOption(JSON.stringify(['team:Platform', 'two']));
    assert.equal(await page.getByTestId('header-tools').count(), 0, 'a Team context shows none of the person\'s own setup');
    await page.getByRole('combobox', { name: 'Project context', exact: true }).selectOption(JSON.stringify(['personal', 'one']));

    // The breadcrumb gets its depth from the page in the frame.
    await page.getByRole('combobox', { name: 'Project context', exact: true }).selectOption(JSON.stringify(['personal', 'one']));
    await page.getByRole('navigation', { name: 'Work sections' }).getByRole('button', { name: /^Queue/ }).click();
    await page.waitForFunction(() => [...document.querySelectorAll('iframe')].some(f => f.src.includes('/frame')));
    const frames = page.frames().filter(f => f.url().includes('/frame'));
    // The frame says where it is and what it shows; the shell may not have registered the frame window yet, so say it until heard.
    for (let i = 0; i < 20 && !(await page.locator('.crumbs').innerText()).includes('Authentication overhaul'); i++) {
      for (const frame of frames) await frame.evaluate(() => { parent.postMessage({ type: 'werkbord.frame', event: 'place', place: '#/p/one/work?plan=pl1' }, '*'); parent.postMessage({ type: 'werkbord.frame', event: 'title', title: 'Authentication overhaul' }, '*'); });
      await page.waitForTimeout(250);
    }
    assert.deepEqual(await page.locator('.crumbs .crumb').allInnerTexts(), ['Project one · Personal', 'Work', 'Plans', 'Authentication overhaul']);
    for (const frame of frames) await frame.evaluate(() => parent.postMessage({ type: 'werkbord.frame', event: 'title', title: '' }, '*'));
    await page.waitForFunction(() => !document.querySelector('.crumbs')?.textContent.includes('Authentication overhaul'));
    await page.getByRole('combobox', { name: 'Project context', exact: true }).selectOption('');

    // Five widths: nothing overflows, the header stays one tidy row, and below 640px one button holds every section.
    for (const width of [1440, 1280, 900, 640, 390]) {
      await page.setViewportSize({ width, height: 900 });
      await page.waitForTimeout(150);
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1), false, `no horizontal scroll at ${width}`);
      const compact = width < 640;
      assert.equal(await page.getByTestId('header-context').isVisible(), compact, `context button at ${width}`);
      assert.equal(await page.getByTestId('header-status').isVisible(), !compact, `inline status at ${width}`);
      if (width >= 640) assert.ok((await header.boundingBox()).height < 90, `the header is compact at ${width}`);
      await page.screenshot({ path: path.join(artifacts, `header-${width}.png`) });
    }
    await page.getByTestId('header-context-button').locator('.trigger').click();
    const all = await page.getByRole('group', { name: /^Context:/ }).innerText();
    assert.match(all, /running/); assert.match(all, /agents/i); assert.match(all, /people/i);
    await page.keyboard.press('Escape');

    // Reduced motion: the running ring keeps a static arc.
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await page.setViewportSize({ width: 1440, height: 900 });
    assert.equal(await page.locator('.wb-agent[data-ring="running"]').first().evaluate(m => getComputedStyle(m, '::before').animationName), 'none');
    assert.deepEqual(errors, []);
    console.log('PASS header: counts, agent rings, popovers, people without presence, Git, breadcrumb depth, five widths and reduced motion');
  } finally { await browser.close(); server.close(); }
})().catch(e => { console.error(e); process.exitCode = 1; });

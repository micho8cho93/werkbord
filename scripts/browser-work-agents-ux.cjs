// The window's Work, sidebar and Analytics, against isolated presentation fixtures (no services, credentials or installed state).
//
//   1. Salie sits under the project picker, drawn as a filled button; the sidebar folds to a rail (button and Cmd+B), and stays folded after a reload.
//   2. Work puts an agent's question first, flags it, counts it, and answers it in place with the question's own context in view.
//   3. Work opens a task beside its list; closing the pane is Back and the list is as it was.
//   4. Analytics does not blank, reload or redraw when the workspace list refreshes in the background.
//
// Run: node scripts/browser-work-agents-ux.cjs (after `make web-shell`).
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const http = require('node:http');
const { chromium } = require('../web/node_modules/playwright');
const root = path.resolve(__dirname, '../desktop/frontend/dist/shell');
const artifacts = process.env.BROWSER_ARTIFACT_DIR || path.resolve(__dirname, '../output/playwright/work-agents-ux');
fs.mkdirSync(artifacts, { recursive: true });
const ok = text => console.log('PASS ' + text);
const personal = { id: 'personal', kind: 'personal', state: 'ready', name: 'Personal', source: 'fixture', role: 'member' };
const calls = [];
let answered = false;
const taskHref = id => `#/p/one/task/${id}`;
const question = { id: 'q1', runId: 'run_1', projectId: 'one', taskId: 'task-ask', kind: 'approval', prompt: 'Allow the agent to run npm test?', context: 'npm test -- --watch=false', options: ['Allow', 'Deny'], allowFreeText: false, askedAt: new Date(Date.now() - 4 * 60000).toISOString(), agent: 'claude-code' };
const summary = () => ({
  schema: 'workspace-summary/v1', workspace: personal, at: new Date().toISOString(),
  projects: [{ id: 'one', name: 'Project one', href: '#/p/one/work' }],
  work: [{ id: 'task-run', title: 'Refactor the parser', project: 'Project one', status: 'doing', execution: 'running', href: taskHref('task-run') }],
  queue: [
    { id: 'task-run', title: 'Refactor the parser', project: 'Project one', group: 'now', detail: 'doing', href: taskHref('task-run') },
    { id: 'task-review', title: 'Tidy the settings page', project: 'Project one', group: 'now', detail: 'review', href: taskHref('task-review') },
    { id: 'task-high', title: 'Ship the release notes', project: 'Project one', group: 'now', detail: 'backlog', priority: 'high', href: taskHref('task-high') },
    { id: 'task-ask', title: 'Fix the login flow', project: 'Project one', group: 'now', detail: 'doing', href: taskHref('task-ask') },
    { id: 'task-ready', title: 'Write the changelog', project: 'Project one', group: 'next', detail: 'backlog', href: taskHref('task-ready') },
  ],
  attention: [
    ...(answered ? [] : [{ id: 'question-q1', kind: 'needs_input', severity: 'warning', title: question.prompt, detail: 'Fix the login flow', project: 'Project one', href: taskHref('task-ask'), at: question.askedAt, question }]),
    { id: 'rev', kind: 'review', severity: 'info', title: 'Tidy the settings page is ready for review', href: taskHref('task-review') },
  ],
  schedule: [], planned: [],
});
const bridge = `window.go={main:{App:new Proxy({},{get:(_,method)=>async(...args)=>{const r=await fetch('/call',{method:'POST',body:JSON.stringify({method,args})});const d=await r.json();if(!r.ok)throw Error(d.error);return d}})}}`;
const server = http.createServer(async (req, res) => {
  if (req.url === '/call') {
    let body = ''; for await (const chunk of req) body += chunk;
    const { method, args } = JSON.parse(body); calls.push({ method, args });
    let result = null;
    if (method === 'Workspaces') result = { items: [personal], selected: 'personal', problems: [], team: { state: 'ready' } };
    else if (method === 'Overview') result = { entries: [{ workspace: personal, summary: summary() }], at: new Date().toISOString() };
    else if (method === 'LoadWorkspace' || method === 'OpenWorkspace') result = { id: args[0], kind: 'personal', url: `http://127.0.0.1:${server.address().port}/frame`, origin: `http://127.0.0.1:${server.address().port}` };
    else if (method === 'Premium') result = { active: false, canCheck: false, buyUrl: '' };
    else if (method === 'Analytics') result = { created: 12, completed: 7, durationSamples: 5, averageTaskSeconds: 3600, end: new Date().toISOString(), days: Array.from({ length: 30 }, (_, i) => ({ date: new Date(Date.now() - (29 - i) * 864e5).toISOString().slice(0, 10), created: i % 4, completed: i % 3 })), categories: [{ name: 'bug', created: 4, completed: 3 }] };
    else if (method === 'Relay' && args[1] === 'AnswerQuestion') { answered = true; result = { status: 200, body: { ...question, state: 'answered', answer: args[2][0].answer } }; }
    res.setHeader('Content-Type', 'application/json'); res.end(JSON.stringify(result)); return;
  }
  if (req.url.startsWith('/frame')) {
    res.setHeader('Content-Type', 'text/html');
    res.end(`<html><body>Fixture task page<script>parent.postMessage({type:'werkbord.frame',event:'ready'},'*')</script></body></html>`); return;
  }
  const url = req.url === '/' ? '/shell/index.html' : req.url.split('?')[0];
  const file = path.join(root, url);
  if (!file.startsWith(root) || !fs.existsSync(file)) { res.statusCode = 404; res.end(); return; }
  const ext = path.extname(file);
  res.setHeader('Content-Type', ({ '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.woff2': 'font/woff2' })[ext] || 'application/octet-stream');
  const content = fs.readFileSync(file, ext === '.html' ? 'utf8' : undefined);
  res.end(ext === '.html' ? content.replace('<head>', `<head><script>${bridge}</script>`) : content);
});
const delay = ms => new Promise(r => setTimeout(r, ms));

(async () => {
  await new Promise(r => server.listen(0, '127.0.0.1', r));
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
    const errors = []; page.on('pageerror', e => errors.push(e.message));
    const url = `http://127.0.0.1:${server.address().port}/`;
    const sidebar = page.getByTestId('sidebar');
    await page.goto(url);
    await page.getByTestId('project-context').waitFor();

    // ---- 1. Salie under the project picker; the sidebar folds ----
    const order = await sidebar.evaluate(el => [...el.querySelectorAll('.project-picker, [data-testid=sidebar-salie], .destinations')].map(n => n.matches('.project-picker') ? 'picker' : n.matches('.destinations') ? 'destinations' : 'salie'));
    assert.deepEqual(order, ['picker', 'salie', 'destinations'], 'Salie sits between the project picker and the destinations');
    const salie = page.getByTestId('sidebar-salie');
    const look = await salie.evaluate(el => { const s = getComputedStyle(el), nav = getComputedStyle(document.querySelector('.destinations .nav')); return { fill: s.backgroundImage !== 'none' || s.backgroundColor !== nav.backgroundColor, weight: Number(s.fontWeight) }; });
    assert.equal(look.fill, true, 'Salie is a filled button, unlike the destinations');
    assert.ok(look.weight >= 600);
    assert.equal(await sidebar.evaluate(el => el.getBoundingClientRect().width), 224);
    await page.getByTestId('sidebar-toggle').click();
    await page.waitForFunction(() => document.querySelector('[data-testid=sidebar]').getBoundingClientRect().width === 64);
    assert.equal(await sidebar.getAttribute('data-collapsed'), 'true');
    assert.equal(await page.getByTestId('sidebar-toggle').getAttribute('aria-expanded'), 'false');
    assert.equal(await salie.getAttribute('aria-label') !== null, true, 'a folded Salie keeps its name');
    assert.equal(await page.locator('[data-testid=sidebar] [data-page=agents]').getAttribute('title'), 'Agents', 'a folded destination is named by its tooltip');
    await page.screenshot({ path: path.join(artifacts, 'sidebar-folded.png') });
    await page.reload(); await page.getByTestId('project-context').waitFor();
    assert.equal(await sidebar.getAttribute('data-collapsed'), 'true', 'the fold is remembered');
    await page.keyboard.press('Meta+b');
    await page.waitForFunction(() => document.querySelector('[data-testid=sidebar]').getBoundingClientRect().width === 224);
    await page.keyboard.press('Control+b');
    await page.waitForFunction(() => document.querySelector('[data-testid=sidebar]').getBoundingClientRect().width === 64);
    await page.keyboard.press('Control+b');
    await page.waitForFunction(() => document.querySelector('[data-testid=sidebar]').getBoundingClientRect().width === 224);
    ok('Salie sits under the project picker as a filled button, and the sidebar folds to a rail, by button and Cmd+B, and stays folded');

    // ---- 2. Work: the order, the flags, and answering in place ----
    await page.getByRole('button', { name: 'Work', exact: true }).first().click();
    const flags = page.getByTestId('work-flags');
    await flags.waitFor();
    const chips = (await flags.locator('button').allInnerTexts()).map(t => t.replace(/\s+/g, ' ').trim());
    assert.deepEqual(chips, ['1 answer needed', '1 ready for review', '1 high priority'], 'one chip for each reason that has something');
    const nowRows = await page.locator('[data-section=now] button.task strong').allInnerTexts();
    assert.deepEqual(nowRows, ['Fix the login flow', 'Tidy the settings page', 'Ship the release notes', 'Refactor the parser'], 'an answer, then a review, then high priority, then the rest of what is moving');
    await page.screenshot({ path: path.join(artifacts, 'work-attention.png') });
    const panel = page.getByTestId('next-decision');
    assert.match(await panel.innerText(), /Waiting for you/);
    assert.match(await panel.innerText(), /npm test -- --watch=false/, 'an approval shows what it is for, without opening anything');
    await panel.getByRole('button', { name: 'Allow', exact: true }).click();
    await page.waitForFunction(() => document.querySelector('[data-testid=next-decision]').textContent.includes('No agent is waiting on you'));
    const sent = calls.find(c => c.method === 'Relay' && c.args[1] === 'AnswerQuestion');
    assert.ok(sent, 'the answer was sent');
    assert.deepEqual({ project: sent.args[2][0].projectId, question: sent.args[2][0].questionId, answer: sent.args[2][0].answer }, { project: 'one', question: 'q1', answer: 'Allow' });
    assert.equal(await page.getByTestId('work-flags').locator('button', { hasText: 'answer needed' }).count(), 0, 'the flag goes with the question');
    ok('Work puts the answer first, flags and counts what needs a look, and answers an approval in place');

    // ---- 3. A task beside the list; closing the pane is Back ----
    await page.getByRole('searchbox', { name: 'Search work' }).fill('Tidy');
    await page.locator('[data-section=now] button.task', { hasText: 'Tidy the settings page' }).click();
    await page.getByTestId('work-pane-bar').waitFor();
    assert.equal(await page.locator('iframe[data-workspace=personal]').isVisible(), true, 'the task is shown');
    assert.equal(await page.locator('[data-section=now] button.task[aria-current=true]').count(), 1, 'the open task is marked in the list');
    assert.equal(await page.getByRole('searchbox', { name: 'Search work' }).inputValue(), 'Tidy', 'the list is as it was');
    const listWidth = await page.locator('.page-area').evaluate(el => el.getBoundingClientRect().width);
    assert.ok(listWidth >= 360 && listWidth <= 520, `the list keeps a readable column (${listWidth}px)`);
    await page.screenshot({ path: path.join(artifacts, 'work-pane.png') });
    await page.getByTestId('work-pane-close').click();
    await page.getByTestId('work-pane-bar').waitFor({ state: 'detached' });
    assert.equal(await page.getByRole('searchbox', { name: 'Search work' }).inputValue(), 'Tidy');
    await page.locator('[data-section=now] button.task', { hasText: 'Tidy the settings page' }).click();
    await page.getByTestId('work-pane-bar').waitFor();
    await page.setViewportSize({ width: 900, height: 900 });
    await page.getByTestId('work-pane-bar').waitFor({ state: 'detached' });
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, 'no horizontal overflow when the pane gives way');
    await page.setViewportSize({ width: 1440, height: 1000 });
    ok('a task opens beside the list, Back is closing it, and a window too narrow for both gives the task the page');

    // ---- 4. Analytics stays put while the workspace list refreshes ----
    await page.getByRole('button', { name: 'Analytics', exact: true }).click();
    await page.locator('.statistics').first().waitFor();
    await page.locator('.statistics').first().evaluate(el => { el.dataset.mark = 'kept'; });
    await page.evaluate(() => {
      window.__flashes = 0;
      new MutationObserver(() => { if (/Loading analytics|Updating…/.test(document.body.innerText)) window.__flashes++; }).observe(document.body, { childList: true, subtree: true, characterData: true });
    });
    const before = calls.filter(c => c.method === 'Analytics').length;
    const lists = calls.filter(c => c.method === 'Workspaces').length;
    await delay(9500); // two workspace-list refreshes (every 4s)
    assert.ok(calls.filter(c => c.method === 'Workspaces').length - lists >= 2, 'the workspace list was refreshed meanwhile');
    assert.equal(calls.filter(c => c.method === 'Analytics').length, before, 'a refresh of the workspace list does not reload Analytics');
    assert.equal(await page.evaluate(() => window.__flashes), 0, 'no loading or updating state flashed');
    assert.equal(await page.locator('.statistics[data-mark=kept]').count(), 1, 'the figures were not redrawn');
    await page.screenshot({ path: path.join(artifacts, 'analytics.png') });
    ok('Analytics keeps its charts, and does not blank, reload or flash, while the workspace list refreshes');

    assert.deepEqual(errors, []);
  } finally { await browser.close(); await new Promise(r => server.close(r)); }
})().catch(error => { console.error(error); process.exitCode = 1; server.close(); });

// Unified agent sessions: one real execution, one identity, wherever it is opened.
// Real controller (the browser fixture) with its runner, a scripted Salie, native PTYs and Chromium. Run: node scripts/browser-agent-sessions.cjs
//
//   1. Salie proposes starting an agent; nothing starts until the person confirms; the conversation links to the session it started.
//   2. The same session opens in Agents, with its feed, and keeps taking instructions.
//   3. Back to Salie, and Salie is where it was, with the session still running.
//   4. A Work task opens the same session. A terminal can be standalone or for a task, and is listed once.
//   5. After the controller restarts, every session is still there under the same key, with no duplicates.
const fs = require('node:fs'), os = require('node:os'), path = require('node:path');
const cp = require('node:child_process'), net = require('node:net');
const { once } = require('node:events');
const assert = require('node:assert/strict');
const root = path.resolve(__dirname, '..');
const { chromium } = require(path.join(root, 'web/node_modules/playwright'));
const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'werkbord-agent-sessions-'));
const artifacts = process.env.BROWSER_ARTIFACT_DIR || path.join(root, 'output/playwright/agent-sessions');
fs.mkdirSync(artifacts, { recursive: true });
const token = 'disposable-browser-credential';
const children = [];
let browser, lastPage;
const delay = ms => new Promise(r => setTimeout(r, ms));
const ok = text => console.log('PASS ' + text);
async function freePort() {
  const s = net.createServer(); s.listen(0, '127.0.0.1'); await once(s, 'listening');
  const p = s.address().port; await new Promise(r => s.close(r)); return p;
}
function start(bin, args, cwd, env) {
  const c = cp.spawn(bin, args, { cwd, env: { ...process.env, ...env }, stdio: ['ignore', 'pipe', 'pipe'] });
  c.log = ''; c.stdout.on('data', b => c.log += b); c.stderr.on('data', b => c.log += b); children.push(c); return c;
}
async function stop(c) {
  if (c.exitCode !== null) return;
  c.kill('SIGTERM'); await Promise.race([once(c, 'exit'), delay(5000)]);
  if (c.exitCode === null) c.kill('SIGKILL');
}
async function ready(url, child) {
  for (let i = 0; i < 200; i++) {
    if (child.exitCode !== null) throw Error(child.log);
    if (await fetch(url).then(r => r.ok).catch(() => false)) return;
    await delay(100);
  }
  throw Error('did not start: ' + child.log);
}
const until = async (fn, message, tries = 100) => { for (let i = 0; i < tries; i++) { if (await fn()) return; await delay(100); } throw new Error(message); };

(async () => {
  if (!['darwin', 'linux'].includes(process.platform)) { console.log('SKIP agent sessions browser: unsupported execution platform'); return; }
  const bin = path.join(dir, 'fixture');
  cp.execFileSync('go', ['build', '-o', bin, './scripts/browser-fixture'], { cwd: root, stdio: 'inherit' });
  const cli = path.join(dir, 'native-cli');
  fs.writeFileSync(cli, '#!/bin/sh\n[ -t 0 ] && [ -t 1 ] && [ -t 2 ] || exit 17\nprintf "\\033[32mNative PTY ready\\033[0m\\n"\nwhile IFS= read -r line; do printf "REPLY:%s\\n" "$line"; done\n', { mode: 0o700 });
  const apiPort = await freePort(), webPort = await freePort();
  const base = `http://127.0.0.1:${apiPort}`, web = `http://127.0.0.1:${webPort}`;
  const env = { WERKBORD_BROWSER_DIR: dir, WERKBORD_BROWSER_ADDR: `127.0.0.1:${apiPort}`, WERKBORD_BROWSER_ASSISTANT: '1', WERKBORD_BROWSER_TERMINALS: '1', WERKBORD_BROWSER_EXECUTION: '1', WERKBORD_BROWSER_TERMINAL_CLI: cli };
  let controller = start(bin, [], root, env);
  await ready(base + '/api/health', controller);
  async function api(method, url, data) {
    const r = await fetch(base + url, { signal: AbortSignal.timeout(25000), method, headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' }, body: data === undefined ? undefined : JSON.stringify(data) });
    const text = await r.text(); assert.ok(r.ok, `${method} ${url}: ${r.status} ${text}`); return text ? JSON.parse(text) : null;
  }
  await api('POST', '/api/onboarding/complete', {});
  const repo = path.join(dir, 'repo');
  cp.execFileSync('git', ['init', '-q', repo]);
  cp.execFileSync('git', ['-C', repo, '-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.test', 'commit', '--allow-empty', '-qm', 'Fixture']);
  const project = await api('POST', '/api/projects', { path: repo, name: 'Sessions fixture' });
  await api('POST', '/api/onboarding/tours/seen', { surface: 'desktop' }); // the first-visit tour is not what is tested here
  await api('PUT', '/api/settings/selection', { agents: { codex: { directCLI: true, billing: 'auto', enabled: false }, 'claude-code': { directCLI: true, billing: 'auto', enabled: false } } });
  const task = await api('POST', `/api/projects/${project.id}/tasks`, { title: 'Fix the login', execution: { agent: 'fixture', mode: 'manual' } });
  const taskRuns = async () => (await api('GET', `/api/projects/${project.id}/tasks/${task.id}/runs`)).runs ?? [];
  const sessions = async (query = '') => (await api('GET', '/api/agent-sessions' + query)).sessions;
  const vite = start(process.execPath, [path.join(root, 'web/node_modules/vite/bin/vite.js'), '--host', '127.0.0.1', '--port', String(webPort), '--strictPort'], path.join(root, 'web'), { WERKBORD_URL: base });
  await ready(web, vite);
  browser = await chromium.launch({ headless: true, ...(process.env.BROWSER_EXECUTABLE ? { executablePath: process.env.BROWSER_EXECUTABLE } : {}) });
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  await context.addInitScript(t => { localStorage.setItem('werkbord.token', t); localStorage.setItem('werkbord.theme', 'light'); }, token);
  const page = await context.newPage(); lastPage = page;
  const errors = []; page.on('pageerror', e => { errors.push(e.message); console.log('pageerror:', e.message); }); page.setDefaultTimeout(15000);

  // ---- 1. Launch an agent through Salie ----
  await page.goto(web + '/#/control');
  await page.getByTestId('assistant-button').first().click();
  await page.getByRole('textbox', { name: 'Message to Salie' }).fill(`start the agent on ${project.id} ${task.id}`);
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  const card = page.locator('section.confirm');
  await card.getByRole('button', { name: 'Confirm and start' }).waitFor();
  assert.equal((await taskRuns()).length, 0, 'nothing starts before the person confirms');
  assert.equal((await sessions()).filter(s => s.kind === 'run').length, 0);
  await card.getByRole('button', { name: 'Confirm and start' }).click();
  await card.getByTestId('open-in-agents').waitFor();
  const runs = await taskRuns();
  assert.equal(runs.length, 1, 'confirming starts exactly one agent');
  const run = runs[0], key = `run:${run.id}`;
  assert.match(run.origin, /^salie:ast_/, 'the run records the conversation that launched it');
  let list = await sessions();
  assert.deepEqual(list.map(s => s.key), [key], 'one execution, one identity');
  assert.equal(list[0].launchedBy, run.origin.slice('salie:'.length));
  assert.equal(list[0].taskId, task.id);
  ok('Salie proposed, the person confirmed, one session started and the conversation links to it by key');
  await page.screenshot({ path: path.join(artifacts, 'salie-launched.png') });

  // ---- 2. The same session, in Agents ----
  await card.getByTestId('open-in-agents').click();
  await page.waitForFunction(k => decodeURIComponent(location.hash).split('?')[0] === '#/sessions/' + k, key);
  await page.getByTestId('assistant').waitFor({ state: 'detached' }); // Salie folds away; what it started goes on
  const live = page.getByRole('region', { name: 'Live task session' });
  await live.waitFor();
  await live.getByText('Fixture process ready').first().waitFor();
  // One title, compact metadata, and the renderer's own header is not repeated; the open session is the page, not a list beside it.
  assert.equal(await page.locator('h2.session-title').count(), 1, 'the session title is shown once');
  assert.match(await page.getByTestId('session-meta').innerText(), /Fixture/i);
  assert.equal(await live.locator('header').count(), 0, 'the run view adds no second heading');
  assert.equal(await page.locator('[data-session]').count(), 0, 'an open session is not a list');
  assert.equal(await page.getByTestId('session-context').count(), 1, 'a session with a task shows its task context');
  await live.getByRole('textbox', { name: 'Additional instructions' }).fill('commit');
  await live.getByRole('button', { name: 'Send', exact: true }).click();
  await live.getByText('Committed fixture work').first().waitFor();
  assert.equal((await taskRuns()).length, 1, 'continuing the session does not start another');
  ok('Agents opens the same session with its feed and keeps taking instructions');
  await page.screenshot({ path: path.join(artifacts, 'agents-run.png') });

  // ---- 3. Return to Salie without interrupting the execution ----
  // Salie started it, so Salie is the page above the session: the breadcrumb is the way back (there is no second control for it).
  assert.equal(await page.getByTestId('session-salie').count(), 0);
  await page.getByTestId('crumbs').getByRole('button', { name: /Salie/ }).click();
  await page.getByTestId('assistant').waitFor();
  await page.getByTestId('assistant-log').getByText(`start the agent on ${project.id} ${task.id}`).waitFor();
  assert.equal(await page.getByTestId('assistant').locator('section.confirm').count(), 1, 'the same conversation, not a new one');
  assert.ok(['running', 'waiting_for_user', 'blocked', 'starting'].includes((await taskRuns())[0].state), 'the session is still alive');
  await page.keyboard.press('Escape');
  await page.getByTestId('assistant').waitFor({ state: 'detached' });
  ok('Return to Salie lands on the conversation that started the session, and the session keeps running');

  // ---- 4. A Work task opens the same session; terminals are standalone or for a task ----
  await page.goto(web + `/#/p/${project.id}/task/${task.id}`);
  await page.locator('section.panel').getByRole('button', { name: 'Open Session', exact: true }).first().click();
  await page.waitForFunction(k => decodeURIComponent(location.hash).split('?')[0] === '#/sessions/' + k, key);
  await page.getByRole('region', { name: 'Live task session' }).waitFor();
  assert.equal((await taskRuns()).length, 1);
  ok('Open Session on a Work task opens the very same session; still one run');

  // The Calendar reaches the same session through its task.
  const today = new Date().toISOString().slice(0, 10);
  const listed = await api('GET', `/api/projects/${project.id}/tasks`), current = (listed.tasks ?? listed).find(t => t.id === task.id) ?? task;
  await api('PATCH', `/api/projects/${project.id}/tasks/${task.id}`, { version: current.version, plan: { start: today, end: today } });
  await page.goto(web + `/#/p/${project.id}/calendar`);
  await page.locator('.planned-task', { hasText: 'Fix the login' }).first().click();
  await page.getByTestId('task-session').getByRole('button', { name: 'Open session' }).click();
  await page.waitForFunction(k => decodeURIComponent(location.hash).split('?')[0] === '#/sessions/' + k, key);
  await page.getByRole('region', { name: 'Live task session' }).waitFor();
  assert.equal((await taskRuns()).length, 1);
  ok('the Calendar opens the task’s session through the task sheet; still one run');
  await page.goto(web + '/#/sessions');
  async function openTerminal(taskTitle) {
    await page.evaluate(() => { location.hash = '#/sessions'; });
    await page.getByRole('button', { name: 'New session', exact: true }).click();
    const form = page.locator('form.launch'), selects = form.locator('select');
    await selects.nth(0).selectOption({ label: 'codex' });
    await selects.nth(1).selectOption({ label: 'Sessions fixture' });
    if (taskTitle) await selects.nth(2).selectOption({ label: taskTitle });
    const was = await page.evaluate(() => location.hash);
    await page.getByRole('button', { name: 'Open session', exact: true }).click();
    await page.waitForFunction(h => location.hash !== h && decodeURIComponent(location.hash).startsWith('#/sessions/terminal:'), was);
    await page.getByRole('status').filter({ hasText: /^Connected$/ }).waitFor();
    await page.waitForFunction(() => document.querySelector('.terminal')?.textContent.includes('Native PTY ready'));
  }
  await openTerminal('Fix the login');
  await page.locator('.xterm-helper-textarea').pressSequentially('for the task'); await page.keyboard.press('Enter');
  await page.waitForFunction(() => document.querySelector('.terminal')?.textContent.includes('REPLY:for the task'));
  await openTerminal('');
  list = await sessions();
  assert.equal(list.length, 3, 'a run and two terminals');
  assert.equal(new Set(list.map(s => s.key)).size, 3);
  const linked = list.find(s => s.kind === 'terminal' && s.taskId === task.id), standalone = list.find(s => s.kind === 'terminal' && !s.taskId);
  assert.ok(linked && standalone, JSON.stringify(list));
  assert.deepEqual((await sessions('?task=' + task.id)).map(s => s.key).sort(), [key, linked.key].sort(), 'the task shows its run and its terminal, not the standalone one');
  // A standalone terminal has no task context and gives the conversation the room; the one for a task shows it.
  assert.equal(await page.getByTestId('session-context').count(), 0, 'a standalone session has no task panel');
  assert.match(await page.getByTestId('session-meta').innerText(), /Standalone/);
  await page.evaluate(() => { location.hash = '#/sessions'; });
  await until(async () => (await page.locator('[data-session]').count()) === 3, 'every session listed once');
  // The overview is the page: runners and sessions, no empty detail pane waiting for a choice.
  assert.equal(await page.locator('section.open').count(), 0, 'no empty session pane');
  await page.getByRole('heading', { name: 'Runners', exact: true }).waitFor();
  await page.getByRole('button', { name: 'New session', exact: true }).waitFor();
  await page.screenshot({ path: path.join(artifacts, 'agents-overview.png') });
  // Switching sessions changes only which one is shown: nothing is started, stopped or duplicated.
  const runningBefore = (await api('GET', '/api/terminals')).filter(t => t.state === 'running').length;
  await page.locator(`[data-session="${linked.key}"] button`).click();
  await page.getByTestId('session-context').waitFor();
  await page.getByTestId('session-switch').selectOption(standalone.key);
  await page.waitForFunction(k => decodeURIComponent(location.hash).split('?')[0] === '#/sessions/' + k, standalone.key);
  await page.getByRole('status').filter({ hasText: /^Connected$/ }).waitFor();
  assert.equal(await page.getByTestId('session-context').count(), 0, 'the standalone session gives the room back');
  assert.equal((await api('GET', '/api/terminals')).filter(t => t.state === 'running').length, runningBefore, 'switching stops and starts nothing');
  assert.equal((await taskRuns()).length, 1);
  assert.equal((await sessions()).length, 3);
  await page.evaluate(() => { location.hash = '#/sessions'; });
  ok('a terminal opens standalone or for a task, through the same terminal renderer, and is listed once');
  await page.screenshot({ path: path.join(artifacts, 'agents-terminal.png') });
  await page.setViewportSize({ width: 390, height: 844 });
  await delay(250);
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 2), false, 'no horizontal overflow on a narrow window');
  await page.screenshot({ path: path.join(artifacts, 'agents-narrow.png') });
  await page.setViewportSize({ width: 1440, height: 1000 });

  // ---- 5. Reconnect to existing sessions after the controller restarts ----
  const before = (await sessions()).map(s => s.key).sort();
  await stop(controller);
  controller = start(bin, [], root, env);
  await ready(base + '/api/health', controller);
  await page.goto(web + '/#/sessions/' + encodeURIComponent(linked.key));
  const after = await sessions();
  assert.deepEqual(after.map(s => s.key).sort(), before, 'the same identities, none new, none lost');
  assert.equal(new Set(after.map(s => s.key)).size, after.length, 'no duplicates');
  assert.equal((await taskRuns()).length, 1, 'a restart starts no run');
  assert.equal(after.find(s => s.key === linked.key).state, 'interrupted', 'a restart never relaunches an agent');
  await page.getByRole('status').filter({ hasText: /Interrupted after device restart/ }).waitFor();
  await page.evaluate(() => { location.hash = '#/sessions'; });
  await until(async () => (await page.locator('[data-session]').count()) === 3, 'sessions listed after the restart');
  await page.locator(`[data-session="${key}"] button`).click();
  await page.getByRole('region', { name: 'Live task session' }).waitFor();
  await page.getByRole('region', { name: 'Session output' }).getByText('Committed fixture work').first().waitFor();
  ok('after a restart the same sessions are there under the same keys, with their history, and nothing is relaunched or duplicated');
  assert.deepEqual(errors, []);
  console.log('PASS unified agent sessions: launch through Salie, open in Agents, continue, return to Salie, open from Work, reconnect after restart');
})().catch(async e => {
  console.error(e);
  if (lastPage && !lastPage.isClosed()) { console.error('location:', await lastPage.evaluate(() => location.href).catch(() => '')); console.error(await lastPage.locator('body').innerText().catch(() => '')); await lastPage.screenshot({ path: path.join(artifacts, 'failure.png') }).catch(() => {}); }
  process.exitCode = 1;
}).finally(async () => {
  await browser?.close();
  for (const c of children.reverse()) await stop(c);
  fs.rmSync(dir, { recursive: true, force: true });
});

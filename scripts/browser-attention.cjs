// A real waiting runner is answered through the same attention list in the web and built window.
const cp = require('node:child_process');
const fs = require('node:fs'), path = require('node:path'), http = require('node:http'), assert = require('node:assert/strict');
const { chromium } = require('../web/node_modules/playwright');
const base = process.env.PERSONAL_BROWSER_URL, token = 'disposable-browser-credential';
const origin = process.env.PLANNING_SHELL_URL;
const root = path.resolve(__dirname, '../desktop/frontend/dist/shell');
const api = async (method, route, body) => {
  const r = await fetch(base + '/api' + route, { method, headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined });
  const t = await r.text(); assert(r.ok, method + route + ' ' + t); return t ? JSON.parse(t) : null;
};
const incomingRepos = [];
const wait = async fn => { for (let i = 0; i < 100; i++) { if (await fn()) return; await new Promise(r => setTimeout(r, 100)); } throw Error('runner did not reach expected state'); };
(async () => {
  await api('POST', '/onboarding/complete', {});
  await api('POST', '/onboarding/tours/seen', { surface: 'desktop' });
  const project = await api('POST', '/projects', { path: fs.realpathSync(process.env.WERKBORD_BROWSER_REPO), name: 'Attention fixture' });
  const workspace = { id: 'personal', kind: 'personal', name: 'Individual', state: 'ready', source: 'local' };
  const load = async () => ({ id: 'personal', kind: 'personal', url: base + '/#token=' + token, origin: base });
  let folderChoice = '';
  const calls = {
    ChooseDirectory: async () => folderChoice,
    Premium: async () => ({ active: false }), Info: async () => ({ version: 'fixture', platform: 'darwin' }),
    Workspaces: async () => ({ items: [workspace], selected: 'personal', problems: [], team: { state: 'ready' } }),
    Overview: async () => ({ entries: [{ workspace, summary: await api('GET', '/workspace/v1/summary') }], at: new Date().toISOString() }),
    OpenWorkspace: load, LoadWorkspace: load, AssistantWorkspaces: async () => [], RememberPlace: async () => null,
    PendingInvitation: async () => '', Relay: async (_id, method, args) => {
      if (method === 'Info') return calls.Info();
      if (method === 'ChooseDirectory') return folderChoice;
      assert.equal(method, 'AnswerQuestion');
      const q = args[0]; return { status: 200, body: await api('POST', `/projects/${q.projectId}/questions/${q.questionId}/answer`, { answer: q.answer }) };
    },
  };
  const server = http.createServer(async (req, res) => {
    if (req.url === '/call') {
      let raw = ''; for await (const part of req) raw += part;
      try { const { method, args } = JSON.parse(raw); res.setHeader('Content-Type', 'application/json'); res.end(JSON.stringify(await calls[method](...args) ?? null)); }
      catch (e) { res.writeHead(500); res.end(JSON.stringify({ error: e.message })); } return;
    }
    const file = path.resolve(root, '.' + new URL(req.url, origin).pathname);
    if (!file.startsWith(root + '/') || !fs.existsSync(file) || !fs.statSync(file).isFile()) { res.writeHead(404); res.end(); return; }
    res.setHeader('Content-Type', ({ '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.woff2': 'font/woff2', '.svg': 'image/svg+xml' })[path.extname(file)] || 'application/octet-stream');
    fs.createReadStream(file).pipe(res);
  });
  await new Promise(r => server.listen(new URL(origin).port, '127.0.0.1', r));
  const browser = await chromium.launch();
  try {
    for (const side of ['web', 'window']) {
      const task = await api('POST', `/projects/${project.id}/tasks`, { title: 'Question for ' + side, execution: { agent: 'fixture' } });
      const run = await api('POST', `/projects/${project.id}/tasks/${task.id}/runs`, { agentId: 'fixture' });
      await wait(async () => (await api('GET', `/projects/${project.id}/tasks/${task.id}/runs`)).runs.some(r => r.id === run.id && r.state === 'waiting_for_user' && r.waiting === 'idle'));
      await api('POST', `/projects/${project.id}/runs/${run.id}/input`, { text: 'ask' });
      await wait(async () => (await api('GET', `/projects/${project.id}/tasks/${task.id}/runs`)).runs.some(r => r.id === run.id && r.state === 'waiting_for_user' && r.waiting !== 'idle'));
      const ctx = await browser.newContext({ viewport: { width: 1440, height: 1000 }, colorScheme: 'dark', reducedMotion: 'reduce' });
      if (side === 'web') await ctx.route('**/api/folders?*', route => route.fulfill({ json: { path: folderChoice, parent: path.dirname(folderChoice), folders: [] } }));
      await ctx.addInitScript(t => { localStorage.setItem('werkbord.token', t); localStorage.setItem('werkbord.theme', 'dark'); if (window.parent === window) { window.runtime = { EventsOn: () => () => {} }; window.go = { main: { App: new Proxy({}, { get: (_, method) => async (...args) => { const r = await fetch('/call', { method: 'POST', body: JSON.stringify({ method, args }) }); const d = await r.json(); if (!r.ok) throw Error(d.error); return d; } }) } }; } }, token);
      const page = await ctx.newPage(); const errors = []; page.on('pageerror', e => errors.push(e.message));
      await page.goto(side === 'web' ? base + '/#/control?section=feed' : origin + '/shell/index.html');
      if (side === 'web') await page.waitForURL(base + '/#/control');
      await page.getByTestId('next-decision').getByText('1 waiting', { exact: false }).waitFor();
      const list = page.locator('#work-attention'); await list.locator('summary').click();
      const item = list.locator('li').filter({ hasText: 'Commit the fixture work?' });
      await item.getByRole('button', { name: 'Answer', exact: true }).click();
      await item.getByRole('button', { name: 'Yes', exact: true }).click();
      await wait(async () => !(await api('GET', `/projects/${project.id}/tasks/${task.id}/runs`)).runs.some(r => r.id === run.id && r.state === 'waiting_for_user' && r.waiting !== 'idle'));
      await wait(async () => JSON.stringify(await api('GET', `/projects/${project.id}/runs/${run.id}/events`)).includes('Committed fixture work'));
      await item.waitFor({ state: 'detached' });
      await page.getByTestId('next-decision').getByRole('heading', { name: 'Your next decision' }).waitFor();
      assert.deepEqual(errors, []);
      console.log('PASS ' + side + ': attention answer reaches the runner, removes the question and updates Waiting for you');
      await api('POST', `/projects/${project.id}/runs/${run.id}/finish`, {});
      // Feed's execution actions now live in the shared notices list.
      const makeRun = async (name, policy) => {
        const t = await api('POST', `/projects/${project.id}/tasks`, { title: name + ' ' + side, execution: { agent: 'fixture' } });
        const r = await api('POST', `/projects/${project.id}/tasks/${t.id}/runs`, { agentId: 'fixture', ...(policy ? { policy: { interaction: policy } } : {}) });
        await wait(async () => (await api('GET', `/projects/${project.id}/runs/${r.id}`)).waiting === 'idle');
        return { t, r };
      };
      const activeWork = await makeRun('Inspect ongoing activity');
      await api('POST', `/projects/${project.id}/runs/${activeWork.r.id}/input`, { text: 'work-long' });
      await wait(async () => (await api('GET', `/projects/${project.id}/runs/${activeWork.r.id}`)).state === 'running');
      await page.reload();
      const activity = page.locator('.work-overview .activity').filter({ hasText: 'Inspecting source files' });
      await activity.locator('summary').click();
      await activity.getByText('Inspecting source files and running the migration checks', { exact: true }).waitFor();
      await api('POST', `/projects/${project.id}/runs/${activeWork.r.id}/stop`, {});
      const failed = await makeRun('Retry this failure');
      await api('POST', `/projects/${project.id}/runs/${failed.r.id}/input`, { text: 'fail' });
      await wait(async () => (await api('GET', `/projects/${project.id}/runs/${failed.r.id}`)).state === 'failed');
      await page.reload();
      await page.locator('#work-attention > summary').click();
      await page.getByLabel('Filter notices by kind').selectOption('failed');
      await page.getByLabel('Filter notices by agent').selectOption('fixture');
      const summary = await api('GET', '/workspace/v1/summary');
      const failedItem = summary.attention.find(a => a.kind === 'failed');
      await page.getByLabel('Filter notices by runner').selectOption(failedItem.run.runnerId);
      const failureRow = page.locator('#work-attention li').filter({ hasText: 'Retry this failure ' + side });
      await failureRow.getByRole('button', { name: 'Run again', exact: true }).click();
      let retried;
      await wait(async () => { retried = (await api('GET', `/projects/${project.id}/tasks/${failed.t.id}/runs`)).runs.at(-1); return retried.id !== failed.r.id && retried.waiting === 'idle'; });
      assert.equal(await page.getByLabel('Filter notices by kind').inputValue(), 'failed', 'settling the last matching item retains the selected filter');
      await api('POST', `/projects/${project.id}/runs/${retried.id}/finish`, {});
      const blocked = await makeRun('Resolve this blocker', 'autonomous_stop_if_blocked');
      await api('POST', `/projects/${project.id}/runs/${blocked.r.id}/input`, { text: 'ask' });
      await wait(async () => (await api('GET', `/projects/${project.id}/runs/${blocked.r.id}`)).state === 'blocked');
      await page.reload();
      await page.locator('#work-attention > summary').click();
      const blockedRow = page.locator('#work-attention li').filter({ hasText: 'Resolve this blocker ' + side });
      await blockedRow.getByRole('button', { name: 'No', exact: true }).click();
      await wait(async () => (await api('GET', `/projects/${project.id}/runs/${blocked.r.id}`)).waiting === 'idle');
      await api('POST', `/projects/${project.id}/runs/${blocked.r.id}/finish`, {});
      assert.equal(await page.getByRole('navigation', { name: 'Work sections' }).getByText(/^Feed/).count(), 0, 'there is no duplicate Feed tab');
      // Incoming imports are projected onto Overview, including the window's own page.
      const source = 'werkbord-source:v1:workspace:project:member:';
      await api('PUT', '/integration/v1/waiting', { schema: 'werkbord.integration/v1', source, items: [{ sourceRef: source + 'ticket', repository: 'https://github.com/acme/missing-' + side, title: 'Incoming migration ticket', from: 'Fixture workspace' }] });
      const pendingSummary = await api('GET', '/workspace/v1/summary');
      assert.equal(pendingSummary.pendingRepositories?.length, 1, 'incoming repository survives the summary contract: ' + JSON.stringify(pendingSummary.pendingRepositories));
      await page.reload();
      await page.getByTestId('waiting-repository').filter({ hasText: 'Incoming migration ticket' }).waitFor();
      await page.getByTestId('waiting-repository').getByRole('button', { name: 'Choose folder…' }).waitFor();
      const incomingRepo = path.join(process.env.BROWSER_ARTIFACT_DIR, 'incoming-repo-' + side);
      fs.mkdirSync(incomingRepo);
      const git = (...args) => cp.execFileSync('git', ['-C', incomingRepo, '-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.test', '-c', 'core.hooksPath=/dev/null', ...args], { stdio: 'ignore' });
      git('init', '-b', 'main'); git('commit', '--allow-empty', '-m', 'Incoming repository fixture'); git('remote', 'add', 'origin', 'https://github.com/acme/missing-' + side);
      incomingRepos.push(incomingRepo);
      folderChoice = incomingRepo;
      await page.setViewportSize({ width: 900, height: 1000 });
      await page.evaluate(() => { document.documentElement.dataset.theme = 'dark'; document.querySelector('main').scrollTop = 0; });
      assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'incoming work fits a narrow window');
      await page.screenshot({ path: path.join(process.env.BROWSER_ARTIFACT_DIR, 'work-migration-' + side + '.png') });
      await page.getByTestId('waiting-repository').getByRole('button', { name: 'Choose folder…' }).click();
      if (side === 'web') await page.getByRole('button', { name: 'Use this folder', exact: true }).click();
      await wait(async () => (await api('GET', '/projects')).projects.some(p => p.repoPath === fs.realpathSync(incomingRepo)));
      await page.getByText('Repository added. The ticket appears in Work after its source checks again.').waitFor();
      await api('PUT', '/integration/v1/waiting', { schema: 'werkbord.integration/v1', source, items: [] });
      await page.locator('#work-attention > summary').click();
      await page.getByRole('button', { name: 'Alerts settings', exact: true }).click();
      if (side === 'web') await page.waitForURL(/#\/settings\?alerts$/);
      else { await page.getByTestId('settings-column').waitFor(); assert.equal(await page.locator('[data-section="general/alerts"]').getAttribute('aria-current'), 'page'); }
      assert.deepEqual(errors, [], 'Feed migration has no page errors');
      console.log('PASS ' + side + ': notice filters, failed-run retry, blocker choice, incoming repository section, alerts shortcut and no Feed tab');
      await ctx.close();
    }
  } finally { await browser.close(); await new Promise(r => server.close(r)); for (const repo of incomingRepos) fs.rmSync(repo, { recursive: true, force: true }); }
})().catch(e => { console.error(e); process.exitCode = 1; });

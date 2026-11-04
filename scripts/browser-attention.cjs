// A real waiting runner is answered through the same attention list in the web and built window.
const fs = require('node:fs'), path = require('node:path'), http = require('node:http'), assert = require('node:assert/strict');
const { chromium } = require('../web/node_modules/playwright');
const base = process.env.PERSONAL_BROWSER_URL, token = 'disposable-browser-credential';
const origin = process.env.PLANNING_SHELL_URL;
const root = path.resolve(__dirname, '../desktop/frontend/dist/shell');
const api = async (method, route, body) => {
  const r = await fetch(base + '/api' + route, { method, headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined });
  const t = await r.text(); assert(r.ok, method + route + ' ' + t); return t ? JSON.parse(t) : null;
};
const wait = async fn => { for (let i = 0; i < 100; i++) { if (await fn()) return; await new Promise(r => setTimeout(r, 100)); } throw Error('runner did not reach expected state'); };
(async () => {
  await api('POST', '/onboarding/complete', {});
  await api('POST', '/onboarding/tours/seen', { surface: 'desktop' });
  const project = await api('POST', '/projects', { path: fs.realpathSync(process.env.WERKBORD_BROWSER_REPO), name: 'Attention fixture' });
  const workspace = { id: 'personal', kind: 'personal', name: 'Individual', state: 'ready', source: 'local' };
  const load = async () => ({ id: 'personal', kind: 'personal', url: base + '/#token=' + token, origin: base });
  const calls = {
    Premium: async () => ({ active: false }), Info: async () => ({ version: 'fixture', platform: 'darwin' }),
    Workspaces: async () => ({ items: [workspace], selected: 'personal', problems: [], team: { state: 'ready' } }),
    Overview: async () => ({ entries: [{ workspace, summary: await api('GET', '/workspace/v1/summary') }], at: new Date().toISOString() }),
    OpenWorkspace: load, LoadWorkspace: load, AssistantWorkspaces: async () => [], RememberPlace: async () => null,
    PendingInvitation: async () => '', Relay: async (_id, method, args) => {
      if (method === 'Info') return calls.Info();
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
      const ctx = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
      await ctx.addInitScript(t => { localStorage.setItem('werkbord.token', t); if (window.parent === window) { window.runtime = { EventsOn: () => () => {} }; window.go = { main: { App: new Proxy({}, { get: (_, method) => async (...args) => { const r = await fetch('/call', { method: 'POST', body: JSON.stringify({ method, args }) }); const d = await r.json(); if (!r.ok) throw Error(d.error); return d; } }) } }; } }, token);
      const page = await ctx.newPage(); const errors = []; page.on('pageerror', e => errors.push(e.message));
      await page.goto(side === 'web' ? base + '/#/control' : origin + '/shell/index.html');
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
      await ctx.close();
    }
  } finally { await browser.close(); await new Promise(r => server.close(r)); }
})().catch(e => { console.error(e); process.exitCode = 1; });

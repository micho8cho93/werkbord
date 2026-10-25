// Phase 3: real authenticated API + native PTYs + xterm. The default CLI is a disposable test fixture.
// WERKBORD_NATIVE_SMOKE=1 additionally starts the installed Claude Code and Codex CLIs, without instructions.
const fs = require('node:fs'), os = require('node:os'), path = require('node:path');
const cp = require('node:child_process'), net = require('node:net');
const { once } = require('node:events');
const assert = require('node:assert/strict');
const root = path.resolve(__dirname, '..');
const { chromium } = require(path.join(root, 'web/node_modules/playwright'));
const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'werkbord-native-'));
const artifacts = path.join(root, 'output/playwright/salie-terminals'); fs.mkdirSync(artifacts, { recursive: true });
const token = 'disposable-browser-credential';
const children = []; let browser;
const delay = ms => new Promise(r => setTimeout(r, ms));
async function port() { const s = net.createServer(); s.listen(0, '127.0.0.1'); await once(s, 'listening'); const p = s.address().port; await new Promise(r => s.close(r)); return p; }
function start(bin, args, cwd, env) { const c = cp.spawn(bin, args, { cwd, env: { ...process.env, ...env }, stdio: ['ignore', 'pipe', 'pipe'] }); c.log = ''; c.stdout.on('data', b => c.log += b); c.stderr.on('data', b => c.log += b); children.push(c); return c; }
async function ready(url, child) { for (let i = 0; i < 200; i++) { if (child.exitCode !== null) throw Error(child.log); if (await fetch(url).then(r => r.ok).catch(() => false)) return; await delay(100); } throw Error(child.log); }
(async () => {
  const bin = path.join(dir, 'fixture'); cp.execFileSync('go', ['build', '-o', bin, './scripts/browser-fixture'], { cwd: root, stdio: 'inherit' });
  const cli = path.join(dir, 'native-cli');
  fs.writeFileSync(cli, '#!/bin/sh\n[ -t 0 ] && [ -t 1 ] && [ -t 2 ] || exit 17\nstty -echo -icanon min 1 time 0\nprintf \"\\033[6n\"\nresponse=$(dd bs=1 count=6 2>/dev/null)\n[ \"$response\" = \"$(printf \"\\033[1;1R\")\" ] || exit 19\nstty echo icanon\nprintf "\\033[32mNative PTY ready\\033[0m\\n"\nwhile IFS= read -r line; do printf "REPLY:%s\\n" "$line"; done\n', { mode: 0o700 });
  const apiPort = await port(), webPort = await port(); const base = `http://127.0.0.1:${apiPort}`, web = `http://127.0.0.1:${webPort}`;
  const controller = start(bin, [], root, { WERKBORD_BROWSER_DIR: dir, WERKBORD_BROWSER_ADDR: `127.0.0.1:${apiPort}`, WERKBORD_BROWSER_ASSISTANT: '1', WERKBORD_BROWSER_TERMINALS: '1', WERKBORD_BROWSER_TERMINAL_CLI: process.env.WERKBORD_NATIVE_SMOKE === '1' ? '' : cli });
  await ready(base + '/api/health', controller);
  async function api(method, url, data) { const r = await fetch(base + url, { method, headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' }, body: data === undefined ? undefined : JSON.stringify(data) }); const text = await r.text(); assert.ok(r.ok, `${method} ${url}: ${r.status} ${text}`); return JSON.parse(text); }
  await api('POST', '/api/onboarding/complete', {});
  const repo = path.join(dir, 'repo'); cp.execFileSync('git', ['init', '-q', repo]);
  const project = await api('POST', '/api/projects', { path: repo, name: 'Terminal fixture' });
  await api('PUT', '/api/settings/selection', { agents: { codex: { directCLI: true, billing: 'auto', enabled: false }, 'claude-code': { directCLI: true, billing: 'auto', enabled: false } } });
  const catalog = await api('GET', '/api/agents/catalog'); const device = catalog.agents.find(a => a.id === 'codex').interactive.device;
  if (process.env.WERKBORD_NATIVE_SMOKE === '1') {
    const sessions = [];
    for (const agentId of ['claude-code', 'codex']) sessions.push(await api('POST', `/api/projects/${project.id}/terminals`, { agentId, device, cols: 100, rows: 30 }));
    try {
      await delay(1000);
      for (const s of sessions) {
        const output = await api('GET', `/api/projects/${project.id}/terminals/${s.id}/output?after=0`);
        assert.ok(output.data && Buffer.from(output.data, 'base64').length > 0, `${s.agentId} produced no terminal bytes`);
        assert.equal(output.session.state, 'running');
        console.log(`PASS installed ${s.agentId} native interactive startup; ${Buffer.from(output.data, 'base64').length} bytes (contents withheld)`);
      }
      assert.equal((await api('GET', '/api/terminals')).filter(s => s.state === 'running').length, 2);
    } finally { for (const s of sessions) await api('POST', `/api/projects/${project.id}/terminals/${s.id}/stop`, {}); }
    return;
  }
  const vite = start(process.execPath, [path.join(root, 'web/node_modules/vite/bin/vite.js'), '--host', '127.0.0.1', '--port', String(webPort), '--strictPort'], path.join(root, 'web'), { DEVBOARD_URL: base }); await ready(web, vite);
  browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  await context.addInitScript(token => { localStorage.setItem('devboard.token', token); localStorage.setItem('werkbord.theme', 'light'); }, token);
  const page = await context.newPage(); const errors = []; page.on('pageerror', e => errors.push(e.message)); page.setDefaultTimeout(10000);
  await page.goto(web + '/#/personal'); await page.getByRole('button', { name: 'Create with Salie', exact: true }).click();
  async function launch(agent) { await page.getByRole('button', { name: 'New tab', exact: true }).click(); await page.getByRole('dialog', { name: 'New tab', exact: true }).getByRole('button', { name: agent, exact: true }).click(); await page.getByRole('button', { name: 'Open terminal', exact: true }).click(); await page.locator('.terminal-session .identity').filter({ hasText: agent }).waitFor(); await page.getByRole('status').filter({ hasText: /^Connected$/ }).waitFor(); }
  await launch('claude-code'); await page.locator('.xterm-helper-textarea').pressSequentially('alpha'); await page.keyboard.press('Enter');
  await launch('codex'); await page.locator('.xterm-helper-textarea').pressSequentially('beta'); await page.keyboard.press('Enter');
  let sessions = await api('GET', '/api/terminals'); assert.equal(sessions.filter(s => s.state === 'running').length, 2);
  for (const s of sessions) {
    let text = '';
    const expected = s.agentId === 'codex' ? 'REPLY:beta' : 'REPLY:alpha';
    for (let i = 0; i < 50; i++) {
      const out = await api('GET', `/api/projects/${project.id}/terminals/${s.id}/output?after=0`);
      text = Buffer.from(out.data || '', 'base64').toString();
      if (text.includes(expected)) break;
      await delay(100);
    }
    assert.ok(text.includes(expected), `${s.agentId}: ${JSON.stringify(text)}`);
    assert.ok(!text.includes(s.agentId === 'codex' ? 'alpha' : 'beta'));
  }

  await page.screenshot({ path: path.join(artifacts, 'desktop.png') });
  await page.getByRole('button', { name: /Close codex .* tab/ }).click();
  sessions = await api('GET', '/api/terminals'); assert.equal(sessions.filter(s => s.state === 'running').length, 2);
  await page.getByRole('button', { name: 'New tab', exact: true }).click(); await page.getByRole('dialog', { name: 'New tab', exact: true }).getByRole('button', { name: /codex · Terminal fixture · running/ }).click();
  await page.getByRole('status').filter({ hasText: /^Connected$/ }).waitFor();
  await page.keyboard.press('Escape'); assert.equal(await page.getByTestId('assistant').count(), 1);
  await page.setViewportSize({ width: 390, height: 844 }); await delay(250); await page.screenshot({ path: path.join(artifacts, 'narrow.png') });
  await page.getByRole('button', { name: 'Terminate', exact: true }).click();
  await page.getByRole('status').filter({ hasText: /^Process ended$/ }).waitFor();
  assert.equal((await api('GET', '/api/terminals')).filter(s => s.state === 'running').length, 1);
  assert.deepEqual(errors, []); console.log('PASS native PTY UI: two isolated sessions, keyboard input, close/reopen, Escape, resize, terminate; desktop/narrow captures');
})().catch(e => { console.error(e); process.exitCode = 1; }).finally(async () => { await browser?.close(); for (const child of children.reverse()) { child.kill('SIGTERM'); await Promise.race([once(child, 'exit'), delay(2500)]); if (child.exitCode === null) child.kill('SIGKILL'); } fs.rmSync(dir, { recursive: true, force: true }); });

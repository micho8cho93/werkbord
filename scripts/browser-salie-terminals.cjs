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
const children = []; let browser, lastPage;
const delay = ms => new Promise(r => setTimeout(r, ms));
async function port() { const s = net.createServer(); s.listen(0, '127.0.0.1'); await once(s, 'listening'); const p = s.address().port; await new Promise(r => s.close(r)); return p; }
function start(bin, args, cwd, env) { const c = cp.spawn(bin, args, { cwd, env: { ...process.env, ...env }, stdio: ['ignore', 'pipe', 'pipe'] }); c.log = ''; c.stdout.on('data', b => c.log += b); c.stderr.on('data', b => c.log += b); children.push(c); return c; }
async function ready(url, child) { for (let i = 0; i < 200; i++) { if (child.exitCode !== null) throw Error(child.log); if (await fetch(url).then(r => r.ok).catch(() => false)) return; await delay(100); } throw Error(child.log); }
(async () => {
  if (!['darwin','linux'].includes(process.platform)) { console.log('SKIP native PTY browser: unsupported execution platform'); return; }
  const bin = path.join(dir, 'fixture'); cp.execFileSync('go', ['build', '-o', bin, './scripts/browser-fixture'], { cwd: root, stdio: 'inherit' });
  const cli = path.join(dir, 'native-cli');
  fs.writeFileSync(cli, '#!/bin/sh\n[ -t 0 ] && [ -t 1 ] && [ -t 2 ] || exit 17\nstty -echo -icanon min 1 time 0\nprintf \"\\033[6n\"\nresponse=$(dd bs=1 count=6 2>/dev/null)\n[ \"$response\" = \"$(printf \"\\033[1;1R\")\" ] || exit 19\nstty echo icanon\nprintf "\\033[32mNative PTY ready\\033[0m\\n"\nwhile IFS= read -r line; do printf "REPLY:%s\\n" "$line"; done\n', { mode: 0o700 });
  const apiPort = await port(), webPort = await port(); const base = `http://127.0.0.1:${apiPort}`, web = `http://127.0.0.1:${webPort}`;
  const controller = start(bin, [], root, { WERKBORD_BROWSER_DIR: dir, WERKBORD_BROWSER_ADDR: `127.0.0.1:${apiPort}`, WERKBORD_BROWSER_ASSISTANT: '1', WERKBORD_BROWSER_TERMINALS: '1', WERKBORD_BROWSER_EXECUTION: '1', WERKBORD_BROWSER_TERMINAL_CLI: process.env.WERKBORD_NATIVE_SMOKE === '1' ? '' : cli });
  await ready(base + '/api/health', controller);
  async function api(method, url, data, signal = AbortSignal.timeout(25000)) { const r = await fetch(base + url, { signal, method, headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' }, body: data === undefined ? undefined : JSON.stringify(data) }); const text = await r.text(); assert.ok(r.ok, `${method} ${url}: ${r.status} ${text}`); return text ? JSON.parse(text) : null; }
  await api('POST', '/api/onboarding/complete', {});
  const repo = path.join(dir, 'repo'); cp.execFileSync('git', ['init', '-q', repo]); cp.execFileSync('git',['-C',repo,'-c','user.name=Fixture','-c','user.email=fixture@example.test','commit','--allow-empty','-qm','Fixture']);
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
  const vite = start(process.execPath, [path.join(root, 'web/node_modules/vite/bin/vite.js'), '--host', '127.0.0.1', '--port', String(webPort), '--strictPort'], path.join(root, 'web'), { WERKBORD_URL: base }); await ready(web, vite);
  browser = await chromium.launch({ headless: true, ...(process.env.BROWSER_EXECUTABLE ? {executablePath:process.env.BROWSER_EXECUTABLE} : {}) });
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  await context.addInitScript(token => { localStorage.setItem('werkbord.token', token); localStorage.setItem('werkbord.theme', 'light'); }, token);
  const page = await context.newPage(); lastPage = page; const errors = []; page.on('pageerror', e => errors.push(e.message)); page.setDefaultTimeout(10000);
  await page.goto(web + '/#/control'); await page.getByTestId('assistant-button').first().click();
  async function launch(agent) { await page.getByRole('button', { name: 'New tab', exact: true }).click(); await page.getByRole('dialog', { name: 'New tab', exact: true }).getByRole('button', { name: agent, exact: true }).click(); await page.getByRole('button', { name: 'Open terminal', exact: true }).click(); await page.locator('.terminal-session .identity').filter({ hasText: agent }).waitFor(); await page.getByRole('status').filter({ hasText: /^Connected$/ }).waitFor(); await page.waitForFunction(()=>document.querySelector('.terminal')?.textContent.includes('Native PTY ready')); }
  await launch('claude-code'); await page.locator('.xterm-helper-textarea').pressSequentially('alpha'); await page.keyboard.press('Enter');
  await launch('codex'); await page.locator('.xterm-helper-textarea').pressSequentially('beta'); await page.keyboard.press('Enter');
  let sessions = await api('GET', '/api/terminals'); assert.equal(sessions.filter(s => s.state === 'running').length, 2);
  for (const s of sessions) {
    let text = '', after = 0;
    const expected = s.agentId === 'codex' ? 'REPLY:beta' : 'REPLY:alpha';
    for (let i = 0; i < 50; i++) {
      const out = await api('GET', `/api/projects/${project.id}/terminals/${s.id}/output?after=${after}`);
      after = out.next; text += Buffer.from(out.data || '', 'base64').toString();
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

  // Phase 4: an independently enrolled phone attaches after the computer UI has closed.
  const made = await api('POST', '/api/phones/codes', {});
  const enrolled = await api('POST', '/api/phones/enroll', { code: made.code, name: 'Session phone' });
  await page.setViewportSize({ width: 1440, height: 1000 });
  await launch('codex');
  sessions = (await api('GET', '/api/terminals')).filter(s => s.state === 'running');
  // Exercise the owner's actual grant UI for the active terminal; grant the other via the same API.
  await page.getByRole('button', { name: 'Phone access', exact: true }).click();
  await page.getByRole('checkbox', { name: 'Session phone', exact: true }).check();
  await delay(200);
  for (const s of sessions) await api('PUT', `/api/projects/${s.projectId}/terminals/${s.id}/phone`, { phoneId: enrolled.id, enabled: true });
  await context.close();
  const mobile = await browser.newContext({ viewport: { width: 320, height: 640 }, hasTouch: true, isMobile: true });
  await mobile.addInitScript(token => { localStorage.setItem('werkbord.token', token); localStorage.setItem('werkbord-tour-phone-deck-v2', 'fixture'); }, enrolled.token);
  const nativeInput = [];
  let phone = await mobile.newPage(); lastPage = phone;
  phone.on('request',r=>{if(r.url().includes('/terminals/') && r.url().endsWith('/input')) nativeInput.push(JSON.parse(r.postData()).data);}); phone.setDefaultTimeout(15000); phone.on('pageerror', e => errors.push(e.message));
    await phone.goto(web + '/#/deck');
  await phone.getByTestId('nav-assistant').click();
  const selector = () => phone.getByRole('combobox', { name: 'Salie session', exact: true });
  async function selectTerminal(agent) {
    const option = selector().locator('option').filter({ hasText: new RegExp('^' + agent + ' · Terminal fixture · Terminal') });
    await option.waitFor({ state: 'attached' });
    await selector().selectOption(await option.getAttribute('value'));
    await phone.getByTestId('assistant').getByRole('status').filter({ hasText: /^Connected$/ }).waitFor(); // not the deck's own connection chip, which stays mounted while the assistant opens
  }
  await selectTerminal('claude-code');
  assert.equal(await phone.getByRole('tab').count(), 0, 'mobile uses a compact session selector');
  assert.equal(await phone.getByRole('button', { name: 'Phone access', exact: true }).count(), 0);
  await phone.getByRole('button',{name:'Keyboard',exact:true}).click(); await phone.keyboard.insertText('mobile instruction'); await phone.keyboard.press('Enter');
  const claude = sessions.find(s => s.agentId === 'claude-code');
  async function outputContains(session, text) {
    let after = 0, output = '';
    for (let i = 0; i < 80; i++) { const out = await api('GET', `/api/projects/${session.projectId}/terminals/${session.id}/output?after=${after}`, undefined, AbortSignal.timeout(5000)); after = out.next; output += Buffer.from(out.data || '', 'base64').toString(); if (output.includes(text)) return; await delay(100); }
    throw Error('Missing terminal output: ' + text);
  }
  await outputContains(claude, 'REPLY:mobile instruction'); console.log('PASS mobile native input after desktop close');
  for (const name of ['Escape', 'Tab', 'Arrow up', 'Arrow down', 'Arrow left', 'Arrow right']) {
    const button = phone.getByRole('button', { name, exact: true });
    const box = await button.boundingBox(); assert.ok(box.height >= 43.9 && box.width >= 43.9, name + ' touch size'); await button.click();
  }
  await phone.getByRole('button', { name: 'Ctrl', exact: true }).click();
  assert.equal(await phone.getByRole('button', { name: 'Ctrl', exact: true }).getAttribute('aria-pressed'), 'true');
  await phone.getByRole('button', { name: 'Ctrl', exact: true }).click();
  await phone.getByRole('button', {name:'Ctrl',exact:true}).click(); await phone.keyboard.insertText('u');
  await phone.waitForTimeout(300);
  assert.ok(nativeInput.join('').includes('\x1b\t\x1b[A\x1b[B\x1b[D\x1b[C'));
  assert.ok(nativeInput.join('').includes('\x15'), 'Ctrl-U reaches the native PTY');
  await phone.getByRole('combobox', { name: 'Terminal text size' }).selectOption('16');
  for (const scheme of ['light', 'dark']) for (const [width, height] of [[320,640],[430,844],[844,390]]) {
    await phone.emulateMedia({ colorScheme: scheme }); await phone.setViewportSize({width,height}); await delay(200);
    assert.equal(await phone.evaluate(() => document.documentElement.scrollWidth > innerWidth + 2), false);
    assert.ok((await phone.locator('.terminal-session > .terminal').boundingBox()).height > 40, 'terminal remains visible');
    await phone.screenshot({path:path.join(artifacts,`phone-${scheme}-${width}.png`)});
  }
  await phone.setViewportSize({width:390,height:600});
  await mobile.setOffline(true); await phone.getByRole('status').filter({hasText:/Disconnected/}).waitFor();
  await mobile.setOffline(false); await phone.getByTestId('assistant').getByRole('status').filter({hasText:/^Connected$/}).waitFor();
  console.log('PASS mobile layout and network recovery'); await selectTerminal('codex'); await selectTerminal('claude-code');
  await phone.reload(); await phone.getByTestId('nav-assistant').click(); await selectTerminal('claude-code');
  // A lost input response must block subsequent input and must never resend the uncertain bytes.
  let uncertain = 0;
  await phone.route('**/terminals/*/input', async route => { uncertain++; await route.abort('failed'); });
  await phone.getByRole('button',{name:'Keyboard',exact:true}).click(); await phone.keyboard.insertText('x');
  await phone.getByRole('alert').filter({hasText:'Input delivery is uncertain'}).waitFor();
  await phone.getByRole('button',{name:'Keyboard',exact:true}).click(); await phone.keyboard.insertText('y'); await delay(300); assert.equal(uncertain,1);
  await phone.unroute('**/terminals/*/input');
  await phone.getByRole('button', {name:'I checked the output · Enable new input'}).click();
  await phone.getByRole('button',{name:'Keyboard',exact:true}).click(); await phone.keyboard.insertText('after reconnect'); await phone.keyboard.press('Enter');
  await outputContains(claude, 'REPLY:after reconnect');
  // Salie remains available alongside the terminal; no phone-side CLI launcher is offered.
  await phone.getByRole('button', {name:'New tab',exact:true}).click();
  assert.equal(await phone.getByRole('link',{name:'Manage Coding Agents →'}).count(),0);
  await phone.getByRole('button', {name:'New Salie Conversation',exact:true}).click();
  await phone.getByRole('textbox', {name:'Message to Salie'}).fill('mobile conversation');
  await phone.getByRole('button',{name:'Send',exact:true}).click();
  await phone.getByTestId('assistant-log').getByText('mobile conversation',{exact:true}).waitFor();
  // Existing task sessions still attach through the phone's reviewed task/run routes.
  const task = await api('POST',`/api/projects/${project.id}/tasks`,{title:'Mobile task session',execution:{agent:'fixture',mode:'manual'}});
  await api('POST',`/api/projects/${project.id}/tasks/${task.id}/runs`,{});
  await phone.getByTestId('nav-deck').click(); await phone.getByTestId('assistant').waitFor({state:'detached'}); await phone.goto(web+`/#/p/${project.id}/task/${task.id}`);
  await phone.getByRole('region',{name:'Mobile task session',exact:true}).getByRole('button',{name:'Open Session',exact:true}).click();
  await phone.getByRole('region',{name:'Live task session'}).waitFor();
  await phone.getByRole('textbox',{name:'Additional instructions'}).fill('phone task instruction');
  await phone.getByRole('region',{name:'Live task session'}).getByRole('button',{name:'Send',exact:true}).click();
  await phone.getByRole('region',{name:'Session output'}).getByText('phone task instruction',{exact:false}).waitFor();
  assert.ok(await selector().locator('option').filter({hasText:' · Task · '}).count());
  await phone.getByRole('button',{name:'Stop run',exact:true}).click();
  await selectTerminal('codex'); await phone.getByRole('button',{name:'Terminate',exact:true}).click();
  await phone.getByRole('status').filter({hasText:/Process ended/}).waitFor();
  await selectTerminal('claude-code');
  // Closing the phone, too, leaves the very same native process accepting input.
  await phone.close();
  assert.equal((await api('GET','/api/terminals')).find(s=>s.id===claude.id).state,'running');
  phone = await mobile.newPage(); lastPage = phone; await phone.goto(web+'/#/deck'); await phone.getByTestId('nav-assistant').click(); await selectTerminal('claude-code');
  await api('DELETE','/api/phones/'+enrolled.id);
  const revoked = await fetch(base+`/api/projects/${project.id}/terminals/${claude.id}/input`,{method:'POST',headers:{Authorization:'Bearer '+enrolled.token,'Content-Type':'application/json'},body:JSON.stringify({data:'denied',client:'revoked-browser-client',sequence:1})});
  assert.equal(revoked.status,401);
  await mobile.close();
  console.log('PASS enrolled mobile sessions: explicit grants, same process after desktop/phone close, live input, controls, small screens/themes, reconnect, uncertain input fence, Salie, termination and revocation');
  assert.deepEqual(errors, []); console.log('PASS native PTY UI: two isolated sessions, keyboard input, close/reopen, Escape, resize, terminate; desktop/narrow captures');
})().catch(async e => { console.error(e); if (lastPage && !lastPage.isClosed()) { console.error(await lastPage.locator('body').innerText()); await lastPage.screenshot({path:path.join(artifacts,'failure.png')}); } process.exitCode = 1; }).finally(async () => { await browser?.close(); for (const child of children.reverse()) { child.kill('SIGTERM'); await Promise.race([once(child, 'exit'), delay(2500)]); if (child.exitCode === null) child.kill('SIGKILL'); } fs.rmSync(dir, { recursive: true, force: true }); });

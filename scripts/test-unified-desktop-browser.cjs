// The built Svelte shell + its real Go registry/relay + real Personal and Team services.
// Native window transport/dialogs and private-overlay TCP are the only substitutions.
const fs = require('node:fs'), os = require('node:os'), path = require('node:path'), cp = require('node:child_process'), net = require('node:net'), assert = require('node:assert/strict');
const { once } = require('node:events');
const { chromium } = require('../web/node_modules/playwright');
const root = path.resolve(__dirname, '..'), temp = fs.mkdtempSync(path.join(os.tmpdir(), 'werkbord-shell-browser-'));
const children = []; let browser, testPage;
const delay = ms => new Promise(r => setTimeout(r, ms));
function run(cmd, args) { const r = cp.spawnSync(cmd, args, { cwd: root, encoding: 'utf8', timeout: 180000 }); if (r.status !== 0) throw Error(r.stderr || r.stdout); return r.stdout; }
function spawn(cmd, args, env = {}, cwd = root) { const p = cp.spawn(cmd, args, { cwd, env: { ...process.env, ...env }, stdio: ['ignore', 'pipe', 'pipe'] }); p.diagnostics = ''; p.stdout.on('data', d => p.diagnostics += d); p.stderr.on('data', d => p.diagnostics += d); children.push(p); return p; }
async function port() { const s = net.createServer(); s.listen(0, '127.0.0.1'); await once(s, 'listening'); const p = s.address().port; await new Promise(r => s.close(r)); return p; }
async function wait(fn, label) { for (let i = 0; i < 900; i++) { for (const c of children) if (c.exitCode !== null) throw Error(c.diagnostics); if (await fn()) return; await delay(100); } throw Error('Timed out: ' + label); }
async function api(base, token, method, url, body) { const r = await fetch(base + url, { method, headers: { Authorization: 'Bearer ' + token, ...(body ? { 'Content-Type': 'application/json' } : {}) }, body: body ? JSON.stringify(body) : undefined }); if (r.status === 204) return null; const d = await r.json(); if (!r.ok) throw Error(r.status + ' ' + JSON.stringify(d)); return d; }
(async () => {
 const origin = 'http://127.0.0.1:' + await port(), personalURL = 'http://127.0.0.1:' + await port();
 const env = { WERKBORD_UNIFIED_BROWSER_FIXTURE: temp, WERKBORD_BROWSER_SHELL_ORIGIN: origin, WERKBORD_BROWSER_PERSONAL: personalURL };
 const personalBinary = path.join(temp, 'personal'); run('go', ['build', '-o', personalBinary, './scripts/browser-fixture']);
 spawn(personalBinary, [], { ...env, WERKBORD_BROWSER_ADDR: new URL(personalURL).host, WERKBORD_BROWSER_EXECUTION: '1' });
 await wait(async () => { try { return (await fetch(personalURL + '/api/health')).ok; } catch { return false; } }, 'Personal');
 const flags = run('sh', ['scripts/team-build-flags.sh']).trim();
 spawn('go', ['test', '-ldflags', flags, '-run', '^TestUnifiedDesktopBrowserFixture$', '-count=1', '-timeout', '12m', './internal/team/server'], { ...env, WERKBORD_REQUIRE_RQLITE: '1', WERKBORD_SKIP_RQLITE: '0' });
 await wait(() => fs.existsSync(path.join(temp, 'team-ready.json')), 'Team Hub');
 const m = JSON.parse(fs.readFileSync(path.join(temp, 'team-ready.json')));
 const dev = (method, route, body) => api(m.first, m.firstKey, method, '/api/device/v1' + route, body);
 const team = (method, route, body) => api(m.first, m.firstKey, method, '/api/team/v1' + route, body);
 const other = (method, route, body) => api(m.other, m.otherKey, method, '/api/device/v1' + route, body);
 const otherTeam = (method, route, body) => api(m.other, m.otherKey, method, '/api/team/v1' + route, body);
 const license = fs.readFileSync(path.join(temp, 'license.json'), 'utf8');
 for (const device of [dev, other]) { await device('POST', '/license', { document: license }); await device('POST', '/create', { name: device === dev ? 'Northstar Studio' : 'Second workspace', owner: 'Ada' }); }
 await wait(async () => (await dev('GET', '/state')).connected && (await other('GET', '/state')).connected, 'two independent hosts');
 const invite = await otherTeam('POST', '/enrollment-invitations', { label: 'Second membership', role: 'member', capabilities: [], requireApproval: true, expiresInHours: 1 });
 const slot = (await dev('POST', '/workspaces', {})).slot;
 const secondDev = (method, route, body) => api(m.first, m.firstKey, method, '/w/' + slot + '/api/device/v1' + route, body);
 await secondDev('POST', '/join', { link: invite.link, name: 'Ada member', deviceName: 'Shared Mac' });
 await wait(async () => { const s = await secondDev('GET', '/state'); if (s.error) throw Error(s.error); return (await otherTeam('GET', '/enrollments')).length > 0; }, 'enrollment request');
 const enrollment = (await otherTeam('GET', '/enrollments'))[0]; await otherTeam('POST', '/enrollments/' + enrollment.id + '/approve', {});
 await wait(async () => (await secondDev('GET', '/state')).connected, 'second workspace joined on same device');
 spawn('go', ['test', '-run', '^TestWorkspaceShellBrowserFixture$', '-count=1', '-timeout', '12m', './internal/shell'], env, path.join(root, 'desktop'));
 await wait(() => fs.existsSync(path.join(temp, 'shell-ready')), 'Go desktop shell');
 const repo = path.join(temp, 'repo'); fs.mkdirSync(repo); run('git', ['-C', repo, 'init', '-b', 'main']); run('git', ['-C', repo, '-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.test', '-c', 'core.hooksPath=/dev/null', 'commit', '--allow-empty', '-m', 'Initial fixture']); run('git', ['-C', repo, 'remote', 'add', 'origin', 'https://github.com/acme/shop']);
 const full = 'disposable-browser-credential'; await api(personalURL, full, 'POST', '/api/projects', { name: 'Customer portal', path: repo });
 const project = await team('POST', '/projects', { name: 'Customer portal', repository: 'https://github.com/acme/shop' });
 const ticket = await team('POST', `/projects/${project.id}/tickets`, { title: 'Improve sign-in', status: 'available' }); await team('POST', `/projects/${project.id}/tickets/${ticket.id}/claim`, {});
 const scheduled = await team('POST', `/projects/${project.id}/tickets`, { title: 'Review the customer portal navigation', status: 'available' }); await team('POST', `/projects/${project.id}/tickets/${scheduled.id}/claim`, {});
 await team('PUT', `/projects/${project.id}/tickets/${scheduled.id}/schedule`, { at: new Date(Date.now() + 3600000).toISOString(), timezone: 'Europe/Madrid', missedPolicy: 'run_late', graceSeconds: 60, priority: 1 });
 browser = await chromium.launch({ headless: true }); const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
 await context.addInitScript(() => { if (window.parent !== window) return; window.go = { main: { App: new Proxy({}, { get: (_, method) => async (...args) => { const r = await fetch('/fixture/call', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ Method: method, Args: args }) }); const d = await r.json(); if (!r.ok) throw Error(d.error); return d; } }) } }; });
 const page = await context.newPage(); testPage = page; page.setDefaultTimeout(90000); const errors = []; page.on('pageerror', e => errors.push(e.message));
 // Switching is the product name in the shown workspace's own header (or, on the shell's own pages, in their bar).
 let current = 'personal';
 const switcher = async () => { const shell = page.getByTestId('workspace-switcher'); if (await shell.count()) await shell.click(); else await page.frameLocator('iframe[data-workspace="' + current + '"]').locator('[data-testid=product-switch]:visible').click(); await page.getByTestId('switcher-menu').waitFor(); };
 const switchTo = async (id) => { await switcher(); await page.getByTestId('switcher-menu').locator('[data-workspace="' + id + '"]').click(); await page.locator('iframe[data-workspace="' + id + '"]').waitFor({ state: 'visible' }); current = id; };
 await page.goto(origin + '/shell/shell/index.html'); await page.locator('iframe[data-workspace=personal]').waitFor();
 for (const section of ['Overview', 'Board', 'Git', 'Runs']) await page.frameLocator('iframe[data-workspace=personal]').getByText(section, { exact: true }).first().waitFor();
 console.log('PASS Individual keeps its own design: rail, project tabs and the product switcher in its header');
 await switchTo('team:main'); const firstFrame = page.frameLocator('iframe[data-workspace="team:main"]');
 await firstFrame.locator('nav').getByRole('button', { name: 'Settings', exact: true }).click();
 await firstFrame.getByRole('button', { name: 'Connect my Individual runner', exact: true }).click();
 await wait(async () => (await dev('GET', '/state')).runner.connected, 'explicit narrow grant connection');
 // Claimed before the runner was connected: the Team service on this computer copies it into Individual, and the ticket opens there.
 await wait(async () => ((await dev('GET', '/state')).sync?.tickets || []).some(t => t.ticketId === ticket.id && t.taskId), 'claimed ticket copied into Individual');
 await page.locator('[data-page=mywork]').click(); await page.getByRole('button', { name: new RegExp(ticket.key + ' Improve sign-in') }).click(); current = 'team:main';
 await firstFrame.getByTestId('open-in-individual').click();
 await page.locator('iframe[data-workspace=personal]').waitFor({ state: 'visible' }); current = 'personal';
 await page.frameLocator('iframe[data-workspace=personal]').getByRole('heading', { name: ticket.key + ': Improve sign-in' }).waitFor();
 console.log('PASS claim in Team, task in Individual, Open in Individual switches to it');
 // A held ticket whose repository is not a project here waits for it in Individual's Needs you; linking a clone imports it.
 const billing = await team('POST', '/projects', { name: 'Billing', repository: 'https://github.com/acme/billing' });
 const invoice = await team('POST', `/projects/${billing.id}/tickets`, { title: 'Retry failed invoices', status: 'available' }); await team('POST', `/projects/${billing.id}/tickets/${invoice.id}/claim`, {});
 await wait(async () => (await api(personalURL, 'disposable-browser-credential', 'GET', '/api/integration/waiting')).items.length === 1, 'ticket waiting for its repository');
 await page.frameLocator('iframe[data-workspace=personal]').getByRole('link', { name: 'Close', exact: true }).click();
 await page.frameLocator('iframe[data-workspace=personal]').getByRole('link', { name: /Control Center/ }).first().click();
 await page.frameLocator('iframe[data-workspace=personal]').getByTestId('waiting-repository').filter({ hasText: invoice.key }).waitFor();
 const billingRepo = path.join(temp, 'billing'); fs.mkdirSync(billingRepo); run('git', ['-C', billingRepo, 'init', '-b', 'main']); run('git', ['-C', billingRepo, '-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.test', '-c', 'core.hooksPath=/dev/null', 'commit', '--allow-empty', '-m', 'Initial fixture']); run('git', ['-C', billingRepo, 'remote', 'add', 'origin', 'git@github.com:acme/billing.git']);
 await assert.rejects(api(personalURL, 'disposable-browser-credential', 'POST', '/api/integration/waiting/link', { path: repo, repository: 'https://github.com/acme/billing' }), /not a clone/);
 const linked = await api(personalURL, 'disposable-browser-credential', 'POST', '/api/integration/waiting/link', { path: billingRepo, repository: 'https://github.com/acme/billing' });
 await wait(async () => (await api(personalURL, 'disposable-browser-credential', 'GET', `/api/projects/${linked.id}/tasks`)).tasks.some(t => t.title.startsWith(invoice.key)), 'linked repository imports the waiting ticket');
 assert.equal((await api(personalURL, 'disposable-browser-credential', 'GET', '/api/integration/waiting')).items.length, 0);
 console.log('PASS waiting for a repository in Individual, wrong folder refused, linking imports the ticket');
 await switchTo('team:main');
 await page.locator('[data-page=mywork]').click(); await page.getByRole('button', { name: new RegExp(ticket.key + ' Improve sign-in') }).click(); current = 'team:main';
 await firstFrame.getByRole('button', { name: 'Agent controls', exact: true }).click();
 await firstFrame.getByRole('button', { name: 'Review execution policy', exact: true }).click(); await firstFrame.getByRole('button', { name: 'Approve and start my run', exact: true }).click();
 await firstFrame.getByRole('button', { name: 'Stop my run', exact: true }).waitFor();
 const projects = (await api(personalURL, full, 'GET', '/api/projects')).projects; const localProject = projects.find(p => p.name === 'Customer portal');
 let active; await wait(async () => { const runs = (await api(personalURL, full, 'GET', `/api/projects/${localProject.id}/runs`)).runs; active = runs.find(r => ['running', 'waiting_for_user'].includes(r.state)); return !!active; }, 'agent process running');
 console.log('PASS shell opening, Team ticket deep link and locally approved agent process');
 await page.locator('[data-page=mywork]').click(); // the open ticket covers Team's rail; switch from a shell page, leaving it open
 await switchTo('personal'); await switchTo('team:' + slot);
 const secondFrame = page.frameLocator('iframe[data-workspace="team:' + slot + '"]'); await secondFrame.locator('nav').getByRole('button', { name: 'Settings', exact: true }).click();
 assert.equal(await secondFrame.getByRole('button', { name: 'Make Workspace Host', exact: true }).count(), 0, 'member has no host authority');
 await secondFrame.locator('[name=offer-workspace-host]').check(); await secondFrame.locator('[name=computer-sleeps]').check(); await secondFrame.getByRole('button', { name: 'Save settings', exact: true }).click();
 await wait(async () => (await secondDev('GET', '/state')).settings.offerWorkspaceHost, 'volunteer persisted');
 await switchTo('team:main'); await firstFrame.getByRole('button', { name: 'Stop my run', exact: true }).waitFor();
 assert.equal((await api(personalURL, full, 'GET', `/api/projects/${localProject.id}/runs/${active.id}`)).state, active.state, 'workspace switching leaves execution alive');
 const artifacts = process.env.BROWSER_ARTIFACT_DIR || path.join(root, '.impeccable/review'); fs.mkdirSync(artifacts, { recursive: true });
 await page.locator('[data-page=mywork]').click(); await page.getByRole('button', { name: /Review the customer portal/ }).waitFor(); await page.screenshot({ path: path.join(artifacts, 'desktop.png'), fullPage: true });
 await page.setViewportSize({ width: 480, height: 844 }); await page.screenshot({ path: path.join(artifacts, 'mobile.png'), fullPage: true }); assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
 assert(await page.getByTestId('work-item').filter({ hasText: ticket.key + ' ' + ticket.title }).locator('.state').isVisible(), 'Team status visible at native minimum');
 await page.locator('[data-page=calendar]').click(); await page.getByTestId('scheduled-item').waitFor();
 await page.screenshot({ path: path.join(artifacts, 'calendar-mobile.png'), fullPage: true }); assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
 const titleWidth = await page.getByTestId('scheduled-item').locator('.title').evaluate(e => e.getBoundingClientRect().width); assert(titleWidth > 200, 'Calendar retains title width');
 await page.setViewportSize({ width: 1440, height: 1000 }); await page.screenshot({ path: path.join(artifacts, 'calendar-desktop.png'), fullPage: true });
 await page.setViewportSize({ width: 1440, height: 1000 }); await switchTo('team:main');
 await page.locator('[data-page=workspaces]').click();
 await page.getByRole('button', { name: 'Back up and adopt…', exact: true }).click();
 await page.getByText('Adoption verified.', { exact: false }).waitFor();
 assert.equal(fs.readFileSync(path.join(temp, 'legacy-personal', 'token'), 'utf8'), 'disposable-migration-credential');
 await page.screenshot({ path: path.join(artifacts, 'migration-desktop.png'), fullPage: true });
 await page.setViewportSize({ width: 480, height: 844 });
 await page.getByRole('heading', { name: 'Your existing installation' }).scrollIntoViewIfNeeded();
 await page.screenshot({ path: path.join(artifacts, 'migration-mobile.png'), fullPage: true });
 assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
 await page.getByRole('button', { name: 'Roll back adoption…', exact: true }).click();
 await page.getByText('Desktop adoption was rolled back.', { exact: false }).waitFor();
 assert.equal(JSON.parse(fs.readFileSync(path.join(temp, 'migration', 'migration.json'))).phase, 'rolled_back');
 await page.setViewportSize({ width: 1440, height: 1000 });
 console.log('PASS explicit migration, backup verification, rollback and component diagnostics in desktop/mobile shell');
 console.log('PASS persistent frames, member restrictions, hosting offer and background run through switching');
 await page.reload(); await page.locator('iframe[data-workspace="team:main"]').waitFor({ state: 'visible' }); await firstFrame.getByRole('button', { name: 'Agent controls', exact: true }).click(); await firstFrame.getByRole('button', { name: 'Stop my run', exact: true }).waitFor();
 await page.close(); assert.equal((await api(personalURL, full, 'GET', `/api/projects/${localProject.id}/runs/${active.id}`)).state, active.state, 'closing frontend leaves runner alive');
 const reopened = await context.newPage(); await reopened.goto(origin + '/shell/shell/index.html'); await reopened.locator('iframe[data-workspace="team:main"]').waitFor({ state: 'visible' });
 await api(personalURL, full, 'POST', `/api/projects/${localProject.id}/runs/${active.id}/stop`, {});
 // Leaving the second team removes only that membership and keeps Personal + first team.
 const state = await secondDev('GET', '/state'); const caps = await otherTeam('GET', '/devices'); const device = caps.find(d => d.id === state.deviceId); assert(!device.capabilities.includes('workspace_host'));
 await secondDev('POST', '/leave', { removeData: false, confirm: 'leave workspace' }); await wait(async () => !(await secondDev('GET', '/state')).enrolled, 'leave second workspace');
 await wait(async () => !(await reopened.locator('iframe[data-workspace="team:' + slot + '"]').count()), 'retired frame gone');
 assert((await dev('GET', '/state')).enrolled); assert((await api(personalURL, full, 'GET', '/api/projects')).projects.length);
 assert.equal(errors.length, 0, errors.join('\n')); console.log('PASS unified desktop: real multi-team membership, permissions, explicit grant, Team agent controls, workspace/project routes, retained frames, restart, background survival, volunteering and isolated leave');
})().catch(async e => { if (testPage && !testPage.isClosed()) { console.error('Shell alerts: ' + await testPage.locator('[role=alert]').allTextContents()); console.error('Visible frame titles: ' + await testPage.locator('iframe:visible').evaluateAll(frames => frames.map(f => f.getAttribute('data-workspace')))); } console.error(e.stack); console.error('Fixture directory: ' + temp); for (const c of children) console.error(c.diagnostics.slice(-5000)); process.exitCode = 1; }).finally(async () => { await browser?.close(); fs.writeFileSync(path.join(temp, 'done'), 'done'); await delay(1000); for (const c of children) if (c.exitCode === null) c.kill('SIGTERM'); if (!process.exitCode) fs.rmSync(temp, { recursive: true, force: true }); });

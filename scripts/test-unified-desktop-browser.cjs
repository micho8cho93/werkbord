// The built Svelte shell + its real Go registry/relay + real Individual and Team services.
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
async function wait(fn, label, tenths = 900) { for (let i = 0; i < tenths; i++) { for (const c of children) if (c.exitCode !== null) throw Error(c.diagnostics); if (await fn()) return; await delay(100); } throw Error('Timed out: ' + label); }
async function api(base, token, method, url, body) { const r = await fetch(base + url, { method, headers: { Authorization: 'Bearer ' + token, ...(body ? { 'Content-Type': 'application/json' } : {}) }, body: body ? JSON.stringify(body) : undefined }); if (r.status === 204) return null; const d = await r.json(); if (!r.ok) throw Error(r.status + ' ' + JSON.stringify(d)); return d; }
(async () => {
 const origin = 'http://127.0.0.1:' + await port(), individualURL = 'http://127.0.0.1:' + await port();
 const env = { WERKBORD_UNIFIED_BROWSER_FIXTURE: temp, WERKBORD_BROWSER_SHELL_ORIGIN: origin, WERKBORD_BROWSER_PERSONAL: individualURL };
 const individualBinary = path.join(temp, 'personal'); run('go', ['build', '-o', individualBinary, './scripts/browser-fixture']);
 spawn(individualBinary, [], { ...env, WERKBORD_BROWSER_ADDR: new URL(individualURL).host, WERKBORD_BROWSER_EXECUTION: '1' });
 await wait(async () => { try { return (await fetch(individualURL + '/api/health')).ok; } catch { return false; } }, 'Individual');
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
 // Joining a second Team over the private network took longer than 90s on a loaded CI runner (4.72.0-preview.1), so it gets five minutes.
 await wait(async () => (await secondDev('GET', '/state')).connected, 'second workspace joined on same device', 3000);
 spawn('go', ['test', '-run', '^TestWorkspaceShellBrowserFixture$', '-count=1', '-timeout', '12m', './internal/shell'], env, path.join(root, 'desktop'));
 await wait(() => fs.existsSync(path.join(temp, 'shell-ready')), 'Go desktop shell');
 const repo = path.join(temp, 'repo'); fs.mkdirSync(repo); run('git', ['-C', repo, 'init', '-b', 'main']); run('git', ['-C', repo, '-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.test', '-c', 'core.hooksPath=/dev/null', 'commit', '--allow-empty', '-m', 'Initial fixture']); run('git', ['-C', repo, 'remote', 'add', 'origin', 'https://github.com/acme/shop']);
 const full = 'disposable-browser-credential'; await api(individualURL, full, 'POST', '/api/projects', { name: 'Customer portal', path: repo });
 const longRepo = path.join(temp, 'long-project'); fs.mkdirSync(longRepo); run('git', ['-C', longRepo, 'init', '-b', 'main']); run('git', ['-C', longRepo, '-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.test', '-c', 'core.hooksPath=/dev/null', 'commit', '--allow-empty', '-m', 'Long name fixture']);
 const longProjectName = 'Northern European Venue Operations and Logistics'; await api(individualURL, full, 'POST', '/api/projects', { name: longProjectName, path: longRepo });
 const project = await team('POST', '/projects', { name: 'Customer portal', repository: 'https://github.com/acme/shop' });
 const ticket = await team('POST', `/projects/${project.id}/tickets`, { title: 'Improve sign-in', status: 'available' }); await team('POST', `/projects/${project.id}/tickets/${ticket.id}/claim`, {});
 const scheduled = await team('POST', `/projects/${project.id}/tickets`, { title: 'Review the customer portal navigation', status: 'available' }); await team('POST', `/projects/${project.id}/tickets/${scheduled.id}/claim`, {});
 await team('PUT', `/projects/${project.id}/tickets/${scheduled.id}/schedule`, { at: new Date(Date.now() + 3600000).toISOString(), timezone: 'Europe/Madrid', missedPolicy: 'run_late', graceSeconds: 60, priority: 1 });
 browser = await chromium.launch({ headless: true }); const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
 await context.addInitScript(() => { if (window.parent !== window) return; window.runtime = { EventsOn: (event, cb) => { if (event === 'shell:go') window.__shellGo = cb; return () => {}; } }; window.go = { main: { App: new Proxy({}, { get: (_, method) => async (...args) => { const r = await fetch('/fixture/call', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ Method: method, Args: args }) }); const d = await r.json(); if (!r.ok) throw Error(d.error); return d; } }) } }; });
 const page = await context.newPage(); testPage = page; page.setDefaultTimeout(90000); const errors = []; page.on('pageerror', e => errors.push(e.message));
 // Switching is the workspace name at the top of the window's one sidebar, which is also where every workspace is navigated.
 let current = 'personal';
 const switcher = async () => { await page.getByTestId('workspace-switcher').click(); await page.getByTestId('switcher-menu').waitFor(); };
 const switchUniversal = async () => { await switcher(); await page.getByTestId('switcher-menu').locator('[data-workspace="universal"]').click(); };
 const side = page.getByRole('navigation', { name: 'Werkbord', exact: true });
 const switchTo = async (id) => { await switcher(); await page.getByTestId('switcher-menu').locator('[data-workspace="' + id + '"]').click(); await page.locator('iframe[data-workspace="' + id + '"]').waitFor({ state: 'visible' }); current = id; };
 const setTheme = async theme => {
   const size = page.viewportSize();
   await page.setViewportSize({ width: 1440, height: 1000 });
   const active = side.locator('[aria-current=page]').first();
   const place = await active.getAttribute('data-page'), project = await active.getAttribute('data-project');
   await side.locator('[data-page=settings]').click();
   await page.getByTestId('settings-sections').getByRole('button', { name: 'Appearance', exact: true }).click();
   await page.getByRole('group', { name: 'Theme' }).getByRole('button', { name: theme === 'dark' ? 'Dark' : 'Light', exact: true }).click();
   if (project) await side.locator('[data-project="' + project + '"]').click();
   else if (place && place !== 'settings') await side.locator('[data-page="' + place + '"]').click();
   if (size) await page.setViewportSize(size);
 };

 const assertUniversalStartup = async (window) => {
   await wait(async () => await window.locator('iframe[data-workspace=personal]').count() === 1, 'background Individual setup check');
   assert.equal(await window.locator('iframe[data-workspace]:visible').count(), 0, 'completed users start on Universal without displaying a workspace');
   assert.equal(await window.locator('iframe[data-workspace=personal]').isVisible(), false, 'Individual checks setup in a hidden frame');
   assert.equal(await window.locator('iframe[data-workspace^="team:"]').count(), 0, 'startup does not restore a Team workspace');
 };

 await page.goto(origin + '/shell/shell/index.html'); await page.getByRole('heading', { name: 'My Work', exact: true }).waitFor();
 await assertUniversalStartup(page);
 const shellSkip = page.getByRole('link', { name: 'Skip to main content', exact: true }); await page.keyboard.press('Tab'); assert.equal(await shellSkip.evaluate(el => el === document.activeElement), true, 'shell skip link is first in the page focus order');
 assert((await shellSkip.boundingBox()).y >= 0, 'shell skip link is visible when focused'); await page.keyboard.press('Enter');
 assert.equal(await page.locator('#shell-main').evaluate(el => el === document.activeElement), true, 'shell skip link moves focus to its main work region');
 await switchTo('personal');
 for (const section of ['Overview', 'Board', 'Git', 'Runs']) await page.frameLocator('iframe[data-workspace=personal]').getByText(section, { exact: true }).first().waitFor();
 // One sidebar, no second one: Individual's own rail is not shown in the window, and its project sections stay in its header.
 assert.equal(await page.frameLocator('iframe[data-workspace=personal]').locator('aside.rail').count(), 0, 'Individual shows no rail of its own in the window');
 await side.locator('[data-project][aria-current=page]').waitFor(); // the sidebar marks the project Individual is showing
 for (const entry of ['Workspace', 'Projects', 'Customer portal', 'Settings', 'Devices & Hosting']) await side.getByRole('button', { name: entry }).first().waitFor();
 assert.equal(await page.getByText('Live', { exact: true }).count(), 0, 'no live status indicator in the sidebar');
 assert.equal(await page.frameLocator('iframe[data-workspace=personal]').getByText('Live', { exact: true }).count(), 0, 'no live status indicator in Individual');
 console.log('PASS Individual: one sidebar for the window, project sections in its header, no status indicator');
 // Cross-workspace navigation is shown in Universal mode, not while an individual workspace is open.
 await switchUniversal();
 for (const entry of ['My Work', 'Calendar']) await side.getByRole('button', { name: entry, exact: true }).waitFor();
 await side.locator('[data-page="inbox"]').waitFor(); // the accessible name also includes its waiting-item count
 console.log('PASS Universal: cross-workspace destinations appear in the shared sidebar');
 await switchTo('personal');
 // At intermediate widths the icon rail has a keyboard- and pointer-discoverable way to reveal names.
 await page.setViewportSize({ width: 1024, height: 768 });
 const railToggle = side.getByTestId('sidebar-label-toggle');
 await railToggle.waitFor();
 await page.waitForFunction(() => matchMedia('(max-width: 1107px)').matches && document.querySelector('[data-testid="sidebar-label-toggle"]')?.getAttribute('aria-label') === 'Show sidebar labels');
 assert.equal(await railToggle.getAttribute('aria-label'), 'Show sidebar labels');
 assert.equal(await railToggle.getAttribute('aria-expanded'), 'false');
 await railToggle.focus(); await page.keyboard.press('Enter');
 await page.getByRole('button', { name: 'Hide sidebar labels', exact: true }).waitFor();
 assert.equal(await railToggle.getAttribute('aria-expanded'), 'true', 'keyboard activation reveals the section labels');
 const longProjectLink = side.locator('[data-project]').filter({ hasText: longProjectName });
 await longProjectLink.waitFor();
 assert.equal(await longProjectLink.getAttribute('title'), longProjectName, 'a long project name remains available in the expanded rail');
 assert.equal(await side.evaluate(el => el.scrollWidth <= el.clientWidth), true, 'the expanded shell rail does not overflow horizontally');
 const shellReview = path.join(root, '.impeccable/review/workstream-4/shell'); fs.mkdirSync(shellReview, { recursive: true });
 for (const themeName of ['light', 'dark']) {
   await page.evaluate(value => document.documentElement.dataset.theme = value, themeName);
   await page.screenshot({ path: path.join(shellReview, `rail-expanded-1024-${themeName}.png`) });
 }
 await railToggle.click();
 await page.getByRole('button', { name: 'Show sidebar labels', exact: true }).waitFor();
 assert.equal(await railToggle.getAttribute('aria-expanded'), 'false', 'pointer activation collapses the labeled rail');
 console.log('PASS shell rail expands its accessible labels and long project name by keyboard or pointer at 1024px');
 await page.setViewportSize({ width: 1440, height: 1000 });
 // In workspace mode the sidebar shows workspace destinations; native Universal destinations remain routable.
 const many = await api(origin, '', 'POST', '/fixture/call', { Method: 'Workspaces', Args: [] });
 await page.route('**/fixture/call', async route => { try {
   const body = route.request().postDataJSON();
   if (body.Method === 'Workspaces') return await route.fulfill({ json: { ...many, items: many.items.filter(w => w.id === 'personal') } });
   if (body.Method === 'Overview') {
     const overview = await api(origin, '', 'POST', '/fixture/call', body);
     overview.entries = overview.entries.filter(e => e.workspace.id === 'personal');
     overview.entries[0].summary.attention.push({ id: 'one-question', kind: 'needs_input', severity: 'warning', title: 'A question', href: '#/control' });
     return await route.fulfill({ json: overview });
   }
   return await route.continue();
 } catch (e) { if (!/already handled|closed/i.test(String(e))) throw e; } });
 await wait(async () => (await side.locator('[data-page=ws-home]').innerText()).includes('1'), 'Workspace retains its count');
 assert.equal(await side.locator('[data-page=mywork], [data-page=calendar]').count(), 0, 'workspace mode keeps cross-workspace destinations out of the sidebar');
 for (const destination of ['mywork', 'calendar', 'inbox']) {
   await page.evaluate(destination => window.__shellGo(destination), destination);
   await page.getByRole('heading', { name: destination === 'mywork' ? 'My Work' : destination === 'calendar' ? 'Calendar' : 'Workspaces', exact: true }).waitFor();
 }
 await page.unrouteAll({ behavior: 'ignoreErrors' });
 await switcher();
 const restoredSwitcher = page.getByTestId('switcher-menu');
 await restoredSwitcher.locator('[data-workspace="team:main"]').waitFor();
 await restoredSwitcher.locator('[data-workspace="personal"]').click();
 await page.locator('iframe[data-workspace="personal"]').waitFor({ state: 'visible' });
 // The theme is chosen in Settings and reaches the workspace that is open.
 assert.equal(await side.getByRole('button', { name: /(light|dark) theme/i }).count(), 0);
 const themeOf = id => page.frameLocator('iframe[data-workspace="' + id + '"]').locator('html').getAttribute('data-theme');
 await setTheme('dark'); const chosen = await page.locator('html').getAttribute('data-theme'); assert(chosen === 'light' || chosen === 'dark');
 await wait(async () => (await themeOf('personal')) === chosen, 'theme reaches Individual');
 await switchTo('team:main'); const firstFrame = page.frameLocator('iframe[data-workspace="team:main"]');
 await wait(async () => (await themeOf('team:main')) === chosen, 'theme reaches Team');
 assert.equal(await firstFrame.locator('aside.rail').count(), 0, 'Team shows no rail of its own in the window');
 for (const entry of ['Workspace', 'Projects', 'Reviews', 'Members', 'Settings']) await side.getByRole('button', { name: entry, exact: true }).first().waitFor();
 // A project is opened from the sidebar; its own sections are tabs above it, and the sidebar marks it.
 await side.getByRole('button', { name: 'Customer portal' }).click();
 await firstFrame.getByRole('navigation', { name: 'Project sections' }).getByRole('button', { name: 'Git', exact: true }).waitFor();
 await side.locator('[data-project][aria-current=page]').waitFor();
 assert.equal(await firstFrame.locator('[name=project-switch]').count(), 0, 'the sidebar lists the projects, so the page has no second project list');
 await side.getByRole('button', { name: 'Reviews', exact: true }).click(); await side.locator('[data-page=ws-reviews][aria-current=page]').waitFor();
 console.log('PASS Team: the same sidebar lists its places, theme follows Settings, no rail of its own');
 // The same Settings, with the same Appearance, from Individual and from Universal; the theme is the window's and the sidebar follows it.
 const sections = page.getByTestId('settings-sections');
 await switchTo('personal'); await side.locator('[data-page=settings]').click(); await sections.waitFor();
 for (const label of ['Appearance', 'Alerts', 'Project history', 'Task defaults', 'Routing rules', 'Runners', 'Agents', 'This computer', 'Phones', 'GitHub', 'MCP services', 'Individual memory']) await sections.getByRole('button', { name: label, exact: true }).waitFor();
 assert.equal(await sections.getByRole('button', { name: 'Labels', exact: true }).count(), 0, 'Labels are Team\'s, not Individual\'s');
 await sections.getByRole('button', { name: 'Appearance', exact: true }).click();
 await page.getByRole('group', { name: 'Theme' }).getByRole('button', { name: 'Dark', exact: true }).click();
 assert.equal(await page.locator('html').getAttribute('data-theme'), 'dark', 'the sidebar follows the theme chosen in Settings');
 await page.getByRole('group', { name: 'Theme' }).getByRole('button', { name: 'Light', exact: true }).click();
 assert.equal(await page.locator('html').getAttribute('data-theme'), 'light');
 await sections.getByRole('button', { name: 'Phones', exact: true }).click();
 await page.frameLocator('iframe[data-hosted="personal"]').getByRole('button', { name: 'Add a phone', exact: true }).waitFor();
 assert.equal(await page.frameLocator('iframe[data-hosted="personal"]').locator('.sections').isVisible(), false, 'Individual\'s own list of sections is not shown inside the window\'s Settings');
 await switchUniversal(); await side.locator('[data-page=settings]').click(); await sections.waitFor();
 for (const owner of ['Individual', 'Northstar Studio']) await sections.locator('[data-owner="' + owner + '"]').waitFor();
 await page.getByTestId('help-button').click(); await page.frameLocator('iframe[data-hosted="personal"]').getByRole('heading', { name: 'Find the next step' }).waitFor();
 assert.equal(await page.getByTestId('help-button').count(), 0, 'Help does not offer itself while it is open');
 console.log('PASS Settings and Help: one screen from Universal, Individual and Team; theme follows Settings');
 // Settings is one screen of the window, whichever part of Werkbord it is opened from; Team's sections are shown by Team's own page.
 await switchTo('team:main'); await side.locator('[data-page=settings]').click();
 await sections.waitFor();
 for (const label of ['Appearance', 'Alerts', 'Labels', 'This computer', 'Workspace Hosts', 'Phones', 'MCP clients', 'Team memory', 'Backups', 'License']) await sections.getByRole('button', { name: label, exact: true }).waitFor();
 for (const label of ['Task defaults', 'Routing rules', 'Individual memory', 'GitHub']) assert.equal(await sections.getByRole('button', { name: label, exact: true }).count(), 0, label + ' is Individual\'s, not Team\'s');
 const hostedMain = page.frameLocator('iframe[data-hosted="team:main"]');
 await sections.getByRole('button', { name: 'This computer', exact: true }).click();
 assert.equal(await hostedMain.locator('.settings-sections').count(), 0, 'Team\'s own list of sections is not shown inside the window\'s Settings');
 await hostedMain.getByRole('button', { name: /^(Connect existing runner|Reconnect runner|Runner connected)$/ }).waitFor();
 const runnerConnected = (await dev('GET', '/state')).runner.connected;
 if (runnerConnected) assert(await hostedMain.getByRole('button', { name: 'Runner connected', exact: true }).isDisabled(), 'connected runner setup cannot be repeated');
 else await hostedMain.getByRole('button', { name: /^(Connect existing runner|Reconnect runner)$/ }).click();
 await wait(async () => (await dev('GET', '/state')).runner.connected, 'explicit narrow grant connection');
 // Claimed before the runner was connected: the Team service on this computer copies it into Individual, and the ticket opens there.
 await wait(async () => ((await dev('GET', '/state')).sync?.tickets || []).some(t => t.ticketId === ticket.id && t.taskId), 'claimed ticket copied into Individual');
 await switchUniversal(); await page.locator('[data-page=mywork]').click(); await page.getByRole('button', { name: new RegExp(ticket.key + ' Improve sign-in') }).click(); current = 'team:main';
 const teamTicket = firstFrame.locator('.ticket');
 await teamTicket.getByRole('heading', { name: new RegExp(ticket.key + ' · Improve sign-in') }).waitFor();
 await teamTicket.getByTestId('open-in-individual').click({ timeout: 5000 });
 await page.locator('iframe[data-workspace=personal]').waitFor({ state: 'visible' }); current = 'personal';
 await page.frameLocator('iframe[data-workspace=personal]').getByRole('heading', { name: ticket.key + ': Improve sign-in' }).waitFor();
 console.log('PASS claim in Team, task in Individual, Open in Werkbord switches to it');
 // A held ticket whose repository is not a project here waits for it in Individual's Needs you; linking a clone imports it.
 const billing = await team('POST', '/projects', { name: 'Billing', repository: 'https://github.com/acme/billing' });
 const invoice = await team('POST', `/projects/${billing.id}/tickets`, { title: 'Retry failed invoices', status: 'available' }); await team('POST', `/projects/${billing.id}/tickets/${invoice.id}/claim`, {});
 await wait(async () => (await api(individualURL, 'disposable-browser-credential', 'GET', '/api/integration/waiting')).items.length === 1, 'ticket waiting for its repository');
 await page.frameLocator('iframe[data-workspace=personal]').getByRole('link', { name: 'Close panel', exact: true }).click();
 await side.locator('[data-page=ws-home]').click();
 await page.frameLocator('iframe[data-workspace=personal]').getByTestId('waiting-repository').filter({ hasText: invoice.key }).waitFor();
 const billingRepo = path.join(temp, 'billing'); fs.mkdirSync(billingRepo); run('git', ['-C', billingRepo, 'init', '-b', 'main']); run('git', ['-C', billingRepo, '-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.test', '-c', 'core.hooksPath=/dev/null', 'commit', '--allow-empty', '-m', 'Initial fixture']); run('git', ['-C', billingRepo, 'remote', 'add', 'origin', 'git@github.com:acme/billing.git']);
 await assert.rejects(api(individualURL, 'disposable-browser-credential', 'POST', '/api/integration/waiting/link', { path: repo, repository: 'https://github.com/acme/billing' }), /not a clone/);
 const linked = await api(individualURL, 'disposable-browser-credential', 'POST', '/api/integration/waiting/link', { path: billingRepo, repository: 'https://github.com/acme/billing' });
 await wait(async () => (await api(individualURL, 'disposable-browser-credential', 'GET', `/api/projects/${linked.id}/tasks`)).tasks.some(t => t.title.startsWith(invoice.key)), 'linked repository imports the waiting ticket');
 assert.equal((await api(individualURL, 'disposable-browser-credential', 'GET', '/api/integration/waiting')).items.length, 0);
 console.log('PASS waiting for a repository in Individual, wrong folder refused, linking imports the ticket');
 await switchTo('team:main');
 await switchUniversal(); await page.locator('[data-page=mywork]').click(); await page.getByRole('button', { name: new RegExp(ticket.key + ' Improve sign-in') }).click(); current = 'team:main';
 await firstFrame.getByRole('button', { name: 'Review and start agent', exact: true }).click();
 await firstFrame.getByRole('region', { name: 'Your private agent', exact: true }).getByRole('button', { name: 'Start agent', exact: true }).click();
 await firstFrame.getByRole('button', { name: 'Stop my run', exact: true }).waitFor();
 const projects = (await api(individualURL, full, 'GET', '/api/projects')).projects; const localProject = projects.find(p => p.name === 'Customer portal');
 let active; await wait(async () => { const runs = (await api(individualURL, full, 'GET', `/api/projects/${localProject.id}/runs`)).runs; active = runs.find(r => ['running', 'waiting_for_user'].includes(r.state)); return !!active; }, 'agent process running');
 console.log('PASS shell opening, Team ticket deep link and locally approved agent process');
 await switchUniversal(); await page.locator('[data-page=mywork]').click(); // leave the open ticket as it is; switch from a shell page
 await switchTo('personal'); await switchTo('team:' + slot);
 const secondFrame = page.frameLocator('iframe[data-hosted="team:' + slot + '"]'); await side.locator('[data-page=settings]').click(); await page.locator('[data-section="team:' + slot + '/computer"]').click();
 assert.equal(await secondFrame.getByRole('button', { name: 'Make Workspace Host', exact: true }).count(), 0, 'member has no host authority');
 await secondFrame.locator('[name=offer-workspace-host]').check(); await secondFrame.locator('[name=computer-sleeps]').check(); await secondFrame.getByRole('button', { name: 'Save settings', exact: true }).click();
 await wait(async () => (await secondDev('GET', '/state')).settings.offerWorkspaceHost, 'volunteer persisted');
 await switchTo('team:main'); await firstFrame.getByRole('button', { name: 'Stop my run', exact: true }).waitFor();
 assert.equal((await api(individualURL, full, 'GET', `/api/projects/${localProject.id}/runs/${active.id}`)).state, active.state, 'workspace switching leaves execution alive');
 const artifacts = process.env.BROWSER_ARTIFACT_DIR || path.join(root, '.impeccable/review'); fs.mkdirSync(artifacts, { recursive: true });
 const importedTask = (await api(individualURL, full, 'GET', `/api/projects/${localProject.id}/tasks`)).tasks.find(t => t.id === active.taskId);
 assert(importedTask, 'the execution belongs to an imported local task');
 await switchUniversal(); await page.locator('[data-page=mywork]').click(); await page.getByRole('button', { name: /Review the customer portal/ }).waitFor();
 const individualWork = page.getByTestId('work-item').filter({ hasText: importedTask.title });
 await individualWork.getByText('Reported: Waiting for your next message', { exact: true }).waitFor();
 await page.setViewportSize({ width: 1440, height: 900 });
 const reportBounds = await individualWork.locator('.badge').evaluate(el => {
   const range = document.createRange(); range.selectNodeContents(el);
   const text = range.getBoundingClientRect(), cell = el.closest('.report').getBoundingClientRect();
   return { textRight: text.right, cellRight: cell.right, textLeft: text.left, cellLeft: cell.left };
 });
 assert(reportBounds.textRight <= reportBounds.cellRight + 1 && reportBounds.textLeft >= reportBounds.cellLeft - 1, 'reported execution text stays inside its My Work cell');
 await switchUniversal(); await page.locator('[data-page=inbox]').click();
 const idleAction = page.getByRole('button', { name: 'Review turn: ' + importedTask.title + ' is waiting for your next message', exact: true });
 await idleAction.waitFor();
 await api(individualURL, full, 'POST', `/api/projects/${localProject.id}/runs/${active.id}/input`, { text: 'ask' });
 const answer = page.getByRole('button', { name: 'Answer', exact: true });
 await answer.waitFor();
 const collapsedQuestionTitle = page.getByText('An agent is asking: Commit the fixture work?', { exact: true });
 await collapsedQuestionTitle.waitFor();
 assert.equal(await idleAction.count(), 0, 'an unanswered question replaces idle attention');
 const capture = async name => {
   for (const theme of ['light', 'dark']) {
     if (await page.locator('html').getAttribute('data-theme') !== theme) await setTheme(theme);
     for (const [size, width, height] of [['desktop', 1440, 900], ['mobile', 375, 812]]) {
       await page.setViewportSize({ width, height });
       assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), name + ' has no horizontal overflow');
       await page.screenshot({ path: path.join(artifacts, `${name}-${size}-${theme}.png`), fullPage: true });
     }
   }
 };
 await capture('attention');
 await answer.click();
 const inline = page.getByRole('region', { name: 'Answer: Commit the fixture work?', exact: true });
 await inline.waitFor();
 const questionRow = page.getByTestId('attention-item').filter({ hasText: 'Answering this question' });
 assert.equal(await page.getByText('Commit the fixture work?', { exact: true }).count(), 1, 'expanded shell Inbox states the question only in its answer card');
 assert(await questionRow.innerText().then(text => text.includes('Individual') && text.includes('Customer portal') && text.includes(importedTask.title)), 'expanded question keeps workspace, project and task provenance');
 assert.equal(await inline.locator(':focus').count(), 0, 'no choice is focused on expansion');
 await capture('attention-expanded');
 const realOverview = await api(origin, '', 'POST', '/fixture/call', { Method: 'Overview', Args: [] });
 await page.route('**/fixture/call', async route => {
   const body = route.request().postDataJSON();
   if (body.Method !== 'Overview') return route.continue();
   const stale = structuredClone(realOverview);
   const own = stale.entries.find(e => e.workspace.id === 'personal');
   own.error = 'Fixture offline'; own.summary.at = '2026-01-01T00:00:00Z';
   return route.fulfill({ json: stale });
 });
 await wait(async () => !(await inline.getByRole('button', {name:'Yes',exact:true}).isEnabled()), 'stale disables choices');
 assert(await inline.getByText(/Last reported:/).isVisible(), 'stale answer names last report');
 await page.unrouteAll({ behavior: 'ignoreErrors' });
 await wait(async () => await inline.getByRole('button', {name:'No',exact:true}).isEnabled(), 'reconnected answer enabled');
 await inline.getByRole('button', { name: 'No', exact: true }).click();
 await wait(async () => (await api(individualURL, full, 'GET', `/api/projects/${localProject.id}/runs/${active.id}`)).waiting === 'idle', 'inline answer reaches the runner');
 assert.equal(await inline.count(), 0, 'settled question closes its editor');
 await page.locator('[data-page=mywork]').click();
 await individualWork.getByText('Reported: Waiting for your next message', { exact: true }).waitFor();
 assert(await individualWork.getByText(/^State recorded /).isVisible(), 'source state timestamp is visible');
 await capture('mywork');
 console.log('PASS inline Individual answer, no focused choice, stale disabling, reconnect, delivery and responsive attention/My Work');
 assert(await page.getByTestId('work-item').filter({ hasText: ticket.key + ' ' + ticket.title }).locator('.state').isVisible(), 'Team status visible at native minimum');
 await page.setViewportSize({ width: 480, height: 844 });
 await page.locator('[data-page=calendar]').click();
 const calendarEvent = page.getByRole('button', { name: /Review the customer portal navigation/ }).first(); await calendarEvent.waitFor();
 await page.screenshot({ path: path.join(artifacts, 'calendar-mobile.png'), fullPage: true }); assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
 const eventWidth = await calendarEvent.evaluate(e => e.getBoundingClientRect().width); assert(eventWidth > 100, 'Calendar keeps a readable event target');
 await page.setViewportSize({ width: 1440, height: 1000 }); await page.screenshot({ path: path.join(artifacts, 'calendar-desktop.png'), fullPage: true });
 await page.setViewportSize({ width: 1440, height: 1000 });
 // Universal dates and the contextual editor save through actual device-service frames, not the native relay.
 const day = new Date().toISOString().slice(0,10);
 const addDay = (d,n) => new Date(Date.parse(d+'T12:00Z')+n*86400000).toISOString().slice(0,10);
 const planProject = await api(individualURL, full, 'POST', '/api/projects', { name: 'Universal date gestures', kind: 'work' });
 const planTask = await api(individualURL, full, 'POST', `/api/projects/${planProject.id}/tasks`, { title: 'Plan release dates', plan: {start:day,end:addDay(day,2)} });
 const teamPlan = await team('POST', `/projects/${project.id}/tickets`, { title: 'Plan team release dates', status:'available', plan:{start:day,end:addDay(day,2)} });
 await page.getByRole('group',{name:'Calendar presentation'}).getByRole('button',{name:'Timeline',exact:true}).click();
 await page.getByRole('button',{name:'Week',exact:true}).click();
 for (const [workspace, id, read] of [['personal',planTask.id,async()=> (await api(individualURL,full,'GET',`/api/projects/${planProject.id}/tasks`)).tasks.find(t=>t.id===planTask.id)],['team:main',teamPlan.id,()=>team('GET',`/projects/${project.id}/tickets/${teamPlan.id}`)]]) {
   const bar = page.locator(`[data-plan-item="${workspace}/${id}/plan"]`);
   await bar.waitFor();
   const gesture = async (item, dx) => { await wait(async()=>await bar.getAttribute('aria-busy')!=='true','saved date gesture'); await item.scrollIntoViewIfNeeded(); await item.evaluate(el=>{const grid=el.closest('[data-plan-grid]');if(!grid)return;const r=el.getBoundingClientRect(),g=grid.getBoundingClientRect();if(r.right>g.right-100 || r.left<g.left+300)grid.scrollLeft+=(r.left+r.right)/2-(g.left+g.right+260)/2;}); const r=await item.boundingBox(); await page.mouse.move(r.x+r.width/2,r.y+r.height/2); await page.mouse.down(); await page.mouse.move(r.x+r.width/2+dx,r.y+r.height/2,{steps:12}); await page.mouse.up(); };
   let before=await read(); await gesture(bar,56); await wait(async()=> (await read()).plan.start===addDay(before.plan.start,1),'Universal '+workspace+' timeline move');
   before=await read(); await gesture(bar.locator('[data-plan-edge=end]'),56); await wait(async()=> (await read()).plan.end===addDay(before.plan.end,1),'Universal '+workspace+' resize');
   await wait(async()=>await bar.getAttribute('aria-busy')!=='true','calendar refresh');
   await bar.click(); const editor=page.getByRole('dialog',{name:'Edit task'}); await editor.locator('input[maxlength="200"]').waitFor();
   await editor.locator('input[maxlength="200"]').fill('Edited in Universal '+workspace); await editor.getByRole('button',{name:'Save changes',exact:true}).click();
   await wait(async()=> (await read()).title==='Edited in Universal '+workspace,'Universal '+workspace+' editor saves');
   // Persistence is acknowledged before the sheet's exit transition removes its DOM.
   // Require both the saved title above and actual editor removal, without assuming they share a frame.
   await editor.waitFor({ state: 'detached' });
   assert(await side.getByRole('button',{name:'Calendar',exact:true}).isVisible()); assert.equal(await editor.count(),0,'editor closes after acknowledged save');
 }
 // Scheduled events retain their time of day and still carry a shared request, distinct from planned dates.
 const beforeSchedule=(await team('GET',`/projects/${project.id}/schedules`)).find(s=>s.ticketId===scheduled.id);
 const scheduledBar=page.locator(`[data-plan-item="team:main/${beforeSchedule.id}/schedule"]`); await scheduledBar.waitFor();
 const bounds=await scheduledBar.boundingBox(); await page.mouse.move(bounds.x+bounds.width/2,bounds.y+bounds.height/2); await page.mouse.down(); await page.mouse.move(bounds.x+bounds.width/2+56,bounds.y+bounds.height/2,{steps:12}); await page.mouse.up();
 await wait(async()=> { const s=(await team('GET',`/projects/${project.id}/schedules`)).find(s=>s.ticketId===scheduled.id); return new Date(s.at).getDate() !== new Date(beforeSchedule.at).getDate(); },'Universal Team schedule reschedules');
 console.log('PASS Universal Individual and Team timeline moves, range resizing, contextual editor persistence and shared schedule rescheduling through real workspace services');
 await page.screenshot({path:path.join(artifacts,'universal-planning-desktop.png')});
 await switchTo('team:main');
 await page.locator('[data-page=workspaces]').click();
 await page.screenshot({ path: path.join(artifacts, 'workspaces-desktop.png'), fullPage: true });
 await page.setViewportSize({ width: 480, height: 844 });
 await page.screenshot({ path: path.join(artifacts, 'workspaces-mobile.png'), fullPage: true });
 assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
 await page.setViewportSize({ width: 1440, height: 1000 });
 console.log('PASS workspaces and devices page in desktop/mobile shell');
 console.log('PASS persistent frames, member restrictions, hosting offer and background run through switching');
 await page.reload(); await page.getByRole('heading', { name: 'My Work', exact: true }).waitFor(); await switchTo('team:main'); await firstFrame.getByRole('button', { name: 'Stop my run', exact: true }).waitFor();
 await page.close(); assert.equal((await api(individualURL, full, 'GET', `/api/projects/${localProject.id}/runs/${active.id}`)).state, active.state, 'closing frontend leaves runner alive');
 const reopened = await context.newPage(); await reopened.goto(origin + '/shell/shell/index.html'); await reopened.getByRole('heading', { name: 'My Work', exact: true }).waitFor();
 await assertUniversalStartup(reopened);
 await reopened.getByTestId('workspace-switcher').click(); await reopened.getByTestId('switcher-menu').locator('[data-workspace="team:main"]').click(); await reopened.locator('iframe[data-workspace="team:main"]').waitFor({ state: 'visible' });
 await api(individualURL, full, 'POST', `/api/projects/${localProject.id}/runs/${active.id}/stop`, {});
 // Leaving the second team removes only that membership and keeps Individual + first team.
 const state = await secondDev('GET', '/state'); const caps = await otherTeam('GET', '/devices'); const device = caps.find(d => d.id === state.deviceId); assert(!device.capabilities.includes('workspace_host'));
 await secondDev('POST', '/leave', { removeData: false, confirm: 'leave workspace' }); await wait(async () => !(await secondDev('GET', '/state')).enrolled, 'leave second workspace');
 await wait(async () => !(await reopened.locator('iframe[data-workspace="team:' + slot + '"]').count()), 'retired frame gone');
 assert((await dev('GET', '/state')).enrolled); assert((await api(individualURL, full, 'GET', '/api/projects')).projects.length);
 assert.equal(errors.length, 0, errors.join('\n')); console.log('PASS unified desktop: real multi-team membership, permissions, explicit grant, Team agent controls, workspace/project routes, retained frames, restart, background survival, volunteering and isolated leave');
})().catch(async e => { if (testPage && !testPage.isClosed()) { console.error('Shell alerts: ' + await testPage.locator('[role=alert]').allTextContents()); console.error('Visible frame titles: ' + await testPage.locator('iframe:visible').evaluateAll(frames => frames.map(f => f.getAttribute('data-workspace')))); } console.error(e.stack); console.error('Fixture directory: ' + temp); for (const c of children) console.error(c.diagnostics.slice(-5000)); process.exitCode = 1; }).finally(async () => { await browser?.close(); fs.writeFileSync(path.join(temp, 'done'), 'done'); await delay(1000); for (const c of children) if (c.exitCode === null) c.kill('SIGTERM'); if (!process.exitCode) fs.rmSync(temp, { recursive: true, force: true }); });

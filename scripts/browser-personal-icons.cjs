// Real controller + SQLite + built shell: semantic Lucide selection, no collisions,
// optimistic settings, and actual sidebar/header rendering at three window sizes.
// Run after make web web-embed web-shell: node scripts/browser-personal-icons.cjs
const fs = require('node:fs'), os = require('node:os'), path = require('node:path'), http = require('node:http');
const cp = require('node:child_process'), net = require('node:net'), assert = require('node:assert/strict');
const { once } = require('node:events');
const root = path.resolve(__dirname, '..');
const { chromium } = require(path.join(root, 'web/node_modules/playwright'));
const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'werkbord-library-icons-'));
const output = path.join(root, 'output/playwright/personal-icons');
const token = 'disposable-browser-credential';
let controller, server, browser;
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));
async function freePort() { const s = net.createServer(); s.listen(0, '127.0.0.1'); await once(s, 'listening'); const p = s.address().port; await new Promise(resolve => s.close(resolve)); return p; }
(async () => {
    fs.mkdirSync(output, { recursive: true });
    const shellPort = await freePort(), apiPort = await freePort();
    const shell = `http://127.0.0.1:${shellPort}`, base = `http://127.0.0.1:${apiPort}`;
    const binary = path.join(temp, 'fixture');
    cp.execFileSync('go', ['build', '-o', binary, './scripts/browser-fixture'], { cwd: root, stdio: 'inherit' });
    controller = cp.spawn(binary, [], { cwd: root, stdio: ['ignore', 'ignore', 'pipe'], env: { ...process.env, WERKBORD_BROWSER_ADDR: `127.0.0.1:${apiPort}`, WERKBORD_BROWSER_DIR: temp, WERKBORD_BROWSER_SHELL_ORIGIN: shell } });
    let log = ''; controller.stderr.on('data', data => log += data);
    for (let i = 0; i < 100; i++) {
        if (controller.exitCode !== null) throw Error(log);
        if (await fetch(base + '/api/health').then(r => r.ok).catch(() => false)) break;
        await sleep(100);
    }
    async function api(method, route, body, expected) {
        const response = await fetch(base + route, { method, headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body) });
        const raw = await response.text();
        if (expected) assert.equal(response.status, expected, route); else assert.ok(response.ok, route + ': ' + raw);
        return raw ? JSON.parse(raw) : null;
    }
    await api('POST', '/api/onboarding/complete', {});
    const names = ['Focus planner', 'Reading journal', 'Launch room', 'Research hub'];
    const workspaces = [];
    for (let i = 0; i < names.length; i++) workspaces.push(await api('POST', '/api/personal/v1/workspaces', {
        name: 'Demo · ' + names[i], icon: 'auto', uiSchemaVersion: 1, collectionIds: [], readBindings: [],
        visibility: { universal: true, individual: i < 2, teamWorkspaceIds: i < 2 ? [] : ['team:main'] },
        definition: { views: [], layout: { type: 'text', text: '[Demo] A workspace for ' + names[i].toLowerCase() + '.' } },
    }));
    assert.deepEqual(workspaces.map(w => w.icon), ['lucide-target', 'lucide-book-open', 'lucide-rocket', 'lucide-microscope']);
    const { id, revision, createdAt, updatedAt, ...duplicate } = workspaces[0]; void id; void revision; void createdAt; void updatedAt;
    await api('POST', '/api/personal/v1/workspaces', { ...duplicate, name: 'Duplicate explicit icon' }, 409);
    const hidden = await api('POST', '/api/personal/v1/workspaces', { ...duplicate, icon: 'auto', visibility: { universal: false, individual: false, teamWorkspaceIds: [] } });
    const occupied = new Set([...workspaces.map(w => w.icon), hidden.icon]);
    assert.equal(occupied.size, 5);
    const items = [{ id: 'personal', kind: 'personal', name: 'Individual', state: 'ready' }, { id: 'team:main', kind: 'team', name: 'Demo Team', state: 'ready', role: 'owner' }];
    const source = id => ({ id, kind: id === 'personal' ? 'personal' : 'team', url: base + '/#token=' + token, origin: base, place: '' });
    const calls = {
        Info: () => ({ version: 'fixture', platform: 'darwin' }),
        Workspaces: () => ({ items, selected: 'personal', problems: null, team: { state: 'ready' }, teamInstaller: false, invitation: false }),
        Overview: () => ({ entries: items.map(workspace => ({ workspace, summary: { schema: 'workspace-summary/v1', workspace, projects: [], work: [], attention: [], schedule: [], at: new Date().toISOString() } })), at: new Date().toISOString() }),
        LoadWorkspace: source, OpenWorkspace: source, RememberPlace: () => null, PendingInvitation: () => '',
        AssistantWorkspaces: () => [], Relay: () => calls.Info(),
        PersonalTeamProjects: () => ({ projects: [], at: new Date().toISOString() }),
        PersonalWorkspaceCall: async (method, route, body) => {
            const response = await fetch(base + route, { method, headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' }, body: body === null ? undefined : JSON.stringify(body) });
            const raw = await response.text(); return { status: response.status, body: raw ? JSON.parse(raw) : null };
        },
    };
    const dist = path.join(root, 'desktop/frontend/dist');
    const mime = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.woff2': 'font/woff2' };
    server = http.createServer((request, response) => {
        if (request.method === 'POST' && request.url === '/fixture/call') {
            let body = ''; request.on('data', part => body += part); request.on('end', async () => {
                try { const { method, args } = JSON.parse(body); if (!calls[method]) throw Error('Unknown native method: ' + method); response.setHeader('Content-Type', 'application/json'); response.end(JSON.stringify(await calls[method](...args) ?? null)); }
                catch (error) { response.statusCode = 500; response.end(JSON.stringify({ error: error.message })); }
            }); return;
        }
        const file = path.join(dist, new URL(request.url, shell).pathname);
        if (!file.startsWith(dist + path.sep) || !fs.existsSync(file) || !fs.statSync(file).isFile()) { response.statusCode = 404; response.end(); return; }
        response.setHeader('Content-Type', mime[path.extname(file)] || 'application/octet-stream'); response.end(fs.readFileSync(file));
    });
    server.listen(shellPort, '127.0.0.1'); await once(server, 'listening');
    browser = await chromium.launch({ headless: true });
    const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
    await page.addInitScript(() => {
        localStorage.setItem('werkbord.theme', 'light');
        if (window.parent === window) window.go = { main: { App: new Proxy({}, { get: (_, method) => async (...args) => {
            const r = await fetch('/fixture/call', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ method, args }) }); const data = await r.json(); if (!r.ok) throw Error(data.error); return data;
        } }) } };
    });
    const errors = [], remote = [];
    page.on('pageerror', error => errors.push(error.message));
    page.on('request', request => { if (!new URL(request.url()).hostname.match(/^(127\.0\.0\.1|localhost)$/)) remote.push(request.url()); });
    page.setDefaultTimeout(10000);
    await page.goto(shell + '/shell/shell/index.html');
    const group = page.locator('.group.generated');
    await group.getByRole('button', { name: workspaces[0].name, exact: true }).waitFor();
    assert.deepEqual((await group.locator('svg[data-workspace-icon]').evaluateAll(icons => icons.map(icon => icon.dataset.workspaceIcon))).sort(), [...occupied].filter(icon => icon !== hidden.icon).sort());
    await group.getByRole('button', { name: workspaces[0].name, exact: true }).click();
    await page.getByRole('heading', { name: workspaces[0].name, exact: true }).waitFor();
    for (const [label, width, height] of [['desktop', 1440, 1000], ['intermediate', 760, 1000], ['mobile', 390, 844]]) {
        await page.setViewportSize({ width, height });
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth - innerWidth), 0);
        await page.screenshot({ path: path.join(output, label + '.png'), fullPage: true });
    }
    await page.setViewportSize({ width: 1440, height: 1000 });
    await group.screenshot({ path: path.join(output, 'icon-preview.png') });
    await page.evaluate(() => document.documentElement.dataset.theme = 'dark');
    await page.screenshot({ path: path.join(output, 'dark.png'), fullPage: true });
    await page.evaluate(() => document.documentElement.dataset.theme = 'light');
    await page.getByLabel('Manage workspace', { exact: true }).click();
    await page.getByRole('button', { name: 'Name, icon & locations', exact: true }).click();
    await page.getByRole('button', { name: 'New icon', exact: true }).click();
    await page.waitForFunction(() => !document.querySelector('.icon-choice button').disabled);
    const chosen = await page.locator('.icon-choice svg').getAttribute('data-workspace-icon');
    assert.ok(!occupied.has(chosen), 'replacement preview reused another live icon');
    await page.screenshot({ path: path.join(output, 'settings.png'), fullPage: true });
    await page.getByRole('button', { name: 'Save changes', exact: true }).click();
    await page.locator('.management').waitFor({ state: 'detached' });
    assert.equal((await api('GET', '/api/personal/v1/workspaces/' + workspaces[0].id)).icon, chosen);
    assert.deepEqual(errors, []); assert.deepEqual(remote, []);
    console.log('PASS: four semantic library icons, cross-location/hidden uniqueness, collision refusal, exact replacement save, three widths, dark theme and no external requests');
    console.log('Screenshots: ' + output);
})().catch(error => { console.error(error); process.exitCode = 1; }).finally(async () => {
    if (browser) await browser.close();
    if (server) await new Promise(resolve => server.close(resolve));
    if (controller && controller.exitCode === null) { controller.kill('SIGTERM'); await Promise.race([once(controller, 'exit'), sleep(4000)]); }
    fs.rmSync(temp, { recursive: true, force: true });
});

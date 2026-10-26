// The productivity views inside the desktop window's own shell: a document written with real key presses, shortcuts that open
// the window's pages, and the files view. Real controller + SQLite + built shell.
// Run after make web web-embed web-shell: node scripts/browser-personal-productivity-shell.cjs
const fs = require('node:fs'), os = require('node:os'), path = require('node:path'), http = require('node:http');
const cp = require('node:child_process'), net = require('node:net'), assert = require('node:assert/strict');
const { once } = require('node:events');
const root = path.resolve(__dirname, '..');
const { chromium } = require(path.join(root, 'web/node_modules/playwright'));
const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'werkbord-productivity-shell-'));
const output = path.join(root, 'output/playwright/personal-productivity-shell');
const token = 'disposable-browser-credential';
let controller, server, browser, lastPage;
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
    const ok = text => console.log('PASS ' + text);
    const items = [{ id: 'personal', kind: 'personal', name: 'Individual', state: 'ready' }, { id: 'team:main', kind: 'team', name: 'Demo Team', state: 'ready', role: 'owner' }];
    const source = id => ({ id, kind: id === 'personal' ? 'personal' : 'team', url: base + '/#token=' + token, origin: base, place: '' });
    const calls = {
        Premium: () => ({ active: false, canCheck: false, buyUrl: '' }),
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
    lastPage = page;
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
    const field = (key, name, type, extra = {}) => ({ key, name, type, required: false, ...extra });
    const ops = ['create', 'update', 'delete'];
    const notes = await api('POST', '/api/personal/v1/collections', { name: 'Notes', schemaVersion: 1, fields: [field('title', 'Title', 'text', { required: true }), field('body', 'Body', 'longtext'), field('parent', 'Parent', 'text')] });
    const material = await api('POST', '/api/personal/v1/collections', { name: 'Material', schemaVersion: 1, fields: [field('title', 'Title', 'text', { required: true }), field('notes', 'Notes', 'longtext')] });
    const hub = await api('POST', '/api/personal/v1/workspaces', {
        name: 'Writing desk', icon: 'auto', uiSchemaVersion: 1, readBindings: [], visibility: { universal: true, individual: true, teamWorkspaceIds: [] },
        collectionIds: [notes.id, material.id],
        definition: {
            views: [
                { id: 'doc', name: 'Documents', kind: 'document', collectionId: notes.id, fields: ['title'], bodyField: 'body', operations: ops },
                { id: 'files', name: 'Material', kind: 'files', collectionId: material.id, fields: ['title'], bodyField: 'notes', operations: ops },
            ],
            layout: { type: 'stack', children: [
                { type: 'grid', columns: 4, children: ['home', 'projects', 'calendar', 'data-library'].map(target => ({ type: 'shortcut', target })) },
                { type: 'view', viewId: 'doc' }, { type: 'view', viewId: 'files' },
            ] },
        },
    });
    await page.goto(shell + '/shell/shell/index.html');
    await page.getByTestId('hub-workspace-personal').click();
    const side = page.locator('[data-section="personal"]');
    await side.getByRole('button', { name: 'Writing desk', exact: true }).click();
    await page.getByRole('heading', { name: 'Writing desk', exact: true }).waitFor();
    await page.getByRole('button', { name: 'Customize with Salie', exact: true }).waitFor();

    // A document, written in the window's own shell.
    const docs = page.getByRole('region', { name: 'Documents', exact: true });
    await docs.getByTestId('document-new').click();
    await docs.getByRole('textbox', { name: 'Title', exact: true }).fill('Shell note');
    await page.keyboard.press('Enter');
    await docs.getByRole('textbox', { name: 'Document text' }).click();
    await page.keyboard.type('## Heading');
    await page.keyboard.press('Enter');
    await page.keyboard.type('Plain and **strong** words.');
    const records = async cid => (await api('GET', `/api/personal/v1/collections/${cid}/records?limit=100`)).items || [];
    for (let i = 0; i < 100 && !(await records(notes.id))[0]?.values.body?.includes('strong'); i++) await sleep(100);
    assert.equal((await records(notes.id))[0].values.body, '## Heading\n\nPlain and **strong** words.');
    ok('a document is written and saved from the desktop shell');

    // Shortcuts open the window's own pages, from the same list in the window.
    assert.equal(await page.locator('[data-shortcut]').count(), 4);
    for (const target of ['home', 'projects', 'calendar', 'data-library']) assert.equal(await page.locator(`[data-shortcut="${target}"]`).isEnabled(), true, target);
    await page.locator('[data-shortcut="data-library"]').click();
    await page.getByRole('heading', { name: /Data library|Personal data library|Library/i }).first().waitFor();
    ok('the Data library shortcut opens the window’s library');
    await side.getByRole('button', { name: 'Writing desk', exact: true }).click();
    await page.locator('[data-shortcut="projects"]').click();
    await page.frameLocator('iframe[data-workspace=personal]').getByRole('heading', { name: 'Projects', exact: true }).waitFor();
    await page.locator('nav[aria-label="Werkbord"] [data-open=personal][aria-current=page]').waitFor();
    ok('the Projects shortcut opens Projects in the workspace on show');
    await side.getByRole('button', { name: 'Writing desk', exact: true }).click();
    await page.locator('[data-shortcut="calendar"]').click();
    await page.getByRole('heading', { name: 'Calendar', exact: true }).waitFor();
    ok('the Calendar shortcut opens the window’s calendar');

    // The file catalogue works here too: the files stay in the window's own storage.
    await page.getByTestId('back-to-universal').click();
    await page.getByTestId('hub-generated-' + hub.id).click();
    const files = page.getByRole('region', { name: 'Material', exact: true });
    await files.getByTestId('files-add').setInputFiles([{ name: 'Brochure.md', mimeType: 'text/markdown', buffer: Buffer.from('# Brochure\n\nSome **copy**.') }]);
    await files.locator('.stage strong', { hasText: 'copy' }).waitFor();
    ok('a file is added and read from the desktop shell');
    await page.screenshot({ path: path.join(output, 'shell.png'), fullPage: true });
    assert.deepEqual(errors, []); assert.deepEqual(remote, []);
    void hub;
    console.log('Screenshots: ' + output);
})().catch(async error => {
    console.error(error);
    if (lastPage) {
        console.error(await lastPage.locator('body').ariaSnapshot());
        await lastPage.screenshot({ path: path.join(output, 'failure.png'), fullPage: true });
    }
    process.exitCode = 1;
}).finally(async () => {
    if (browser) await browser.close();
    if (server) await new Promise(resolve => server.close(resolve));
    if (controller && controller.exitCode === null) { controller.kill('SIGTERM'); await Promise.race([once(controller, 'exit'), sleep(4000)]); }
    fs.rmSync(temp, { recursive: true, force: true });
});

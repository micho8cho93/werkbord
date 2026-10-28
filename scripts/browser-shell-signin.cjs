// A window whose Personal page is refused its sign-in (a token that could not be read or had gone stale when the page was opened): the built
// shell page, with a stand-in for the native app, in front of the real Individual page in a real frame.
//
// It checks that the page does not stop at a prompt for a token the person cannot find: it asks the app to sign it in again, the app hands
// over the page's address as it is now, and the page loads again at the place it was. And that when signing in again cannot help, the
// window says so in words, instead of a bare token field, and "Try again" works once the app can sign it in.
const fs = require('node:fs'), os = require('node:os'), path = require('node:path'), http = require('node:http');
const cp = require('node:child_process'), net = require('node:net');
const { once } = require('node:events');
const assert = require('node:assert/strict');
const root = path.resolve(__dirname, '..');
const dist = path.join(root, 'desktop', 'frontend', 'dist');
const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'werkbord-shell-signin-'));
const artifacts = process.env.BROWSER_ARTIFACT_DIR || path.join(temp, 'artifacts');
fs.mkdirSync(artifacts, { recursive: true });
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || path.join(root, 'web/node_modules/playwright'));
const token = 'disposable-browser-credential';
const children = [];
const sleep = ms => new Promise(r => setTimeout(r, ms));
async function freePort() { const s = net.createServer(); s.listen(0, '127.0.0.1'); await once(s, 'listening'); const n = s.address().port; await new Promise(r => s.close(r)); return n; }
async function api(base, method, p, data) {
    const res = await fetch(base + p, { method, headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' }, body: data ? JSON.stringify(data) : undefined });
    const raw = await res.text(); if (res.status >= 400) throw Error(method + ' ' + p + ' ' + res.status + ' ' + raw);
    return raw ? JSON.parse(raw) : null;
}

const BRIDGE = `if (window.parent === window && !window.go) window.go = { main: { App: new Proxy({}, { get: (_, method) => async (...args) => { const r = await fetch('/fixture/call', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ Method: method, Args: args }) }); const d = await r.json(); if (!r.ok) throw Error(d.error); return d; } }) } };`;

(async () => {
    const fixture = path.join(temp, 'fixture');
    cp.execFileSync('go', ['build', '-o', fixture, './scripts/browser-fixture'], { cwd: root, stdio: 'inherit' });
    const repo = path.join(temp, 'repo'); fs.mkdirSync(repo);
    cp.execFileSync('git', ['-C', repo, 'init', '-b', 'main'], { stdio: 'ignore' });
    cp.execFileSync('git', ['-C', repo, '-c', 'user.name=F', '-c', 'user.email=f@example.test', '-c', 'core.hooksPath=/dev/null', 'commit', '--allow-empty', '-m', 'init'], { stdio: 'ignore' });

    const shellPort = await freePort(), indPort = await freePort();
    const shellOrigin = 'http://127.0.0.1:' + shellPort, ind = 'http://127.0.0.1:' + indPort;
    const child = cp.spawn(fixture, [], { cwd: root, stdio: ['ignore', 'ignore', 'pipe'], env: { ...process.env, GIT_CONFIG_GLOBAL: '/dev/null', WERKBORD_BROWSER_ADDR: '127.0.0.1:' + indPort, WERKBORD_BROWSER_SHELL_ORIGIN: shellOrigin } });
    children.push(child);
    for (let i = 0; i < 100 && !(await fetch(ind + '/api/health').then(r => r.ok).catch(() => false)); i++) await sleep(100);
    await api(ind, 'POST', '/api/onboarding/complete', {});
    await api(ind, 'POST', '/api/projects', { path: repo, name: 'Window project' });

    // What the stand-in app hands out as the Personal page's token: the first answers are wrong, as when the token could not be read.
    let wrongAnswers = 1;
    let loads = 0;
    let unreadable = false;
    const items = [{ id: 'personal', kind: 'personal', name: 'Individual', state: 'ready', source: 'local' }];
    const target = () => {
        loads++;
        if (unreadable) throw new Error('this app could not read Werkbord’s access token: there is no token file at /nowhere/token');
        const t = wrongAnswers > 0 ? (wrongAnswers--, 'stale-token') : token;
        return { id: 'personal', kind: 'personal', url: ind + '/#token=' + t, origin: ind, place: '' };
    };
    const calls = {
        Premium: () => ({ active: false, canCheck: false, buyUrl: '' }),
        Info: () => ({ version: 'fixture', platform: 'darwin' }),
        Workspaces: () => ({ items, selected: 'personal', problems: null, team: { state: 'ready' }, teamInstaller: false, invitation: false }),
        Overview: () => ({ entries: [], at: new Date().toISOString() }),
        PersonalTeamProjects: () => ({ projects: [], at: new Date().toISOString() }),
        PendingInvitation: () => '', RememberPlace: () => null, OpenExternal: () => null,
        Relay: (_id, method) => method === 'Info' ? calls.Info() : null,
        LoadWorkspace: () => target(), OpenWorkspace: () => target(),
    };
    const types = { '.html': 'text/html; charset=utf-8', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml', '.woff2': 'font/woff2', '.woff': 'font/woff' };
    const server = http.createServer((req, res) => {
        const url = new URL(req.url, shellOrigin);
        if (req.method === 'POST' && url.pathname === '/fixture/call') {
            let body = ''; req.on('data', d => body += d); req.on('end', async () => {
                const { Method, Args } = JSON.parse(body || '{}');
                const f = calls[Method];
                res.setHeader('Content-Type', 'application/json');
                if (!f) { res.statusCode = 404; return res.end(JSON.stringify({ error: 'no ' + Method })); }
                try { res.end(JSON.stringify(await f(...(Args || [])) ?? null)); }
                catch (e) { res.statusCode = 500; res.end(JSON.stringify({ error: e instanceof Error ? e.message : String(e) })); }
            });
            return;
        }
        let file = path.join(dist, decodeURIComponent(url.pathname));
        if (!file.startsWith(dist)) { res.statusCode = 403; return res.end(); }
        if (fs.existsSync(file) && fs.statSync(file).isDirectory()) file = path.join(file, 'index.html');
        if (!fs.existsSync(file)) { res.statusCode = 404; return res.end('not found'); }
        let data = fs.readFileSync(file);
        res.setHeader('Content-Type', types[path.extname(file)] || 'application/octet-stream');
        if (url.pathname === '/shell/shell/index.html') data = Buffer.from(String(data).replace('<head>', '<head><script>' + BRIDGE + '</script>'));
        res.end(data);
    });
    server.listen(shellPort, '127.0.0.1'); await once(server, 'listening');

    const browser = await chromium.launch({ headless: true, ...(process.env.BROWSER_EXECUTABLE ? { executablePath: process.env.BROWSER_EXECUTABLE } : {}) });
    const errors = [];
    let current = null;
    async function open() {
        const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
        const page = await ctx.newPage(); current = page;
        page.setDefaultTimeout(20000);
        page.on('pageerror', e => errors.push(e.message));
        await page.goto(shellOrigin + '/shell/shell/index.html');
        await page.getByRole('heading', { name: 'Home', exact: true }).waitFor();
        await page.locator('nav[aria-label="Werkbord"] [data-open="personal"]').click();
        await page.locator('iframe[data-workspace=personal]').waitFor();
        return page;
    }
    try {
        // 1. The first sign-in is wrong; the window recovers by itself and never leaves the person at a token prompt.
        {
            wrongAnswers = 1; loads = 0;
            const page = await open();
            const frame = page.frameLocator('iframe[data-workspace=personal]');
            await frame.locator('main').waitFor();
            assert.equal(await frame.locator('#token').count(), 0, 'no token field is left on screen');
            assert.ok(loads >= 2, 'the app was asked for the page again (' + loads + ' loads)');
            assert.equal(await page.locator('iframe[data-workspace=personal]').count(), 1, 'one frame, not a second beside it');
            console.log('PASS a refused page is signed in again by the app, and opens');
            await page.context().close();
        }
        // 2. Signing in again cannot help: it says why, offers Try again, and Try again works once the app can sign it in.
        {
            wrongAnswers = 1000; loads = 0; unreadable = false;
            const page = await open();
            const frame = page.frameLocator('iframe[data-workspace=personal]');
            const failed = frame.getByTestId('sign-in-failed');
            await failed.waitFor({ timeout: 40000 });
            assert.match(await failed.innerText(), /could not sign this window in/);
            assert.match(await failed.innerText(), /did not help/, 'it says signing in again was tried');
            await frame.getByRole('button', { name: 'Try again' }).waitFor();
            await page.screenshot({ path: path.join(artifacts, 'shell-signin-failed.png') });
            unreadable = true;
            await frame.getByRole('button', { name: 'Try again' }).click();
            await frame.getByTestId('sign-in-failed').filter({ hasText: 'no token file' }).waitFor();
            console.log('PASS when it cannot be signed in again, the window says why');
            unreadable = false; wrongAnswers = 0;
            await frame.getByRole('button', { name: 'Try again' }).click();
            await frame.locator('main').waitFor();
            assert.equal(await frame.locator('#token').count(), 0);
            console.log('PASS Try again signs the window in once the app can');
            await page.context().close();
        }
        assert.deepEqual(errors, [], 'no page errors: ' + errors.join('\n'));
    } catch (e) {
        if (current) await current.screenshot({ path: path.join(artifacts, 'shell-signin-error.png') }).catch(() => {});
        throw e;
    } finally {
        await browser.close();
        console.log('Browser artifacts: ' + artifacts);
    }
})().catch(e => { console.error(e && e.stack || e); process.exitCode = 1; }).finally(async () => {
    for (const c of children) { c.kill('SIGTERM'); await Promise.race([once(c, 'exit'), sleep(4000)]); }
    process.exit();
});

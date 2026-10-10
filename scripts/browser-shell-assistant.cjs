// The assistant as the desktop window's sidebar offers it: the built shell page (web/shell, built into desktop/frontend/dist), with a
// stand-in for the native app answering its calls, in front of the real Individual page in a real frame, with the scripted-provider
// assistant behind it (scripts/browser-fixture/assistant.go). Team is a plain page here: the point is the window, not Team.
//
// It checks that the assistant opens out of the sidebar's own button across the frame boundary (the circle's centre is the
// button's, though the button is outside the page the surface lives in), that the workspace that was showing is put back when it
// closes and has lost nothing, that what waits for the person is shown on the sidebar button without the conversation ever
// crossing, that choosing somewhere else in the sidebar closes it, and that reduced motion is respected here too.
const fs = require('node:fs'), os = require('node:os'), path = require('node:path'), http = require('node:http');
const cp = require('node:child_process'), net = require('node:net');
const { once } = require('node:events');
const assert = require('node:assert/strict');
const root = path.resolve(__dirname, '..');
const dist = path.join(root, 'desktop', 'frontend', 'dist');
const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'werkbord-shell-assistant-'));
const artifacts = process.env.BROWSER_ARTIFACT_DIR || path.join(temp, 'artifacts');
fs.mkdirSync(artifacts, { recursive: true });
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || path.join(root, 'web/node_modules/playwright'));
const token = 'disposable-browser-credential';
const children = [];
const sleep = ms => new Promise(r => setTimeout(r, ms));
const ok = msg => console.log('PASS ' + msg);
async function freePort() { const s = net.createServer(); s.listen(0, '127.0.0.1'); await once(s, 'listening'); const n = s.address().port; await new Promise(r => s.close(r)); return n; }
async function api(base, method, p, data) {
    const res = await fetch(base + p, { method, headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' }, body: data ? JSON.stringify(data) : undefined });
    const raw = await res.text(); if (res.status >= 400) throw Error(method + ' ' + p + ' ' + res.status + ' ' + raw);
    return raw ? JSON.parse(raw) : null;
}

const BRIDGE = `if (window.parent === window && !window.go) window.go = { main: { App: new Proxy({}, { get: (_, method) => async (...args) => { const r = await fetch('/fixture/call', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ Method: method, Args: args }) }); const d = await r.json(); if (!r.ok) throw Error(d.error); return d; } }) } };
window.__frameMessages = []; window.addEventListener('message', e => { try { window.__frameMessages.push(JSON.stringify(e.data)); } catch (_) {} });`;

(async () => {
    const fixture = path.join(temp, 'fixture');
    cp.execFileSync('go', ['build', '-o', fixture, './scripts/browser-fixture'], { cwd: root, stdio: 'inherit' });
    const repo = path.join(temp, 'repo'); fs.mkdirSync(repo);
    cp.execFileSync('git', ['-C', repo, 'init', '-b', 'main'], { stdio: 'ignore' });
    cp.execFileSync('git', ['-C', repo, '-c', 'user.name=F', '-c', 'user.email=f@example.test', '-c', 'core.hooksPath=/dev/null', 'commit', '--allow-empty', '-m', 'init'], { stdio: 'ignore' });

    const shellPort = await freePort(), indPort = await freePort();
    const shellOrigin = 'http://127.0.0.1:' + shellPort, ind = 'http://127.0.0.1:' + indPort;
    const child = cp.spawn(fixture, [], { cwd: root, stdio: ['ignore', 'ignore', 'pipe'], env: { ...process.env, GIT_CONFIG_GLOBAL: '/dev/null', WERKBORD_BROWSER_ADDR: '127.0.0.1:' + indPort, WERKBORD_BROWSER_ASSISTANT: '1', WERKBORD_BROWSER_SHELL_ORIGIN: shellOrigin } });
    children.push(child);
    for (let i = 0; i < 100 && !(await fetch(ind + '/api/health').then(r => r.ok).catch(() => false)); i++) await sleep(100);
    await api(ind, 'POST', '/api/onboarding/complete', {});
    const project = await api(ind, 'POST', '/api/projects', { path: repo, name: 'Window project' });

    // The stand-in for the native app: what the shell asks, in the shapes internal/workspace defines.
    const entry = (id, kind, name) => ({ id, kind, name, state: 'ready' });
    const items = [{ ...entry('personal', 'personal', 'Individual'), source: 'local' }, { ...entry('team:main', 'team', 'Northstar'), source: 'team', role: 'owner' }];
    const summary = e => ({ schema: 'workspace-summary/v1', workspace: e, projects: e.kind === 'personal' ? [{ id: project.id, name: 'Window project', href: `#/p/${project.id}/board` }] : [], work: [], attention: [], schedule: [], at: new Date().toISOString() });
    const calls = {
        Info: () => ({ version: 'fixture', platform: 'darwin' }),
        Workspaces: () => ({ items, selected: 'personal', problems: null, team: { state: 'ready' }, teamInstaller: false, invitation: false }),
        Overview: () => ({ entries: items.map(i => ({ workspace: entry(i.id, i.kind, i.name), summary: summary(entry(i.id, i.kind, i.name)) })), at: new Date().toISOString() }),
        OpenWorkspace: id => id === 'personal' ? { id, kind: 'personal', url: ind + '/#token=' + token, origin: ind, place: '' } : { id, kind: 'team', url: shellOrigin + '/team-stub.html', origin: shellOrigin, place: '' },
        RememberPlace: () => null, Relay: () => null, PendingInvitation: () => '', OpenExternal: () => null,
    };
    const types = { '.html': 'text/html; charset=utf-8', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml', '.woff2': 'font/woff2', '.woff': 'font/woff' };
    const server = http.createServer((req, res) => {
        const url = new URL(req.url, shellOrigin);
        if (req.method === 'POST' && url.pathname === '/fixture/call') {
            let body = ''; req.on('data', d => body += d); req.on('end', () => {
                const { Method, Args } = JSON.parse(body || '{}');
                const f = calls[Method];
                res.setHeader('Content-Type', 'application/json');
                if (!f) { res.statusCode = 404; return res.end(JSON.stringify({ error: 'no ' + Method })); }
                res.end(JSON.stringify(f(...(Args || [])) ?? null));
            });
            return;
        }
        if (url.pathname === '/team-stub.html') {
            res.setHeader('Content-Type', 'text/html; charset=utf-8');
            return res.end('<!doctype html><meta charset=utf-8><title>Team</title><body style="font:14px system-ui;padding:24px"><h1>Northstar (a stand-in for Team)</h1><label>Draft <input id="draft" placeholder="a half-written note"></label><script>parent.postMessage({type:"werkbord.frame",event:"ready"},"*")</script>');
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
    async function open(opts = {}) {
        const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, ...opts });
        const page = await ctx.newPage(); current = page;
        page.setDefaultTimeout(15000);
        page.on('pageerror', e => errors.push(e.message));
        await page.goto(shellOrigin + '/shell/shell/index.html');
        await page.locator('iframe[data-workspace=personal]').waitFor();
        await page.frameLocator('iframe[data-workspace=personal]').locator('main').waitFor();
        return { ctx, page };
    }
    const sampleInFrame = (frame, until) => frame.evaluate(until => new Promise(resolve => {
        const out = []; const t0 = performance.now();
        const parse = c => { const m = /circle\(([\d.]+)px at (-?[\d.]+)px (-?[\d.]+)px\)/.exec(c || ''); return m ? { r: +m[1], x: +m[2], y: +m[3] } : null; };
        // A frame that is hidden (the shell puts the other workspace back once the assistant has closed) gets no animation frames.
        setTimeout(() => resolve(out), until + 100);
        const tick = () => {
            const el = document.querySelector('[data-testid="assistant"]');
            const cs = el && getComputedStyle(el);
            out.push(el ? { clip: cs.clipPath, circle: parse(cs.clipPath), anims: el.getAnimations().length, phase: el.dataset.phase } : { gone: true });
            performance.now() - t0 < until ? requestAnimationFrame(tick) : resolve(out);
        };
        requestAnimationFrame(tick);
    }), until);

    try {
        const { ctx, page } = await open();
        const side = page.getByRole('navigation', { name: 'Werkbord', exact: true });
        const button = side.getByRole('button', { name: /^Assistant/ });
        const personal = page.frameLocator('iframe[data-workspace=personal]');
        await button.waitFor();
        assert.equal(await button.getAttribute('aria-haspopup'), 'dialog');
        assert.equal(await button.getAttribute('aria-expanded'), 'false');
        await page.screenshot({ path: path.join(artifacts, 'shell-assistant-button.png') });
        ok('the sidebar has an Assistant button');

        // Go to a different workspace and leave something half-written there.
        await page.getByTestId('workspace-switcher').click();
        await page.getByTestId('switcher-menu').locator('[data-workspace="team:main"]').click();
        const teamFrame = page.frameLocator('iframe[data-workspace="team:main"]');
        await teamFrame.locator('#draft').fill('a half-written note');
        await page.locator('iframe[data-workspace="team:main"]').waitFor({ state: 'visible' });
        assert.equal(await page.locator('iframe[data-workspace=personal]').isVisible(), false);

        // Open it from the sidebar: it opens in Individual's page, out of the sidebar's button.
        const frameEl = await page.locator('iframe[data-workspace=personal]').elementHandle();
        const indFrame = await frameEl.contentFrame();
        const bb = await button.boundingBox();
        const sampling = sampleInFrame(indFrame, 1100).catch(() => []);
        await sleep(30);
        await button.click();
        const samples = await sampling;
        await personal.getByTestId('assistant').waitFor();
        const fb = await page.locator('iframe[data-workspace=personal]').boundingBox();
        const want = { x: bb.x + bb.width / 2 - fb.x, y: bb.y + bb.height / 2 - fb.y };
        const circles = samples.filter(s => s.circle);
        assert.ok(circles.length >= 6, 'it takes many frames: ' + circles.length);
        assert.ok(want.x < 0, 'the button is outside the page the surface lives in (' + want.x + ')');
        for (const c of circles) assert.ok(Math.abs(c.circle.x - want.x) < 2 && Math.abs(c.circle.y - want.y) < 2, `grows from the sidebar button (${want.x},${want.y}), not (${c.circle.x},${c.circle.y})`);
        for (let i = 1; i < circles.length; i++) assert.ok(circles[i].circle.r >= circles[i - 1].circle.r - 0.01);
        assert.ok(circles.at(-1).circle.r > fb.width * 0.9, 'it ends covering the page');
        assert.equal(await button.getAttribute('aria-expanded'), 'true');
        assert.equal(await page.locator('iframe[data-workspace=personal]').isVisible(), true, 'Individual was brought forward');
        await page.screenshot({ path: path.join(artifacts, 'shell-assistant-open.png') });
        ok('opens out of the sidebar’s own button, across the frame, from another workspace');

        // Close with the same button: it collapses back, and the workspace that was showing is put back.
        const closing = sampleInFrame(indFrame, 900).catch(() => []);
        await sleep(30);
        await button.click();
        const shrink = (await closing).filter(s => s.circle);
        assert.ok(shrink.length >= 3, 'closing takes frames: ' + shrink.length);
        for (const c of shrink) assert.ok(Math.abs(c.circle.x - want.x) < 2 && Math.abs(c.circle.y - want.y) < 2, 'collapses toward the button');
        await page.locator('iframe[data-workspace="team:main"]').waitFor({ state: 'visible' });
        assert.equal(await teamFrame.locator('#draft').inputValue(), 'a half-written note', 'the workspace you came from lost nothing');
        assert.equal(await page.evaluate(() => document.activeElement?.getAttribute('data-page')), 'assistant', 'focus is back on the button');
        assert.equal(await button.getAttribute('aria-expanded'), 'false');
        ok('closing collapses toward the button and puts you back where you were, untouched');

        // Something waits for the person: the button says so, and no word of the conversation crossed.
        await button.click();
        await personal.getByTestId('assistant').waitFor();
        await personal.getByRole('textbox', { name: 'Message to the assistant' }).fill(`add a ticket to ${project.id}`);
        await personal.getByRole('button', { name: 'Send', exact: true }).click();
        await personal.locator('section.confirm').waitFor();
        await personal.getByTestId('assistant-close').click(); // closed from inside the page, with its own button
        await page.locator('iframe[data-workspace="team:main"]').waitFor({ state: 'visible' });
        await page.waitForFunction(() => /1 change is waiting for you/.test(document.querySelector('[data-page=assistant]')?.getAttribute('aria-label') || ''));
        assert.equal(await button.locator('.num').innerText(), '1');
        const crossed = (await page.evaluate(() => window.__frameMessages)).filter(m => /assistant/.test(m));
        assert.ok(crossed.length > 0, 'the page did report');
        for (const m of crossed) assert.ok(!/add a ticket|Create the ticket|From the assistant/.test(m), 'no conversation crossed: ' + m);
        assert.ok(crossed.every(m => /^\{"type":"werkbord.frame","event":"assistant","open":(true|false),"waiting":\d+,"running":(true|false)\}$/.test(m)), 'only a count and two flags cross: ' + crossed.at(-1));
        await page.screenshot({ path: path.join(artifacts, 'shell-assistant-badge.png') });
        ok('a change waiting for you shows on the sidebar button; only a count and two flags ever cross to the shell');

        await button.click();
        await personal.locator('section.confirm[data-state="waiting"]').waitFor();
        ok('and is still waiting when it is opened again from another workspace');

        // Choosing somewhere else in the sidebar closes it, then goes there.
        await side.getByRole('button', { name: 'Control Center' }).click();
        await personal.getByTestId('assistant').waitFor({ state: 'detached' });
        assert.equal(await button.getAttribute('aria-expanded'), 'false');
        assert.equal(await page.locator('iframe[data-workspace=personal]').isVisible(), true);
        ok('choosing another place in the sidebar closes the assistant and goes there');

        // Another page of the shell while it is open.
        await button.click();
        await personal.getByTestId('assistant').waitFor();
        await side.getByRole('button', { name: 'My Work' }).click();
        await personal.getByTestId('assistant').waitFor({ state: 'detached' });
        await page.locator('[data-page=mywork][aria-current=page]').waitFor();
        ok('going to a page of the window itself closes it too');
        await ctx.close();

        // Reduced motion applies here as well.
        {
            const { ctx, page } = await open({ reducedMotion: 'reduce' });
            const button = page.getByRole('navigation', { name: 'Werkbord', exact: true }).getByRole('button', { name: /^Assistant/ });
            const frame = await (await page.locator('iframe[data-workspace=personal]').elementHandle()).contentFrame();
            const sampling = sampleInFrame(frame, 500).catch(() => []);
            await sleep(30);
            await button.click();
            const s = await sampling;
            assert.ok(s.some(x => !x.gone));
            assert.ok(s.every(x => x.gone || (x.clip === 'none' && x.anims === 0)), 'no movement under reduced motion');
            ok('reduced motion is respected from the sidebar too');
            await ctx.close();
        }

        assert.deepEqual(errors, [], 'no page errors: ' + errors.join('\n'));
        console.log('PASS the assistant from the desktop window’s sidebar');
    } catch (e) {
        if (current) await current.screenshot({ path: path.join(artifacts, 'shell-failure.png') }).catch(() => {});
        throw e;
    } finally {
        await browser.close();
        console.log('Browser artifacts: ' + artifacts);
    }
})().catch(e => { console.error(e && e.stack || e); process.exitCode = 1; }).finally(async () => {
    for (const c of children) { c.kill('SIGTERM'); await Promise.race([once(c, 'exit'), sleep(4000)]); }
    process.exit();
});

// The assistant as the desktop window's sidebar offers it: the built shell page (web/shell, built into desktop/frontend/dist), with a
// stand-in for the native app answering its calls, in front of the real Individual page in a real frame, with the scripted-provider
// assistant behind it (scripts/browser-fixture/assistant.go). Team is a plain page here: the point is the window, not Team.
//
// The assistant is a frame of its own, as large as the window and above everything in it, the sidebar included. It checks that the
// assistant opens out of the sidebar's own button and covers the whole window (the circle's centre is the button's, in the window's
// coordinates; it ends covering every pixel, the sidebar too), slowly enough to be seen; that whatever workspace was showing is
// still there, with a half-written note untouched, when it closes; that what waits for the person is shown on the sidebar button
// without the conversation ever crossing; that the window under it cannot be reached while it is open; and that reduced motion is
// respected here too.
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
    const teamTickets = [], workspaceRequests = new Map();
    const summary = e => ({ schema: 'workspace-summary/v1', workspace: e, projects: e.kind === 'personal' ? [{ id: project.id, name: 'Window project', href: `#/p/${project.id}/board` }] : [], work: [], attention: [], schedule: [], at: new Date().toISOString() });
    const calls = {
        Info: () => ({ version: 'fixture', platform: 'darwin' }),
        Workspaces: () => ({ items, selected: 'personal', problems: null, team: { state: 'ready' }, teamInstaller: false, invitation: false }),
        Overview: () => ({ entries: items.map(i => ({ workspace: entry(i.id, i.kind, i.name), summary: summary(entry(i.id, i.kind, i.name)) })), at: new Date().toISOString() }),
        LoadWorkspace: id => id === 'personal' ? { id, kind: 'personal', url: ind + '/#token=' + token, origin: ind, place: '' } : null,
        AssistantWorkspaces: () => [{ id: 'team:main', name: 'Northstar' }],
        AssistantWorkspaceCall: (session, request, proposal, id, method, args) => {
            assert.ok(session.startsWith('ast_'), 'the bridge is tied to an assistant session');
            assert.equal(id, 'team:main');
            const p = { id: 'p_portfolio', name: 'Personal Portfolio Website' };
            if (method === 'list_projects') return [p];
            if (method === 'get_project') { assert.equal(args.projectId, p.id); return p; }
            if (method === 'list_tickets') return teamTickets;
            if (method === 'get_overview') return [{ projectId: p.id, name: p.name, review: 0 }];
            assert.equal(method, 'create_ticket');
            assert.ok(proposal.startsWith('act_'), 'the bridge carries the approved proposal');
            assert.equal(args.projectId, p.id);
            if (workspaceRequests.has(request)) return workspaceRequests.get(request);
            if (args.title === 'Extract coding projects from GitHub') return { kind: 'werkbord_workspace_failure', code: 'permission_denied', message: 'The Team member no longer has permission to create tickets.' };
            const ticket = { id: 't_' + (teamTickets.length + 1), projectId: p.id, title: args.title, state: 'backlog', version: 1 };
            teamTickets.push(ticket); workspaceRequests.set(request, ticket); return ticket;
        },
        OpenWorkspace: id => id === 'personal' ? { id, kind: 'personal', url: ind + '/#token=' + token, origin: ind, place: '' } : { id, kind: 'team', url: shellOrigin + '/team-stub.html', origin: shellOrigin, place: '' },
        RememberPlace: () => null, Relay: (_id, method) => method === 'Info' ? calls.Info() : null, PendingInvitation: () => '', OpenExternal: () => null,
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
                try { res.end(JSON.stringify(f(...(Args || [])) ?? null)); }
                catch (e) { res.statusCode = 500; res.end(JSON.stringify({ error: e instanceof Error ? e.message : String(e) })); }
            });
            return;
        }
        if (url.pathname === '/team-stub.html') {
            res.setHeader('Content-Type', 'text/html; charset=utf-8');
            return res.end('<!doctype html><meta charset=utf-8><title>Team</title><body style="font:14px system-ui;padding:24px"><h1>Northstar (a stand-in for Team)</h1><label>Draft <input id="draft" placeholder="a half-written note"></label><script>window.__lastPlace="";addEventListener("message",e=>{if(e.data?.type==="werkbord.navigate")window.__lastPlace=e.data.href});parent.postMessage({type:"werkbord.frame",event:"ready"},"*")</script>');
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
        await ctx.addInitScript(() => {
            window.__speechStarts = 0;
            window.SpeechRecognition = class {
                start() {
                    window.__speechStarts++;
                    queueMicrotask(() => { this.onstart?.(); this.onerror?.({ error: 'no-speech' }); this.onend?.(); });
                }
                stop() { this.onend?.(); }
                abort() { this.onend?.(); }
            };
        });
        const page = await ctx.newPage(); current = page;
        page.setDefaultTimeout(15000);
        page.on('pageerror', e => errors.push(e.message));
        await page.goto(shellOrigin + '/shell/shell/index.html');
        await page.getByRole('heading', { name: 'My Work', exact: true }).waitFor();
        await page.getByTestId('workspace-switcher').click();
        await page.getByTestId('switcher-menu').locator('[data-workspace=personal]').click();
        await page.locator('iframe[data-workspace=personal]').waitFor();
        await page.frameLocator('iframe[data-workspace=personal]').locator('main').waitFor();
        return { ctx, page };
    }
    const sampleInFrame = (frame, until) => frame.evaluate(until => new Promise(resolve => {
        const out = []; const t0 = performance.now();
        const parse = c => { const m = /circle\(([\d.]+)px at (-?[\d.]+)px (-?[\d.]+)px\)/.exec(c || ''); return m ? { r: +m[1], x: +m[2], y: +m[3] } : null; };
        setTimeout(() => resolve(out), until + 100);
        const tick = () => {
            const el = document.querySelector('[data-testid="assistant"]');
            const cs = el && getComputedStyle(el);
            const edge = document.querySelector('.fx.edge');
            out.push(el ? { t: performance.now() - t0, clip: cs.clipPath, circle: parse(cs.clipPath), anims: el.getAnimations().length, phase: el.dataset.phase, edge: edge ? { r: parseFloat(getComputedStyle(edge).getPropertyValue('--r')), opacity: +getComputedStyle(edge).opacity } : null } : { gone: true, t: performance.now() - t0 });
            performance.now() - t0 < until ? requestAnimationFrame(tick) : resolve(out);
        };
        requestAnimationFrame(tick);
    }), until);

    try {
        const { ctx, page } = await open();
        const side = page.getByRole('navigation', { name: 'Werkbord', exact: true });
        const button = side.getByRole('button', { name: /^Salie/ });
        const surfaceEl = page.getByTestId('assistant-frame');
        const assistantFrame = page.frameLocator('[data-testid=assistant-frame]');
        await button.waitFor();
        assert.equal(await button.getAttribute('aria-haspopup'), 'dialog');
        assert.equal(await button.getAttribute('aria-expanded'), 'false');
        assert.equal(await surfaceEl.count(), 0, 'nothing is loaded for the assistant until it is opened');
        await page.screenshot({ path: path.join(artifacts, 'shell-assistant-button.png') });
        ok('the sidebar has a Salie button');

        // Go to a different workspace and leave something half-written there.
        await page.getByTestId('workspace-switcher').click();
        await page.getByTestId('switcher-menu').locator('[data-workspace="team:main"]').click();
        const teamFrame = page.frameLocator('iframe[data-workspace="team:main"]');
        await teamFrame.locator('#draft').fill('a half-written note');
        await page.locator('iframe[data-workspace="team:main"]').waitFor({ state: 'visible' });
        assert.equal(await page.locator('iframe[data-workspace=personal]').isVisible(), false);

        // Open it from the sidebar. The first time, its frame is made; it covers the whole window.
        const workspaceEl = page.locator('iframe[data-workspace="personal"]');
        await workspaceEl.evaluate(el => { el.style.width = '800px'; el.hidden = false; });
        const workspaceFrame = await (await workspaceEl.elementHandle()).contentFrame();
        await workspaceFrame.waitForSelector('.shell.framed', { state: 'attached' });
        assert.equal(await workspaceFrame.getByRole('navigation', { name: 'Primary', exact: true }).count(), 0, 'narrow embedded pages never render phone tabs');
        assert.equal(await workspaceFrame.locator('.shell.framed').evaluate(el => el.getBoundingClientRect().height === window.innerHeight), true, 'narrow embedded pages fit the viewport');
        await workspaceEl.evaluate(el => { el.style.width = ''; el.hidden = true; });
        const bb = await button.boundingBox();
        await button.click();
        await surfaceEl.waitFor({ state: 'attached' });
        const frameEl = await surfaceEl.elementHandle();
        const indFrame = await frameEl.contentFrame();
        await indFrame.waitForSelector('[data-testid="assistant"]');
        const samples = await sampleInFrame(indFrame, 1300).catch(() => []);
        const want = { x: bb.x + bb.width / 2, y: bb.y + bb.height / 2 };
        const fb = await surfaceEl.boundingBox();
        assert.deepEqual({ x: fb.x, y: fb.y, w: fb.width, h: fb.height }, { x: 0, y: 0, w: 1440, h: 900 }, 'the frame is the whole window');
        assert.equal(await surfaceEl.evaluate(el => getComputedStyle(el).visibility), 'visible');
        await assistantFrame.getByTestId('assistant').waitFor();
        const surfaceBounds = await assistantFrame.getByTestId('assistant').boundingBox();
        assert.deepEqual({ x: surfaceBounds.x, y: surfaceBounds.y, w: surfaceBounds.width, h: surfaceBounds.height }, { x: 0, y: 0, w: 1440, h: 900 }, 'the assistant content covers the whole frame without a phone navigation gap');
        const circles = samples.filter(s => s.circle);
        assert.ok(want.x < 260, 'the button is in the sidebar (' + want.x + ')');
        for (const c of circles) assert.ok(Math.abs(c.circle.x - want.x) < 2 && Math.abs(c.circle.y - want.y) < 2, `grows from the sidebar button (${want.x},${want.y}), not (${c.circle.x},${c.circle.y})`);
        for (let i = 1; i < circles.length; i++) assert.ok(circles[i].circle.r >= circles[i - 1].circle.r - 0.01);
        // It reaches the farthest corner, so the sidebar is under it too.
        const far = Math.hypot(Math.max(want.x, 1440 - want.x), Math.max(want.y, 900 - want.y));
        assert.ok(circles.at(-1).circle.r >= far - 1, 'it ends covering the whole window, the sidebar included');
        assert.ok(circles.length >= 25, 'it is slow enough to be seen: ' + circles.length + ' frames');
        const span = circles.at(-1).t - circles[0].t;
        assert.ok(circles.some(c => c.phase === 'opening') && span > 700, 'it takes most of a second: ' + Math.round(span) + ' ms');
        // The light on its edge follows the circle’s radius, then goes out.
        const lit = samples.filter(s => s.edge && s.circle && s.edge.opacity > 0.3);
        assert.ok(lit.length >= 8, 'the edge is lit for a good part of it: ' + lit.length);
        for (const s of lit) assert.ok(Math.abs(s.edge.r - s.circle.r) < s.circle.r * 0.1 + 6, `the light is on the edge (${s.edge.r} vs ${s.circle.r})`);
        assert.equal(await button.getAttribute('aria-expanded'), 'true');
        assert.equal(await page.locator('.shell').getAttribute('inert'), '', 'what is under it cannot be reached while it is open');
        assert.equal(await page.locator('iframe[data-workspace="team:main"]').isVisible(), true, 'the workspace that was showing was not touched');
        await page.screenshot({ path: path.join(artifacts, 'shell-assistant-open.png') });
        ok('opens out of the sidebar’s own button, slowly, over the whole window, sidebar included, from another workspace');

        // Apple's service can end immediately when system Dictation is off. No restart loop and no lost typed draft.
        const message = assistantFrame.getByRole('textbox', { name: 'Message to the assistant' });
        await message.fill('keep this draft');
        await assistantFrame.getByRole('button', { name: 'Dictate a message', exact: true }).click();
        await assistantFrame.getByTestId('assistant-dictation-notice').getByRole('button', { name: 'Use dictation', exact: true }).click();
        await assistantFrame.getByTestId('assistant-dictation-note').filter({ hasText: 'System Settings → Keyboard' }).waitFor();
        assert.equal(await message.inputValue(), 'keep this draft');
        assert.equal(await indFrame.evaluate(() => window.__speechStarts), 1, 'a failed Mac startup is not retried');
        assert.equal(await assistantFrame.getByRole('button', { name: 'Dictate a message', exact: true }).count(), 1, 'the microphone can be tried again');
        await message.fill('');
        ok('a failed Mac dictation startup points to Keyboard settings, stops once, and preserves the draft');

        // Close with the Close button: it collapses back toward the button, and the workspace that was showing has lost nothing.
        const closing = sampleInFrame(indFrame, 1000).catch(() => []);
        await sleep(30);
        await assistantFrame.getByTestId('assistant-close').click();
        const shrink = (await closing).filter(s => s.circle);
        assert.ok(shrink.length >= 15, 'closing takes frames: ' + shrink.length);
        for (const c of shrink) assert.ok(Math.abs(c.circle.x - want.x) < 2 && Math.abs(c.circle.y - want.y) < 2, 'collapses toward the button');
        await page.waitForFunction(() => getComputedStyle(document.querySelector('[data-testid=assistant-frame]')).visibility === 'hidden');
        assert.equal(await teamFrame.locator('#draft').inputValue(), 'a half-written note', 'the workspace you came from lost nothing');
        assert.equal(await page.evaluate(() => document.activeElement?.getAttribute('data-page')), 'assistant', 'focus is back on the button');
        assert.equal(await button.getAttribute('aria-expanded'), 'false');
        assert.equal(await page.locator('.shell').getAttribute('inert'), null);
        // And the window under it can be used again.
        await page.getByTestId('workspace-switcher').click();
        await page.getByTestId('switcher-menu').locator('[data-workspace="universal"]').click();
        await side.getByRole('button', { name: 'My Work' }).click();
        await page.locator('[data-page=mywork][aria-current=page]').waitFor();
        await page.getByTestId('workspace-switcher').click();
        await page.getByTestId('switcher-menu').locator('[data-workspace="team:main"]').click();
        await page.locator('iframe[data-workspace="team:main"]').waitFor({ state: 'visible' });
        ok('closing collapses toward the button and leaves the window as it was');

        // Something waits for the person: the button says so, and no word of the conversation crossed.
        await button.click();
        await assistantFrame.getByTestId('assistant').waitFor();
        await assistantFrame.getByRole('textbox', { name: 'Message to the assistant' }).fill('Create a single portfolio ticket for the Northstar workspace');
        await assistantFrame.getByRole('button', { name: 'Send', exact: true }).click();
        await assistantFrame.locator('section.confirm').waitFor();
        await assistantFrame.getByTestId('assistant-close').click(); // closed from inside the page, with its own button
        await page.waitForFunction(() => getComputedStyle(document.querySelector('[data-testid=assistant-frame]')).visibility === 'hidden');
        await page.waitForFunction(() => /1 change is waiting for you/.test(document.querySelector('[data-page=assistant]')?.getAttribute('aria-label') || ''));
        assert.equal(await button.locator('.num').innerText(), '1');
        const crossed = (await page.evaluate(() => window.__frameMessages)).filter(m => /assistant/.test(m));
        assert.ok(crossed.length > 0, 'the page did report');
        for (const m of crossed) assert.ok(!/add a ticket|Create the ticket|From the assistant/.test(m), 'no conversation crossed: ' + m);
        assert.ok(crossed.every(m => /^\{"type":"werkbord.frame","event":"assistant","open":(true|false),"waiting":\d+,"running":(true|false)\}$/.test(m)), 'only a count and two flags cross: ' + crossed.at(-1));
        await page.screenshot({ path: path.join(artifacts, 'shell-assistant-badge.png') });
        ok('a change waiting for you shows on the sidebar button; only a count and two flags ever cross to the shell');

        await button.click();
        await assistantFrame.locator('section.confirm[data-state="waiting"]').waitFor();
        ok('and is still waiting when it is opened again, from the same frame');
        await assistantFrame.locator('section.confirm[data-state="waiting"]').getByRole('button', { name: 'Not now', exact: true }).click();
        await assistantFrame.getByRole('textbox', { name: 'Message to the assistant' }).fill('Create a single portfolio ticket for the Northstar workspace');
        await assistantFrame.getByRole('button', { name: 'Send', exact: true }).click();
        const single = assistantFrame.locator('section.confirm[data-state="waiting"]');
        await single.waitFor();
        assert.equal(teamTickets.length, 0, 'the Team task is only proposed before approval');
        await single.getByRole('button', { name: 'Confirm', exact: true }).click();
        const added = assistantFrame.locator('section.confirm[data-state="confirmed"]');
        await added.waitFor();
        await added.getByText('Added to Backlog.').waitFor();
        assert.equal(teamTickets.length, 1, 'one confirmation creates the Team task');
        assert.equal(await assistantFrame.getByRole('dialog').filter({ has: assistantFrame.getByRole('heading', { name: 'Assistant' }) }).count(), 1, 'the assistant remains the only dialog; no second task confirmation opens');
        await added.getByRole('button', { name: 'Open task', exact: true }).click();
        await page.locator('iframe[data-workspace="team:main"]').waitFor({ state: 'visible' });
        await page.waitForFunction(() => document.querySelector('iframe[data-workspace="team:main"]')?.contentWindow?.__lastPlace === '?tab=board&project=p_portfolio&ticket=t_1');
        ok('one confirmation adds a Team task to Backlog and its success link opens that exact ticket');
        await assistantFrame.getByTestId('assistant-close').click();
        await page.waitForFunction(() => getComputedStyle(document.querySelector('[data-testid=assistant-frame]')).visibility === 'hidden');
        await button.click();
        await assistantFrame.getByTestId('assistant').waitFor();
        await assistantFrame.getByRole('textbox', { name: 'Message to the assistant' }).fill('Add portfolio tickets to the Northstar workspace');
        await assistantFrame.getByRole('button', { name: 'Send', exact: true }).click();
        const proposals = assistantFrame.locator('section.confirm[data-state="waiting"]');
        await proposals.nth(2).waitFor();
        assert.equal(teamTickets.length, 1, 'batch proposals change nothing before approval');
        assert.match(await proposals.first().innerText(), /Northstar/);
        // Three changes waiting: one press answers them all, each with its own request, in order.
        const all = assistantFrame.getByTestId('assistant-confirm-all');
        assert.match(await all.innerText(), /3 changes are waiting/);
        const reply = assistantFrame.locator('article.reply').last();
        await reply.getByText('Reply finished.', { exact: true }).waitFor();
        await reply.locator('.activity').waitFor({ state: 'detached' });
        await indFrame.waitForFunction(() => {
            const bar = document.querySelector('[data-testid="assistant-confirm-all"]');
            const log = bar?.closest('[role=log]');
            return log && bar.getBoundingClientRect().bottom <= log.getBoundingClientRect().bottom + 1;
        });
        const geo = await all.evaluate(el => { const r = el.getBoundingClientRect(), sc = el.closest('[role=log]'), box = sc.getBoundingClientRect(); return { barBottom: r.bottom, boxBottom: box.bottom, top: sc.scrollTop, height: sc.scrollHeight, client: sc.clientHeight }; });
        assert.ok(geo.barBottom <= geo.boxBottom + 1, 'the bar is fully in view, not hidden behind the box: ' + JSON.stringify(geo));
        await page.screenshot({ path: path.join(artifacts, 'shell-assistant-confirm-all.png') });
        await all.getByRole('button', { name: 'Confirm all 3' }).click();
        for (let j = 0; j < 200 && teamTickets.length !== 3; j++) await sleep(30);
        assert.equal(teamTickets.length, 3, 'the batch continues after one permission failure');
        await assistantFrame.getByText('2 added · 1 failed').waitFor();
        await assistantFrame.locator('section.confirm[data-state="failed"]').filter({ hasText: 'coding projects from GitHub' }).getByRole('alert').waitFor();
        assert.equal(await all.count(), 0, 'nothing is left to answer, so the bar is gone');
        assert.deepEqual(teamTickets.map(t => t.title), ['A single approved ticket', 'Extract LinkedIn and CV information', 'Create a website design system']);
        assert.equal((await api(ind, 'GET', `/api/projects/${project.id}/tasks`)).tasks.length, 0, 'Team approval never writes the Individual board');
        await page.screenshot({ path: path.join(artifacts, 'shell-assistant-team-tickets.png') });
        ok('Confirm all continues after a permission failure and reports two added, one failed, without changing the Individual board');
        await assistantFrame.getByTestId('assistant-close').click();
        await page.waitForFunction(() => getComputedStyle(document.querySelector('[data-testid=assistant-frame]')).visibility === 'hidden');

        // BROWSER_FILMSTRIP=1: stills of the opening, with the animation held at fixed moments, for a person to look at.
        if (process.env.BROWSER_FILMSTRIP) {
            await button.click();
            await assistantFrame.getByTestId('assistant').waitFor();
            for (const t of [60, 180, 320, 450, 580, 700, 820, 960, 1250]) {
                await indFrame.evaluate(t => document.getAnimations().forEach(a => { a.pause(); a.currentTime = t; }), t);
                await sleep(60);
                await page.screenshot({ path: path.join(artifacts, `film-${String(t).padStart(4, '0')}.png`) });
            }
            await indFrame.evaluate(() => document.getAnimations().forEach(a => a.play()));
            await sleep(900);
            await assistantFrame.getByTestId('assistant-close').click();
            await page.waitForFunction(() => getComputedStyle(document.querySelector('[data-testid=assistant-frame]')).visibility === 'hidden');
        }

        // The page of the Individual workspace has no assistant of its own in the window: there is exactly one conversation.
        await page.getByTestId('workspace-switcher').click();
        await page.getByTestId('switcher-menu').locator('[data-workspace="personal"]').click();
        const personal = page.frameLocator('iframe[data-workspace=personal]');
        await personal.locator('main').waitFor();
        assert.equal(await personal.getByTestId('assistant-button').count(), 0);
        assert.equal(await personal.getByTestId('assistant').count(), 0);
        ok('the workspace’s own page carries no second assistant');
        await ctx.close();

        // Reduced motion applies here as well.
        {
            const { ctx, page } = await open({ reducedMotion: 'reduce' });
            const button = page.getByRole('navigation', { name: 'Werkbord', exact: true }).getByRole('button', { name: /^Salie/ });
            await button.click();
            const frame = await (await page.getByTestId('assistant-frame').elementHandle()).contentFrame();
            const sampling = sampleInFrame(frame, 500).catch(() => []);
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

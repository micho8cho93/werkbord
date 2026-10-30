// Agents and Salie as the desktop window offers them: the built shell page (web/shell, built into desktop/frontend/dist), a stand-in
// for the native app, the real Individual page in a real frame, and the real controller with its runner and a scripted Salie behind
// them (scripts/browser-fixture). It checks the window's own wiring of the unified agent sessions:
//
//   - Salie starts an agent after the person confirms, and its card opens that session in the window's Agents destination;
//   - Agents is the person's own Werkbord at the session, in the frame that was already there, and Salie folds away without stopping;
//   - Return to Salie opens Salie on the conversation that started the session, and the sidebar's Agents comes back to the session;
//   - a task's Open Session in the Individual page opens the same session in Agents, and there is still one run.
const fs = require('node:fs'), os = require('node:os'), path = require('node:path'), http = require('node:http');
const cp = require('node:child_process'), net = require('node:net');
const { once } = require('node:events');
const assert = require('node:assert/strict');
const root = path.resolve(__dirname, '..');
const dist = path.join(root, 'desktop', 'frontend', 'dist');
const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'werkbord-shell-agents-'));
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
    const child = cp.spawn(fixture, [], { cwd: root, stdio: ['ignore', 'ignore', 'pipe'], env: { ...process.env, GIT_CONFIG_GLOBAL: '/dev/null', WERKBORD_BROWSER_ADDR: '127.0.0.1:' + indPort, WERKBORD_BROWSER_ASSISTANT: '1', WERKBORD_BROWSER_EXECUTION: '1', WERKBORD_BROWSER_SHELL_ORIGIN: shellOrigin } });
    children.push(child);
    for (let i = 0; i < 100 && !(await fetch(ind + '/api/health').then(r => r.ok).catch(() => false)); i++) await sleep(100);
    await api(ind, 'POST', '/api/onboarding/complete', {});
    const project = await api(ind, 'POST', '/api/projects', { path: repo, name: 'Window project' });
    await api(ind, 'POST', '/api/onboarding/tours/seen', { surface: 'desktop' }); // the first-visit tour is not what is tested here

    // The stand-in for the native app: what the shell asks, in the shapes internal/workspace defines.
    const entry = (id, kind, name) => ({ id, kind, name, state: 'ready' });
    const items = [{ ...entry('personal', 'personal', 'Individual'), source: 'local' }, { ...entry('team:main', 'team', 'Northstar'), source: 'team', role: 'owner' }];
    const teamTickets = [], workspaceRequests = new Map();
    const summary = e => ({ schema: 'workspace-summary/v1', workspace: e, projects: e.kind === 'personal' ? [{ id: project.id, name: 'Window project', href: `#/p/${project.id}/work` }] : [], work: [], attention: [], schedule: [], at: new Date().toISOString() });
    const calls = {
        Premium: () => ({ active: false, canCheck: false, buyUrl: '' }),
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
    let page;
    try {
        const task = await api(ind, 'POST', `/api/projects/${project.id}/tasks`, { title: 'Fix the login', execution: { agent: 'fixture', mode: 'manual' } });
        const runs = async () => (await api(ind, 'GET', `/api/projects/${project.id}/tasks/${task.id}/runs`)).runs ?? [];
        const sessions = async () => (await api(ind, 'GET', '/api/agent-sessions')).sessions;
        const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
        // The iframe can finish loading before asynchronous device setup registers
        // Salie's listener. Opening from the sidebar must survive that delay.
        await ctx.route('**/api/device', async route => {
            if (new URL(route.request().frame().url()).searchParams.get('surface') === 'assistant') await sleep(800);
            await route.continue();
        });
        page = await ctx.newPage(); page.setDefaultTimeout(20000);
        page.on('pageerror', e => errors.push(e.message));
        await page.goto(shellOrigin + '/shell/shell/index.html');
        const side = page.getByTestId('sidebar');
        const salieFrame = page.locator('[data-testid=assistant-frame]');
        const salie = page.frameLocator('[data-testid=assistant-frame]');
        const hidden = () => page.waitForFunction(() => getComputedStyle(document.querySelector('[data-testid=assistant-frame]')).visibility === 'hidden');
        await side.getByRole('button', { name: 'Work', exact: true }).waitFor();
        assert.equal(await page.getByTestId('salie-button').count(), 0, 'there is no floating Salie button');
        assert.equal(await side.getByTestId('sidebar-salie').count(), 1, 'Salie is a permanent destination in the sidebar');

        // Salie proposes, the person confirms, and the card leads to the session.
        await side.getByTestId('sidebar-salie').click();
        await salie.getByTestId('assistant').waitFor();
        await salie.getByRole('textbox', { name: 'Message to Salie' }).fill(`start the agent on ${project.id} ${task.id}`);
        await salie.getByRole('button', { name: 'Send', exact: true }).click();
        const card = salie.locator('section.confirm');
        await card.getByRole('button', { name: 'Confirm and start' }).waitFor();
        assert.equal((await runs()).length, 0, 'nothing starts before the person confirms');
        await card.getByRole('button', { name: 'Confirm and start' }).click();
        await card.getByTestId('open-in-agents').waitFor();
        const run = (await runs())[0], key = 'run:' + run.id;
        assert.equal((await runs()).length, 1);
        ok('Salie started one agent after the person confirmed');

        // Agents: the person's own Werkbord at the session; Salie folds away and keeps what it was doing.
        await card.getByTestId('open-in-agents').click();
        await hidden();
        await page.waitForFunction(() => document.querySelector('[data-testid=sidebar] [data-page=agents]')?.getAttribute('aria-current') === 'page');
        const individual = page.frameLocator('iframe[data-workspace=personal]');
        // The first visit to a page of the person's own Werkbord offers its quick tour; it is not what is tested here.
        const skipTour = async () => { const skip = individual.getByRole('button', { name: 'Skip tour', exact: true }); if (await skip.count()) await skip.click(); };
        await individual.getByRole('region', { name: 'Live task session' }).waitFor();
        await individual.getByText('Fixture process ready').first().waitFor();
        assert.equal(await individual.locator('[data-session]').count(), 1, 'one execution, one entry');
        assert.equal(await individual.locator(`[data-session="${key}"]`).getAttribute('aria-current'), 'true');
        assert.deepEqual((await sessions()).map(s => s.key), [key]);
        assert.equal(await page.locator('iframe[data-workspace=personal]').isVisible(), true);
        await individual.getByRole('textbox', { name: 'Additional instructions' }).fill('commit');
        await individual.getByRole('region', { name: 'Live task session' }).getByRole('button', { name: 'Send', exact: true }).click();
        await individual.getByText('Committed fixture work').first().waitFor();
        assert.equal((await runs()).length, 1, 'continuing does not start another');
        await page.screenshot({ path: path.join(artifacts, 'shell-agents-session.png') });
        ok('Agents shows the session in the person’s own Werkbord frame and keeps taking instructions');

        // Back to Salie, on the conversation that started it.
        await individual.getByTestId('session-salie').click();
        await salie.getByTestId('assistant').waitFor();
        assert.equal(await salieFrame.evaluate(el => getComputedStyle(el).visibility), 'visible');
        await salie.getByTestId('assistant-log').getByText(`start the agent on ${project.id} ${task.id}`).waitFor();
        assert.equal(await salie.locator('section.confirm').count(), 1, 'the same conversation, not a new one');
        assert.ok(['running', 'waiting_for_user', 'blocked', 'starting'].includes((await runs())[0].state), 'the session is still alive');
        await salie.getByTestId('assistant-close').click();
        await hidden();
        ok('Return to Salie opens the conversation that started the session; the session was not interrupted');

        // Work, then Agents: the window comes back to the session that was open.
        await side.getByRole('button', { name: 'Work', exact: true }).click();
        await page.waitForFunction(() => document.querySelector('[data-testid=sidebar] [data-page=inbox]')?.getAttribute('aria-current') === 'page');
        await side.getByRole('button', { name: 'Agents', exact: true }).click();
        await individual.getByRole('region', { name: 'Live task session' }).waitFor();
        assert.equal(await individual.locator(`[data-session="${key}"]`).getAttribute('aria-current'), 'true');
        // A task page asks for the session through the window too.
        await skipTour();
        await individual.getByTestId('session-task').click();
        await page.waitForFunction(() => document.querySelector('[data-testid=sidebar] [data-page=inbox]')?.getAttribute('aria-current') === 'page');
        await individual.locator('section.panel').waitFor();
        await skipTour();
        await individual.locator('section.panel').getByRole('button', { name: 'Open Session', exact: true }).first().click();
        await page.waitForFunction(() => document.querySelector('[data-testid=sidebar] [data-page=agents]')?.getAttribute('aria-current') === 'page');
        await individual.getByRole('region', { name: 'Live task session' }).waitFor();
        assert.equal((await runs()).length, 1);
        assert.equal((await sessions()).length, 1);
        ok('a task’s Open Session opens the same session in Agents; still one run and one session');
        assert.deepEqual(errors, [], 'no page errors: ' + errors.join('\n'));
        console.log('PASS Agents and Salie from the desktop window’s sidebar');
    } catch (e) {
        if (page) await page.screenshot({ path: path.join(artifacts, 'shell-agents-failure.png') }).catch(() => {});
        throw e;
    } finally {
        await browser.close();
        console.log('Browser artifacts: ' + artifacts);
    }
})().catch(e => { console.error(e && e.stack || e); process.exitCode = 1; }).finally(async () => {
    for (const c of children) { c.kill('SIGTERM'); await Promise.race([once(c, 'exit'), sleep(4000)]); }
    process.exit();
});

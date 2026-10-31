// Agents and Salie as the desktop window offers them: the built shell page (web/shell, built into desktop/frontend/dist), a stand-in
// for the native app, the real Individual page in a real frame, and the real controller with its runner and a scripted Salie behind
// them (scripts/browser-fixture). It checks the window's own wiring of the unified agent sessions:
//
//   - Salie starts an agent after the person confirms, and its card opens that session in the window's Agents destination;
//   - Agents is the person's own Werkbord at the session, in the frame that was already there, and Salie folds away without stopping;
//   - Return to Salie opens Salie on the conversation that started the session, and the sidebar's Agents comes back to the session;
//   - a task's Open Session in the Individual page opens the same session in Agents, and there is still one run;
//   - Back follows where the person came from, the whole way (session → task → session → Salie; Work → task → Work, with the project
//     context, search and section as they were left; Work → plan → task), and a pasted address with its route goes back with no history.
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
    const queueItems = [];
    const summary = e => ({ schema: 'workspace-summary/v1', workspace: e, projects: e.kind === 'personal' ? [{ id: project.id, name: 'Window project', href: `#/p/${project.id}/work` }] : [], work: [], queue: e.kind === 'personal' ? queueItems : [], attention: [], schedule: [], at: new Date().toISOString() });
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
        const plan = await api(ind, 'POST', `/api/projects/${project.id}/work-plans`, { id: '', projectId: project.id, title: 'Launch plan', description: '', taskIds: [task.id], decisions: '', questions: '', version: 0 });
        queueItems.push(
            { id: task.id, title: 'Fix the login', project: 'Window project', href: `#/p/${project.id}/task/${task.id}`, group: 'now' },
            { id: plan.id, title: 'Launch plan', project: 'Window project', href: `#/p/${project.id}/work?plan=${plan.id}`, group: 'plan', detail: '0 of 1 complete' });
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
        // One page header, one project selector: the window's context line is the only one, and Agents has no copy of its heading or of the
        // project filter. What the session was opened from is the one way back.
        assert.equal(await individual.getByRole('heading', { level: 1, name: 'Agents' }).count(), 1);
        assert.equal(await page.getByText('Reported in this context').count(), 0);
        assert.equal(await individual.locator('#sessions-project').count(), 0, 'the window’s project context is the one selector');
        assert.match(await individual.getByTestId('crumbs').innerText(), /Salie/);
        assert.equal(await individual.getByTestId('session-salie').count(), 0, 'Salie is the breadcrumb, not a second control');
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
        await individual.getByTestId('crumbs').getByRole('button', { name: /Salie/ }).click();
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

        // Back, the whole way: Salie → session → task → session → Salie, each step the page it came from, nothing restarted.
        // The task's panel opens over its board (or plan), which stays where it was: the panel's trail is the last in the page.
        const crumbs = () => individual.getByTestId('crumbs').last();
        const current = name => page.waitForFunction(n => document.querySelector(`[data-testid=sidebar] [data-page=${n}]`)?.getAttribute('aria-current') === 'page', name);
        const onSession = async () => { await individual.getByRole('region', { name: 'Live task session' }).waitFor(); assert.equal(await individual.locator(`[data-session="${key}"]`).getAttribute('aria-current'), 'true'); };
        assert.match(await crumbs().innerText(), /Fix the login/, 'the nearest page above the session is its task');
        await crumbs().getByRole('link', { name: /Fix the login/ }).click();
        await individual.locator('section.panel').waitFor(); await current('inbox');
        assert.match(await crumbs().innerText(), /Session/);
        await crumbs().getByRole('link', { name: /Session/ }).click();
        await onSession(); await current('agents');
        await crumbs().getByRole('button', { name: /Salie/ }).click();
        await salie.getByTestId('assistant').waitFor();
        await salie.getByTestId('assistant-close').click();
        await hidden();
        assert.equal((await runs()).length, 1); assert.equal((await sessions()).length, 1);
        ok('Back follows the route the person took (session → task → session → Salie) and the session is the same one throughout');

        // Work → task → Back: Work as it was left, with the project context it had.
        const context = page.getByTestId('project-context');
        await side.getByRole('button', { name: 'Work', exact: true }).click(); await current('inbox');
        await context.selectOption('');
        await page.getByRole('searchbox', { name: 'Search work' }).fill('Fix');
        assert.equal(await context.inputValue(), '', 'All Projects');
        await page.locator('.queue-card button.task', { hasText: 'Fix the login' }).click();
        await individual.locator('section.panel').waitFor();
        assert.notEqual(await context.inputValue(), '', 'opening a task moves the context to its project');
        assert.match(await crumbs().innerText(), /Work/);
        await crumbs().getByRole('button', { name: /Work/ }).click();
        await current('inbox');
        assert.equal(await page.getByRole('searchbox', { name: 'Search work' }).inputValue(), 'Fix', 'the search is as it was left');
        assert.equal(await context.inputValue(), '', 'Back puts All Projects back');
        await page.screenshot({ path: path.join(artifacts, 'shell-work-back.png') });
        ok('Work → task → Back returns to Work with its project context and search');

        // Work → plan → task → session, and back down the same route.
        await page.getByRole('searchbox', { name: 'Search work' }).fill('');
        await page.locator('.work-tabs').getByRole('button', { name: /^Plans/ }).click();
        await page.locator('button.task', { hasText: 'Launch plan' }).click();
        await individual.getByRole('heading', { level: 1, name: 'Launch plan' }).waitFor();
        assert.match(await crumbs().innerText(), /Work/);
        await individual.getByRole('link', { name: 'Fix the login' }).first().click();
        await individual.locator('section.panel').waitFor();
        assert.match(await crumbs().innerText(), /Work[\s\S]*Launch plan/, 'Work › Launch plan above the task');
        await individual.locator('section.panel').getByRole('button', { name: 'Open Session', exact: true }).first().click();
        await current('agents'); await onSession();
        assert.match(await crumbs().innerText(), /Work[\s\S]*Launch plan[\s\S]*Fix the login/);
        await crumbs().getByRole('link', { name: /Fix the login/ }).click();
        await individual.locator('section.panel').waitFor();
        await crumbs().getByRole('link', { name: /Launch plan/ }).click();
        await individual.getByRole('heading', { level: 1, name: 'Launch plan' }).waitFor();
        await crumbs().getByRole('button', { name: /Work/ }).click();
        await current('inbox');
        assert.equal(await page.locator('.work-tabs [aria-current=page]').innerText().then(t => t.replace(/\d+/g, '').trim()), 'Plans', 'the section is as it was left');
        assert.equal((await runs()).length, 1); assert.equal((await sessions()).length, 1);
        ok('Work → plan → task → session and back, step by step, to Work on its Plans section');

        // A pasted address carries its route: no history is needed to go back, and a reload keeps it.
        const direct = await browser.newContext({ viewport: { width: 1440, height: 900 } });
        const lone = await direct.newPage(); lone.setDefaultTimeout(20000);
        lone.on('pageerror', e => errors.push(e.message));
        await lone.goto(ind + '/#token=' + token);
        await lone.waitForFunction(() => !location.hash.includes('token='));
        await lone.evaluate(h => { location.hash = h; }, `#/p/${project.id}/task/${task.id}?from=session%3Arun%253A${run.id}`);
        const loneCrumbs = lone.getByTestId('crumbs');
        await loneCrumbs.waitFor();
        await lone.reload();
        await loneCrumbs.waitFor();
        assert.equal(await lone.evaluate(() => history.length) <= 3, true);
        await loneCrumbs.getByRole('link', { name: /Session/ }).click();
        await lone.waitForFunction(k => decodeURIComponent(location.hash).includes('#/sessions/' + k), key);
        await lone.getByRole('region', { name: 'Live task session' }).waitFor();
        assert.equal((await runs()).length, 1); assert.equal((await sessions()).length, 1);
        await direct.close();
        ok('a pasted address with its route goes back to the same session after a reload, with no history behind it');
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

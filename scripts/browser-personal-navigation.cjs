// Disposable controller/SQLite and built desktop shell. Native boundaries are covered by desktop Go tests.
const fs = require('node:fs'), os = require('node:os'), path = require('node:path'), http = require('node:http');
const cp = require('node:child_process'), net = require('node:net');
const { once } = require('node:events');
const assert = require('node:assert/strict');
const root = path.resolve(__dirname, '..');
const dist = path.join(root, 'desktop', 'frontend', 'dist');
const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'werkbord-personal-navigation-'));
const artifacts = process.env.BROWSER_ARTIFACT_DIR || path.join(root, 'output/playwright/personal-navigation');
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
    let teamAccess = 'ready';
    let savedWorkspace;
    const personalCalls = [];
    const { todoCollection, todoWorkspace } = await import(path.join(root, 'web/src/lib/personal/example.ts'));
    const collection = await api(ind, 'POST', '/api/personal/v1/collections', todoCollection);
    const spec = todoWorkspace(collection.id);
    spec.visibility.teamWorkspaceIds = ['team:main'];
    spec.name = 'Personal planning with a deliberately long workspace name';
    savedWorkspace = await api(ind, 'POST', '/api/personal/v1/workspaces', spec);
    const record = await api(ind, 'POST', '/api/personal/v1/collections/' + collection.id + '/records', {values: {title: 'Shared across every location', done: false, priority: 'High'}});
    const summary = e => ({ schema: 'workspace-summary/v1', workspace: e, projects: e.kind === 'personal' ? [{ id: project.id, name: 'Window project', href: `#/p/${project.id}/board` }] : [], work: [], attention: [], schedule: [], at: new Date().toISOString() });
    const calls = {
        PersonalWorkspaceCall: async (method, path, body) => {
            personalCalls.push({method, path});
            const res = await fetch(ind + path, {method, headers: {Authorization: 'Bearer ' + token, 'Content-Type': 'application/json'}, body: body === null ? undefined : JSON.stringify(body)});
            const raw = await res.text(); return {status: res.status, body: raw ? JSON.parse(raw) : null};
        },
        PersonalTeamProjects: (id, team) => {
            assert.equal(id, savedWorkspace.id); assert.equal(team, 'team:main');
            if (teamAccess === 'revoked') throw Error('permission denied');
            return {projects: [{id: 'permitted', name: 'Permitted Team project'}], at: new Date(Date.now() - (teamAccess === 'stale' ? 60000 : 0)).toISOString()};
        },
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
    let current = null;
    async function switchTo(page, id) {
        // There is one sidebar with a section for each workspace: Home is its own entry, and a workspace opens from its header.
        await page.locator('nav[aria-label="Werkbord"] ' + (id === 'universal' ? '[data-page=inbox]' : '[data-open="' + id + '"]')).click();
    }
    const page = await browser.newPage({viewport: {width: 1440, height: 900}}); current = page;
    await page.addInitScript(() => { window.__received = []; addEventListener('message', e => window.__received.push(e.data)); });
    page.setDefaultTimeout(15000); page.on('pageerror', e => errors.push(e.message));
    const side = page.getByRole('navigation', {name: 'Werkbord', exact: true});
    // The saved workspace is listed in each section it was made for; the first is enough to open it.
    const generated = side.locator('[data-page="generated-' + savedWorkspace.id + '"]').first();
    const openSaved = async () => { if (await generated.count()) await generated.click(); else { const back = page.getByTestId('back-to-universal'); if (await back.count()) await back.click(); await page.getByTestId('hub-generated-' + savedWorkspace.id).click(); } await page.locator('.saved-workspace').waitFor(); await page.getByText('Shared across every location', {exact:true}).first().waitFor(); };
    // The sidebar is Home and Calendar, then a section for Personal and one for each team, then what is about this computer. A
    // button without a page is Salie's ('theme' here).
    const ordered = async (scope, expected) => {
        const actual = await side.locator((scope ? scope + ' ' : '') + 'button.nav').evaluateAll(els => els.map(e => e.dataset.page || (e.dataset.project ? 'project' : 'theme')));
        assert.deepEqual(actual, expected);
    };
    const saved = 'generated-' + savedWorkspace.id;
    const footer = ['add-team', 'workspaces', 'personal-library', 'settings'];
    try {
        await page.goto(shellOrigin + '/shell/shell/index.html');
        await page.getByTestId('hub-generated-' + savedWorkspace.id).waitFor();
        // Home has the same sidebar as everywhere: its saved workspace, Personal and the team each in their own section.
        await ordered('', ['theme', 'inbox', 'calendar', saved, 'create-generated', 'project', 'add-project', 'ws-analytics', saved, 'ws-reviews', 'ws-members', 'ws-analytics', saved, ...footer]);
        assert.equal(await side.evaluate(el => el.getBoundingClientRect().width), 208);
        await page.screenshot({path:path.join(artifacts,'home-default.png')});
        await page.getByTestId('sidebar-label-toggle').click();
        assert.equal(await side.evaluate(el => el.getBoundingClientRect().width), 48);
        await page.getByTestId('sidebar-label-toggle').click();
        assert.equal(await side.evaluate(el => el.getBoundingClientRect().width), 208);
        await page.getByTestId('hub-library').click();
        await page.getByRole('heading', {name: 'Data library', exact: true}).waitFor();
        await page.getByTestId('back-to-universal').click();
        await page.getByTestId('hub-generated-' + savedWorkspace.id).waitFor();
        await page.screenshot({path:path.join(artifacts,'home.png')});
        // At intermediate widths the sidebar is a rail of icons until the person asks for the labels.
        await page.setViewportSize({width: 1000, height: 800});
        assert.equal(await side.evaluate(el => el.getBoundingClientRect().width), 48);
        await page.screenshot({path:path.join(artifacts,'home-window.png')});
        await page.setViewportSize({width: 390, height: 844});
        assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
        await page.screenshot({path:path.join(artifacts,'home-narrow.png'), fullPage:true});
        await page.setViewportSize({width:1440,height:900});
        ok('Home has the one sidebar, with a section for Personal and each team, and reaches saved workspaces, calendar and the library on its page');

        await switchTo(page,'personal');
        await page.frameLocator('iframe[data-workspace=personal]').locator('main').waitFor();
        await ordered('[data-section=personal]', ['project', 'add-project', 'ws-analytics', saved]);
        await openSaved();
        assert.equal(await generated.getAttribute('aria-current'), 'page');
        assert.equal(await generated.getAttribute('aria-label'), spec.name);
        assert.equal(await generated.locator('svg[data-workspace-icon]').getAttribute('data-workspace-icon'), savedWorkspace.icon);
        await side.locator('[data-page=ws-analytics]').click();
        await page.frameLocator('iframe[data-workspace=personal]').getByRole('heading',{name:'Analytics',exact:true}).waitFor();
        const individualHandle = await page.locator('iframe[data-workspace=personal]').elementHandle();
        const individualFrame = await individualHandle.contentFrame();
        await individualFrame.evaluate(()=>window.__personalSentinel='retained');
        const place = await individualFrame.evaluate(()=>location.hash);
        await openSaved();
        assert.equal(await page.locator('iframe[data-workspace=personal]').isVisible(), false);
        assert.equal(await individualFrame.evaluate(()=>location.hash),place);
        await page.screenshot({path:path.join(artifacts,'individual.png')});
        ok('Personal entries append after Analytics and preserve its live frame and route');

        await switchTo(page,'team:main');
        const teamFrame = page.frameLocator('iframe[data-workspace="team:main"]');
        await teamFrame.locator('#draft').fill('My unfinished Team draft');
        await side.locator('[data-page=ws-analytics]').click();
        await ordered('[data-section="team:main"]', ['ws-reviews', 'ws-members', 'ws-analytics', saved]);
        const teamHandle = await page.locator('iframe[data-workspace="team:main"]').elementHandle();
        await openSaved();
        await page.getByText('Team projects · Northstar · read-only',{exact:true}).click();
        await page.getByText('Permitted Team project',{exact:true}).waitFor();
        await page.screenshot({path:path.join(artifacts,'team.png')});
        assert.equal(await page.locator('iframe[data-workspace="team:main"]').isVisible(),false);
        await side.locator('[data-page=ws-analytics]').click();
        assert.equal(await teamFrame.locator('#draft').inputValue(),'My unfinished Team draft');
        assert.equal(await page.locator('iframe[data-workspace="team:main"]').evaluate((el, previous)=>el===previous,teamHandle),true);
        assert.equal(await individualFrame.evaluate(()=>window.__personalSentinel),'retained');
        assert.equal(await individualFrame.evaluate(()=>location.hash),place);
        await openSaved();
        ok('Team ordering, identical record data, both frame identities and unfinished navigation state preserved');

        await page.getByTestId('sidebar-label-toggle').click();
        assert.equal(await generated.getAttribute('aria-label'),spec.name);
        assert.equal(await generated.locator('svg').getAttribute('width'),'16');
        await generated.focus(); assert.equal(await generated.evaluate(el=>el===document.activeElement),true);
        assert.equal(await generated.evaluate(el=>el.getBoundingClientRect().width<=36),true);
        await page.screenshot({path:path.join(artifacts,'collapsed.png')});
        await page.getByTestId('sidebar-label-toggle').click();
        ok('collapsed rail retains full accessible label, tooltip, keyboard focus and 16px icon');

        // One saved definition and record source in all locations. Change a value through
        // the shell-managed renderer and reopen it in each of the other contexts.
        const editable = page.locator('.saved-workspace input[type=checkbox]').first();
        await editable.check();
        await page.getByText('Saved.',{exact:true}).waitFor();
        const persisted = await api(ind,'GET','/api/personal/v1/collections/'+collection.id+'/records/'+record.id);
        assert.equal(persisted.values.done,true); assert.equal(persisted.id,record.id);
        for (const context of ['universal','personal','team:main']) {
            await switchTo(page,context); await openSaved();
            assert.equal(await page.locator('.saved-workspace input[type=checkbox]').first().isChecked(),true);
        }
        ok('one workspace, one collection and the same record ID after mutation in all three display locations');

        await page.getByRole('button',{name:'Customize with Salie',exact:true}).click();
        const salie = page.frameLocator('[data-testid=assistant-frame]');
        await salie.locator('[data-testid=assistant]').waitFor();
        const assistantWindow = await (await page.getByTestId('assistant-frame').elementHandle()).contentFrame();
        await assistantWindow.waitForFunction(id=>window.__received.some(x=>x.type==='werkbord.assistant'&&x.personalWorkspace===id),savedWorkspace.id);
        const handoff = await assistantWindow.evaluate(id=>window.__received.find(x=>x.type==='werkbord.assistant'&&x.personalWorkspace===id),savedWorkspace.id);
        assert.equal(handoff.personalRevision,1);
        await salie.getByRole('button',{name:'Close assistant',exact:true}).click();
        await page.getByTestId('assistant-frame').waitFor({state:'hidden'});
        ok('Customize with Salie carries the exact saved ID and revision as structured trusted context');

        // Re-read errors and stale responses must never be presented as current projects.
        teamAccess='revoked';
        await switchTo(page,'universal'); await openSaved();
        await page.getByText('Team projects · Northstar · read-only',{exact:true}).click();
        await page.getByText('Team projects are unavailable.',{exact:false}).waitFor();
        assert.equal(await page.getByText('Permitted Team project',{exact:true}).count(),0);
        teamAccess='stale';
        await page.getByRole('button',{name:'Retry',exact:true}).click();
        await page.getByText('This Team snapshot is stale.',{exact:false}).waitFor();
        items[1].state='offline';
        await page.getByText('Team access is unavailable.',{exact:false}).waitFor();
        assert.equal(await page.getByText('Permitted Team project',{exact:true}).count(),0);
        await page.screenshot({path:path.join(artifacts,'offline.png')});
        items[1].state='ready';teamAccess='ready';
        ok('revoked, stale and offline Team data show explicit states with no current project rows');

        await page.getByLabel('Manage workspace',{exact:true}).click();
        await page.getByRole('button',{name:'Name, icon & locations',exact:true}).click();
        await page.getByLabel('Name',{exact:true}).fill('My planning');
        await page.getByRole('button',{name:'New icon',exact:true}).click();
        await page.getByRole('button',{name:'Save changes',exact:true}).click();
        await page.getByRole('heading',{name:'My planning',exact:true}).waitFor();
        const updated=await api(ind,'GET','/api/personal/v1/workspaces/'+savedWorkspace.id);
        assert.equal(updated.name,'My planning');assert.match(updated.icon,/^lucide-[a-z0-9-]+$/);assert.notEqual(updated.icon,savedWorkspace.icon);assert.equal(updated.revision,2);
        assert.deepEqual(updated.visibility,spec.visibility);
        const unchanged=await api(ind,'GET','/api/personal/v1/collections/'+collection.id+'/records/'+record.id);
        assert.deepEqual(unchanged,persisted);
        assert.equal(await page.locator('iframe[data-workspace]').count(),2);
        ok('management changes name and fitting unused Lucide icon with optimistic revisions, preserving records and all locations');
        await switchTo(page,'team:main'); await openSaved();
        await page.getByLabel('Manage workspace',{exact:true}).click();
        await page.getByRole('button',{name:'Remove from this location',exact:true}).click();
        await page.locator('iframe[data-workspace="team:main"]').waitFor({state:'visible'});
        assert.equal(await generated.count(),0);
        assert.equal(await teamFrame.locator('#draft').inputValue(),'My unfinished Team draft');
        await switchTo(page,'universal'); await openSaved();
        await page.getByLabel('Manage workspace',{exact:true}).click();
        await page.getByRole('button',{name:'Name, icon & locations',exact:true}).click();
        await page.getByRole('checkbox',{name:'Northstar',exact:true}).check();
        await page.getByRole('button',{name:'Save changes',exact:true}).click();
        await page.getByRole('heading',{name:'My planning',exact:true}).waitFor();
        await switchTo(page,'team:main'); await openSaved();
        assert.equal(await generated.count(),1);
        await page.getByLabel('Manage workspace',{exact:true}).click();
        await page.getByRole('button',{name:'Delete workspace…',exact:true}).click();
        await page.getByRole('button',{name:'Delete workspace only',exact:true}).click();
        await page.getByRole('checkbox',{name:'I confirm this deletion and the personal data choices above.',exact:true}).check();
        await page.getByRole('button',{name:'Confirm deletion',exact:true}).click();
        await page.getByRole('heading',{name:'Data library',exact:true}).waitFor();
        await side.locator('[data-page=ws-analytics]').click();
        await page.locator('iframe[data-workspace="team:main"]').waitFor({state:'visible'});
        assert.equal(await generated.count(),0);
        assert.deepEqual(await api(ind,'GET','/api/personal/v1/collections/'+collection.id+'/records/'+record.id),persisted);
        assert.equal(await teamFrame.locator('#draft').inputValue(),'My unfinished Team draft');
        ok('removing/restoring a display location changes one association; deletion returns to the retained core frame and keeps records');
        assert.deepEqual(errors,[]);
        assert.ok(personalCalls.every(c=>c.path.startsWith('/api/personal/v1/')));
        console.log('PASS Phase 4 personal workspace desktop navigation');
    } catch(e) {
        await page.screenshot({path:path.join(artifacts,'failure.png')}).catch(()=>{}); throw e;
    } finally { await browser.close(); server.close(); console.log('Browser artifacts: '+artifacts); }
})().catch(e=>{console.error(e&&e.stack||e);process.exitCode=1;}).finally(async()=>{
    for(const c of children){c.kill('SIGTERM');await Promise.race([once(c,'exit'),sleep(4000)]);}
});

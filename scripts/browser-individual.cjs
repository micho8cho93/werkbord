// Use the disposable browser fixture; it emulates a remote runner without executing an agent.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const crypto = require('node:crypto');
const fs = require('node:fs');
const assert = require('node:assert/strict');
const base = process.env.PERSONAL_BROWSER_URL || 'http://127.0.0.1:17421';
const path = require('node:path');
const repo = fs.realpathSync(process.env.WERKBORD_BROWSER_REPO);
const artifacts = process.env.BROWSER_ARTIFACT_DIR || fs.mkdtempSync(path.join(require('node:os').tmpdir(), 'werkbord-individual-browser-'));
fs.mkdirSync(artifacts, { recursive: true });
const token = 'disposable-browser-credential';
async function api(method, path, data) { const res = await fetch(base + path, { method, headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' }, body: data ? JSON.stringify(data) : undefined }); const raw = await res.text(); if (res.status >= 400)
    throw Error(res.status + ' ' + raw); return raw ? JSON.parse(raw) : null; }
(async () => {
    const browser = await chromium.launch({ headless: true, ...(process.env.BROWSER_EXECUTABLE ? { executablePath: process.env.BROWSER_EXECUTABLE } : {}) });
    const ctx = await browser.newContext();
    const page = await ctx.newPage();
    let errors = [];
    page.on('pageerror', e => errors.push(e.message));
    page.setDefaultTimeout(10000);
    await page.goto(base + '/#token=' + token);
    await page.getByRole('heading', { name: 'Welcome to Werkbord', exact: true }).waitFor();
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.screenshot({ path: path.join(artifacts, 'onboarding-desktop.png') });
    await page.setViewportSize({ width: 390, height: 844 });
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 2), false, 'onboarding mobile document overflows');
    await page.screenshot({ path: path.join(artifacts, 'onboarding-mobile.png') });
    await page.getByRole('button', { name: 'Continue without a project', exact: true }).click();
    await page.setViewportSize({ width: 1440, height: 1000 });
    console.log('PASS first-run setup and continue into empty project workflow, desktop/mobile');
    await api('POST', '/api/onboarding/complete', {});
    let projects = await api('GET', '/api/projects');
    let p = projects.projects.find(p => p.repoPath === repo);
    if (!p)
        p = await api('POST', '/api/projects', { path: repo, name: 'Disposable individual' });
    const pair = await api('POST', '/api/runners/pair', { projects: [p.id], allowClone: false });
    const secret = pair.code.split('.')[2];
    const { publicKey, privateKey } = crypto.generateKeyPairSync('ed25519');
    async function signed(path, data) { const body = JSON.stringify(data); const r = await fetch(base + path, { method: 'POST', headers: { 'X-Runner-Signature': crypto.sign(null, Buffer.from(body), privateKey).toString('base64url') }, body }); const raw = await r.text(); if (r.status >= 400)
        throw Error(r.status + ' ' + raw); return JSON.parse(raw); }
    let remote = await signed('/api/runner/join', { secret, publicKey: publicKey.export({ format: 'der', type: 'spki' }).subarray(-32).toString('base64url'), name: 'Browser remote', os: 'linux', arch: 'amd64', version: 'test' });
    remote = await api('PUT', '/api/runners/' + remote.id, { ...remote, automatic: true, capacity: 2 });
    let seq = 0;
    const caps = { cpu: 4, repositories: [p.id], agents: [{ id: 'codex', name: 'Remote Codex', available: true, installed: true }], options: [{ agentId: 'codex', models: [{ id: 'default', name: 'Agent default' }, { id: 'remote-model', name: 'Remote model' }], reasoning: [{ id: 'default', name: 'Agent default' }, { id: 'remote-reason', name: 'Remote reasoning' }], customModels: false }] };
    async function sync(reports = []) { return signed('/api/runner/sync', { protocol: 1, runnerId: remote.id, sequence: ++seq, at: new Date().toISOString(), capabilities: caps, reports }); }
    ;
    await sync();
    const timer = setInterval(() => sync().catch(() => { }), 10000);
    try {
        await api('PUT', `/api/projects/${p.id}/execution`, { runner: remote.id, agent: 'codex', model: 'remote-model', reasoning: 'remote-reason' });
        const task = await api('POST', `/api/projects/${p.id}/tasks`, { title: 'Remote eligibility ' + Date.now() });
        await page.goto(base + '/#token=' + token);
        await page.goto(base + `/#/p/${p.id}/task/${task.id}`);
        await page.getByRole('button', { name: 'Start agent', exact: true }).waitFor();
        assert.equal(await page.getByRole('button', { name: 'Start agent', exact: true }).isEnabled(), true);
        assert.equal(await page.locator('select[id$="model"] option[value="remote-model"]').count() > 0, true);
        assert.equal(await page.locator('select[id$="reasoning"] option[value="remote-reason"]').count() > 0, true);
        const startResponse = page.waitForResponse(r => r.request().method() === 'POST' && r.url().endsWith('/runs'));
        await page.getByRole('button', { name: 'Start agent', exact: true }).click();
        const start = await startResponse;
        assert.equal(start.status(), 201);
        const run = await start.json();
        assert.equal(run.remote, true);
        assert.equal(run.runnerId, remote.id);
        console.log('PASS inherited remote eligibility, remote model/reasoning, Start on remote while controller agent unavailable');
        const reply = await sync();
        const job = reply.jobs.find(j => j.run.id === run.id);
        const handoff = '<devboard-handoff>' + JSON.stringify({ summary: 'Readable browser handoff', objective: 'Improve the board', results: 'Preserved the run history', tests: ['Browser flow passed'], nextAction: 'Review the work' }) + '</devboard-handoff>';
        await sync([{ runId: run.id, observations: [{ seq: 1, kind: 'accepted' }, { seq: 2, kind: 'started', branch: 'devboard/' + remote.id + '/' + run.id, baseCommit: 'a'.repeat(40) }, { seq: 3, kind: 'event', event: { Kind: 'output', Stream: 'assistant', Text: handoff } }] }]);
        await page.getByText('Readable browser handoff', { exact: true }).waitFor();
        assert.equal(await page.locator('.text').filter({ hasText: '<devboard-handoff>' }).count(), 0);
        const message = page.getByRole('textbox', { name: 'Message to the agent', exact: true });
        await message.fill('First line');
        await message.press('Shift+Enter');
        await message.press('End');
        await message.press('x');
        assert((await message.inputValue()).includes('\n'));
        const inputResponse = page.waitForResponse(r => r.request().method() === 'POST' && r.url().endsWith('/input'));
        await message.press('Enter');
        assert.equal((await inputResponse).status(), 200);
        await page.waitForFunction(() => document.querySelector('#message')?.value === '');
        await sync([{ runId: run.id, observations: [{ seq: 4, kind: 'event', event: { Kind: 'question', Question: { Ref: 'keyboard-clarification', Kind: 'clarification', Prompt: 'Which behavior should the fixture use?', AllowFreeText: true } } }] }]);
        const answer = page.locator('textarea[id^="answer-"]');
        await answer.fill('Keep the history');
        const answerResponse = page.waitForResponse(r => r.request().method() === 'POST' && r.url().endsWith('/answer'));
        await answer.press('Enter');
        assert((await answerResponse).ok());
        await page.getByRole('button', { name: 'Stop agent', exact: true }).click();
        const stopResponse = page.waitForResponse(r => r.request().method() === 'POST' && r.url().endsWith('/stop'));
        await page.getByRole('button', { name: 'Confirm stop', exact: true }).click();
        assert.equal((await stopResponse).status(), 200);
        await sync([{ runId: run.id, observations: [{ seq: 5, kind: 'ended', result: { state: 'stopped', exitCode: 0 }, headCommit: 'b'.repeat(40), uncommitted: false }] }]);
        await page.getByRole('button', { name: 'Close task', exact: true }).waitFor({ state: 'visible' });
        const closeResponse = page.waitForResponse(r => r.request().method() === 'PATCH' && r.url().endsWith('/tasks/' + task.id));
        await page.getByRole('button', { name: 'Close task', exact: true }).click();
        assert.equal((await closeResponse).status(), 200);
        await page.getByRole('button', { name: /^Archive/ }).click();
        await page.getByRole('link', { name: task.title, exact: true }).waitFor();
        await page.getByRole('link', { name: task.title, exact: true }).click();
        await page.getByText('Readable browser handoff', { exact: true }).waitFor();
        await page.setViewportSize({ width: 390, height: 844 });
        const sections = page.getByRole('navigation', { name: 'Task details sections' });
        await sections.getByRole('button', { name: 'Details', exact: true }).click();
        await page.getByRole('heading', { name: 'Details', exact: true }).waitFor();
        await sections.getByRole('button', { name: 'Activity', exact: true }).click();
        await page.getByText('Readable browser handoff', { exact: true }).waitFor();
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 2), false);
        await page.screenshot({ path: path.join(artifacts, 'individual-task-mobile.png') });
        await page.setViewportSize({ width: 1440, height: 1000 });
        await page.screenshot({ path: path.join(artifacts, 'individual-task-desktop.png') });
        await page.getByRole('button', { name: 'Restore task', exact: true }).click();
        await page.getByRole('button', { name: 'Close task', exact: true }).waitFor();
        console.log('PASS Enter-to-send, Shift+Enter newline, readable handoff, header Stop, close/archive/history/restore');

        await page.goto(base + `/#/p/${p.id}/board`);
        await page.getByRole('button', { name: 'Back to board', exact: true }).click();
        let finished = await api('POST', `/api/projects/${p.id}/tasks`, { title: 'Clear Done fixture', description: 'Details to recall' });
        finished = await api('PATCH', `/api/projects/${p.id}/tasks/${finished.id}`, { version: finished.version, state: 'done' });
        await page.goto(base + `/#/p/${p.id}/board`);
        await page.getByRole('button', { name: 'Clear Done', exact: true }).click();
        await page.getByRole('button', { name: /^Archive/ }).click();
        await page.getByRole('link', { name: finished.title, exact: true }).waitFor();
        await page.getByRole('button', { name: 'Restore', exact: true }).click();
        await page.getByRole('button', { name: 'Back to board', exact: true }).click();
        await page.getByRole('link', { name: finished.title, exact: true }).waitFor();
        const dragTask = await api('POST', `/api/projects/${p.id}/tasks`, { title: 'Drag to Doing fixture' });
        await page.getByRole('link', { name: dragTask.title, exact: true }).waitFor();
        await page.getByRole('link', { name: dragTask.title, exact: true }).locator('..').dragTo(page.getByRole('region', { name: 'Doing', exact: true }));
        await page.getByRole('region', { name: 'Doing', exact: true }).getByRole('link', { name: dragTask.title, exact: true }).waitFor();
        assert.equal((await api('GET', `/api/projects/${p.id}/tasks`)).tasks.find(t => t.id === dragTask.id).state, 'doing');
        const dragRun = (await api('GET', `/api/projects/${p.id}/runs`)).runs.find(r => r.taskId === dragTask.id);
        await sync([{ runId: dragRun.id, observations: [{ seq: 1, kind: 'ended', result: { state: 'stopped' } }] }]);
        console.log('PASS Clear Done preserves details, restore to Done, backlog drop persists Doing while remote start is queued');
        await page.goto(base + '/#/settings');
        await page.locator('#global-priority').selectOption('high');
        await api('PUT', '/api/settings/execution', { priority: 'low' });
        await page.waitForTimeout(400);
        assert.equal(await page.locator('#global-priority').inputValue(), 'high');
        await page.getByRole('button', { name: 'Save defaults', exact: true }).click();
        await page.getByText('Saved', { exact: true }).waitFor();
        await api('PUT', '/api/settings/execution', { priority: 'low' });
        await page.waitForFunction(() => document.querySelector('#global-priority')?.value === 'low');
        console.log('PASS individual settings preserve dirty edits and follow updates after save');
        await page.goto(base + `/#/p/${p.id}/defaults`);
        await page.locator('#project-priority').selectOption('high');
        await api('PUT', `/api/projects/${p.id}/execution`, { runner: remote.id, agent: 'codex', priority: 'low' });
        await page.waitForTimeout(400);
        assert.equal(await page.locator('#project-priority').inputValue(), 'high');
        console.log('PASS unsaved project defaults survive SSE refresh');
        for (const [name, route] of [['board', `/p/${p.id}/board`], ['project-settings', `/p/${p.id}/defaults`], ['settings', '/settings'], ['runs', `/p/${p.id}/runs`], ['git', `/p/${p.id}/git`], ['projects', '/projects']]) {
            await page.goto(base + '/#' + route);
            if (name === 'git') await page.getByRole('navigation', { name: 'Git sections' }).waitFor({ timeout: 120000 });
            else await page.waitForTimeout(250);
            for (const [size, width, height] of [['desktop', 1440, 1000], ['mobile', 390, 844]]) {
                await page.setViewportSize({ width, height });
                assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 2), false, name + ' ' + size + ' document overflows');
                await page.screenshot({ path: path.join(artifacts, 'individual-' + name + '-' + size + '.png') });
            }
        }
        await page.setViewportSize({ width: 1440, height: 1000 });
        await page.goto(base + `/#/p/${p.id}/git`);
        await page.getByRole('navigation', { name: 'Git sections' }).waitFor({ timeout: 120000 });
        await page.getByRole('navigation', { name: 'Git sections' }).getByRole('link', { name: /^Branches/ }).click();
        await page.getByRole('region', { name: 'All branches', exact: true }).waitFor();
        await page.getByRole('navigation', { name: 'Git sections' }).getByRole('link', { name: /^Worktrees/ }).click();
        await page.getByRole('region', { name: 'Worktrees', exact: true }).waitFor();
        await page.goto(base + '/#/projects');
        await page.getByRole('textbox', { name: 'Selected folder', exact: true }).fill(repo);
        await page.getByRole('button', { name: 'Choose folder…', exact: true }).click();
        await page.getByRole('button', { name: 'Use this folder', exact: true }).click();
        assert.equal(await page.getByRole('textbox', { name: 'Selected folder', exact: true }).inputValue(), repo);
        console.log('PASS section navigation, folder browser, all changed routes desktop/mobile without overflow');
        const privateContext = await browser.newContext();
        await privateContext.addInitScript(() => {
            Object.defineProperty(window, 'localStorage', { get() { throw new DOMException('Storage unavailable', 'SecurityError'); } });
        });
        const privatePage = await privateContext.newPage();
        privatePage.on('pageerror', e => errors.push(e.message));
        await privatePage.goto(base + '/#token=' + token);
        await privatePage.waitForFunction(() => !location.hash.includes('token='));
        // (There is no connection-status indicator any more: the board being on screen is the sign that sign-in worked.)
        await privatePage.waitForFunction(() => document.querySelector('main, [role="main"]'));
        await privateContext.close();
        assert.deepEqual(errors, []);
        console.log('PASS page-only sign-in with browser storage unavailable, desktop/mobile without overflow');
        console.log('PASS individual browser flows without JavaScript errors');
    }
    finally {
        clearInterval(timer);
        await browser.close();
    }
})().catch(e => { console.error(e.stack); process.exit(1); });

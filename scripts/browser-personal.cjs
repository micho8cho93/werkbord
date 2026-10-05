// Use the disposable browser fixture; it emulates a remote runner without executing an agent.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const crypto = require('node:crypto');
const fs = require('node:fs');
const assert = require('node:assert/strict');
const base = process.env.PERSONAL_BROWSER_URL || 'http://127.0.0.1:17421';
const path = require('node:path');
const repo = fs.realpathSync(process.env.WERKBORD_BROWSER_REPO);
const artifacts = process.env.BROWSER_ARTIFACT_DIR || fs.mkdtempSync(path.join(require('node:os').tmpdir(), 'werkbord-personal-browser-'));
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
    await api('POST', '/api/onboarding/complete', {});
    let projects = await api('GET', '/api/projects');
    let p = projects.projects.find(p => p.repoPath === repo);
    if (!p)
        p = await api('POST', '/api/projects', { path: repo, name: 'Disposable personal' });
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
        await page.getByText('Change for this run', { exact: true }).click();
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
        await sync([{ runId: run.id, observations: [{ seq: 1, kind: 'accepted' }, { seq: 2, kind: 'started', branch: 'devboard/' + remote.id + '/' + run.id, baseCommit: 'a'.repeat(40) }, { seq: 3, kind: 'ended', result: { state: 'completed', exitCode: 0 }, headCommit: 'b'.repeat(40), uncommitted: false }] }]);
        await page.getByText('Handoff', { exact: true }).first().waitFor();
        await page.goto(base + '/#/settings');
        await page.locator('#global-priority').selectOption('high');
        await api('PUT', '/api/settings/execution', { priority: 'low' });
        await page.waitForTimeout(400);
        assert.equal(await page.locator('#global-priority').inputValue(), 'high');
        await page.getByRole('button', { name: 'Save defaults', exact: true }).click();
        await page.getByText('Saved', { exact: true }).waitFor();
        await api('PUT', '/api/settings/execution', { priority: 'low' });
        await page.waitForFunction(() => document.querySelector('#global-priority')?.value === 'low');
        console.log('PASS personal settings preserve dirty edits and follow updates after save');
        await page.goto(base + `/#/p/${p.id}/defaults`);
        await page.locator('#project-priority').selectOption('high');
        await api('PUT', `/api/projects/${p.id}/execution`, { runner: remote.id, agent: 'codex', priority: 'low' });
        await page.waitForTimeout(400);
        assert.equal(await page.locator('#project-priority').inputValue(), 'high');
        console.log('PASS unsaved project defaults survive SSE refresh');
        assert.deepEqual(errors, []);
        await page.setViewportSize({ width: 390, height: 844 });
        await page.screenshot({ path: path.join(artifacts, 'personal-mobile.png') });
        console.log('PASS personal browser flows without JavaScript errors');
    }
    finally {
        clearInterval(timer);
        await browser.close();
    }
})().catch(e => { console.error(e.message); process.exit(1); });

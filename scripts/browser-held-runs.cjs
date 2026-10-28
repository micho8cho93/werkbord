// Work held on a revoked runner: the owner sees it in Settings → Runners, can release it only after saying they inspected the
// machine, and the button sends exactly the confirmation the controller requires. The controller's side (listing, no release by
// time, owner-only) is covered by Go tests; the two held-run endpoints are answered here so the page can be seen with a held run.
const assert = require('node:assert/strict');
const fs = require('node:fs'), os = require('node:os'), path = require('node:path'), net = require('node:net');
const cp = require('node:child_process');
const { once } = require('node:events');
const root = path.resolve(__dirname, '..');
const { chromium } = require(path.join(root, 'web/node_modules/playwright'));
const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'werkbord-held-'));
const token = 'disposable-browser-credential';
const sleep = ms => new Promise(r => setTimeout(r, ms));
async function port() { const s = net.createServer(); s.listen(0, '127.0.0.1'); await once(s, 'listening'); const p = s.address().port; await new Promise(r => s.close(r)); return p; }
(async () => {
    const base = 'http://127.0.0.1:' + await port();
    const fixture = path.join(temp, 'fixture');
    cp.execFileSync('go', ['build', '-o', fixture, './scripts/browser-fixture'], { cwd: root, stdio: 'inherit' });
    const child = cp.spawn(fixture, [], { cwd: root, stdio: ['ignore', 'ignore', 'inherit'], env: { ...process.env, WERKBORD_BROWSER_ADDR: new URL(base).host } });
    const browser = await chromium.launch({ headless: true });
    try {
        for (let i = 0; i < 150; i++) { if (await fetch(base + '/api/health').then(r => r.ok).catch(() => false)) break; await sleep(100); }
        await fetch(base + '/api/onboarding/complete', { method: 'POST', headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' }, body: '{"skipped":[]}' });
        const held = [{ runnerId: 'rnr_lost', runnerName: 'Old laptop', hostname: 'laptop.local', runId: 'run_held', projectId: 'prj_1', taskId: 'task_1', taskTitle: 'Fix the importer', state: 'running', startedAt: new Date(Date.now() - 3600e3).toISOString() }];
        const releases = [];
        {
            const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 } });
            const page = await ctx.newPage();
            let list = held.slice();
            await page.route('**/api/runners/held-runs', route => route.fulfill({ json: { runs: list } }));
            await page.route('**/api/runners/*/runs/*/resolve', async route => {
                releases.push({ url: new URL(route.request().url()).pathname, body: route.request().postDataJSON() });
                list = [];
                await route.fulfill({ json: { resolved: true } });
            });
            await page.goto(base + '/#token=' + token);
            await page.goto(base + '/#/settings?runners');
            const section = page.getByRole('region', { name: 'Work held on revoked machines' });
            await section.waitFor();
            assert.match(await section.innerText(), /Fix the importer[\s\S]*Old laptop \(laptop\.local\)/);
            assert.match(await section.innerText(), /Nothing releases it on a timer/);
            const release = section.getByRole('button', { name: 'Release this work', exact: true });
            assert(await release.isDisabled(), 'releasable without saying the machine was inspected');
            await section.getByRole('checkbox').check();
            assert(await release.isEnabled());
            await release.click();
            await page.getByText(/Released “Fix the importer”/).waitFor();
            assert.deepEqual(releases, [{ url: '/api/runners/rnr_lost/runs/run_held/resolve', body: { confirmStopped: true } }]);
            assert.equal(await section.getByRole('button', { name: 'Release this work' }).count(), 0, 'released work is still offered');
            await ctx.close();
        }
        console.log('held runner work: ok');
    } finally {
        await browser.close();
        child.kill();
        fs.rmSync(temp, { recursive: true, force: true });
    }
})().catch(e => { console.error(e); process.exit(1); });

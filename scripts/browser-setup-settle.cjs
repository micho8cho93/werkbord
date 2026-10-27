// Guided setup settles once and stays settled: while Werkbord is still finding out the state of this computer, the screen says it is
// checking; it never shows a step as to do, a requirement as unmet or a reconnect prompt that the next answer takes back.
const assert = require('node:assert/strict');
const fs = require('node:fs'), os = require('node:os'), path = require('node:path'), net = require('node:net');
const cp = require('node:child_process');
const { once } = require('node:events');
const root = path.resolve(__dirname, '..');
const { chromium } = require(path.join(root, 'web/node_modules/playwright'));
const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'werkbord-settle-'));
const token = 'disposable-browser-credential';
const sleep = ms => new Promise(r => setTimeout(r, ms));
async function port() { const s = net.createServer(); s.listen(0, '127.0.0.1'); await once(s, 'listening'); const p = s.address().port; await new Promise(r => s.close(r)); return p; }
// What a person can see of setup, reduced to the words that mean something.
const OBSERVE = () => {
    window.__timeline = [];
    let last = '';
    const read = () => {
        const q = s => document.querySelector(s);
        const snapshot = JSON.stringify({
            loading: !!q('[data-testid="setup-loading"]'),
            heading: q('.step-intro h2')?.textContent?.trim() ?? '',
            progress: q('.progress-label')?.textContent?.trim() ?? '',
            current: q('.progress [aria-current="step"]')?.textContent?.replace(/\s+/g, ' ').trim() ?? '',
            done: [...document.querySelectorAll('.progress .chip')].map(c => c.dataset.done === 'true' ? '1' : '0').join(''),
            notices: [...document.querySelectorAll('.onboarding .muted, .onboarding .error')].map(e => e.textContent.trim()).filter(t => /Reconnect|could not|Detecting|Checking|unavailable|No local runner/i.test(t)).join(' | '),
            dialog: !!q('[role="dialog"]'),
        });
        if (snapshot !== last) { last = snapshot; window.__timeline.push({ t: Math.round(performance.now()), ...JSON.parse(snapshot) }); }
    };
    new MutationObserver(read).observe(document, { childList: true, subtree: true, characterData: true, attributes: true });
    read();
};
(async () => {
    const base = 'http://127.0.0.1:' + await port();
    const repo = path.join(temp, 'repo'); fs.mkdirSync(repo);
    cp.execFileSync('git', ['init', '-b', 'main', repo], { stdio: 'ignore' });
    cp.execFileSync('git', ['-C', repo, '-c', 'user.name=Fixture', '-c', 'user.email=f@example.test', 'commit', '--allow-empty', '-m', 'init'], { stdio: 'ignore' });
    const fixture = path.join(temp, 'fixture');
    cp.execFileSync('go', ['build', '-o', fixture, './scripts/browser-fixture'], { cwd: root, stdio: 'inherit' });
    const child = cp.spawn(fixture, [], { cwd: root, stdio: ['ignore', 'ignore', 'inherit'], env: { ...process.env, WERKBORD_BROWSER_EXECUTION: '1', WERKBORD_BROWSER_ADDR: new URL(base).host } });
    const browser = await chromium.launch({ headless: true });
    try {
        for (let i = 0; i < 150; i++) { if (await fetch(base + '/api/health').then(r => r.ok).catch(() => false)) break; await sleep(100); }
        const api = async (method, p, data) => { const r = await fetch(base + '/api' + p, { method, headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' }, body: data ? JSON.stringify(data) : undefined }); assert(r.ok, p + ' ' + r.status); return r.json(); };
        const failures = [];
        // A slow answer for each thing setup asks about, one at a time and all together, as a slow disk or a cold start would give.
        const scenarios = {
            'no delay': {},
            'slow GitHub': { github: 900 },
            'slow phones': { phones: 900 },
            'slow agents': { agents: 1200 },
            'slow runners': { runners: 1200 },
            'slow everything': { github: 900, phones: 1100, agents: 1200, runners: 700 },
        };
        for (const [name, delays] of Object.entries(scenarios)) {
            const ctx = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
            const page = await ctx.newPage();
            await ctx.addInitScript(OBSERVE);
            for (const [key, ms] of Object.entries(delays)) await page.route(new RegExp(`/api/${key}($|\\?)`), async route => { await sleep(ms); await route.continue(); });
            await page.goto(base + '/#token=' + token);
            const intro = page.getByRole('dialog', { name: 'Let’s set up Werkbord', exact: true });
            await intro.waitFor();
            if (name === 'no delay') await page.waitForTimeout(500);
            await intro.getByRole('button', { name: 'Start guided setup', exact: true }).click();
            await page.waitForFunction(() => document.querySelector('.step-intro h2') && !document.querySelector('[data-testid="setup-loading"]'), null, { timeout: 15000 });
            await page.waitForTimeout(1500);
            const timeline = await page.evaluate(() => window.__timeline);
            // Everything a person could read while the screen was not saying it is loading.
            const shown = timeline.filter(s => !s.loading && s.heading);
            const distinct = key => [...new Set(shown.map(s => s[key]))];
            const finalState = timeline[timeline.length - 1];
            console.log(`\n${name}: ${timeline.length} states`);
            for (const s of timeline) console.log(`  ${String(s.t).padStart(5)}ms ${s.loading ? '[loading] ' : ''}${s.dialog ? '[dialog] ' : ''}${s.heading || '-'} | ${s.progress.slice(0, 58) || '-'} | done=${s.done} | ${s.current.slice(0, 30)}${s.notices ? ' | ' + s.notices.slice(0, 90) : ''}`);
            if (!timeline.some(s => s.loading)) console.log('  (no loading screen was needed: every answer arrived before the first paint of the steps)');
            if (distinct('heading').length > 1) failures.push(`${name}: the step shown changes by itself: ${distinct('heading').join(' -> ')}`);
            if (distinct('done').length > 1) failures.push(`${name}: a step's done mark changes after it is shown: ${distinct('done').join(' -> ')}`);
            if (distinct('progress').length > 1) failures.push(`${name}: the progress line changes after it is shown: ${distinct('progress').join(' -> ')}`);
            // A notice that is shown and later taken back.
            const noticesOf = s => s.notices.split(' | ').filter(Boolean);
            const lasting = new Set(noticesOf(finalState));
            for (const s of shown) for (const n of noticesOf(s)) if (!lasting.has(n) && !/^Checking…$/.test(n)) failures.push(`${name}: the notice "${n.slice(0, 70)}" is shown and then taken back`);
            await ctx.close();
        }
        console.log('\n' + (failures.length ? 'FLICKER:\n' + failures.join('\n') : 'no flicker'));
        if (process.env.SETTLE_REPORT_ONLY !== '1') assert.deepEqual(failures, []);
        if (!failures.length) console.log('PASS setup settles once and stays settled under slow answers');
    } finally { await browser.close(); child.kill('SIGTERM'); fs.rmSync(temp, { recursive: true, force: true }); }
})().catch(e => { console.error(e); process.exitCode = 1; });

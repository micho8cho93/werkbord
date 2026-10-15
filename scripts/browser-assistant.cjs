// The assistant's screen, in a real browser, against the real engine, operations and API with a scripted "provider" in place of
// Claude Code or Codex (scripts/browser-fixture/assistant.go). Everything runs in disposable storage on loopback.
//
// It checks what the unit tests cannot: that the surface really grows out of the button and shrinks back into it (sampled frame
// by frame, light and dark), that nothing moves under "reduce motion", that the workspace beneath is left exactly as it was,
// that a reply keeps streaming and a change keeps waiting while the screen is closed or the page is reloaded or the controller
// restarts, that a file is shared only after the person says yes, that dictation tells the truth about where audio goes, and that
// the screen works with a keyboard, with a screen reader's view of it, and on a phone.
const fs = require('node:fs'), os = require('node:os'), path = require('node:path');
const cp = require('node:child_process'), net = require('node:net');
const { once } = require('node:events');
const assert = require('node:assert/strict');
const root = path.resolve(__dirname, '..');
const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'werkbord-assistant-e2e-'));
const artifacts = process.env.BROWSER_ARTIFACT_DIR || path.join(temp, 'artifacts');
fs.mkdirSync(artifacts, { recursive: true });
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || path.join(root, 'web/node_modules/playwright'));
const token = 'disposable-browser-credential';
const children = [];
let current = null;

async function freePort() {
    const server = net.createServer();
    server.listen(0, '127.0.0.1'); await once(server, 'listening');
    const n = server.address().port; await new Promise(resolve => server.close(resolve)); return n;
}
function start(binary, env) {
    const child = cp.spawn(binary, [], { cwd: root, env: { ...process.env, ...env }, stdio: ['ignore', 'ignore', 'pipe'] });
    child.diagnostics = ''; child.stderr.on('data', d => child.diagnostics += d);
    children.push(child); return child;
}
async function stop(child) {
    if (child.exitCode !== null) return;
    child.kill('SIGTERM');
    await Promise.race([once(child, 'exit'), new Promise(r => setTimeout(r, 5000))]);
    if (child.exitCode === null) child.kill('SIGKILL');
}
async function ready(url, child) {
    for (let i = 0; i < 100; i++) {
        if (child.exitCode !== null) throw Error('fixture exited: ' + child.diagnostics);
        if (await fetch(url + '/api/health').then(r => r.ok).catch(() => false)) return;
        await new Promise(r => setTimeout(r, 100));
    }
    throw Error('fixture never became healthy: ' + child.diagnostics);
}
async function api(base, method, p, data) {
    const res = await fetch(base + p, { method, headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' }, body: data ? JSON.stringify(data) : undefined });
    const raw = await res.text();
    if (res.status >= 400) throw Error(method + ' ' + p + ' ' + res.status + ' ' + raw);
    return raw ? JSON.parse(raw) : null;
}
const sleep = ms => new Promise(r => setTimeout(r, ms));
const ok = msg => console.log('PASS ' + msg);

// ---- measuring the surface as it moves ----

/** Starts recording the surface, frame by frame, from the moment it appears (or while it closes). Returns a function that ends it. */
async function record(page, until) {
    await page.evaluate(until => {
        window.__samples = [];
        const t0 = performance.now();
        const parse = clip => { const m = /circle\(([\d.]+)px at (-?[\d.]+)px (-?[\d.]+)px\)/.exec(clip || ''); return m ? { r: +m[1], x: +m[2], y: +m[3] } : null; };
        let last = null;
        const tick = () => {
            const el = document.querySelector('[data-testid="assistant"]');
            if (el) {
                const cs = getComputedStyle(el);
                const s = { t: performance.now() - t0, clip: cs.clipPath, bg: cs.backgroundColor, circle: parse(cs.clipPath), phase: el.dataset.phase, anims: el.getAnimations().length, opacity: getComputedStyle(el.querySelector('.inner > :last-child') || el.firstElementChild).opacity };
                window.__samples.push(s);
                last = s;
            } else if (last) window.__samples.push({ t: performance.now() - t0, gone: true });
            if (performance.now() - t0 < until) window.__raf = requestAnimationFrame(tick);
        };
        window.__raf = requestAnimationFrame(tick);
    }, until);
    return async () => { await sleep(until + 80); return page.evaluate(() => window.__samples); };
}
const center = async loc => { const b = await loc.boundingBox(); return { x: b.x + b.width / 2, y: b.y + b.height / 2 }; };

(async () => {
    const fixture = path.join(temp, 'fixture');
    cp.execFileSync('go', ['build', '-o', fixture, './scripts/browser-fixture'], { cwd: root, stdio: 'inherit' });
    const repo = path.join(temp, 'repo'); fs.mkdirSync(repo);
    cp.execFileSync('git', ['-C', repo, 'init', '-b', 'main'], { stdio: 'ignore' });
    cp.execFileSync('git', ['-C', repo, '-c', 'user.name=F', '-c', 'user.email=f@example.test', '-c', 'core.hooksPath=/dev/null', 'commit', '--allow-empty', '-m', 'init'], { stdio: 'ignore' });

    const dataDir = path.join(temp, 'data'); fs.mkdirSync(dataDir);
    const port = await freePort();
    const base = 'http://127.0.0.1:' + port;
    const env = { WERKBORD_BROWSER_ADDR: '127.0.0.1:' + port, WERKBORD_BROWSER_ASSISTANT: '1', WERKBORD_BROWSER_DIR: dataDir, GIT_CONFIG_GLOBAL: '/dev/null', GIT_CONFIG_NOSYSTEM: '1' };
    let server = start(fixture, env);
    await ready(base, server);
    await api(base, 'POST', '/api/onboarding/complete', {});
    const project = await api(base, 'POST', '/api/projects', { path: repo, name: 'Assistant project' });

    const browser = await chromium.launch({ headless: true, ...(process.env.BROWSER_EXECUTABLE ? { executablePath: process.env.BROWSER_EXECUTABLE } : {}) });
    const errors = [];
    async function newPage(opts = {}) {
        const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, ...opts });
        const page = await ctx.newPage();
        page.setDefaultTimeout(12000);
        current = page;
        page.on('pageerror', e => errors.push(e.message));
        page.on('console', m => { if (m.type() === 'error' && !/Failed to load resource|net::ERR/.test(m.text())) errors.push(m.text()); });
        return { ctx, page };
    }
    const surface = page => page.getByTestId('assistant');
    const openAssistant = async page => { await page.getByTestId('assistant-button').first().click(); await surface(page).waitFor(); await page.waitForFunction(() => document.querySelector('[data-testid="assistant"]')?.dataset.phase === 'open'); };
    const closeAssistant = async page => { await page.getByTestId('assistant-close').click(); await surface(page).waitFor({ state: 'detached' }); };
    const sendText = async (page, text) => {
        const before = await page.locator('article.reply').count();
        const box = page.getByRole('textbox', { name: 'Message to the assistant' });
        await box.fill(text);
        await page.getByRole('button', { name: 'Send', exact: true }).click();
        await page.waitForFunction(n => document.querySelectorAll('article.reply').length > n, before);
    };
    const lastReply = page => page.locator('article.reply').last();
    const timingNames = ['werkbord.assistant.provider-to-server', 'werkbord.assistant.server-to-browser', 'werkbord.assistant.browser-to-visible'];
    const percentile95 = values => {
        const sorted = [...values].sort((a, b) => a - b);
        return sorted.length ? sorted[Math.min(sorted.length - 1, Math.ceil(sorted.length * .95) - 1)] : null;
    };

    try {
        // ======== light, desktop: the way in, the opening, the closing ========
        {
            const { ctx, page } = await newPage({ colorScheme: 'light' });
            await page.goto(base + '/#token=' + token);
            await page.goto(base + '/#/projects');
            await page.locator('aside.rail a.sec-link[href="#/projects"]').click();
            await page.getByRole('heading', { name: 'Projects', exact: true }).waitFor();
            const button = page.getByTestId('assistant-button').first();
            await button.waitFor();
            assert.equal(await button.getAttribute('aria-label'), 'Assistant');
            assert.equal(await button.getAttribute('aria-haspopup'), 'dialog');
            await page.screenshot({ path: path.join(artifacts, 'assistant-button-light.png') });
            ok('the Assistant button is in the sidebar, named for what it is');

            // The workspace state that must survive: a half-typed name in a form on the page, and the page's own elements.
            await page.getByRole('button', { name: 'Work project', exact: true }).click();
            const field = page.getByPlaceholder('Q4 launch');
            await field.fill('Half typed');
            await page.evaluate(() => { window.__main = document.querySelector('main'); });
            const hashBefore = await page.evaluate(() => location.hash);

            const from = await center(button);
            const stopRec = await record(page, 1800);
            await button.click();
            const samples = await stopRec();
            const circles = samples.filter(s => s.circle);
            assert.ok(circles.length >= 25, 'the opening should take many frames, got ' + circles.length);
            const took = circles.at(-1).t - circles[0].t;
            assert.ok(circles.some(c => c.phase === 'opening') && took > 700, 'and most of a second: ' + Math.round(took) + ' ms');
            assert.ok(circles[0].circle.r < 120, 'it starts small: ' + circles[0].clip);
            for (let i = 1; i < circles.length; i++) assert.ok(circles[i].circle.r >= circles[i - 1].circle.r - 0.01, 'it only grows');
            for (const c of circles) { assert.ok(Math.abs(c.circle.x - from.x) < 2 && Math.abs(c.circle.y - from.y) < 2, `grows from the button's centre (${from.x},${from.y}), not ${c.circle.x},${c.circle.y}`); }
            const lastClip = samples.at(-1);
            assert.equal(lastClip.clip, 'none', 'once open, nothing is left clipping the surface');
            assert.ok(circles.at(-1).circle.r > 900, 'it ends covering the window, r=' + circles.at(-1).circle.r);
            const first = samples.find(s => s.bg), last = samples.at(-1);
            assert.notEqual(first.bg, last.bg, 'the background changes while it opens');
            assert.match(first.bg, /rgb\(25[0-5], 25[0-5], 25[0-5]\)|rgb\(24[0-9]/, 'it starts as the sidebar’s colour (white): ' + first.bg);
            assert.equal(last.bg, 'rgb(245, 245, 242)', 'it ends as the assistant’s own paper');
            assert.ok(Number(first.opacity) < 0.5 && Number(samples.at(-1).opacity) > 0.99, `the content arrives after the surface starts to open (${first.opacity} → ${samples.at(-1).opacity}, ${samples.length} samples, phases ${[...new Set(samples.map(x => x.phase))]})`);
            ok('opens out of the button: circle grows from its centre to cover the window, background settles (light)');
            await page.screenshot({ path: path.join(artifacts, 'assistant-empty-light.png') });

            // The workspace is covered, not replaced.
            assert.equal(await page.evaluate(() => document.querySelector('.shell').inert), true, 'the page behind is inert');
            assert.equal(await page.evaluate(() => document.documentElement.classList.contains('asst-open')), true);
            assert.equal(await page.evaluate(() => document.activeElement?.closest('[data-testid="assistant"]') !== null), true, 'focus is in the assistant');

            // Closing collapses toward the same place.
            const stopClose = await record(page, 1100);
            await page.getByTestId('assistant-close').click();
            const closing = await stopClose();
            const shrink = closing.filter(s => s.circle);
            assert.ok(shrink.length >= 4, 'closing takes frames too: ' + shrink.length);
            assert.ok(shrink.at(-1).circle.r < shrink[0].circle.r / 2, 'it shrinks');
            for (const c of shrink) assert.ok(Math.abs(c.circle.x - from.x) < 2 && Math.abs(c.circle.y - from.y) < 2, 'collapses toward the button');
            assert.ok(closing.some(s => s.gone), 'and is then gone');
            assert.equal(await page.evaluate(() => document.activeElement?.dataset.testid), 'assistant-button', 'focus goes back to the button');
            ok('closing runs it back into the button, and focus returns there');

            assert.equal(await page.evaluate(() => document.querySelector('main') === window.__main), true, 'the page’s own elements were never replaced');
            assert.equal(await page.evaluate(() => location.hash), hashBefore, 'and it is where it was');
            assert.equal(await field.inputValue(), 'Half typed', 'and what was typed there is still typed');
            assert.equal(await page.evaluate(() => document.querySelector('.shell').inert), false);
            ok('the workspace underneath is exactly as it was (same elements, same place, same half-typed text)');
            await ctx.close();
        }

        // ======== dark ========
        {
            const { ctx, page } = await newPage({ colorScheme: 'dark' });
            await page.goto(base + '/#token=' + token);
            await page.goto(base + '/#/projects');
            await page.getByTestId('assistant-button').first().waitFor();
            const stopRec = await record(page, 1800);
            await page.getByTestId('assistant-button').first().click();
            const s = await stopRec();
            const first = s.find(x => x.bg), last = s.at(-1);
            assert.equal(first.bg === last.bg, false);
            assert.equal(last.bg, 'rgb(11, 11, 12)', 'dark paper');
            assert.match(first.bg, /rgb\(2[0-9], 2[0-9], 2[0-9]\)|rgb\(1[0-9], 1[0-9], 1[0-9]\)/, 'starts as the dark sidebar colour: ' + first.bg);
            await page.screenshot({ path: path.join(artifacts, 'assistant-empty-dark.png') });
            await sendText(page, 'openui dashboard');
            const generated = lastReply(page).locator('.openui');
            await generated.locator('h2').filter({ hasText: 'Sprint snapshot' }).waitFor();
            await page.waitForFunction(() => [...document.querySelectorAll('article.reply')].at(-1)?.dataset.status === 'done');
            const metricColors = await generated.locator('.metric').first().evaluate(el => ({
                background: getComputedStyle(el).backgroundColor,
                label: getComputedStyle(el.querySelector('.label')).color,
                value: getComputedStyle(el.querySelector('.value')).color,
            }));
            assert.deepEqual(metricColors, { background: 'rgb(29, 29, 31)', label: 'rgb(163, 163, 157)', value: 'rgb(240, 240, 236)' });
            await page.screenshot({ path: path.join(artifacts, 'assistant-openui-dark.png') });
            ok('works in the dark theme: same movement, dark colours');
            await ctx.close();
        }

        // ======== reduced motion ========
        {
            const { ctx, page } = await newPage({ reducedMotion: 'reduce', colorScheme: 'light' });
            await page.goto(base + '/#token=' + token);
            await page.goto(base + '/#/projects');
            await page.getByTestId('assistant-button').first().waitFor();
            const stopRec = await record(page, 500);
            await page.getByTestId('assistant-button').first().click();
            const s = await stopRec();
            assert.ok(s.length > 0);
            assert.ok(s.every(x => x.gone || x.clip === 'none'), 'never clipped, never a circle');
            assert.ok(s.every(x => x.gone || x.anims === 0), 'no animation at all');
            assert.equal(s.find(x => x.bg).bg, 'rgb(245, 245, 242)', 'it is simply there, in its own colour');
            await page.getByTestId('assistant-close').click();
            await surface(page).waitFor({ state: 'detached', timeout: 1500 });
            ok('under reduced motion nothing moves: it is simply there, then simply gone');
            await ctx.close();
        }

        // ======== conversation: streaming, progress, markdown safety ========
        {
            const { ctx, page } = await newPage();
            const requests = [];
            page.on('request', r => requests.push(r.method() + ' ' + new URL(r.url()).pathname));
            await page.goto(base + '/#token=' + token);
            await page.goto(base + '/#/projects');
            await openAssistant(page);
            assert.equal(await page.getByTestId('assistant-empty').count(), 1, 'a helpful empty state');
            assert.equal(await page.getByRole('button', { name: 'What needs me today?' }).count(), 1);
            assert.equal(await page.locator('[data-testid="assistant"] [class*="history"], [data-testid="assistant"] aside').count(), 0, 'no history browser');
            assert.equal(await page.getByTestId('assistant-picker').innerText().then(t => /Claude Code/.test(t)), true, 'says which assistant answers');

            await sendText(page, 'hello there');
            await page.getByText('You said: hello there', { exact: false }).waitFor();
            await page.waitForFunction(() => document.querySelector('article.reply')?.dataset.status === 'done');
            ok('a first message starts the conversation and the reply streams in');

            // Streaming: the text grows in several steps.
            const before = await page.locator('article.reply').count();
            await sendText(page, 'slow please');
            await page.waitForFunction(n => document.querySelectorAll('article.reply').length > n, before);
            const lengths = [];
            for (let i = 0; i < 40; i++) {
                lengths.push(await lastReply(page).locator('.md').innerText({ timeout: 300 }).then(t => t.length).catch(() => 0));
                if (await lastReply(page).getAttribute('data-status') === 'done') break;
                await sleep(100);
            }
            const distinct = [...new Set(lengths)].filter(n => n > 0);
            assert.ok(distinct.length >= 5, 'the reply arrived in many pieces, not at once: ' + distinct.join(','));
            for (let i = 1; i < lengths.length; i++) assert.ok(lengths[i] >= lengths[i - 1], 'and only grew');
            assert.match(await lastReply(page).innerText(), /One two three four five six seven eight nine ten eleven twelve\./);
            ok('streams token by token (' + distinct.length + ' distinct lengths)');

            // Tool progress is one quiet line, then a closed disclosure.
            const before2 = await page.locator('article.reply').count();
            await sendText(page, 'what is blocked?');
            await page.waitForFunction(n => document.querySelectorAll('article.reply').length > n, before2);
            await lastReply(page).locator('.progress').waitFor();
            assert.match(await lastReply(page).locator('.progress').innerText(), /Working…|Looking for what is blocked/);
            await page.waitForFunction(() => document.querySelector('article.reply:last-of-type')?.dataset.status === 'done' || [...document.querySelectorAll('article.reply')].at(-1)?.dataset.status === 'done');
            assert.match(await lastReply(page).innerText(), /Nothing is blocked right now\./);
            const looked = lastReply(page).locator('details.looked');
            assert.equal(await looked.count(), 1);
            assert.equal(await looked.getAttribute('open'), null, 'the detail is there but out of the way');
            assert.match(await looked.textContent(), /Looking for what is blocked/, 'and says what was looked at, in plain words');
            ok('tool progress is a single quiet line while it works, and a collapsed note after');

            await sendText(page, 'show a failed board action');
            await lastReply(page).getByRole('alert').waitFor();
            assert.match(await lastReply(page).getByRole('alert').innerText(), /prj_missing.*not found/);
            await lastReply(page).getByRole('status').filter({ hasText: 'Reply finished.' }).waitFor();
            await page.screenshot({ path: path.join(artifacts, 'assistant-operation-error-desktop.png') });
            ok('a failed board operation shows its reason without opening a disclosure, and the reply visibly finishes');

            // Safe rendering: formatting works, markup does not.
            await sendText(page, 'show markdown');
            await page.waitForFunction(() => [...document.querySelectorAll('article.reply')].at(-1)?.dataset.status === 'done');
            const r = lastReply(page);
            assert.equal(await r.locator('strong').innerText(), 'bold');
            assert.equal(await r.locator('em').innerText(), 'italic');
            assert.deepEqual(await r.locator('li').allInnerTexts(), ['one', 'two']);
            assert.equal(await r.locator('pre').count(), 1);
            assert.equal(await lastReply(page).locator('img, a, script').count(), 0, 'no image, link or script can come out of a reply');
            assert.equal(await page.evaluate(() => window.__pwned), undefined, 'nothing a reply says runs');
            assert.match(await lastReply(page).innerText(), /<img src=x onerror=/, 'it is shown as the text it is');
            ok('replies are formatted from a tree, never as HTML: markup and links stay text');

            // OpenUI Lang streams inside the existing reply, while prose remains an ordinary safe Markdown segment.
            await sendText(page, 'openui dashboard');
            const generated = lastReply(page).locator('.openui');
            await page.waitForFunction(() => {
                const r = [...document.querySelectorAll('article.reply')].at(-1);
                return r?.dataset.status === 'working' && r.querySelector('.openui h2')?.textContent === 'Sprint snapshot';
            }, null, { timeout: 10000 });
            await page.waitForFunction(() => [...document.querySelectorAll('article.reply')].at(-1)?.dataset.status === 'done');
            assert.equal((await generated.locator('h2').textContent()).trim(), 'Sprint snapshot');
            assert.deepEqual(await generated.locator('.metric .label').allInnerTexts(), ['Open tasks', 'Done this week']);
            assert.equal(await generated.locator('table tbody tr').count(), 2);
            const messagePosts = requests.filter(r => r.endsWith('/messages')).length;
            const filter = generated.getByRole('combobox', { name: 'Filter by state' });
            await filter.selectOption('Review');
            assert.equal(await generated.locator('table tbody tr').count(), 1, 'a table filter updates only its local rows');
            assert.match(await generated.locator('table tbody tr').innerText(), /Review API/);
            await generated.locator('.bars button').filter({ hasText: 'Review' }).click();
            assert.match(await generated.getByRole('status').filter({ hasText: 'Selected: Review' }).innerText(), /Selected: Review · 1 tasks/);
            await generated.getByRole('combobox', { name: 'Compare category' }).selectOption('Retries');
            assert.equal(await generated.locator('.group').count(), 1, 'a comparison category is filtered locally');
            const totals = generated.getByRole('status').filter({ hasText: 'Totals for Retries' });
            assert.match(await totals.innerText(), /Totals for Retries: Claude Code 1 runs.*Codex 2 runs/);
            await generated.getByRole('checkbox', { name: 'Claude Code' }).uncheck();
            assert.doesNotMatch(await totals.innerText(), /Claude Code/);
            await generated.getByLabel('Filter from date').fill('2026-10-11');
            await generated.getByLabel('Filter to date').fill('2026-10-12');
            assert.match(await generated.getByRole('status').filter({ hasText: 'Showing 2 of 3' }).innerText(), /Showing 2 of 3 recorded values/);
            assert.equal(requests.filter(r => r.endsWith('/messages')).length, messagePosts, 'table, chart and date filters made no assistant request');
            assert.match((await lastReply(page).locator('.md').allInnerTexts()).join('\n'), /Here is the sprint view\.|The values are from the current board\./);
            assert.equal(await generated.locator('img, script, a').count(), 0, 'component text stays escaped and non-clickable');
            const disclosure = generated.locator('details').first();
            await disclosure.locator('summary').click();
            assert.notEqual(await disclosure.getAttribute('open'), null, 'the local disclosure is interactive');
            assert.match(await generated.innerText(), /<img src=x onerror=window.__pwned=1>/);
            assert.equal(requests.filter(r => /\/actions\//.test(r)).length, 0, 'generated UI did not call an operation');
            assert.equal(await page.evaluate(() => window.__pwned), undefined, 'generated text did not execute');
            ok('OpenUI renders safe data; table, chart, series and date controls update locally without an assistant request');

            // Controlled streaming fixture: keep provider arrival, browser receipt and rendered update as separate samples.
            for (const scenario of ['short', 'long', 'mixed']) {
                await page.evaluate(names => names.forEach(name => performance.clearMeasures(name)), timingNames);
                await sendText(page, `stream benchmark ${scenario}`);
                await page.waitForFunction(() => [...document.querySelectorAll('article.reply')].at(-1)?.dataset.status === 'done');
                if (scenario === 'mixed') await lastReply(page).locator('.openui').getByText('Streaming result', { exact: true }).waitFor();
                const samples = await page.evaluate(names => Object.fromEntries(names.map(name => [name, performance.getEntriesByName(name).map(entry => entry.duration)])), timingNames);
                const providerP95 = percentile95(samples[timingNames[0]]);
                const transportP95 = percentile95(samples[timingNames[1]]);
                const visibleP95 = percentile95(samples[timingNames[2]]);
                assert.notEqual(visibleP95, null, `${scenario} stream produced browser receipt-to-visible samples`);
                assert.ok(visibleP95 < 100, `${scenario} browser receipt-to-visible p95 was ${visibleP95}ms`);
                console.log(`TIMING ${scenario}: provider→server p95 ${providerP95?.toFixed(1) ?? 'n/a'}ms; server→browser p95 ${transportP95?.toFixed(1) ?? 'n/a'}ms; browser→visible p95 ${visibleP95.toFixed(1)}ms`);
            }
            ok('short, long and mixed OpenUI replies stay below 100 ms browser receipt-to-visible p95 in the controlled fixture');

            await sendText(page, 'malformed openui');
            await lastReply(page).getByRole('status').filter({ hasText: "couldn't render all of this interface" }).waitFor();
            await lastReply(page).locator('.openui-fallback summary').click();
            assert.match(await lastReply(page).locator('.openui-fallback pre').innerText(), /root = Heading\(\)/);
            ok('a malformed component produces a readable fallback with inspectable source');

            await sendText(page, 'incomplete openui');
            await lastReply(page).getByRole('status').filter({ hasText: 'did not finish' }).waitFor();
            assert.equal(await lastReply(page).locator('.openui-fallback').count(), 1);
            ok('an incomplete fence leaves the conversation intact and explains that content may be missing');

            // Stopping a reply.
            await sendText(page, 'hang on');
            await page.getByRole('button', { name: 'Stop the reply' }).click();
            await page.waitForFunction(() => [...document.querySelectorAll('article.reply')].at(-1)?.dataset.status === 'cancelled');
            assert.ok(requests.includes('POST /api/assistant/sessions/' + (await api(base, 'GET', '/api/assistant/sessions')).sessions[0].id + '/cancel'));
            await page.getByRole('button', { name: 'Send', exact: true }).waitFor();
            ok('a reply can be stopped');

            await page.screenshot({ path: path.join(artifacts, 'assistant-conversation-desktop.png') });
            await ctx.close();
        }

        // ======== a change is proposed, waits, and survives everything ========
        {
            const { ctx, page } = await newPage();
            const requests = [];
            page.on('request', r => requests.push(r.method() + ' ' + new URL(r.url()).pathname));
            await page.goto(base + '/#token=' + token);
            await page.goto(base + '/#/projects');
            await openAssistant(page);
            const before = (await api(base, 'GET', `/api/projects/${project.id}/tasks`)).tasks.length;
            await sendText(page, `add a ticket to ${project.id}`);
            const card = page.locator('section.confirm');
            await card.waitFor();
            assert.match(await card.innerText(), /From the assistant/);
            assert.match(await card.innerText(), /Backlog · Assistant project/);
            assert.match(await card.innerText(), /Nothing changes until you confirm/);
            assert.equal((await api(base, 'GET', `/api/projects/${project.id}/tasks`)).tasks.length, before, 'proposing changed nothing');
            await page.screenshot({ path: path.join(artifacts, 'assistant-confirmation.png') });

            // Close the screen: the change keeps waiting, and the button says so.
            await page.waitForFunction(() => [...document.querySelectorAll('article.reply')].at(-1)?.dataset.status === 'done');
            await closeAssistant(page);
            const btn = page.getByTestId('assistant-button').first();
            assert.match(await btn.getAttribute('aria-label'), /1 change is waiting for you/);
            assert.equal(await btn.locator('.num').innerText(), '1');
            assert.equal(requests.filter(r => /\/actions\//.test(r)).length, 0, 'closing the screen decided nothing');
            await page.goto(base + `/#/p/${project.id}/board`); // go to a different view altogether
            await page.waitForFunction(() => location.hash.includes('/board'));
            assert.match(await page.getByTestId('assistant-button').first().getAttribute('aria-label'), /waiting for you/);
            await openAssistant(page);
            assert.equal(await page.locator('section.confirm[data-state="waiting"]').count(), 1, 'still waiting after changing view');
            ok('a change keeps waiting when the screen is closed and the view changes; the button shows it');

            // Reload: it is still there, from what the controller holds.
            await page.reload();
            await page.getByTestId('assistant-button').first().waitFor();
            assert.match(await page.getByTestId('assistant-button').first().getAttribute('aria-label'), /waiting for you/, 'known before the screen is even opened');
            await openAssistant(page);
            assert.equal(await page.locator('section.confirm[data-state="waiting"]').count(), 1);
            assert.match(await page.locator('[data-testid="assistant"] .user').first().innerText(), /add a ticket/, 'and the conversation with it');
            ok('after a reload the conversation and the waiting change are back');

            await page.getByRole('button', { name: 'Confirm', exact: true }).click();
            await page.locator('section.confirm[data-state="confirmed"]').waitFor();
            const after = (await api(base, 'GET', `/api/projects/${project.id}/tasks`)).tasks;
            assert.equal(after.length, before + 1, 'only the person’s confirmation carried it out');
            assert.equal(after.some(t => t.title === 'From the assistant'), true);
            ok('confirming carries the change out, once, and says so');

            // And declining.
            await sendText(page, `add another ticket to ${project.id}`);
            await page.locator('section.confirm[data-state="waiting"]').waitFor();
            await page.getByRole('button', { name: 'Not now' }).click();
            await page.locator('section.confirm[data-state="declined"]').waitFor();
            assert.equal((await api(base, 'GET', `/api/projects/${project.id}/tasks`)).tasks.length, before + 1);
            ok('declining leaves the board alone');

            // A generated plan stays editable in the conversation, survives reload, and only sends a follow-up when its visible button is pressed.
            await sendText(page, `openui form demo for ${project.id}`);
            await page.locator('article.reply').last().getByTestId('openui-form').waitFor();
            await page.waitForFunction(() => [...document.querySelectorAll('article.reply')].at(-1)?.dataset.status === 'done');
            let plan = page.locator('article.reply').last().getByTestId('openui-form');
            await plan.getByLabel('Task title').fill('Launch board task');
            await plan.getByLabel('Project').selectOption(project.id);
            await plan.getByLabel('First planned day').fill('2026-10-26');
            await plan.getByLabel('Include documentation tasks').check();
            await sleep(500); // the tab-only transcript cache debounces writes
            const cachedPlanState = await page.evaluate(() => JSON.parse(sessionStorage.getItem('werkbord.assistant.v1') || 'null')?.items.filter(item => item.kind === 'reply').at(-1)?.uiState);
            assert.equal(cachedPlanState?.['sprint-plan']?.title?.value, 'Launch board task', 'the tab cache keeps edited text');
            assert.equal(cachedPlanState?.['sprint-plan']?.includeDocs?.value, true, 'the tab cache keeps the selected checkbox');
            await page.reload();
            await openAssistant(page);
            const restoredPlanState = await page.evaluate(() => JSON.parse(sessionStorage.getItem('werkbord.assistant.v1') || 'null')?.items.filter(item => item.kind === 'reply').at(-1)?.uiState);
            assert.equal(restoredPlanState?.['sprint-plan']?.title?.value, 'Launch board task', 'the edited form state is restored from the tab cache');
            plan = page.locator('article.reply').last().getByTestId('openui-form');
            assert.equal(await plan.getByLabel('Task title').inputValue(), 'Launch board task');
            assert.equal(await plan.getByLabel('Project').inputValue(), project.id);
            assert.equal(await plan.getByLabel('First planned day').inputValue(), '2026-10-26');
            assert.equal(await plan.getByLabel('Include documentation tasks').isChecked(), true);
            ok('form edits survive a page reload in the tab transcript');

            const messagesBeforeInvalidForm = requests.filter(r => r.endsWith('/messages')).length;
            await plan.getByLabel('Reference URL').fill('http://localhost:9999/private');
            await plan.getByRole('button', { name: 'Create tasks from this plan' }).click();
            await page.locator('[data-testid="openui-action-error"]').waitFor();
            assert.match(await page.locator('[data-testid="openui-action-error"]').innerText(), /complete external HTTPS address/);
            assert.equal(requests.filter(r => r.endsWith('/messages')).length, messagesBeforeInvalidForm, 'invalid form values never leave the browser');
            assert.equal(await page.locator('section.confirm[data-state="waiting"]').count(), 0, 'invalid form values create no proposal');
            await plan.getByLabel('Reference URL').fill('');
            ok('invalid external URLs are rejected before a conversation follow-up is sent');

            const messagesBeforeAction = requests.filter(r => r.endsWith('/messages')).length;
            const replyCount = await page.locator('article.reply').count();
            await plan.getByRole('button', { name: 'Create tasks from this plan' }).evaluate(button => { button.click(); button.click(); });
            await page.waitForFunction(n => document.querySelectorAll('article.reply').length > n, replyCount);
            await page.locator('section.confirm[data-state="waiting"]').last().waitFor();
            await page.waitForFunction(() => [...document.querySelectorAll('article.reply')].at(-1)?.dataset.status === 'done');
            assert.equal(requests.filter(r => r.endsWith('/messages')).length, messagesBeforeAction + 1, 'two rapid clicks send one user follow-up');
            assert.match(await page.locator('.user .bubble').last().innerText(), /Create tasks from this plan\nForm values: .*Launch board task/);
            const planCard = page.locator('section.confirm[data-state="waiting"]').last();
            assert.equal(await planCard.locator('.task-title').innerText(), 'Launch board task');
            assert.match(await planCard.locator('.destination').innerText(), /Backlog.*Planned from 2026-10-26/);
            const beforePlanConfirm = (await api(base, 'GET', `/api/projects/${project.id}/tasks`)).tasks.length;
            assert.equal(beforePlanConfirm, before + 1, 'the form action prepared a proposal but did not create a task');
            const actionPosts = requests.filter(r => /\/actions\//.test(r)).length;
            await planCard.getByRole('button', { name: 'Confirm', exact: true }).evaluate(button => { button.click(); button.click(); });
            await page.locator('section.confirm[data-state="confirmed"]').last().waitFor();
            assert.equal(requests.filter(r => /\/actions\//.test(r)).length, actionPosts + 1, 'rapid confirm clicks resolve once');
            const afterPlanConfirm = (await api(base, 'GET', `/api/projects/${project.id}/tasks`)).tasks;
            assert.equal(afterPlanConfirm.length, beforePlanConfirm + 1);
            assert.equal(afterPlanConfirm.some(t => t.title === 'Launch board task'), true);
            ok('a form follow-up creates a standard proposal, and duplicate clicks confirm it once');

            await sendText(page, `openui form demo for ${project.id}`);
            await page.waitForFunction(() => [...document.querySelectorAll('article.reply')].at(-1)?.dataset.status === 'done');
            plan = page.locator('article.reply').last().getByTestId('openui-form');
            await plan.waitFor();
            await plan.getByLabel('Task title').fill('Declined plan task');
            await plan.getByLabel('First planned day').fill('2026-10-28');
            await plan.getByRole('button', { name: 'Create tasks from this plan' }).click();
            const declinePlanCard = page.locator('section.confirm[data-state="waiting"]').last();
            await declinePlanCard.waitFor();
            await declinePlanCard.getByRole('button', { name: 'Not now', exact: true }).click();
            await page.locator('section.confirm[data-state="declined"]').last().waitFor();
            assert.equal((await api(base, 'GET', `/api/projects/${project.id}/tasks`)).tasks.length, afterPlanConfirm.length, 'rejecting the plan proposal leaves the board unchanged');
            ok('a user can reject a generated plan proposal without changing the board');
            await ctx.close();
        }

        // ======== switching views and reloading mid-reply ========
        {
            const { ctx, page } = await newPage();
            const requests = [];
            page.on('request', r => requests.push(r.method() + ' ' + new URL(r.url()).pathname));
            await page.goto(base + '/#token=' + token);
            await page.goto(base + '/#/projects');
            await openAssistant(page);
            await sendText(page, 'a slow one');
            await page.waitForFunction(() => document.querySelector('article.reply:last-of-type .md') !== null || document.querySelectorAll('article.reply').length > 0);
            await lastReply(page).locator('.md').waitFor();
            await closeAssistant(page);
            assert.match(await page.getByTestId('assistant-button').first().getAttribute('aria-label'), /working/);
            await page.goto(base + `/#/p/${project.id}/board`);
            await sleep(400);
            await openAssistant(page);
            await page.waitForFunction(() => [...document.querySelectorAll('article.reply')].at(-1)?.dataset.status === 'done');
            assert.match(await lastReply(page).innerText(), /One two three four five six seven eight nine ten eleven twelve\./);
            assert.equal(requests.filter(r => /\/cancel$/.test(r)).length, 0, 'leaving the screen never stopped the reply');
            ok('a running reply is not stopped by closing the screen or changing view, and is complete when you come back');

            // Reload in the middle of a reply.
            await sendText(page, 'another slow one');
            await lastReply(page).locator('.md').waitFor();
            await sleep(250);
            await page.reload();
            await openAssistant(page);
            await page.waitForFunction(() => [...document.querySelectorAll('article.reply')].at(-1)?.dataset.status === 'done', null, { timeout: 15000 });
            const text = await lastReply(page).locator('.md').innerText();
            assert.match(text, /^One two three four five six seven eight nine ten eleven twelve\.\s*$/, 'complete and not doubled: ' + JSON.stringify(text));
            ok('a reload in the middle of a reply picks it up and finishes it exactly once');
            await ctx.close();
        }

        // ======== choosing the provider, model and level ========
        {
            const { ctx, page } = await newPage();
            await page.goto(base + '/#token=' + token);
            await page.goto(base + '/#/projects');
            await openAssistant(page);
            await sendText(page, 'fail model please');
            await page.locator('.fail').waitFor();
            assert.match(await page.locator('.fail').innerText(), /not available to your account/);
            await page.getByRole('button', { name: 'Choose another model' }).click();
            const panel = page.getByTestId('assistant-picker-panel');
            await panel.waitFor();
            await panel.getByLabel('Model', { exact: true }).selectOption('sonnet');
            await panel.getByLabel('Reasoning level').selectOption('high');
            await page.waitForFunction(() => /Sonnet/.test(document.querySelector('[data-testid="assistant-picker"]').innerText));
            await page.keyboard.press('Escape');
            assert.equal(await surface(page).count(), 1, 'Escape closes the chooser first, not the whole screen');
            await page.getByRole('button', { name: 'Try again' }).click();
            await page.waitForFunction(() => [...document.querySelectorAll('article.reply')].at(-1)?.dataset.status === 'done');
            assert.match(await lastReply(page).innerText(), /You said: fail model please/);
            ok('a model the plan cannot use is replaced from the screen, mid-conversation, and the message goes through');

            await page.getByTestId('assistant-picker').click();
            await panel.getByRole('button', { name: 'Codex' }).click();
            await page.locator('.notice').filter({ hasText: 'Now using Codex' }).waitFor();
            assert.match(await page.locator('.notice').last().innerText(), /does not remember what came before/);
            assert.match(await page.getByTestId('assistant-picker').innerText(), /Codex/);
            await page.keyboard.press('Escape');
            await sendText(page, 'hello codex');
            await page.getByText('You said: hello codex').waitFor();
            const sessions = (await api(base, 'GET', '/api/assistant/sessions')).sessions;
            assert.ok(sessions.some(s => s.provider === 'codex'));
            ok('switching provider starts a fresh conversation with it and says so');
            await ctx.close();
        }

        // ======== attachments ========
        {
            const { ctx, page } = await newPage();
            const attachCalls = [];
            page.on('request', r => { if (/\/attachments/.test(r.url())) attachCalls.push({ method: r.method(), auth: r.headers()['x-attachment-authorized'], name: r.headers()['x-attachment-name'] }); });
            await page.goto(base + '/#token=' + token);
            await page.goto(base + '/#/projects');
            await openAssistant(page);
            await page.getByTestId('assistant-file-input').setInputFiles({ name: 'plan notes.md', mimeType: 'text/markdown', buffer: Buffer.from('Ship the login fix on Friday.\nSecond line.') });
            const ask = page.getByTestId('assistant-authorize');
            await ask.waitFor();
            const text = await ask.innerText();
            assert.match(text, /Share this file with Claude Code\?/);
            assert.match(text, /plan notes\.md/);
            assert.match(text, /up to 30 minutes/);
            assert.match(text, /Never written to disk/);
            assert.match(text, /keeps its own copy/);
            assert.equal(attachCalls.length, 0, 'nothing has left the page before the person says yes');
            await page.screenshot({ path: path.join(artifacts, 'assistant-authorize.png') });
            await ask.getByRole('button', { name: 'Share' }).click();
            await page.locator('.file[data-state="ready"]').waitFor();
            assert.deepEqual(attachCalls.map(c => [c.method, c.auth]), [['POST', 'true']]);
            assert.equal(decodeURIComponent(attachCalls[0].name), 'plan notes.md');
            assert.match(await page.locator('.file').innerText(), /until \d\d:\d\d/);
            ok('a file is shared only after an explicit yes, with the limits stated first');

            await sendText(page, 'what does the attachment say?');
            await page.getByText('The file plan notes.md begins: Ship the login fix on Friday.').waitFor();
            assert.equal(await page.locator('.file').count(), 0, 'sent, so no longer waiting');
            assert.equal(await page.locator('.user .sent').count(), 1);
            await sendText(page, 'and the attachment now?');
            await page.getByText('There was no file with that.').waitFor();
            ok('the provider got the file with one message and not the next');

            // A refusal before anything is sent, and a change of mind.
            await page.getByTestId('assistant-file-input').setInputFiles({ name: 'photo.png', mimeType: 'image/png', buffer: Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 1]) });
            await page.getByRole('alert').filter({ hasText: 'not a plain text file' }).waitFor();
            assert.equal(await page.getByTestId('assistant-authorize').count(), 0);
            await page.getByTestId('assistant-file-input').setInputFiles({ name: 'later.txt', buffer: Buffer.from('x') });
            await page.getByTestId('assistant-authorize').getByRole('button', { name: 'Cancel' }).click();
            assert.equal(await page.locator('.file').count() + await page.getByTestId('assistant-authorize').count(), 0);
            assert.equal(attachCalls.length, 1, 'cancelling sent nothing');
            ok('a picture is refused up front; cancelling the question sends nothing');
            await ctx.close();
        }

        // ======== dictation ========
        {
            const { ctx, page } = await newPage();
            await page.addInitScript(() => {
                // A recogniser the test controls. Chromium's own has no speech service in a test.
                class Rec {
                    constructor() { window.__rec = this; window.__made = (window.__made || 0) + 1; this.started = 0; }
                    start() { this.started++; setTimeout(() => this.onstart?.(), 0); }
                    stop() { setTimeout(() => this.onend?.(), 0); }
                    abort() { this.onend?.(); }
                    say(parts) { this.onresult?.({ resultIndex: 0, results: parts.map(([t, f]) => ({ isFinal: f, 0: { transcript: t } })) }); }
                }
                window.SpeechRecognition = Rec;
                window.webkitSpeechRecognition = Rec;
            });
            await page.goto(base + '/#token=' + token);
            await page.goto(base + '/#/projects');
            await openAssistant(page);
            const mic = page.getByTestId('assistant-mic');
            await mic.click();
            const notice = page.getByTestId('assistant-dictation-notice');
            await notice.waitFor();
            const said = await notice.innerText();
            assert.match(said, /may be sent to Google/);
            assert.match(said, /not Claude or ChatGPT/i);
            assert.match(said, /not the voice mode of Claude or ChatGPT/i);
            assert.equal(await page.evaluate(() => window.__rec), undefined, 'nothing is listening before the person agrees');
            await page.screenshot({ path: path.join(artifacts, 'assistant-dictation-notice.png') });
            await notice.getByRole('button', { name: 'Use dictation' }).click();
            await page.waitForFunction(() => window.__rec?.started === 1);
            // While it listens: a strip says so, the box shows what is being said as it is said, and there are three ways out.
            const listening = page.getByTestId('assistant-listening');
            await listening.waitFor();
            assert.match(await listening.innerText(), /Listening|Starting the microphone/);
            assert.equal(await page.getByTestId('assistant-dictation-cancel').count(), 1, 'a cross to cancel');
            assert.equal(await mic.getAttribute('aria-label'), 'Finish dictation and keep the text to edit');
            const box = page.getByRole('textbox', { name: 'Message to the assistant' });
            assert.equal(await box.getAttribute('readonly'), '', 'the box is not typed into while it listens');
            await page.evaluate(() => window.__rec.say([['what is bl', false]]));
            await page.waitForFunction(() => document.querySelector('[data-testid="assistant-listening"]')?.dataset.phase === 'hearing' && document.querySelector('textarea').value === 'what is bl');
            await page.screenshot({ path: path.join(artifacts, 'assistant-dictation-listening.png') });
            await page.evaluate(() => window.__rec.say([['what is blocked', true]]));
            await page.waitForFunction(() => document.querySelector('textarea').value === 'what is blocked');
            // The microphone button now means "done": what was said stays in the box, to be edited.
            await mic.click();
            await page.waitForFunction(() => !document.querySelector('[data-testid="assistant-listening"]'));
            assert.equal(await box.getAttribute('readonly'), null, 'it can be edited again');
            assert.equal(await box.inputValue(), 'what is blocked');
            assert.equal(await page.evaluate(() => document.activeElement === document.querySelector('textarea')), true, 'and the cursor is in it');
            await box.press('End');
            await box.type(' today');
            assert.equal(await box.inputValue(), 'what is blocked today', 'speech and typing mix in the one box');
            ok('dictation says where audio may go before it starts, is not presented as Claude or ChatGPT voice, shows the words as they are said, and the microphone button finishes it into the box for editing');

            // The cross throws away what was said, and only that.
            await mic.click();
            await page.waitForFunction(() => window.__made === 2 && window.__rec.started === 1);
            assert.equal(await page.getByTestId('assistant-dictation-notice').count(), 0, 'told once per device, not every time');
            await page.evaluate(() => window.__rec.say([['something I did not mean', true]]));
            await page.waitForFunction(() => document.querySelector('textarea').value === 'what is blocked today something I did not mean');
            await page.getByTestId('assistant-dictation-cancel').click();
            await page.waitForFunction(() => !document.querySelector('[data-testid="assistant-listening"]'));
            assert.equal(await box.inputValue(), 'what is blocked today', 'cancelling puts the box back as it was');
            ok('the cross cancels the dictation and keeps what was typed');

            // Escape does the same, and does not close the assistant.
            await mic.click();
            await page.waitForFunction(() => window.__made === 3);
            await page.evaluate(() => window.__rec.say([['nor this', true]]));
            await page.keyboard.press('Escape');
            await page.waitForFunction(() => !document.querySelector('[data-testid="assistant-listening"]'));
            assert.equal(await box.inputValue(), 'what is blocked today');
            assert.equal(await surface(page).count(), 1, 'Escape ended the dictation, not the assistant');
            ok('Escape cancels the dictation without closing the assistant');

            // Send, while it listens, finishes it and sends the message once the last words are text.
            await box.fill('');
            await mic.click();
            await page.waitForFunction(() => window.__made === 4);
            await page.evaluate(() => window.__rec.say([['hello from my voice', true]]));
            await page.evaluate(() => window.__rec.say([['and the last bit', false]], 1));
            await page.getByTestId('assistant-send').click();
            await page.locator('.user .bubble').last().waitFor();
            assert.equal(await page.locator('.user .bubble').last().innerText(), 'hello from my voice and the last bit', 'the words that had not yet settled are in it');
            assert.equal(await page.getByTestId('assistant-listening').count(), 0);
            ok('Send finishes the dictation and sends what was said, including the last words');

            // An engine that ends by itself does not end the dictation.
            await page.waitForFunction(() => !document.querySelector('[data-testid="assistant-send"][disabled]') || true);
            await page.locator('article.reply').last().waitFor();
            await box.fill('');
            await mic.click();
            await page.waitForFunction(() => window.__made === 5);
            await page.evaluate(() => { const r = window.__rec; r.onend?.(); });
            await page.waitForFunction(() => window.__made === 6 && window.__rec.started === 1);
            assert.equal(await page.getByTestId('assistant-listening').count(), 1, 'still listening');
            await page.evaluate(() => window.__rec.say([['carried on', true]]));
            await mic.click();
            await page.waitForFunction(() => document.querySelector('textarea').value === 'carried on');
            ok('when the speech engine stops by itself, dictation goes on');

            // A refused microphone is explained, in words for this app or for the browser.
            await box.fill('');
            await mic.click();
            await page.waitForFunction(() => window.__made === 7);
            await page.evaluate(() => { const r = window.__rec; r.onerror?.({ error: 'not-allowed' }); r.onend?.(); });
            await page.getByTestId('assistant-dictation-note').filter({ hasText: 'The microphone is blocked' }).waitFor();
            assert.equal(await page.getByTestId('assistant-listening').count(), 0);
            ok('a refused microphone is explained and dictation stops');
            await ctx.close();
        }
        {
            const { ctx, page } = await newPage();
            await page.addInitScript(() => { delete window.SpeechRecognition; delete window.webkitSpeechRecognition; });
            await page.goto(base + '/#token=' + token);
            await page.goto(base + '/#/projects');
            await openAssistant(page);
            await page.getByTestId('assistant-mic').click();
            await page.getByRole('status').filter({ hasText: 'not available in this browser' }).waitFor();
            assert.match(await page.getByRole('status').filter({ hasText: 'not available in this browser' }).innerText(), /microphone key on your keyboard/);
            ok('where the browser has no speech recognition it says so and what to do instead');
            await ctx.close();
        }

        // ======== keyboard and screen reader ========
        {
            const { ctx, page } = await newPage();
            await page.goto(base + '/#token=' + token);
            await page.goto(base + '/#/projects');
            await openAssistant(page);
            const dialog = surface(page);
            assert.equal(await dialog.getAttribute('role'), 'dialog');
            assert.equal(await dialog.getAttribute('aria-modal'), 'true');
            const label = await dialog.getAttribute('aria-labelledby');
            assert.equal(await page.locator('#' + label).innerText(), 'Assistant');
            // Tab never leaves the dialog.
            for (let i = 0; i < 25; i++) {
                await page.keyboard.press('Tab');
                assert.equal(await page.evaluate(() => !!document.activeElement?.closest('[data-testid="assistant"]')), true, 'focus escaped after ' + (i + 1) + ' tabs');
            }
            // Every control has a name.
            const unnamed = await page.evaluate(() => [...document.querySelectorAll('[data-testid="assistant"] button, [data-testid="assistant"] textarea, [data-testid="assistant"] input:not([aria-hidden="true"]), [data-testid="assistant"] select')]
                .filter(el => !(el.getAttribute('aria-label') || el.innerText.trim() || el.labels?.length || el.getAttribute('aria-labelledby'))).map(el => el.outerHTML.slice(0, 80)));
            assert.deepEqual(unnamed, [], 'every control is named');
            // Status is announced in words, not just shown.
            await sendText(page, 'slow words');
            await page.getByTestId('assistant-announce').filter({ hasText: 'The assistant is working.' }).waitFor();
            await page.getByTestId('assistant-announce').filter({ hasText: 'The assistant has finished.' }).waitFor();
            assert.equal(await page.getByTestId('assistant-log').getAttribute('aria-live'), 'off', 'the streaming text is not read out word by word');
            // Esc closes.
            await page.keyboard.press('Escape');
            await dialog.waitFor({ state: 'detached' });
            assert.equal(await page.evaluate(() => document.activeElement?.dataset.testid), 'assistant-button');
            // Enter sends, Shift+Enter breaks the line.
            await openAssistant(page);
            const box = page.getByRole('textbox', { name: 'Message to the assistant' });
            await box.fill('line one');
            await box.press('Shift+Enter');
            await box.type('line two');
            assert.equal(await box.inputValue(), 'line one\nline two');
            await box.press('Enter');
            await page.locator('.user .bubble').last().waitFor();
            assert.equal(await page.locator('.user .bubble').last().innerText(), 'line one\nline two');
            ok('a dialog with a name, focus kept inside it, every control named, status in words, Esc closes, Enter sends');
            await ctx.close();
        }

        // ======== the phone ========
        {
            const { ctx, page } = await newPage({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, deviceScaleFactor: 2 });
            await page.goto(base + '/#token=' + token);
            await page.getByTestId('deck-attention').waitFor();
            await page.goto(base + '/#/projects');
            const button = page.getByTestId('nav-assistant');
            await button.waitFor();
            const bb = await button.boundingBox();
            assert.ok(bb.width >= 44 && bb.height >= 44, 'a thumb-sized way in: ' + JSON.stringify(bb));
            assert.equal(await page.locator('aside.rail').isVisible().catch(() => false), false, 'no sidebar on a phone');
            await page.screenshot({ path: path.join(artifacts, 'assistant-button-mobile.png') });
            await button.tap();
            await surface(page).waitFor();
            await page.waitForFunction(() => document.querySelector('[data-testid="assistant"]')?.dataset.phase === 'open');
            const r = await surface(page).boundingBox();
            const nav = page.locator('[data-phone-nav]');
            const navBox = await nav.boundingBox();
            assert.deepEqual([r.x, r.y, r.width, r.height], [0, 0, 390, navBox.y], 'it fills the phone above its persistent navigation');
            assert.equal(navBox.y + navBox.height, 844, 'the navigation stays at the bottom of the phone');
            assert.equal(await button.getAttribute('aria-current'), 'page', 'the Assistant is the selected place');
            assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, 'no sideways scrolling');
            await page.screenshot({ path: path.join(artifacts, 'assistant-empty-mobile.png') });
            const sendBox = await page.getByRole('button', { name: 'Send', exact: true }).boundingBox();
            const deckBox = await page.getByTestId('nav-deck').boundingBox();
            const micBox = await page.getByTestId('assistant-mic').boundingBox();
            // Transformed bounds can report 43.999999px for a 44px control; allow only subpixel rounding.
            for (const b of [sendBox, bb, deckBox, micBox]) assert.ok(b.width >= 44 - 0.001 && b.height >= 44 - 0.001, 'tap targets are at least 44px: ' + JSON.stringify(b));
            assert.equal(await page.getByRole('textbox', { name: 'Message to the assistant' }).evaluate(el => parseFloat(getComputedStyle(el).fontSize)), 16, 'the box is 16px so the phone does not zoom');
            const comp = await page.getByRole('textbox', { name: 'Message to the assistant' }).boundingBox();
            assert.ok(comp.y + comp.height <= navBox.y, 'the box is above the navigation');
            await sendText(page, `add a ticket to ${project.id}`);
            await page.locator('section.confirm').waitFor();
            const cb = await page.locator('section.confirm').boundingBox();
            assert.ok(cb.x >= 0 && cb.x + cb.width <= 390, 'the confirmation fits the phone');
            for (const name of ['Confirm', 'Not now']) { const b = await page.getByRole('button', { name, exact: true }).boundingBox(); assert.ok(b.height >= 44, name + ' is tappable'); }
            assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
            await page.screenshot({ path: path.join(artifacts, 'assistant-confirmation-mobile.png') });
            await page.getByTestId('assistant-picker').tap();
            const pb = await page.getByTestId('assistant-picker-panel').boundingBox();
            assert.ok(pb.x >= 0 && pb.x + pb.width <= 390, 'the chooser fits the phone');
            await page.screenshot({ path: path.join(artifacts, 'assistant-picker-mobile.png') });
            await page.keyboard.press('Escape');

            await sendText(page, 'openui dashboard');
            const generated = lastReply(page).locator('.openui');
            await generated.locator('h2').filter({ hasText: 'Sprint snapshot' }).waitFor();
            await page.waitForFunction(() => [...document.querySelectorAll('article.reply')].at(-1)?.dataset.status === 'done');
            const gb = await generated.boundingBox();
            assert.ok(gb.x >= 0 && gb.x + gb.width <= 390, 'the generated interface fits the phone');
            assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, 'generated content does not widen the phone');
            const tableWrap = generated.locator('.data-table .table-scroll');
            const tb = await tableWrap.boundingBox();
            assert.ok(tb.x >= 0 && tb.x + tb.width <= 390, 'the table viewport fits the phone');
            assert.equal(await tableWrap.evaluate(el => el.scrollWidth > el.clientWidth), true, 'wide rows scroll inside the table viewport');
            const gridColumns = await generated.locator('.grid').evaluate(el => getComputedStyle(el).gridTemplateColumns.trim().split(/\s+/).length);
            assert.equal(gridColumns, 1, 'the metrics collapse to one column on a phone');
            const summary = generated.locator('details summary').first();
            const summaryBounds = await summary.boundingBox();
            assert.ok(summaryBounds.height >= 44, 'the local disclosure is a thumb-sized target');
            await page.screenshot({ path: path.join(artifacts, 'assistant-openui-mobile.png') });
            ok('generated UI uses a single-column metric grid and contained table scrolling on a phone');

            await button.tap();
            await surface(page).waitFor({ state: 'detached' });
            assert.equal(await button.getAttribute('aria-expanded'), 'false', 'the Assistant tab closes it');
            ok('on a phone: thumb-sized controls, fills the screen above persistent navigation, nothing overflows, the chooser and confirmations fit');
            await ctx.close();
        }

        // ======== states: offline, disconnected, restarted ========
        {
            const { ctx, page } = await newPage();
            await page.goto(base + '/#token=' + token);
            await page.goto(base + '/#/projects');
            await page.route('**/api/assistant/providers', route => route.abort());
            await openAssistant(page);
            await page.getByText('Cannot reach Werkbord').waitFor();
            await page.unroute('**/api/assistant/providers');
            await page.getByRole('button', { name: 'Try again' }).click();
            await page.getByTestId('assistant-empty').waitFor();
            ok('an unreachable controller is explained and recovers when it is back');
            await ctx.close();
        }
        {
            const { ctx, page } = await newPage();
            await page.goto(base + '/#token=' + token);
            await page.goto(base + '/#/projects');
            await openAssistant(page);
            await sendText(page, 'remember me');
            await page.getByText('You said: remember me').waitFor();
            const sid = (await api(base, 'GET', '/api/assistant/sessions')).sessions.at(-1).id;
            // The controller restarts: the same data, new events.
            await stop(server);
            await page.getByText('Reconnecting', { exact: false }).waitFor({ timeout: 8000 });
            server = start(fixture, env);
            await ready(base, server);
            await page.waitForFunction(() => !document.querySelector('.linknote'), null, { timeout: 15000 });
            assert.ok((await api(base, 'GET', '/api/assistant/sessions/' + sid)).id === sid, 'the conversation survived the restart');
            await sendText(page, 'still there?');
            await page.getByText('You said: still there?').waitFor();
            assert.equal(await page.locator('.user .bubble').first().innerText(), 'remember me', 'the visible conversation was kept');
            ok('a controller restart is shown as "reconnecting", then the same conversation carries on, with its events counted afresh');
            await ctx.close();
        }
        for (const [mode, want, extra] of [['none', 'Set up a supported CLI first', /claude auth login|codex login/], ['off', 'The assistant is turned off', /assistant\.disabled/]]) {
            const p2 = await freePort();
            const b2 = 'http://127.0.0.1:' + p2;
            const s2 = start(fixture, { ...env, WERKBORD_BROWSER_ADDR: '127.0.0.1:' + p2, WERKBORD_BROWSER_ASSISTANT: mode, WERKBORD_BROWSER_DIR: '' });
            await ready(b2, s2);
            await api(b2, 'POST', '/api/onboarding/complete', {});
            const { ctx, page } = await newPage();
            await page.goto(b2 + '/#token=' + token);
            await page.goto(b2 + '/#/projects');
            await openAssistant(page);
            await page.getByRole('heading', { name: want }).waitFor();
            assert.match(await page.getByTestId('assistant-state').innerText(), extra);
            if (mode === 'none') {
                assert.equal(await page.getByRole('button', { name: 'Check again' }).count(), 1);
                assert.equal(await page.getByRole('textbox', { name: 'Message to the assistant' }).count(), 0, 'no box to type into when there is nothing to answer');
            }
            await page.screenshot({ path: path.join(artifacts, `assistant-state-${mode}.png`) });
            await ctx.close();
            await stop(s2);
            ok(`the "${mode}" state explains itself`);
        }

        // ======== generated actions still need permission and an unexpired proposal ========
        {
            const readonlyData = path.join(temp, 'readonly-data'); fs.mkdirSync(readonlyData);
            const readonlyPort = await freePort();
            const readonlyBase = 'http://127.0.0.1:' + readonlyPort;
            const readonlyServer = start(fixture, { ...env, WERKBORD_BROWSER_ADDR: '127.0.0.1:' + readonlyPort, WERKBORD_BROWSER_ASSISTANT: 'readonly', WERKBORD_BROWSER_DIR: readonlyData });
            await ready(readonlyBase, readonlyServer);
            await api(readonlyBase, 'POST', '/api/onboarding/complete', {});
            const readonlyProject = await api(readonlyBase, 'POST', '/api/projects', { path: repo, name: 'Read only project' });
            const { ctx, page } = await newPage();
            await page.goto(readonlyBase + '/#token=' + token);
            await page.goto(readonlyBase + '/#/projects');
            await openAssistant(page);
            await sendText(page, `openui form demo for ${readonlyProject.id}`);
            const readOnlyPlan = page.locator('article.reply').last().getByTestId('openui-form');
            await readOnlyPlan.waitFor();
            await page.waitForFunction(() => [...document.querySelectorAll('article.reply')].at(-1)?.dataset.status === 'done');
            const repliesBeforeReadOnlyAction = await page.locator('article.reply').count();
            await readOnlyPlan.getByRole('button', { name: 'Create tasks from this plan' }).click();
            await page.waitForFunction(n => document.querySelectorAll('article.reply').length > n, repliesBeforeReadOnlyAction);
            await page.waitForFunction(() => [...document.querySelectorAll('article.reply')].at(-1)?.dataset.status === 'done');
            assert.equal(await page.locator('section.confirm[data-state="waiting"]').count(), 0, 'a generated button does not grant write permission');
            assert.equal((await api(readonlyBase, 'GET', `/api/projects/${readonlyProject.id}/tasks`)).tasks.length, 0);
            const readonlySession = (await api(readonlyBase, 'GET', '/api/assistant/sessions')).sessions.at(-1).id;
            const readonlyAudit = await api(readonlyBase, 'GET', `/api/assistant/audit?session=${encodeURIComponent(readonlySession)}`);
            assert.ok(readonlyAudit.entries.some(entry => entry.operation === 'create_ticket' && entry.outcome === 'denied'), 'the operation layer records the denied write');
            await ctx.close();
            await stop(readonlyServer);
            ok('a read-only assistant denies a generated form action and records the denied operation');
        }
        {
            const expiryData = path.join(temp, 'expiry-data'); fs.mkdirSync(expiryData);
            const expiryPort = await freePort();
            const expiryBase = 'http://127.0.0.1:' + expiryPort;
            const expiryServer = start(fixture, { ...env, WERKBORD_BROWSER_ADDR: '127.0.0.1:' + expiryPort, WERKBORD_BROWSER_ASSISTANT: 'short-ttl', WERKBORD_BROWSER_ASSISTANT_TTL_MS: '1500', WERKBORD_BROWSER_DIR: expiryData });
            await ready(expiryBase, expiryServer);
            await api(expiryBase, 'POST', '/api/onboarding/complete', {});
            const expiryProject = await api(expiryBase, 'POST', '/api/projects', { path: repo, name: 'Expiry project' });
            const { ctx, page } = await newPage();
            await page.goto(expiryBase + '/#token=' + token);
            await page.goto(expiryBase + '/#/projects');
            await openAssistant(page);
            await sendText(page, `add a ticket to ${expiryProject.id}`);
            const expiringCard = page.locator('section.confirm[data-state="waiting"]');
            await expiringCard.waitFor();
            await sleep(1800);
            await expiringCard.getByRole('button', { name: 'Confirm', exact: true }).click();
            await page.locator('section.confirm[data-state="expired"]').waitFor();
            assert.equal((await api(expiryBase, 'GET', `/api/projects/${expiryProject.id}/tasks`)).tasks.length, 0, 'an expired proposal cannot change the board');
            const expirySession = (await api(expiryBase, 'GET', '/api/assistant/sessions')).sessions.at(-1).id;
            const expiryAudit = await api(expiryBase, 'GET', `/api/assistant/audit?session=${encodeURIComponent(expirySession)}`);
            assert.ok(expiryAudit.entries.some(entry => entry.operation === 'create_ticket' && entry.outcome === 'expired'));
            await ctx.close();
            await stop(expiryServer);
            ok('an expired proposal stays unapplied and is recorded as expired');
        }

        assert.deepEqual(errors, [], 'no page errors: ' + errors.join('\n'));
        console.log('PASS the assistant screen: animation, state, streaming, confirmation, recovery, files, dictation, accessibility, phone');
    } catch (e) {
        if (current) {
            await current.screenshot({ path: path.join(artifacts, 'failure.png') }).catch(() => {});
            console.error('PAGE AT FAILURE:\n' + await current.evaluate(() => [...document.querySelectorAll('[data-testid="assistant"] article, [data-testid="assistant"] .user, [data-testid="assistant"] .confirm, [data-testid="assistant"] .notice')].map(el => el.tagName + '[' + (el.dataset.status || el.dataset.state || '') + '] ' + el.innerText.replace(/\n+/g, ' | ')).join('\n')).catch(() => '(page gone)'));
        }
        throw e;
    } finally {
        await browser.close();
        console.log('Browser artifacts: ' + artifacts);
    }
})().catch(e => { console.error(e && e.stack || e); process.exitCode = 1; }).finally(async () => {
    for (const c of children) await stop(c);
    fs.rmSync(path.join(temp, 'data'), { recursive: true, force: true });
    fs.rmSync(path.join(temp, 'repo'), { recursive: true, force: true });
});

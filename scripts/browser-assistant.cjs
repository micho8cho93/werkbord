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

    try {
        // ======== light, desktop: the way in, the opening, the closing ========
        {
            const { ctx, page } = await newPage({ colorScheme: 'light' });
            await page.goto(base + '/#token=' + token);
            await page.goto(base + '/#/projects');
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
            assert.match(await lastReply(page).locator('.progress').innerText(), /Thinking…|Looking for what is blocked/);
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
            assert.match(await card.innerText(), /Create the ticket "From the assistant" in project "Assistant project"/);
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
            await panel.getByLabel('Thinking level').selectOption('high');
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
            assert.match(await mic.getAttribute('aria-label'), /^Done dictating/);
            const box = page.getByRole('textbox', { name: 'Message to the assistant' });
            assert.equal(await box.getAttribute('readonly'), '', 'the box is not typed into while it listens');
            await page.evaluate(() => window.__rec.onspeechstart?.());
            await page.waitForFunction(() => document.querySelector('[data-testid="assistant-listening"]')?.dataset.phase === 'hearing');
            await page.evaluate(() => window.__rec.say([['what is bl', false]]));
            await page.waitForFunction(() => document.querySelector('textarea').value === 'what is bl');
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
            await page.goto(base + '/#/projects');
            const button = page.locator('[data-testid="assistant-button"]:visible').first();
            await button.waitFor();
            const bb = await button.boundingBox();
            assert.ok(bb.width >= 44 && bb.height >= 44, 'a thumb-sized way in: ' + JSON.stringify(bb));
            assert.equal(await page.locator('aside.rail').isVisible().catch(() => false), false, 'no sidebar on a phone');
            await page.screenshot({ path: path.join(artifacts, 'assistant-button-mobile.png') });
            await button.tap();
            await surface(page).waitFor();
            await page.waitForFunction(() => document.querySelector('[data-testid="assistant"]')?.dataset.phase === 'open');
            const r = await surface(page).boundingBox();
            assert.deepEqual([r.x, r.y, r.width, r.height], [0, 0, 390, 844], 'it fills the phone');
            assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, 'no sideways scrolling');
            await page.screenshot({ path: path.join(artifacts, 'assistant-empty-mobile.png') });
            const sendBox = await page.getByRole('button', { name: 'Send', exact: true }).boundingBox();
            const closeBox = await page.getByTestId('assistant-close').boundingBox();
            const micBox = await page.getByTestId('assistant-mic').boundingBox();
            // Transformed bounds can report 43.999999px for a 44px control; allow only subpixel rounding.
            for (const b of [sendBox, closeBox, micBox]) assert.ok(b.width >= 44 - 0.001 && b.height >= 44 - 0.001, 'tap targets are at least 44px: ' + JSON.stringify(b));
            assert.equal(await page.getByRole('textbox', { name: 'Message to the assistant' }).evaluate(el => parseFloat(getComputedStyle(el).fontSize)), 16, 'the box is 16px so the phone does not zoom');
            const comp = await page.getByRole('textbox', { name: 'Message to the assistant' }).boundingBox();
            assert.ok(comp.y + comp.height <= 844, 'the box is on screen');
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
            await page.getByTestId('assistant-close').tap();
            await surface(page).waitFor({ state: 'detached' });
            ok('on a phone: thumb-sized controls, fills the screen, nothing overflows, the chooser and confirmations fit');
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
        for (const [mode, want, extra] of [['none', 'Sign in to Claude Code or Codex first', /claude auth login|codex login/], ['off', 'The assistant is turned off', /assistant\.disabled/]]) {
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

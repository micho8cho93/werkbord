// The phone's Control Deck and the screens its keys open, in a real browser against the disposable browser fixture (no agent runs).
// Phone sizes, light and dark, reduced motion, and the wide window that must stay the desktop's.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const base = process.env.PERSONAL_BROWSER_URL || 'http://127.0.0.1:17421';
const repo = fs.realpathSync(process.env.WERKBORD_BROWSER_REPO);
const artifacts = process.env.BROWSER_ARTIFACT_DIR || fs.mkdtempSync(path.join(require('node:os').tmpdir(), 'werkbord-phone-browser-'));
fs.mkdirSync(artifacts, { recursive: true });
const token = 'disposable-browser-credential';
async function api(method, route, data) {
    const res = await fetch(base + route, { method, headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' }, body: data ? JSON.stringify(data) : undefined });
    const raw = await res.text();
    if (res.status >= 400) throw Error(`${method} ${route}: ${res.status} ${raw}`);
    return raw ? JSON.parse(raw) : null;
}
const day = (n) => { const d = new Date(); d.setDate(d.getDate() + n); return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`; };
const at = (n, h, m = 0) => { const d = new Date(); d.setDate(d.getDate() + n); d.setHours(h, m, 0, 0); return d.toISOString(); };

const KEYS = [['needs', 'Needs you', '#/control'], ['calendar', 'Calendar', '#/schedule'], ['projects', 'Projects', '#/projects'], ['mywork', 'My Work', '#/mywork'], ['agents', 'Agents', '#/agents'], ['analytics', 'Analytics', '#/analytics'], ['reviews', 'Reviews', '#/reviews'], ['git', 'Git', '#/repositories'], ['runners', 'Runners', '#/runners']];

(async () => {
    await api('POST', '/api/onboarding/complete', {});
    let projects = (await api('GET', '/api/projects')).projects;
    let project = projects.find((p) => p.repoPath === repo) || (await api('POST', '/api/projects', { path: repo, name: 'Phone fixture' }));
    // One planned span, one agent start the next day, and a milestone: what the Calendar must show apart. A rerun finds them again.
    const existing = (await api('GET', `/api/projects/${project.id}/tasks`)).tasks || [];
    const ensure = async (title, body) => existing.find((t) => t.title === title) || api('POST', `/api/projects/${project.id}/tasks`, { title, description: '', ...body });
    await ensure('Phone planned task', { plan: { start: day(0), end: day(2) } });
    const scheduled = await ensure('Phone scheduled task', {});
    await api('PATCH', `/api/projects/${project.id}/tasks/${scheduled.id}`, { version: scheduled.version, orchestration: { enabled: true, scheduledAt: at(1, 9, 30), timezone: Intl.DateTimeFormat().resolvedOptions().timeZone, dependencies: [], expectedPaths: [] } }).catch(() => {});
    await ensure('Phone milestone', { plan: { end: day(3), milestone: true } });

    const browser = await chromium.launch({ headless: true, ...(process.env.BROWSER_EXECUTABLE ? { executablePath: process.env.BROWSER_EXECUTABLE } : {}) });
    const errors = [];
    async function phone(width, height, scheme = 'light', extra = {}) {
        const ctx = await browser.newContext({ viewport: { width, height }, hasTouch: true, isMobile: true, colorScheme: scheme, deviceScaleFactor: 2, ...extra });
        const page = await ctx.newPage();
        page.setDefaultTimeout(10000);
        page.on('pageerror', (e) => errors.push(e.message));
        await page.goto(base + '/#token=' + token);
        return { ctx, page };
    }
    const noOverflow = (page, what) => page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 2).then((o) => assert.equal(o, false, `${what} has no horizontal overflow`));

    // ---- the deck is where the phone opens, nine equal keys in three columns ----
    {
        const { ctx, page } = await phone(390, 844);
        await page.getByTestId('deck-attention').waitFor();
        assert.match(page.url(), /#\/deck$/, 'a phone opens on the Control Deck');
        const keys = page.locator('[data-key]');
        assert.equal(await keys.count(), 9, 'nine keys');
        assert.deepEqual(await keys.evaluateAll((els) => els.map((e) => e.dataset.key)), KEYS.map((k) => k[0]), 'the default arrangement');
        const boxes = await keys.evaluateAll((els) => els.map((e) => { const r = e.getBoundingClientRect(); return { x: Math.round(r.x), y: Math.round(r.y), w: Math.round(r.width), h: Math.round(r.height) }; }));
        assert.equal(new Set(boxes.map((b) => b.w)).size, 1, 'equal widths');
        assert.equal(new Set(boxes.map((b) => b.h)).size, 1, 'equal heights');
        assert.equal(new Set(boxes.map((b) => b.x)).size, 3, 'three columns');
        assert.equal(new Set(boxes.map((b) => b.y)).size, 3, 'three rows');
        assert(boxes.every((b) => b.w >= 44 && b.h >= 44), 'touch targets of at least 44px');
        assert(boxes[0].x > 16 + 10 && boxes[2].x + boxes[2].w < 390 - 16 - 10, 'the keys sit inside a housing with material round them');
        for (const [, label] of KEYS) await page.locator('[data-key]', { hasText: label }).first().waitFor();
        const nav = page.getByRole('navigation', { name: 'Primary' });
        assert.equal(await nav.getByRole('link').count() + await nav.getByRole('button').count(), 2, 'two places at the bottom');
        assert.equal(await nav.getByTestId('nav-deck').getAttribute('aria-current'), 'page');
        await noOverflow(page, 'the deck');

        // Every key opens its page, and Back comes home.
        for (const [id, label, hash] of KEYS) {
            await page.locator(`[data-key="${id}"]`).click();
            await page.waitForFunction((h) => location.hash.startsWith(h), hash);
            await page.getByRole('heading', { level: 1 }).first().waitFor();
            assert.equal(await page.getByRole('heading', { level: 1 }).first().innerText(), label === 'Git' ? 'Git' : label, `${label} has its title`);
            await noOverflow(page, label);
            await page.getByTestId('phone-back').click();
            await page.waitForFunction(() => location.hash === '#/deck');
        }
        console.log('PASS phone deck: nine keys, each opens its page, Back returns');
        await ctx.close();
    }

    // ---- a key is pressed down while it is held, and not moved at all with reduced motion ----
    for (const reduced of [false, true]) {
        const { ctx, page } = await phone(390, 844, 'light', { reducedMotion: reduced ? 'reduce' : 'no-preference' });
        await page.getByTestId('deck-attention').waitFor();
        const key = page.locator('[data-key="calendar"]');
        const cap = key.locator('.cap');
        const rest = await cap.evaluate((e) => getComputedStyle(e).transform);
        const box = await key.boundingBox();
        await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
        await page.mouse.down();
        await page.waitForTimeout(250);
        assert.equal(await key.getAttribute('data-pressed'), 'on', 'a held key is marked pressed');
        const down = await cap.evaluate((e) => getComputedStyle(e).transform);
        if (reduced) assert.equal(down, rest, 'with reduced motion the cap does not move');
        else assert.notEqual(down, rest, 'the cap drops when pressed');
        await page.mouse.up();
        await ctx.close();
    }
    console.log('PASS keys depress on touch and respect reduced motion');

    // ---- the Calendar: Month, Week and Timeline, plan and schedule kept apart ----
    {
        const { ctx, page } = await phone(390, 844);
        await page.goto(base + '/#/schedule');
        await page.getByRole('button', { name: 'Month', exact: true }).waitFor();
        await page.locator(`[data-day="${day(0)}"]`).click();
        await page.getByText('Phone planned task').first().waitFor();
        await page.locator(`[data-day="${day(1)}"]`).click();
        const scheduledRow = page.getByTestId('cal-item').filter({ hasText: 'Phone scheduled task' }).first();
        await scheduledRow.waitFor();
        assert.match(await scheduledRow.innerText(), /Agent starts/, 'a schedule says an agent starts');
        await scheduledRow.click();
        await page.getByText('This is when an agent is set to start. It does not change the planned dates.').waitFor();
        await page.getByRole('link', { name: 'Open task' }).waitFor();
        await page.keyboard.press('Escape');
        await page.getByText('This is when an agent is set to start.').waitFor({ state: 'detached' });
        await page.getByRole('button', { name: 'Week', exact: true }).click();
        await page.getByTestId('week-day').first().waitFor();
        assert.equal(await page.getByTestId('week-day').count(), 7, 'seven days across the week');
        await page.getByRole('button', { name: 'Next week' }).waitFor();
        await page.getByRole('button', { name: 'Timeline', exact: true }).click();
        await page.getByTestId('timeline-scroll').waitFor();
        assert((await page.getByTestId('timeline-bar').count()) >= 2, 'planned work is drawn as bars');
        assert((await page.getByTestId('timeline-start').count()) >= 1, 'an agent start is its own marker');
        for (const zoom of ['Month', 'Quarter', 'Week']) await page.getByRole('group', { name: 'Timeline zoom' }).getByRole('button', { name: zoom, exact: true }).click();
        await noOverflow(page, 'the timeline');
        for (const scheme of ['light', 'dark']) {
            await page.emulateMedia({ colorScheme: scheme });
            await page.screenshot({ path: path.join(artifacts, `phone-timeline-${scheme}.png`) });
        }
        console.log('PASS phone calendar: month, week and timeline; plan and schedule are separate');
        await ctx.close();
    }

    // ---- the Assistant is the second place, and the bar stays over it ----
    {
        const { ctx, page } = await phone(390, 844);
        await page.getByTestId('deck-attention').waitFor();
        await page.getByTestId('nav-assistant').click();
        await page.getByTestId('assistant').waitFor();
        assert.equal(await page.getByTestId('nav-assistant').getAttribute('aria-current'), 'page');
        const navBox = await page.locator('[data-phone-nav]').boundingBox();
        const surfaceBox = await page.getByTestId('assistant').boundingBox();
        assert(surfaceBox.y + surfaceBox.height <= navBox.y + 2, 'the assistant ends where the bar begins');
        await page.getByTestId('nav-deck').click();
        await page.getByTestId('assistant').waitFor({ state: 'detached' });
        assert.match(page.url(), /#\/deck$/);
        console.log('PASS phone assistant: second place in the bar, closes from the deck');
        await ctx.close();
    }

    // ---- sizes and themes: no overflow anywhere, in light and dark ----
    for (const [w, h] of [[320, 568], [360, 740], [430, 932], [844, 390]]) {
        for (const scheme of ['light', 'dark']) {
            const { ctx, page } = await phone(w, h, scheme);
            await page.getByTestId('deck-attention').waitFor();
            for (const hash of ['#/deck', '#/mywork', '#/schedule', '#/agents', '#/reviews', '#/repositories', '#/runners', '#/analytics', '#/control', '#/projects', `#/p/${project.id}/board`]) {
                await page.goto(base + '/' + hash);
                await page.waitForTimeout(300);
                await noOverflow(page, `${hash} at ${w}x${h} ${scheme}`);
            }
            await page.goto(base + '/#/deck');
            await page.screenshot({ path: path.join(artifacts, `phone-deck-${w}x${h}-${scheme}.png`) });
            await ctx.close();
        }
    }
    console.log('PASS phone sizes and themes: no horizontal overflow');

    // ---- the project's board shows one column at a time ----
    {
        const { ctx, page } = await phone(390, 844);
        await page.goto(base + `/#/p/${project.id}/board`);
        const group = page.getByRole('group', { name: 'Board column' });
        await group.waitFor();
        await group.getByRole('button', { name: /^Backlog/ }).click();
        assert.equal(await page.locator('.col:not(.off)').count(), 1, 'one column is shown');
        await ctx.close();
    }
    console.log('PASS phone board: one column at a time');

    // ---- a wide window stays the desktop ----
    {
        const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
        const page = await ctx.newPage();
        page.on('pageerror', (e) => errors.push(e.message));
        await page.goto(base + '/#token=' + token);
        await page.getByRole('link', { name: 'Needs you' }).first().waitFor();
        await page.goto(base + '/#/deck');
        await page.waitForFunction(() => location.hash === '#/control');
        assert.equal(await page.locator('[data-phone-nav]').count(), 0, 'no phone bar on a wide window');
        assert.equal(await page.locator('[data-key]').count(), 0, 'no deck on a wide window');
        assert(await page.locator('aside.rail').isVisible(), 'the desktop rail is there');
        console.log('PASS desktop unchanged: the deck does not exist on a wide window');
        await ctx.close();
    }

    await browser.close();
    assert.deepEqual(errors, [], 'no page errors');
    console.log('PASS phone Control Deck suite');
})().catch((e) => { console.error(e); process.exit(1); });

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

const KEYS = [['needs', 'Workspace', '#/control'], ['calendar', 'Calendar', '#/schedule'], ['projects', 'Projects', '#/projects'], ['mywork', 'My Work', '#/mywork'], ['agents', 'Agents', '#/agents'], ['analytics', 'Analytics', '#/analytics'], ['reviews', 'Reviews', '#/reviews'], ['git', 'Git', '#/repositories'], ['runners', 'Runners', '#/runners']];

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
        await ctx.addInitScript(() => localStorage.setItem('werkbord-tour-phone-deck-v2', 'fixture'));
        const page = await ctx.newPage();
        page.setDefaultTimeout(30000);
        page.on('pageerror', (e) => errors.push(e.message));
        await page.goto(base + '/#token=' + token);
        return { ctx, page };
    }
    const noOverflow = (page, what) => page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 2).then((o) => assert.equal(o, false, `${what} has no horizontal overflow`));

    // ---- the deck is where the phone opens, nine equal keys in three columns ----
    {
        const { ctx, page } = await phone(390, 844);
        await page.getByTestId('deck-grid').waitFor();
        assert.match(page.url(), /#\/deck$/, 'a phone opens on the Control Deck');
        const keys = page.locator('[data-key]');
        assert.equal(await keys.count(), 6, 'six initial navigation keys');
        assert.equal(await page.getByRole('button', { name: 'Empty slot. Add a button', exact: true }).count(), 3, 'three empty sockets');
        assert.equal(await page.getByText('All clear', { exact: true }).count(), 0, 'no redundant All clear banner');
        await page.getByRole('button', { name: 'Jump to', exact: true }).waitFor();
        await page.getByRole('link', { name: 'Help and shortcuts', exact: true }).waitFor();
        assert.deepEqual(await keys.evaluateAll((els) => els.map((e) => e.dataset.key)), KEYS.slice(0,6).map((k) => k[0]), 'the default arrangement');
        const boxes = await page.locator('[data-deck-position] .key-slot').evaluateAll((els) => els.map((e) => { const r = e.getBoundingClientRect(); return { x: Math.round(r.x), y: Math.round(r.y), w: Math.round(r.width), h: Math.round(r.height) }; }));
        assert.equal(new Set(boxes.map((b) => b.w)).size, 1, 'equal widths');
        assert.equal(new Set(boxes.map((b) => b.h)).size, 1, 'equal heights');
        assert.equal(new Set(boxes.map((b) => b.x)).size, 3, 'three columns');
        assert.equal(new Set(boxes.map((b) => b.y)).size, 3, 'three rows');
        assert(boxes.every((b) => b.w >= 44 && b.h >= 44), 'touch targets of at least 44px');
        assert(boxes[0].x > 16 + 10 && boxes[2].x + boxes[2].w < 390 - 16 - 10, 'the keys sit inside a housing with material round them');
        for (const [, label] of KEYS.slice(0,6)) await page.locator('[data-key]', { hasText: label }).first().waitFor();
        const nav = page.getByRole('navigation', { name: 'Primary' });
        assert.equal(await nav.getByRole('link').count() + await nav.getByRole('button').count(), 2, 'two places at the bottom');
        assert.equal(await nav.getByTestId('nav-deck').getAttribute('aria-current'), 'page');
        await noOverflow(page, 'the deck');

        // Restore the optional navigation keys through the library, then exercise all existing routes.
        await page.getByTestId('edit-deck').click();
        for (let i = 6; i < 9; i++) {
            await page.locator(`[data-deck-position="${i}"] .key-slot`).click();
            await page.getByRole('dialog').getByRole('button', { name: KEYS[i][1], exact: true }).click();
        }
        await page.getByRole('button', { name: 'Done', exact: true }).click();
        await page.getByRole('button', { name: 'Edit Deck', exact: true }).waitFor();
        await page.reload();
        await page.waitForFunction(() => document.querySelectorAll('[data-key]').length === 9);
        // Every key opens its page, and Back comes home.
        for (const [id, label, hash] of KEYS) {
            await page.locator(`[data-key="${id}"]`).click();
            await page.waitForFunction((h) => location.hash.startsWith(h), hash);
            await page.getByRole('heading', { level: 1 }).first().waitFor();
            // A count (Needs you) sits in the heading as its own badge, on a second line of text.
            assert.equal((await page.getByRole('heading', { level: 1 }).first().innerText()).split('\n')[0], label, `${label} has its title`);
            await noOverflow(page, label);
            await page.getByTestId('phone-back').click();
            await page.waitForFunction(() => location.hash === '#/deck');
        }
        const saved = await api('GET', '/api/deck');
        await api('PUT', '/api/deck', { version: saved.version, slots: saved.slots.map((s,i) => i < 6 ? s : null) });
        console.log('PASS phone deck: six defaults, three empty slots, library persistence and all navigation routes');
        await ctx.close();
    }

    // ---- a key is pressed down while it is held, and not moved at all with reduced motion ----
    for (const reduced of [false, true]) {
        const { ctx, page } = await phone(390, 844, 'light', { reducedMotion: reduced ? 'reduce' : 'no-preference' });
        await page.getByTestId('deck-grid').waitFor();
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
        // Signing in rewrites the address once the page has started; wait for the deck so that it cannot overwrite where we go next.
        await page.getByTestId('deck-grid').waitFor();
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
        await page.getByTestId('deck-grid').waitFor();
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
            await page.getByTestId('deck-grid').waitFor();
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
        await page.getByTestId('deck-grid').waitFor();
        await page.goto(base + `/#/p/${project.id}/board`);
        const group = page.getByRole('group', { name: 'Board column' });
        await group.waitFor();
        await group.getByRole('button', { name: /^Backlog/ }).click();
        assert.equal(await page.locator('.col:not(.off)').count(), 1, 'one column is shown');
        await ctx.close();
    }
    console.log('PASS phone board: one column at a time');

    // A task advances with one tap; Back ignores intermediate pages and follows the tree.
    {
        const { ctx, page } = await phone(390, 844);
        const task = await ensure('Phone workflow regression', {});
        await page.goto(base + `/#/p/${project.id}/task/${task.id}`);
        await page.getByRole('button', { name: 'Move to In progress', exact: true }).click();
        await page.getByRole('button', { name: 'Submit for review', exact: true }).click();
        await page.getByRole('button', { name: 'Mark done', exact: true }).click();
        await page.getByRole('button', { name: 'Reopen task', exact: true }).waitFor();
        const saved = (await api('GET', `/api/projects/${project.id}/tasks`)).tasks.find(t => t.id === task.id);
        assert.equal(saved.state, 'done', 'completion is persisted');
        await noOverflow(page, 'completed task');
        await page.screenshot({ path: path.join(artifacts, 'phone-task-done.png') });
        await page.goto(base + '/#/settings');
        await page.goto(base + '/#/mywork');
        await page.getByTestId('phone-back').click();
        await page.waitForURL('**/#/deck');
        await page.goto(base + `/#/p/${project.id}/board`);
        await page.getByTestId('phone-back').click();
        await page.waitForURL('**/#/projects');
        await ctx.close();
    }
    console.log('PASS phone task progression and parent navigation');

    // ---- a phone scans a code and is signed in, with a credential of its own; revoking signs it out ----
    {
        const IPHONE = 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1';
        const newPhone = async () => {
            const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true, userAgent: IPHONE });
            const page = await ctx.newPage();
            page.setDefaultTimeout(30000);
            page.on('pageerror', (e) => errors.push(e.message));
            return { ctx, page };
        };
        const ownerRequest = async (method, route, data) => api(method, route, data);
        for (const old of (await ownerRequest('GET', '/api/phones')).phones) await ownerRequest('DELETE', '/api/phones/' + old.id);

        // Scan: the page opens with only the code in its fragment, and nothing else is asked of the person.
        const { code } = await ownerRequest('POST', '/api/phones/codes', {});
        assert(code && code.length >= 43, 'a code of at least 256 bits');
        const { ctx, page } = await newPhone();
        const tokenCalls = [];
        page.on('request', (r) => { if (r.url().includes(code) || (r.postData() || '').includes(token)) tokenCalls.push(r.url()); });
        await page.goto(base + '/#enroll=' + code);
        await page.getByTestId('deck-grid').waitFor();
        await page.getByTestId('phone-welcome').waitFor();
        assert.match(await page.getByTestId('phone-welcome').innerText(), /Signed in as iPhone \(Safari\)/, 'named from the device, without asking');
        assert(!page.url().includes('enroll') && !page.url().includes(code), 'the code is gone from the address bar');
        const stored = await page.evaluate(() => localStorage.getItem('werkbord.token'));
        assert(stored && stored.startsWith('wbp_') && stored !== token, 'the phone holds a credential of its own, not the owner token');
        const listed = (await ownerRequest('GET', '/api/phones')).phones;
        assert.equal(listed.length, 1, 'the owner sees one phone');
        assert.equal(listed[0].name, 'iPhone (Safari)');
        // The name is editable.
        await page.getByTestId('phone-welcome').getByRole('button', { name: 'Rename' }).click();
        await page.getByLabel('Name of this phone').fill('Michel iPhone');
        await page.getByRole('button', { name: 'Save', exact: true }).click();
        await page.getByText('Michel iPhone').first().waitFor();
        assert.equal((await ownerRequest('GET', '/api/phones')).phones[0].name, 'Michel iPhone');
        await page.getByRole('button', { name: 'OK', exact: true }).click();
        console.log('PASS phone enrollment: scan to signed in, with a credential of its own');

        // The code is single-use.
        {
            const second = await newPhone();
            await second.page.goto(base + '/#enroll=' + code);
            await second.page.getByTestId('enroll-failed').waitFor();
            assert.match(await second.page.getByTestId('enroll-failed').innerText(), /did not work/);
            assert.equal(await second.page.evaluate(() => localStorage.getItem('werkbord.token')), null, 'a replayed code signs nothing in');
            await second.ctx.close();
        }

        // Persistent: a reload is still signed in, with no second welcome.
        await page.reload();
        await page.getByTestId('deck-grid').waitFor();
        assert.equal(await page.getByTestId('phone-welcome').count(), 0);

        // Scope: the phone does its work, and the owner's things are refused.
        const asPhone = (method, route, data) => page.evaluate(async ([method, route, data]) => {
            const res = await fetch(route, { method, headers: { Authorization: 'Bearer ' + localStorage.getItem('werkbord.token'), 'Content-Type': 'application/json' }, body: data ? JSON.stringify(data) : undefined });
            return res.status;
        }, [method, route, data]);
        assert.equal(await asPhone('GET', '/api/projects'), 200);
        assert.equal(await asPhone('PUT', '/api/settings/execution', {}), 403, 'a phone cannot change settings');
        assert.equal(await asPhone('POST', '/api/projects', { path: repo }), 403, 'a phone cannot register a repository');
        assert.equal(await asPhone('GET', '/api/phones'), 403, 'a phone cannot list phones');
        assert.notEqual(await asPhone('POST', `/api/projects/${project.id}/git/push`, {}), 403, 'a phone may use Git, push included');
        assert.notEqual(await asPhone('GET', '/api/assistant/audit'), 403, 'the whole Assistant is there');
        assert.equal(await asPhone('POST', `/api/projects/${project.id}/tasks`, { title: 'x', execution: { agent: 'a' } }), 403, 'a phone does not choose how a task runs');
        assert.equal(await asPhone('POST', '/api/phones/codes', {}), 403, 'a phone cannot enroll another');
        // The Assistant is allowed (the surface opens; whether a provider is installed is the machine's business).
        await page.getByTestId('nav-assistant').click();
        await page.getByTestId('assistant').waitFor();
        await page.getByTestId('nav-deck').click();
        // Everything the phone's own screens ask for is on its list: a walk through them meets no refusal.
        {
            const refused = [];
            page.on('response', (res) => { if (res.status() === 403) refused.push(res.request().method() + ' ' + new URL(res.url()).pathname); });
            const tasks = (await ownerRequest('GET', `/api/projects/${project.id}/tasks`)).tasks;
            for (const hash of ['#/deck', ...KEYS.map((k) => k[2]), `#/p/${project.id}/board`, `#/p/${project.id}/task/${tasks[0].id}`, `#/p/${project.id}/git`, `#/p/${project.id}/runs`, `#/p/${project.id}/calendar`, '#/settings']) {
                await page.goto(base + '/' + hash);
                await page.waitForTimeout(500);
            }
            await page.goto(base + '/#/deck');
            assert.deepEqual([...new Set(refused)], [], 'a phone\'s own screens ask for nothing it is refused');
        }
        // What it may not do is not on screen: no way to add a project, no run settings, a Settings with only its own.
        await page.goto(base + '/#/projects');
        await page.getByRole('heading', { level: 1 }).first().waitFor();
        assert.equal(await page.getByText('Add a project').count(), 0, 'no way to add a project');
        const firstTask = (await ownerRequest('GET', `/api/projects/${project.id}/tasks`)).tasks[0];
        await page.goto(base + `/#/p/${project.id}/task/${firstTask.id}`);
        await page.waitForTimeout(500);
        assert.equal(await page.locator('#run-agent, [id^="run-"]').count(), 0, 'no run settings on a task');
        await page.goto(base + '/#/runners');
        await page.waitForTimeout(500);
        assert.equal(await page.getByRole('button', { name: 'Add runner' }).count(), 0, 'no way to pair a runner');
        await page.goto(base + '/#/settings');
        await page.getByTestId('phone-self').waitFor();
        assert.equal(await page.getByRole('link', { name: 'Runners', exact: true }).count(), 0, 'Settings has nothing a phone cannot change');
        // Settings -> Phones shows this phone and lets it sign itself out.
        await page.goto(base + '/#/settings?phones');
        await page.getByTestId('phone-self').waitFor();
        assert.match(await page.getByTestId('phone-self').innerText(), /Michel iPhone/);
        await page.goto(base + '/#/deck');
        await page.getByTestId('deck-grid').waitFor();
        console.log('PASS phone scope: its work is allowed, the owner\'s things are refused');

        // Revoke: the open page is signed out without being touched.
        await ownerRequest('DELETE', '/api/phones/' + listed[0].id);
        await page.getByTestId('signed-out').waitFor({ timeout: 15000 });
        assert.equal(await page.getByTestId('deck-grid').count(), 0, 'nothing of the app is left on screen');
        await page.reload();
        await page.getByTestId('signed-out').waitFor();
        assert.equal(await asPhone('GET', '/api/projects'), 401, 'the revoked credential is refused');
        assert.equal((await ownerRequest('GET', '/api/phones')).phones.length, 0);
        assert.deepEqual(tokenCalls, [], 'neither the code nor the owner token was sent anywhere it should not be');
        console.log('PASS phone revoke: signed out at once, and it stays signed out');
        await ctx.close();
    }

    // ---- Settings -> Phones on the computer: the list, Revoke with a confirm, and Add a phone with its countdown ----
    {
        const { code } = await api('POST', '/api/phones/codes', {});
        const ph = await (await fetch(base + '/api/phones/enroll', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ code, name: 'Desk iPad' }) })).json();
        const ctx = await browser.newContext({ viewport: { width: 1280, height: 860 } });
        const page = await ctx.newPage();
        page.setDefaultTimeout(30000);
        page.on('pageerror', (e) => errors.push(e.message));
        await page.goto(base + '/#token=' + token);
        // The app rewrites the address to "#/" once it has taken the token; on a slow runner that landed after the next goto.
        await page.getByRole('link', { name: 'Workspace' }).first().waitFor();
        await page.goto(base + '/#/settings?phones');
        const row =page.locator(`[data-phone="${ph.id}"]`);
        await row.waitFor();
        assert.match(await row.innerText(), /Desk iPad/);
        assert.match(await row.innerText(), /added .*last seen/);
        await page.getByRole('button', { name: 'Add a phone' }).click();
        const timer = page.getByTestId('phone-countdown');
        await timer.waitFor();
        assert.match(await timer.innerText(), /\b[45]:\d\d\b/, 'a countdown from five minutes');
        const first = await timer.innerText();
        await page.waitForTimeout(2200);
        assert.notEqual(await timer.innerText(), first, 'the countdown moves');
        assert.equal(await page.getByTestId('phone-enroll').locator('img').count(), 1, 'the QR code is shown');
        assert(!(await page.content()).includes(token), 'the owner token is nowhere on the page');
        await page.screenshot({ path: path.join(artifacts, 'settings-phones.png') });
        // Revoke asks first.
        let asked = '';
        page.once('dialog', (d) => { asked = d.message(); d.dismiss(); });
        await row.getByRole('button', { name: 'Revoke' }).click();
        await page.waitForTimeout(200);
        assert.match(asked, /Revoke Desk iPad\?/);
        assert.equal((await api('GET', '/api/phones')).phones.length, 1, 'declining the confirm revokes nothing');
        page.once('dialog', (d) => d.accept());
        await row.getByRole('button', { name: 'Revoke' }).click();
        await row.waitFor({ state: 'detached' });
        assert.equal((await api('GET', '/api/phones')).phones.length, 0);
        console.log('PASS Settings -> Phones: list, add with a countdown, revoke with a confirm');
        await ctx.close();
    }

    // ---- a wide window stays the desktop ----
    {
        const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
        const page = await ctx.newPage();
        page.on('pageerror', (e) => errors.push(e.message));
        await page.goto(base + '/#token=' + token);
        await page.getByRole('link', { name: 'Workspace' }).first().waitFor();
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

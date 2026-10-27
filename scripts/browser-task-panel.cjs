// The task panel says each thing once and keeps the conversation on its own tab: the title is a title, the description appears once (a long
// one is clamped behind Show more), one row of views holds Overview, Chat, Execution, Schedule and Runs, and nothing overflows a phone.
const { chromium, webkit } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const base = process.env.PERSONAL_BROWSER_URL;
const owner = 'disposable-browser-credential';
const artifacts = process.env.BROWSER_ARTIFACT_DIR || fs.mkdtempSync(path.join(require('node:os').tmpdir(), 'werkbord-task-panel-'));
fs.mkdirSync(artifacts, { recursive: true });
async function request(method, route, body) {
    const response = await fetch(base + route, { method, headers: { Authorization: 'Bearer ' + owner, 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body) });
    const raw = await response.text();
    assert(response.ok, `${method} ${route}: ${response.status} ${raw}`);
    return raw ? JSON.parse(raw) : null;
}
(async () => {
    let project = (await request('GET', '/api/projects')).projects.find(p => p.repoPath === fs.realpathSync(process.env.WERKBORD_BROWSER_REPO));
    if (!project) project = await request('POST', '/api/projects', { path: fs.realpathSync(process.env.WERKBORD_BROWSER_REPO), name: 'Task panel fixture' });
    const SHORT = 'Make the sign-in page remember the last email address used.';
    const LONG = Array.from({ length: 9 }, (_, i) => `Step ${i + 1}: describe what the agent should change here, and why it matters to the people who use this page.`).join('\n');
    const short = await request('POST', `/api/projects/${project.id}/tasks`, { title: 'Remember the last email', description: SHORT });
    const long = await request('POST', `/api/projects/${project.id}/tasks`, { title: 'A task with a long brief', description: LONG });
    const browser = await (process.env.BROWSER_ENGINE === 'webkit' ? webkit : chromium).launch({ headless: true });
    const errors = [];
    try {
        for (const [label, viewport] of [['desktop', { width: 1440, height: 900 }], ['phone', { width: 390, height: 844 }]]) {
            const ctx = await browser.newContext({ viewport, isMobile: label === 'phone', hasTouch: label === 'phone', serviceWorkers: 'block' });
            await ctx.addInitScript(token => { localStorage.setItem('werkbord.token', token); localStorage.setItem('werkbord-tour-phone-deck-v2', 'fixture'); localStorage.setItem('werkbord-tour-desktop-v2', 'fixture'); }, owner);
            const page = await ctx.newPage();
            page.setDefaultTimeout(10000);
            page.on('pageerror', e => errors.push(e.message));
            const panel = page.locator('.panel');

            // ---- a short description ----
            await page.goto(base + `/#/p/${project.id}/task/${short.id}`);
            await page.getByRole('heading', { name: short.title, exact: true }).waitFor();
            const title = await page.locator('#task-title').evaluate(el => ({ size: parseFloat(getComputedStyle(el).fontSize), weight: getComputedStyle(el).fontWeight, upper: getComputedStyle(el).textTransform }));
            assert(title.size >= 18 && title.upper === 'none', `${label}: the task title is a title, not a section label: ${JSON.stringify(title)}`);
            assert.equal(await panel.getByText(SHORT, { exact: true }).count(), 1, `${label}: the description appears once`);
            assert.equal(await panel.getByRole('button', { name: /Show more|Show less/ }).count(), 0, `${label}: a short description has nothing to expand`);
            assert.equal(await panel.getByText('Read full brief').count(), 0, `${label}: no second "full brief" copy`);
            const tabs = panel.getByRole('tablist', { name: 'Task detail views', exact: true });
            assert.deepEqual(await tabs.getByRole('tab').allInnerTexts(), ['Overview', 'Chat', 'Execution', 'Schedule', 'Runs (0)'], `${label}: the views`);
            // The labels line up with the content under them.
            const aligned = await page.evaluate(() => {
                const first = document.querySelector('.panel [role=tab]'), content = document.querySelector('.panel .title');
                const left = (el) => { const r = document.createRange(); r.selectNodeContents(el); return r.getBoundingClientRect().left; };
                return Math.abs(left(first) - content.getBoundingClientRect().left);
            });
            assert(aligned <= 2, `${label}: tab labels align with the task's text (off by ${aligned}px)`);
            // Arrow keys move between the views.
            await panel.getByRole('tab', { name: 'Overview', exact: true }).focus();
            await page.keyboard.press('ArrowRight');
            assert.equal(await panel.getByRole('tab', { name: /^Chat/ }).getAttribute('aria-selected'), 'true', `${label}: ArrowRight opens Chat`);
            assert.equal(await panel.getByRole('tab', { name: /^Chat/ }).evaluate(el => el === document.activeElement), true, `${label}: focus follows`);
            await page.keyboard.press('Home');
            assert.equal(await panel.getByRole('tab', { name: 'Overview', exact: true }).getAttribute('aria-selected'), 'true', `${label}: Home opens Overview`);
            assert(await panel.getByText('Set up a run', { exact: true }).isVisible(), `${label}: a task that never ran opens its run setup`);
            assert.equal(await panel.getByText('Next', { exact: true }).count(), 0, `${label}: no "Next" line restating the status`);
            await page.screenshot({ path: path.join(artifacts, `task-panel-${label}-overview.png`) });
            await panel.getByRole('tab', { name: /^Chat/ }).click();
            assert(await panel.getByText('No agent has worked on this task yet.').isVisible(), `${label}: Chat says there is no conversation yet`);
            await page.screenshot({ path: path.join(artifacts, `task-panel-${label}-chat.png`) });

            // ---- a long description ----
            await page.goto(base + `/#/p/${project.id}/task/${long.id}`);
            await page.getByRole('heading', { name: long.title, exact: true }).waitFor();
            const brief = panel.locator('#task-brief');
            const clamped = await brief.evaluate(el => ({ clamped: el.classList.contains('clamped'), tall: el.scrollHeight > el.clientHeight + 2 }));
            assert(clamped.clamped && clamped.tall, `${label}: a long description is clamped`);
            const toggle = panel.getByRole('button', { name: 'Show more', exact: true });
            await toggle.click();
            assert.equal(await panel.getByRole('button', { name: 'Show less', exact: true }).getAttribute('aria-expanded'), 'true', `${label}: Show more opens the rest in the same place`);
            assert.equal(await panel.getByText('Step 9:', { exact: false }).count(), 1, `${label}: the rest of the description appears once`);
            assert.equal(await brief.evaluate(el => el.classList.contains('clamped')), false, `${label}: expanded`);
            await page.screenshot({ path: path.join(artifacts, `task-panel-${label}-long.png`) });
            await panel.getByRole('button', { name: 'Show less', exact: true }).click();
            assert.equal(await brief.evaluate(el => el.classList.contains('clamped')), true, `${label}: Show less clamps it again`);

            assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 2), true, `${label}: nothing overflows sideways`);
            if (label === 'phone') {
                const boxes = await tabs.getByRole('tab').evaluateAll(els => els.map(e => { const r = e.getBoundingClientRect(); return r.height; }));
                assert(boxes.every(h => h >= 44), `phone: every view is a 44px touch target: ${boxes}`);
            }
            await ctx.close();
        }
        assert.deepEqual(errors, []);
        console.log('PASS the task panel says each thing once, with the conversation on its own tab, at desktop and phone widths');
        console.log('Screenshots: ' + artifacts);
    } finally { await browser.close(); }
})().catch(e => { console.error(e); process.exitCode = 1; });

// Measures the two legibility floors on the disposable browser fixture: WCAG contrast of every piece of visible text
// against what is behind it, and the smallest text size. Both themes, desktop and phone, every page that has text.
//
//   PERSONAL_BROWSER_URL=http://127.0.0.1:17499 node scripts/measure-legibility.cjs [--json out.json] [--check]
//
// --check exits 1 when any text is below 4.5:1 (3:1 for large text) or smaller than MIN_PX. Needs the fixture to be seeded
// as for the browser tests (a repository project, a work project, a waiting question and a task in Review).
// --screenshots DIR also captures the final-pass matrix at 1440x900 and 375x812, including New task and Assistant.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const fs = require('node:fs');
const assert = require('node:assert/strict');
const base = process.env.PERSONAL_BROWSER_URL || 'http://127.0.0.1:17421';
const token = 'disposable-browser-credential';
const MIN_PX = 11.5;
const args = process.argv.slice(2);
const jsonOut = args.includes('--json') ? args[args.indexOf('--json') + 1] : '';
const check = args.includes('--check');
const screenshots = args.includes('--screenshots') ? args[args.indexOf('--screenshots') + 1] : '';
if (screenshots) fs.mkdirSync(screenshots, { recursive: true });

async function api(path) {
    const res = await fetch(base + path, { headers: { Authorization: 'Bearer ' + token } });
    if (!res.ok) throw Error(res.status + ' ' + path);
    return res.json();
}

// Runs in the page. Colours are resolved through a canvas so every CSS colour syntax (color-mix, light-dark, oklab) is one rgba.
function measure(minPx) {
    const cv = document.createElement('canvas');
    cv.width = cv.height = 1;
    const cx = cv.getContext('2d', { willReadFrequently: true });
    const rgba = (css) => {
        cx.clearRect(0, 0, 1, 1);
        cx.fillStyle = '#000';
        cx.fillStyle = css;
        cx.fillRect(0, 0, 1, 1);
        const d = cx.getImageData(0, 0, 1, 1).data;
        return [d[0], d[1], d[2], d[3] / 255];
    };
    const over = (top, under) => {
        const a = top[3] + under[3] * (1 - top[3]);
        if (a === 0) return [0, 0, 0, 0];
        return [0, 1, 2].map((i) => (top[i] * top[3] + under[i] * under[3] * (1 - top[3])) / a).concat(a);
    };
    const lum = (c) => {
        const f = (v) => { v /= 255; return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4; };
        return 0.2126 * f(c[0]) + 0.7152 * f(c[1]) + 0.0722 * f(c[2]);
    };
    const ratio = (a, b) => { const x = lum(a), y = lum(b); return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05); };
    const hex = (c) => '#' + c.slice(0, 3).map((v) => Math.round(v).toString(16).padStart(2, '0')).join('');
    const canvasBg = rgba(getComputedStyle(document.documentElement).backgroundColor);
    // The colours a gradient face passes through, worst case taken: a button's text must read on every stop.
    const stops = (img) => (img.match(/(rgba?\([^)]*\)|color\([^)]*\)|#[0-9a-f]{3,8}\b|color-mix\((?:[^()]|\([^)]*\))*\))/gi) || []).map(rgba);
    const backdrops = (el) => {
        let layers = [[0, 0, 0, 0]];
        const chain = [];
        for (let n = el; n; n = n.parentElement) chain.push(n);
        let result = [canvasBg];
        for (const n of chain.reverse()) {
            const cs = getComputedStyle(n);
            const bg = rgba(cs.backgroundColor);
            const grads = cs.backgroundImage && cs.backgroundImage !== 'none' ? stops(cs.backgroundImage).filter((s) => s[3] > 0) : [];
            const next = [];
            for (const under of result) {
                if (grads.length) for (const g of grads) next.push(over(g, over(bg, under)));
                else next.push(over(bg, under));
            }
            result = next;
        }
        return result;
    };
    const rows = [];
    const sizes = [];
    const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
    const seen = new Set();
    for (let t; (t = walker.nextNode()); ) {
        const text = t.nodeValue.replace(/\s+/g, ' ').trim();
        const el = t.parentElement;
        if (!text || !el || seen.has(el)) continue;
        if (['SCRIPT', 'STYLE', 'NOSCRIPT', 'OPTION'].includes(el.tagName)) continue;
        const r = el.getBoundingClientRect();
        if (r.width <= 1.5 || r.height <= 1.5) continue; // visually-hidden labels are for screen readers
        const cs = getComputedStyle(el);
        if (cs.visibility === 'hidden' || cs.display === 'none' || +cs.opacity === 0) continue;
        // Skip anything clipped to nothing or scrolled out of an overflow container's box entirely.
        let clipped = false;
        for (let p = el; p && p !== document.body; p = p.parentElement) {
            const o = getComputedStyle(p);
            if (+o.opacity === 0 || o.visibility === 'hidden') { clipped = true; break; }
        }
        if (clipped) continue;
        seen.add(el);
        const px = parseFloat(cs.fontSize);
        const bold = parseInt(cs.fontWeight, 10) >= 700;
        const large = px >= 24 || (px >= 18.66 && bold);
        const fgRaw = rgba(cs.color);
        // Text inherits the element's own opacity through every ancestor.
        let op = 1;
        for (let p = el; p; p = p.parentElement) op *= +getComputedStyle(p).opacity;
        const sel = el.tagName.toLowerCase() + (typeof el.className === 'string' && el.className.trim() ? '.' + el.className.trim().split(/\s+/).slice(0, 3).join('.') : '');
        const disabled = el.closest('[disabled], [aria-disabled="true"]');
        let worst = Infinity, worstBg = null, worstFg = null;
        for (const bg of backdrops(el)) {
            const fg = over([fgRaw[0], fgRaw[1], fgRaw[2], fgRaw[3] * op], bg);
            const rr = ratio(fg, bg);
            if (rr < worst) { worst = rr; worstBg = bg; worstFg = fg; }
        }
        rows.push({ sel, text: text.slice(0, 48), px, bold, large, ratio: Math.round(worst * 100) / 100, fg: hex(worstFg), bg: hex(worstBg), need: large ? 3 : 4.5, disabled: !!disabled });
        if (px < minPx) sizes.push({ sel, text: text.slice(0, 48), px });
    }
    return { rows, sizes };
}

(async () => {
    const url = new URL(base);
    if (url.protocol !== 'http:' || !['127.0.0.1', 'localhost', '[::1]'].includes(url.hostname)) throw Error('Measurement requires a loopback browser fixture');
    if ((await api('/api/health')).version !== 'browser-fixture') throw Error('Refusing to measure anything except the disposable browser fixture');
    const projects = (await api('/api/projects')).projects;
    const repo = projects.find((p) => p.kind !== 'work');
    const work = projects.find((p) => p.kind === 'work');
    const pages = [['needs-you', '#/control'], ['projects', '#/projects'], ['help', '#/help']];
    for (const s of ['defaults', 'labels', 'routing', 'runners', 'agents', 'computer', 'phone', 'github', 'alerts']) pages.push(['settings-' + s, '#/settings' + (s === 'defaults' ? '' : '?' + s)]);
    // The pages only the phone has: the Control Deck, the screens its keys open, and the Calendar's three views. Measured at phone width only.
    const phoneOnly = [['phone-deck', '#/deck'], ['phone-my-work', '#/mywork'], ['phone-agents', '#/agents'], ['phone-reviews', '#/reviews'], ['phone-git', '#/repositories'], ['phone-runners', '#/runners'], ['phone-calendar-month', '#/schedule', (pg) => pg.getByRole('group', { name: 'Calendar view' }).getByRole('button', { name: 'Month', exact: true }).click()]];
    phoneOnly.push(['phone-calendar-week', '#/schedule', (pg) => pg.getByRole('group', { name: 'Calendar view' }).getByRole('button', { name: 'Week', exact: true }).click()]);
    for (const [label, p] of [['repo', repo], ['work', work]]) {
        if (!p) continue;
        const secs = p.kind === 'work' ? ['overview', 'board', 'calendar', 'defaults'] : ['overview', 'board', 'calendar', 'git', 'runs', 'defaults'];
        for (const s of secs) pages.push([`${label}-${s}`, `#/p/${p.id}/${s}`]);
        const tasks = (await api(`/api/projects/${p.id}/tasks`)).tasks;
        for (const t of tasks.filter((t) => t.state === 'review' || t.state === 'doing').slice(0, 2)) pages.push([`${label}-task-${t.state}`, `#/p/${p.id}/task/${t.id}`]);
        if (tasks[0]) pages.push([`${label}-task-${tasks[0].state}`, `#/p/${p.id}/task/${tasks[0].id}`]);
    }
    for (const p of projects) {
        if (p === repo || p === work) continue;
        const tasks = (await api(`/api/projects/${p.id}/tasks`)).tasks;
        for (const s of ['board', 'runs']) pages.push([`other-${p.name}-${s}`, `#/p/${p.id}/${s}`]);
        for (const t of tasks.filter((t) => t.state === 'review')) pages.push([`other-${p.name}-task-review`, `#/p/${p.id}/task/${t.id}`]);
    }
    // Git's subsections and drill-downs contain text not visible on its overview.
    // Read actual fixture branches/files so the matrix covers committed review evidence without invented API states.
    for (const p of projects.filter(p => p.kind !== 'work')) {
        const prefix = `git-${p.id}`;
        for (const s of ['health', 'branches', 'prs', 'changes', 'worktrees', 'history']) pages.push([`${prefix}-${s}`, `#/p/${p.id}/git?${s}`]);
        pages.push([`${prefix}-working-changes`, `#/p/${p.id}/git/changes`]);
        const git = await api(`/api/projects/${p.id}/git`);
        for (const b of git.branches.filter(b => b.scope === 'local')) {
            const branch = encodeURIComponent(b.name);
            const base = `#/p/${p.id}/git/branch/local/${branch}`;
            pages.push([`${prefix}-${branch}-branch`, base]);
            pages.push([`${prefix}-${branch}-commits`, `#/p/${p.id}/git/commits/local/${branch}`]);
            const compare = await api(`/api/projects/${p.id}/git/compare?scope=local&branch=${branch}`);
            if (compare.files[0]) pages.push([`${prefix}-${branch}-file`, `${base}/file?${new URLSearchParams({ path: compare.files[0].path })}`]);
        }
    }
    const browser = await chromium.launch({ headless: true, ...(process.env.BROWSER_EXECUTABLE ? { executablePath: process.env.BROWSER_EXECUTABLE } : {}) });
    const ctx = await browser.newContext();
    const page = await ctx.newPage();
    const errors = [];
    page.on('pageerror', (e) => errors.push(e.message));
    await page.goto(base + '/#token=' + token);
    await page.waitForTimeout(800);
    await page.goto(base + '/#/control');
    const allTab = page.getByRole('button', { name: /^All: \d+ items?, including \d+ ready for review$/ });
    await allTab.waitFor();
    assert.equal(await page.getByRole('button', { name: /^Review: \d+ items?$/ }).count(), 1, 'Review remains an explicit filter');
    assert(await page.getByText('Workspace and project badges count interruptions. All also includes work ready for review.', { exact: true }).isVisible(), 'the page explains why All can exceed interruption badges');
    assert.match(await page.locator('.rail .project .num.pend').first().getAttribute('aria-label'), /^\d+ interruptions?; excludes work ready for review$/);
    assert(await page.getByText(/\d+ questions? needs? your answer/, { exact: false }).count() > 0, 'project summaries use Needs you vocabulary');
    const all = [];
    const small = [];
    let nodes = 0;
    for (const theme of ['light', 'dark']) {
        for (const [size, width, height] of (screenshots ? [['desktop', 1440, 900], ['phone', 375, 812]] : [['desktop', 1440, 1000], ['phone', 390, 844]])) {
            await page.setViewportSize({ width, height });
            await page.evaluate((t) => { document.documentElement.dataset.theme = t; }, theme);
            await page.emulateMedia({ colorScheme: theme, reducedMotion: 'reduce' });
            for (const [name, hash, act] of size === 'phone' ? [...pages, ...phoneOnly] : pages) {
                await page.goto(base + '/' + hash);
                await page.waitForTimeout(450);
                if (act) { await act(page); await page.waitForTimeout(250); }
                const res = await page.evaluate(measure, MIN_PX);
                nodes += res.rows.length;
                for (const r of res.rows) all.push({ ...r, theme, size, page: name });
                for (const r of res.sizes) small.push({ ...r, theme, size, page: name });
                if (screenshots) {
                    if (await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 2)) throw Error(`${name} overflows at ${size}/${theme}`);
                    await page.screenshot({ path: require('node:path').join(screenshots, `${name}-${size}-${theme}.png`) });
                }
            }
            if (screenshots && repo) {
                await page.goto(base + `/#/p/${repo.id}/board`);
                await page.getByRole('button', { name: 'New task', exact: true }).click();
                await page.getByRole('heading', { name: 'New task in ' + repo.name, exact: true }).waitFor();
                await page.screenshot({ path: require('node:path').join(screenshots, `new-task-${size}-${theme}.png`) });
                await page.getByRole('button', { name: 'Cancel', exact: true }).click();
                // On a phone the Assistant is a place in the bottom bar and the bar closes it; elsewhere it is the page's button.
                await page.locator(size === 'phone' ? '[data-testid="nav-assistant"]' : '[data-testid="assistant-button"]:visible').click();
                await page.getByTestId('assistant').waitFor();
                await page.screenshot({ path: require('node:path').join(screenshots, `assistant-${size}-${theme}.png`) });
                if (size === 'phone') await page.locator('[data-testid="nav-assistant"]').click();
                else await page.getByTestId('assistant-close').click();
            }
        }
    }
    await browser.close();
    if (errors.length) throw Error('Browser errors: ' + errors.join('; '));
    const fail = all.filter((r) => r.ratio < r.need && !r.disabled);
    const group = (list, key) => {
        const m = new Map();
        for (const r of list) {
            const k = key(r);
            const e = m.get(k) || { ...r, pages: new Set(), count: 0 };
            e.pages.add(r.page); e.count++;
            if (r.ratio !== undefined && r.ratio < e.ratio) Object.assign(e, { ratio: r.ratio });
            m.set(k, e);
        }
        return [...m.values()].map((e) => ({ ...e, pages: [...e.pages] }));
    };
    const contrast = group(fail, (r) => `${r.theme}|${r.sel}|${r.fg}|${r.bg}`).sort((a, b) => a.ratio - b.ratio);
    const tiny = group(small, (r) => `${r.sel}|${r.px}`).sort((a, b) => a.px - b.px);
    console.log(`measured ${nodes} text nodes over ${pages.length} pages (and ${phoneOnly.length} phone-only ones) x 2 themes x 2 sizes`);
    console.log(`\nCONTRAST FAILURES: ${contrast.length} distinct (${fail.length} nodes)`);
    for (const c of contrast) console.log(`${c.ratio.toFixed(2).padStart(5)} <${c.need}  ${c.theme.padEnd(5)} ${c.sel.slice(0, 44).padEnd(44)} ${c.fg} on ${c.bg}  ${c.px}px  "${c.text}"  [${c.pages.slice(0, 3).join(', ')}]`);
    console.log(`\nTEXT BELOW ${MIN_PX}px: ${tiny.length} distinct (${small.length} nodes)`);
    for (const c of tiny) console.log(`${String(c.px).padStart(5)}px  ${c.sel.slice(0, 50).padEnd(50)} "${c.text}"  [${c.pages.slice(0, 3).join(', ')}]`);
    if (jsonOut) fs.writeFileSync(jsonOut, JSON.stringify({ nodes, contrast, tiny }, null, 1));
    if (check && (contrast.length || tiny.length)) process.exit(1);
})().catch((e) => { console.error(e); process.exit(2); });

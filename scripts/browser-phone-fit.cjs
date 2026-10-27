// The phone's Control Deck is a single screen: nothing on it scrolls, and nothing sits outside the viewport, at the sizes phones come in.
const { chromium, webkit } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const assert = require('node:assert/strict');
const base = process.env.PERSONAL_BROWSER_URL;
const owner = 'disposable-browser-credential';
async function request(token, method, route, body) {
    const response = await fetch(base + route, { method, headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body) });
    const raw = await response.text();
    assert(response.ok, `${method} ${route}: ${response.status} ${raw}`);
    return raw ? JSON.parse(raw) : null;
}
const SIZES = [[320, 568], [360, 640], [375, 667], [390, 844], [430, 932]];
(async () => {
    // A controller with no project sends its owner to setup first; every other suite registers the fixture repository, so this does too.
    const repo = require('node:fs').realpathSync(process.env.WERKBORD_BROWSER_REPO);
    if (!(await request(owner, 'GET', '/api/projects')).projects.some(p => p.repoPath === repo)) await request(owner, 'POST', '/api/projects', { path: repo, name: 'Phone fit fixture' });
    // The deck is chosen by the width of the window alone, so no phone is enrolled here: enrolling is rate limited, and the suites that
    // follow in the same controller enrol phones of their own.
    const browser = await (process.env.BROWSER_ENGINE === 'webkit' ? webkit : chromium).launch({ headless: true });
    const errors = [];
    const failures = [];
    try {
        for (const colorScheme of ['light', 'dark']) for (const [width, height] of SIZES) {
            const ctx = await browser.newContext({ viewport: { width, height }, isMobile: true, hasTouch: true, serviceWorkers: 'block', colorScheme });
            await ctx.addInitScript(token => { localStorage.setItem('werkbord.token', token); localStorage.setItem('werkbord-tour-phone-deck-v2', 'fixture'); }, owner);
            const page = await ctx.newPage();
            page.on('pageerror', e => errors.push(e.message));
            await page.goto(base + '/#/deck');
            await page.getByTestId('deck-grid').waitFor();
            await page.waitForFunction(() => document.querySelector('[data-testid="deck-grid"]')?.getAttribute('aria-busy') === 'false');
            await page.waitForTimeout(600); // the keys finish arriving
            const m = await page.evaluate(() => {
                const root = document.scrollingElement;
                window.scrollTo(0, 400); const scrolledTo = window.scrollY; window.scrollTo(0, 0);
                const out = [];
                for (const el of document.querySelectorAll('.deck-page *')) {
                    const r = el.getBoundingClientRect();
                    if (!r.width || !r.height) continue;
                    if (getComputedStyle(el).position === 'fixed') continue;
                    if (r.bottom > innerHeight + 1 || r.top < -1 || r.right > innerWidth + 1 || r.left < -1) out.push(`${el.tagName.toLowerCase()}.${String(el.className).split(' ')[0]} ${Math.round(r.top)}..${Math.round(r.bottom)} x ${Math.round(r.left)}..${Math.round(r.right)}`);
                }
                const keys = [...document.querySelectorAll('[data-deck-position]')].map(e => e.getBoundingClientRect());
                return { scrollable: root.scrollHeight - innerHeight, scrolledTo, outside: out.slice(0, 6), keys: keys.length, smallestKey: Math.round(Math.min(...keys.map(k => Math.min(k.width, k.height)))) };
            });
            const label = `${width}x${height} ${colorScheme}`;
            console.log(label, JSON.stringify(m));
            if (m.scrolledTo !== 0) failures.push(`${label}: the Control Deck scrolls`);
            if (m.scrollable > 1) failures.push(`${label}: the page is ${m.scrollable}px taller than the viewport`);
            if (m.outside.length) failures.push(`${label}: outside the viewport: ${m.outside.join('; ')}`);
            if (m.smallestKey < 44) failures.push(`${label}: a key is ${m.smallestKey}px, under the 44px touch target`);
            await ctx.close();
        }
        assert.deepEqual(failures, []);
        assert.deepEqual(errors, []);
        console.log('PASS the Control Deck fits one screen at phone sizes in both themes');
    } finally { await browser.close(); }
})().catch(e => { console.error(e); process.exitCode = 1; });

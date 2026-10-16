import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, mkdirSync } from 'node:fs';
import { createServer } from 'node:http';
import { createRequire } from 'node:module';

const { chromium } = createRequire(new URL('../../../web/package.json', import.meta.url))('playwright');
const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');
const inject = (html) => html.replace('</head>', '<script defer src="/connector/lanes.js"></script><link rel="stylesheet" href="/connector/lanes.css"></head>');
// The Individual fixture supplies only the existing product navigation contract. Team runs its real shipped view.
const individual = inject(`<!doctype html><html><head><meta name="viewport" content="width=device-width, initial-scale=1"></head><body>
<main><h1>Control Deck</h1><a href="#/projects">Projects</a><a href="#/repositories">Git</a><a href="#/schedule">Calendar</a><a data-workspace="individual" href="#/p/workflow/task/result">Open workflow task</a></main>
<nav data-phone-nav aria-label="Primary"><a href="#/deck">Control Deck</a><button data-testid="nav-assistant" aria-expanded="false">Assistant</button></nav>
<script>document.querySelector('button').onclick = function() { this.setAttribute('aria-expanded', this.getAttribute('aria-expanded') !== 'true'); };</script>
</body></html>`);
const summary = { workspace: { name: 'Studio', state: 'ready' }, at: '2026-10-15T09:00:00Z',
 projects: [{ name: 'Website' }], work: [{ title: 'Release', project: 'Website', status: 'doing', execution: 'running' }],
 attention: [{ title: 'Branch conflict', kind: 'conflict' }, { title: 'Review release', kind: 'review' }], schedule: [], planned: [] };

test('phone sections share scope while the deck and Assistant remain the primary destinations', async () => {
 let failure = false;
 let unavailable = false;
 const server = createServer((req, res) => {
   const path = new URL(req.url, 'http://localhost').pathname;
   if (path === '/team/api/summary') {
     res.writeHead(failure ? 401 : 200, { 'content-type': 'application/json' }); res.end(JSON.stringify(summary)); return;
   }
   const assets = {
     '/connector/lanes.js': ['text/javascript', '../static/connector/lanes.js'],
     '/connector/lanes.css': ['text/css', '../static/connector/lanes.css'],
     '/team/phone.js': ['text/javascript', '../../../internal/team/phone/static/phone.js'],
     '/team/phone.css': ['text/css', '../../../internal/team/phone/static/phone.css'],
   };
   if (assets[path]) { const [type, file] = assets[path]; res.writeHead(200, { 'content-type': type }); res.end(read(file)); return; }
   res.writeHead(200, { 'content-type': 'text/html' });
   res.end(path === '/team/' ? unavailable
     ? inject(read('../static/connector/status.html').replace('__TITLE__', 'Ask your admin to turn on phone access').replace('__TEXT__', 'Pair this phone on your member computer.').replace('<script src="/connector/status.js"></script>', ''))
     : inject(read('../../../internal/team/phone/static/index.html')) : individual);
 });
 await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
 const base = `http://127.0.0.1:${server.address().port}`;
 const browser = await chromium.launch({ headless: true });
 try {
   const page = await browser.newPage({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });
   const errors = []; page.on('pageerror', (e) => errors.push(e.message));
   await page.goto(base + '/#/deck');
   assert.equal(await page.locator('#phone-lanes').isVisible(), false);
   await page.getByRole('link', { name: 'Git', exact: true }).click();
   await page.locator('#phone-lanes').waitFor({ state: 'visible' });
   await page.locator('#phone-lanes').getByRole('link', { name: 'Team', exact: true }).click();
   await page.getByText('Branch conflict', { exact: true }).waitFor();
   assert.equal(new URL(page.url()).hash, '#/repositories');
   assert.equal(await page.locator('#phone-lanes [aria-current]').innerText(), 'Team');
   await page.getByRole('link', { name: 'Back to Control Deck', exact: true }).click();
   assert.equal(await page.locator('#phone-lanes').isVisible(), false);
   await page.getByRole('link', { name: 'Projects', exact: true }).click();
   await page.getByText('Website', { exact: true }).waitFor();
   assert.match(page.url(), /\/team\/#\/projects$/);
   await page.reload(); await page.getByText('Website', { exact: true }).waitFor();
   await page.locator('#phone-primary').getByRole('link', { name: 'Assistant', exact: true }).click();
   await page.waitForFunction(() => document.querySelector('[data-testid="nav-assistant"]')?.getAttribute('aria-expanded') === 'true');
   assert.equal(await page.locator('#phone-lanes').isVisible(), false);
   assert.equal(new URL(page.url()).search, '');
   // Scope survives visiting both main destinations and a full page load.
   await page.goto(base + '/#/deck');
   await page.getByRole('link', { name: 'Calendar', exact: true }).click();
   await page.locator('#section-title').filter({ hasText: 'Calendar' }).waitFor();
   assert.match(page.url(), /\/team\/#\/schedule$/);
   await page.locator('#phone-lanes').getByRole('link', { name: 'Individual', exact: true }).click();
   await page.waitForURL(base + '/#/schedule');
   await page.goto(base + '/#/deck');
   await page.getByRole('link', { name: 'Projects', exact: true }).click();
   await page.waitForURL(base + '/#/projects');
   assert.equal(await page.locator('#phone-lanes [aria-current]').innerText(), 'Individual');
   // A workflow's result is explicitly Individual, independent of remembered section scope.
   await page.evaluate(() => localStorage.setItem('werkbord.phone.lane', 'team'));
   await page.goto(base + '/#/deck');
   await page.getByRole('link', { name: 'Open workflow task', exact: true }).click();
   await page.waitForURL(base + '/#/p/workflow/task/result');
   assert.equal(await page.evaluate(() => localStorage.getItem('werkbord.phone.lane')), 'individual');
   // A failed refresh removes sensitive summary rows; switching away remains possible.
   await page.locator('#phone-lanes').getByRole('link', { name: 'Team', exact: true }).click();
   await page.getByText('Website', { exact: true }).waitFor();
   failure = true;
   await page.getByRole('button', { name: 'Refresh', exact: true }).click();
   await page.getByText('This phone was signed out.', { exact: false }).waitFor();
   assert.equal(await page.locator('#items li').count(), 0);
   assert.equal(await page.locator('#phone-lanes').isVisible(), true);
   failure = false;
   await page.getByRole('button', { name: 'Refresh', exact: true }).click();
   await page.getByText('Website', { exact: true }).waitFor();
   // Real Team sections at the narrowest and widest phone sizes, both themes.
   for (const width of [320, 430, 844]) {
     await page.setViewportSize({ width, height: width === 844 ? 390 : 844 });
     for (const colorScheme of ['light', 'dark']) {
       await page.emulateMedia({ colorScheme });
       for (const section of ['projects', 'repositories', 'schedule', 'control', 'reviews', 'mywork', 'agents', 'analytics', 'runners', 'settings', 'help']) {
         await page.goto(base + '/team/#/' + section);
         await page.getByText(/^Updated /).waitFor();
         assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, `${section} ${width} ${colorScheme}`);
       }
     }
   }
   await page.setViewportSize({ width: 390, height: 844 });
   await page.goto(base + '/team/#/projects'); await page.getByText('Website', { exact: true }).waitFor();
   if (process.env.PHONE_NAV_SCREENSHOT) {
     mkdirSync(new URL('../../../output/playwright/', import.meta.url), { recursive: true });
     await page.screenshot({ path: new URL('../../../output/playwright/phone-team-projects.png', import.meta.url).pathname });
   }
   unavailable = true;
   await page.goto(base + '/team/#/repositories');
   await page.reload();
   await page.getByRole('status').filter({ hasText: 'Ask your admin to turn on phone access' }).waitFor();
   await page.locator('#phone-lanes').getByRole('link', { name: 'Individual', exact: true }).click();
   await page.waitForURL(base + '/#/repositories');
   assert.equal(await page.locator('#phone-lanes [aria-current]').innerText(), 'Individual');
   // Scope controls do not get added to the desktop view.
   await page.setViewportSize({ width: 1280, height: 860 });
   await page.locator('#phone-lanes').waitFor({ state: 'hidden' });
   assert.deepEqual(errors, []);
 } finally { await browser.close(); await new Promise((resolve) => server.close(resolve)); }
});

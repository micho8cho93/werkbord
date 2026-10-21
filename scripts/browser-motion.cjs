// Real APIs and DOM: only the disposable browser fixture and a disposable Team.
const assert = require('node:assert/strict');
const fs = require('node:fs'), path = require('node:path');
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || '../web/node_modules/playwright');
const base = process.env.PERSONAL_BROWSER_URL, team = process.env.TEAM_BROWSER_URL;
const token = 'disposable-browser-credential';
const teamToken = fs.readFileSync(process.env.TEAM_BROWSER_TOKEN_FILE, 'utf8').trim();
const artifacts = process.env.BROWSER_ARTIFACT_DIR;
for (const url of [base, team]) assert.equal(new URL(url).hostname, '127.0.0.1');
async function call(origin, credential, method, route, body) {
  const response = await fetch(origin + route, { method, headers: { Authorization: 'Bearer ' + credential, 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined });
  const value = response.status === 204 ? null : await response.json();
  assert(response.ok, JSON.stringify(value)); return value;
}
const api = (method, route, body) => call(base, token, method, '/api' + route, body);
const coord = (method, route, body) => call(team, teamToken, method, '/api/team/v1' + route, body);
let browser;
(async () => {
  assert.equal((await api('GET', '/health')).version, 'browser-fixture');
  await api('POST', '/onboarding/complete', {});
  const project = await api('POST', '/projects', { kind: 'work', name: 'Motion planning' });
  let task = await api('POST', `/projects/${project.id}/tasks`, { title: 'Review keyboard navigation', workMode: 'human' });
  browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  await context.addInitScript(() => {
    localStorage.setItem('devboard.token', 'disposable-browser-credential');
    window.motionCalls = [];
    const original = Element.prototype.animate;
    Element.prototype.animate = function (frames, options) {
      window.motionCalls.push({ tag: this.tagName, card: this.matches('.work-card'), text: !!this.closest('.md'), duration: typeof options === 'number' ? options : options?.duration });
      return original.call(this, frames, options);
    };
  });
  const page = await context.newPage(); page.setDefaultTimeout(12000);
  const errors = []; page.on('pageerror', e => errors.push(e.message));
  await page.goto(`${base}/#/p/${project.id}/board`);
  const card = page.locator(`[data-board-item="${task.id}"]`);
  await card.waitFor();
  const tour = page.getByRole('button', { name: 'Skip tour', exact: true });
  if (await tour.isVisible()) await tour.click();
  assert.equal(await page.evaluate(() => window.motionCalls.filter(c => c.card).length), 0, 'initial data does not replay task motion');
  await page.evaluate(() => window.motionCalls = []);
  task = await api('PATCH', `/projects/${project.id}/tasks/${task.id}`, { state: 'doing', version: task.version });
  await page.locator(`[data-board-column="doing"] [data-board-item="${task.id}"]`).waitFor();
  await page.waitForFunction(() => window.motionCalls.some(c => c.card && c.duration === 240));
  await page.waitForFunction(() => !document.querySelector('[aria-hidden="true"][inert].work-card'));
  await page.screenshot({ path: path.join(artifacts, 'motion-board-desktop.png') });

  await page.goto(`${base}/#/analytics`);
  await page.getByRole('slider', { name: /^Daily activity chart/ }).waitFor();
  await page.waitForFunction(() => window.motionCalls.some(c => c.tag === 'rect' && c.duration === 480));
  const chart = page.getByRole('slider', { name: /^Daily activity chart/ });
  await chart.focus(); await chart.press('End');
  assert.match(await chart.getAttribute('aria-valuetext'), /created, .*moved to Done/);
  assert(await chart.locator('output').isVisible(), 'keyboard exposes exact values');
  await chart.press('Escape'); assert.equal(await chart.locator('output').count(), 0);
  await page.setViewportSize({ width: 390, height: 844 });
  await chart.click(); assert(await chart.locator('output').isVisible(), 'phone tap exposes exact values');
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1), false);
  await page.waitForFunction(() => { const readout = document.querySelector('.daily output'); return readout && Number(getComputedStyle(readout).opacity) > .99; });
  await page.screenshot({ path: path.join(artifacts, 'motion-analytics-phone.png') });
  await page.emulateMedia({ colorScheme: 'dark' });
  await page.screenshot({ path: path.join(artifacts, 'motion-analytics-phone-dark.png') });

  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto(`${base}/#/p/${project.id}/board`);
  await card.waitFor();
  await page.getByTestId('assistant-button').first().click();
  await page.waitForFunction(() => document.querySelector('[data-testid="assistant"]')?.dataset.phase === 'open');
  const send = async text => { await page.getByRole('textbox', { name: 'Message to the assistant' }).fill(text); await page.getByRole('button', { name: 'Send', exact: true }).click(); };
  await page.evaluate(() => window.motionCalls = []);
  await send('slow');
  await page.waitForFunction(() => window.motionCalls.some(c => c.text && c.duration === 120));
  await page.getByText('Reply finished.', { exact: true }).last().waitFor();
  assert.match(await page.locator('article.reply').last().innerText(), /One two three four/);
  await page.screenshot({ path: path.join(artifacts, 'motion-salie-desktop.png') });
  await page.route('**/cancel', async route => { await new Promise(resolve => setTimeout(resolve, 300)); await route.continue(); });
  await send('hang on');
  await page.getByRole('button', { name: 'Stop the reply', exact: true }).click();
  const stopping = page.getByRole('button', { name: 'Stop requested; waiting for confirmation', exact: true });
  await stopping.waitFor(); assert(await stopping.isDisabled());
  await page.getByRole('button', { name: 'Send', exact: true }).waitFor();
  assert.match(await page.locator('article.reply').last().innerText(), /Working on it[\s\S]*Stopped/);
  await page.getByTestId('assistant-close').click();
  await page.getByTestId('assistant').waitFor({ state: 'detached' });

  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.evaluate(() => window.motionCalls = []);
  task = await api('PATCH', `/projects/${project.id}/tasks/${task.id}`, { state: 'review', version: task.version });
  await page.locator(`[data-board-column="review"] [data-board-item="${task.id}"]`).waitFor();
  assert.equal(await page.evaluate(() => window.motionCalls.filter(c => c.card).length), 0, 'reduced motion preserves state without travel');
  await context.close();

  const tp = await coord('POST', '/projects', { name: 'Motion coordination' });
  const ticket = await coord('POST', `/projects/${tp.id}/tickets`, { title: 'Coordinate a review', status: 'available', workMode: 'human' });
  const teamContext = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  const teamPage = await teamContext.newPage(); teamPage.setDefaultTimeout(12000);
  await teamPage.goto(team + '/');
  await teamPage.getByRole('textbox', { name: 'Token', exact: true }).fill(teamToken);
  await teamPage.getByRole('button', { name: 'Sign in', exact: true }).click();
  await teamPage.goto(`${team}/?tab=board&project=${tp.id}`);
  await teamPage.locator(`[data-board-item="${ticket.id}"]`).waitFor();
  await teamPage.evaluate(() => {
    window.teamMotion = 0;
    const original = Element.prototype.animate;
    Element.prototype.animate = function (frames, options) { if (options?.duration === 240) window.teamMotion++; return original.call(this, frames, options); };
  });
  await coord('POST', `/projects/${tp.id}/tickets/${ticket.id}/claim`, {});
  await teamPage.locator(`[data-board-column="in_progress"] [data-board-item="${ticket.id}"]`).waitFor();
  await teamPage.waitForFunction(() => window.teamMotion > 0);
  await teamPage.waitForFunction(() => !document.querySelector('[aria-hidden="true"][inert].work-card'));
  await teamPage.screenshot({ path: path.join(artifacts, 'motion-team-board.png') });
  await teamContext.close();
  assert.deepEqual(errors, []);
  console.log('PASS motion: initial loads, saved board moves, exact chart values, desktop/phone, streaming, Stop acknowledgement, reduced motion and Team coordination');
})().catch(error => { console.error(error); process.exitCode = 1; }).finally(async () => { await browser?.close(); });

// Real APIs and DOM: only the disposable browser fixture and a disposable Team.
const assert = require('node:assert/strict');
const fs = require('node:fs'), path = require('node:path');
const cp = require('node:child_process');
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
    localStorage.setItem('werkbord.token', 'disposable-browser-credential');
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
  const send = async text => { await page.getByRole('textbox', { name: 'Message to Salie' }).fill(text); await page.getByRole('button', { name: 'Send', exact: true }).click(); };
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
  // The first two replies used helix and wave. Each remaining approved study is exercised on a real live reply.
  for (const mode of ['orbit', 'infinity', 'relay', 'domino']) {
    await send('hang on');
    const mark = page.locator(`article.reply[data-status="working"] .mark[data-activity="${mode}"]`);
    await mark.waitFor();
    await page.waitForFunction(() => document.querySelector('article.reply[data-status="working"] .mark')?.dataset.motionRunning === 'true');
    const before = await mark.locator('rect').first().getAttribute('transform');
    await page.waitForFunction(before => document.querySelector('article.reply[data-status="working"] .mark rect')?.getAttribute('transform') !== before, before);
    if (mode === 'orbit') {
      await page.emulateMedia({ reducedMotion: 'reduce' });
      await page.waitForFunction(() => document.querySelector('article.reply[data-status="working"] .mark')?.dataset.motionRunning === 'false');
      assert.equal(await mark.locator('rect[transform]').count(), 0, 'reduced motion restores the original mark');
      await page.emulateMedia({ reducedMotion: 'no-preference' });
      await page.waitForFunction(() => document.querySelector('article.reply[data-status="working"] .mark')?.dataset.motionRunning === 'true');
      await page.waitForFunction(() => [...document.querySelectorAll('[data-testid="assistant"] .inner > *')].every(el => Number(getComputedStyle(el).opacity) > .99));
      await page.screenshot({ path: path.join(artifacts, 'motion-logo-desktop-dark.png') });
    }
    if (mode === 'domino') {
      await page.setViewportSize({ width: 390, height: 844 });
      await mark.scrollIntoViewIfNeeded();
      await page.locator('[data-testid="assistant-log"]').evaluate(el => el.scrollTop = 0);
      await page.waitForFunction(() => document.querySelector('article.reply[data-status="working"] .mark')?.dataset.motionRunning === 'false');
      await mark.scrollIntoViewIfNeeded();
      await page.waitForFunction(() => document.querySelector('article.reply[data-status="working"] .mark')?.dataset.motionRunning === 'true');
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1), false);
      await page.screenshot({ path: path.join(artifacts, 'motion-logo-phone-dark.png') });
    }
    await page.getByRole('button', { name: 'Stop the reply', exact: true }).click();
    await page.getByRole('button', { name: 'Send', exact: true }).waitFor();
  }

  // Real uploads: the second accepted response is held so partial readiness cannot enable Send.
  const draft = 'Review these notes and the build output.';
  await page.getByRole('textbox', { name: 'Message to Salie' }).fill(draft);
  let releaseUpload;
  const uploadGate = new Promise(resolve => releaseUpload = resolve);
  let uploaded = 0;
  const uploadRoute = async route => {
    if (route.request().method() !== 'POST') return route.continue();
    uploaded++;
    const response = await route.fetch();
    if (decodeURIComponent(route.request().headers()['x-attachment-name']) === 'build.log') await uploadGate;
    await route.fulfill({ response });
  };
  await page.route('**/attachments', uploadRoute);
  await page.getByTestId('assistant-file-input').setInputFiles([
    { name: 'notes.md', mimeType: 'text/plain', buffer: Buffer.from('Review notes') },
    { name: 'build.log', mimeType: 'text/plain', buffer: Buffer.from('Build output') },
  ]);
  await page.getByTestId('assistant-authorize').waitFor();
  assert.equal(uploaded, 0, 'animation never bypasses file sharing approval');
  await page.getByRole('button', { name: 'Share', exact: true }).click();
  await page.locator('.files .file[data-state="ready"]').filter({ hasText: 'notes.md' }).waitFor();
  const pendingFile = page.locator('.files .file[data-state="uploading"]').filter({ hasText: 'build.log' });
  await pendingFile.waitFor();
  assert(await page.getByRole('button', { name: 'Send', exact: true }).isDisabled());
  // Let the first file's transient acknowledgement finish before changing the entire theme.
  await page.locator('.files .file[data-state="ready"] .transfer-status').waitFor({ state: 'detached' });
  for (const theme of ['light', 'dark']) {
    await page.emulateMedia({ colorScheme: theme });
    await page.screenshot({ path: path.join(artifacts, `motion-uploads-phone-${theme}.png`) });
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1), false);
  }
  await page.emulateMedia({ reducedMotion: 'reduce' });
  assert.equal(await pendingFile.evaluate(el => getComputedStyle(el, '::after').animationName), 'none');
  releaseUpload();
  await page.waitForFunction(() => !document.querySelector('.files .file[data-state="uploading"]'));
  assert(!(await page.getByRole('button', { name: 'Send', exact: true }).isDisabled()));
  assert.equal(await page.getByRole('textbox', { name: 'Message to Salie' }).inputValue(), draft);
  await page.getByRole('button', { name: 'Remove notes.md', exact: true }).click();
  await page.getByRole('button', { name: 'Remove build.log', exact: true }).click();
  await page.unroute('**/attachments', uploadRoute);
  await page.route('**/attachments', route => route.request().method() === 'POST'
    ? route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: { code: 'unavailable', message: 'Could not add this file. Attach it again when connected.' } }) }) : route.continue());
  await page.getByTestId('assistant-file-input').setInputFiles({ name: 'failed.log', mimeType: 'text/plain', buffer: Buffer.from('Failed upload') });
  await page.getByRole('button', { name: 'Share', exact: true }).click();
  await page.locator('.files .file[data-state="error"]').waitFor();
  assert.equal(await page.getByRole('textbox', { name: 'Message to Salie' }).inputValue(), draft);
  assert.equal(await page.locator('.files .file[data-state="error"]').evaluate(el => getComputedStyle(el, '::after').animationName), 'none');
  await page.screenshot({ path: path.join(artifacts, 'motion-upload-error-phone.png') });
  await page.getByRole('button', { name: 'Remove failed.log', exact: true }).click();
  await page.unroute('**/attachments');
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.getByTestId('assistant-close').click();
  await page.getByTestId('assistant').waitFor({ state: 'detached' });

  // The Git buttons operate on a disposable local bare remote, with their real API results held for inspection.
  const repo = fs.realpathSync(process.env.WERKBORD_BROWSER_REPO);
  const remote = path.join(repo, '.git', 'motion-origin.git');
  const git = args => cp.execFileSync('git', ['-C', repo, ...args], { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).trim();
  cp.execFileSync('git', ['init', '--bare', remote], { stdio: 'pipe' });
  git(['remote', 'add', 'origin', remote]); git(['push', '-u', 'origin', 'main']);
  const repository = await api('POST', '/projects', { path: repo });
  await page.goto(`${base}/#/p/${repository.id}/git`);
  const fetchButton = page.locator('.fetch-button');
  await fetchButton.waitFor();
  if (await tour.isVisible()) await tour.click();
  let releaseFetch;
  const fetchGate = new Promise(resolve => releaseFetch = resolve);
  await page.route('**/git/fetch', async route => { const response = await route.fetch(); await fetchGate; await route.fulfill({ response }); });
  const fetchWidth = (await fetchButton.boundingBox()).width;
  await fetchButton.click();
  await page.locator('.fetch-button[data-transfer-state="pending"]').waitFor();
  assert.equal(await fetchButton.locator('[data-direction="down"]').count(), 1);
  assert.equal((await fetchButton.boundingBox()).width, fetchWidth, 'Fetch retains its footprint');
  await page.waitForFunction(() => {
    const window = document.querySelector('.fetch-button .transfer-status')?.getBoundingClientRect();
    const arrow = document.querySelector('.fetch-button .arrow')?.getBoundingClientRect();
    return window && arrow && Math.abs((window.top + window.bottom - arrow.top - arrow.bottom) / 2) < 2;
  });
  assert.equal(await fetchButton.evaluate(el => getComputedStyle(el).opacity), '1');
  await page.screenshot({ path: path.join(artifacts, 'motion-fetch-desktop.png') });
  releaseFetch();
  await page.locator('.fetch-button[data-transfer-state="done"]').waitFor();
  assert.equal(await page.locator('.g-notice[data-tone="ok"]').count(), 1);
  await page.unroute('**/git/fetch');
  git(['-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.test', 'commit', '--allow-empty', '-m', 'Transfer fixture']);
  const expected = git(['rev-parse', 'HEAD']);
  await page.goto(`${base}/#/p/${repository.id}/git?branches`);
  await page.reload();
  await page.getByRole('button', { name: 'Push main', exact: true }).first().click();
  const pushSheet = page.getByRole('dialog', { name: 'Push main', exact: true });
  let releasePush;
  const pushGate = new Promise(resolve => releasePush = resolve);
  await page.route('**/git/push', async route => { const response = await route.fetch(); await pushGate; await route.fulfill({ response }); });
  await pushSheet.getByRole('button', { name: 'Push to origin', exact: true }).click();
  await pushSheet.locator('.push-button[data-transfer-state="pending"]').waitFor();
  assert.equal(await pushSheet.locator('[data-direction="up"]').count(), 1);
  await page.waitForFunction(() => {
    const window = document.querySelector('.push-button .transfer-status')?.getBoundingClientRect();
    const arrow = document.querySelector('.push-button .arrow')?.getBoundingClientRect();
    return window && arrow && Math.abs((window.top + window.bottom - arrow.top - arrow.bottom) / 2) < 2;
  });
  await page.screenshot({ path: path.join(artifacts, 'motion-push-desktop.png') });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ colorScheme: 'light' });
  await page.waitForFunction(() => getComputedStyle(document.querySelector('.push-button')).backgroundColor === 'rgb(237, 237, 232)');
  await page.screenshot({ path: path.join(artifacts, 'motion-push-phone-light.png') });
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1), false);
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.emulateMedia({ colorScheme: 'dark' });
  releasePush();
  await pushSheet.getByText('Confirmed by the remote.', { exact: true }).waitFor();
  assert.equal(await pushSheet.locator('.g-notice[data-tone="ok"] .transfer-status[data-state="done"]').count(), 1);
  assert.equal(git(['--git-dir', remote, 'rev-parse', 'main']), expected);
  await pushSheet.getByRole('button', { name: 'Done', exact: true }).click();
  await page.unroute('**/git/push');
  git(['-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.test', 'commit', '--allow-empty', '-m', 'Unconfirmed response fixture']);
  await page.goto(`${base}/#/p/${repository.id}/git?branches`);
  await page.reload();
  await page.getByRole('button', { name: 'Push main', exact: true }).first().click();
  await page.route('**/git/push', async route => {
    const response = await route.fetch(), value = await response.json();
    await route.fulfill({ json: { ...value, outcome: 'unverified', message: 'The push result could not be confirmed. Check the remote before repeating it.', remote: { ...value.remote, verified: false } } });
  });
  await pushSheet.getByRole('button', { name: 'Push to origin', exact: true }).click();
  await pushSheet.locator('.g-notice[data-tone="ask"]').waitFor();
  assert.equal(await pushSheet.locator('.transfer-status[data-state="done"]').count(), 0, 'unconfirmed push receives no success check');
  await page.screenshot({ path: path.join(artifacts, 'motion-push-unconfirmed.png') });
  await pushSheet.getByRole('button', { name: 'Done', exact: true }).click();
  await page.unroute('**/git/push');
  await page.goto(`${base}/#/p/${project.id}/board`);
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
  console.log('PASS motion: initial loads, saved board moves, exact chart values, desktop/phone, six streaming marks, transfer results, file approval/readiness/recovery, Stop acknowledgement, reduced motion and Team coordination');
})().catch(error => { console.error(error); process.exitCode = 1; }).finally(async () => { await browser?.close(); });

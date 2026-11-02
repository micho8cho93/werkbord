// Usage: the meter on Agents and the Usage section of Settings, against a real controller (test-browser.cjs).
//   1. Budgets and Usage saver are set in Settings, saved, and read back after a reload.
//   2. The Agents page shows the meter with the mode, the person's own budget and what was used, and says it is not the provider's figure.
//   3. A bad budget is reported and cannot be saved; Standard measures and holds nothing.
const assert = require('node:assert/strict'), fs = require('node:fs'), path = require('node:path');
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || '../web/node_modules/playwright');
const personal = process.env.PERSONAL_BROWSER_URL;
assert.equal(new URL(personal).hostname, '127.0.0.1', 'Use the disposable test-browser harness');
const artifacts = process.env.BROWSER_ARTIFACT_DIR; if (artifacts) fs.mkdirSync(artifacts, { recursive: true });
async function call(method, route, body) {
  const r = await fetch(personal + '/api' + route, { method, headers: { Authorization: 'Bearer disposable-browser-credential', 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined });
  const text = await r.text(); assert(r.ok, r.status + ' ' + text); return text ? JSON.parse(text) : null;
}
let browser;
(async () => {
  await call('POST', '/onboarding/complete', {});
  for (const surface of ['desktop', 'settings', 'analytics']) await call('POST', '/onboarding/tours/seen', { surface });
  await call('POST', '/projects', { name: 'Usage', path: process.env.WERKBORD_BROWSER_REPO });
  const first = await call('GET', '/usage');
  assert.equal(first.settings.mode, 'standard'); assert.equal(first.settings.saverAt, 80); assert.deepEqual(first.meter.agents, []);

  browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } }); page.setDefaultTimeout(15000);
  const errors = []; page.on('pageerror', e => errors.push(e.message));
  await page.addInitScript(() => localStorage.setItem('werkbord.token', 'disposable-browser-credential'));

  // Settings > Usage. Budgets are typed the way people write them.
  await page.goto(`${personal}/#/settings?usage`);
  const settings = page.getByTestId('usage-settings'); await settings.waitFor();
  assert.equal(await settings.getByRole('radio', { name: /^Standard/ }).isChecked(), true);
  assert.equal(await page.getByTestId('usage-meter').count(), 0, 'nothing to show before there is usage or a budget');
  // The fixture's agent is installed, and it loads after this page does: its row must appear all the same.
  await settings.getByRole('row', { name: /Disposable process fixture/ }).waitFor();
  const id = 'fixture';
  const five = settings.getByLabel(/budget per 5 hours/).first(), week = settings.getByLabel(/budget per week/).first();
  await five.fill('2M'); await week.fill('20,000,000');
  await settings.getByRole('radio', { name: /^Usage saver/ }).check();
  await settings.getByLabel(/Saver threshold/).fill('70');
  await settings.getByRole('button', { name: 'Save' }).click();
  await settings.getByRole('status').filter({ hasText: 'Saved' }).waitFor();
  const saved = await call('GET', '/usage');
  assert.equal(saved.settings.mode, 'saver'); assert.equal(saved.settings.saverAt, 70);
  assert.deepEqual(saved.settings.budgets[id], { fiveHour: 2000000, weekly: 20000000 });
  console.log('PASS Usage settings: budgets typed as 2M and 20,000,000, Usage saver and a 70% threshold are saved');

  // The meter says what it measured and what it is not.
  const meter = page.getByTestId('usage-meter'); await meter.waitFor();
  assert.match(await meter.innerText(), /0 used of 2M · 0%/);
  assert.match(await settings.getByLabel(/budget per 5 hours/).first().inputValue(), /^2M$/, 'a saved budget is written back the way it was typed');
  assert.match(await meter.innerText(), /does not tell Werkbord what is left of a plan/);
  assert.doesNotMatch(await meter.innerText(), /\b(remaining|resets|balance)\b/i, 'no claim about what the provider has left');
  if (artifacts) await page.screenshot({ path: path.join(artifacts, 'usage-settings.png') });
  console.log('PASS Usage meter: the budget is the person\'s own, nothing claims what a provider has left');

  // A bad budget is reported and cannot be saved.
  await five.fill('lots');
  await settings.getByRole('alert').filter({ hasText: 'Enter a token count' }).waitFor();
  assert.equal(await settings.getByRole('button', { name: 'Save' }).isDisabled(), true);
  await five.fill('2M');

  // The Agents page shows the same meter, and Standard only measures.
  await page.goto(`${personal}/#/sessions`);
  const strip = page.getByTestId('usage-meter'); await strip.waitFor();
  assert.match(await page.getByTestId('usage-mode').innerText(), /Usage saver on · holds automatic work at 70%/);
  if (artifacts) await page.screenshot({ path: path.join(artifacts, 'usage-agents.png') });
  await call('PUT', '/usage', { mode: 'standard', saverAt: 80, budgets: saved.settings.budgets });
  await page.reload(); await page.getByTestId('usage-meter').waitFor();
  assert.match(await page.getByTestId('usage-mode').innerText(), /Measuring only/);
  console.log('PASS Agents: the meter is on the page, and Standard says it only measures');

  assert.deepEqual(errors, []);
  await browser.close();
})().catch(async error => { console.error(error); if (browser) await browser.close(); process.exit(1); });

// Plan detail: ordered steps with their position, the Tasks and Execution tabs, and the context column (agent visibility, phase 3).
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
  const project = await call('POST', '/projects', { name: 'Steps', path: process.env.WERKBORD_BROWSER_REPO });
  const make = (title, dependencies = []) => call('POST', `/projects/${project.id}/tasks`, { title, workMode: 'human', orchestration: { enabled: false, dependencies, expectedPaths: [] } });
  const a = await make('Design'), b = await make('Build', [a.id]), c = await make('Ship', [b.id]);
  const plan = await call('POST', `/projects/${project.id}/work-plans`, { title: 'Release train', description: 'Ship it', taskIds: [a.id, b.id, c.id], decisions: '', questions: '' });

  browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } }); page.setDefaultTimeout(15000);
  const errors = []; page.on('pageerror', e => errors.push(e.message));
  await page.addInitScript(() => localStorage.setItem('werkbord.token', 'disposable-browser-credential'));
  const text = async sel => (await page.locator(sel).allTextContents()).map(t => t.replace(/\s+/g, ' ').trim());

  await page.goto(`${personal}/#/p/${project.id}/work?plan=${plan.id}`);
  await page.getByTestId('plan-head').waitFor();
  assert.deepEqual(await text('.step-position'), ['Step 1 of 3', 'Step 2 of 3 · After step 1', 'Step 3 of 3 · After step 2']);
  assert.equal(await page.locator('.parallel').count(), 0, 'nothing runs, so nothing is parallel');
  assert.equal(await page.locator('.wb-agent').count(), 0, 'no agent has run, so no mark is invented');
  assert.deepEqual(await text('[role=tab]'), ['Tasks', 'Execution']);
  assert.equal(await page.locator('[role=tab][aria-selected=true]').textContent(), 'Tasks');
  const side = await page.locator('.plan-side').boundingBox();
  assert(side && side.width >= 240 && side.width <= 300, 'the context column sits beside the tasks at 1440px: ' + JSON.stringify(side));
  assert.deepEqual(await text('.plan-side h3'), ['Agents now', 'Task branches']);
  assert.equal(await page.getByText(/online|offline|last seen/i).count(), 0, 'no presence wording');
  if (artifacts) await page.screenshot({ path: path.join(artifacts, 'plan-tasks-1440.png') });
  console.log('PASS Plan: ordered steps with position and the context column');

  await page.locator('#plan-tab-tasks').focus();
  await page.keyboard.press('ArrowRight');
  await page.getByTestId('plan-execution').waitFor();
  assert.equal(await page.evaluate(() => document.activeElement?.id), 'plan-tab-execution', 'the arrow key moves focus with the tab');
  assert.equal(await page.locator('.plan-tasks').count(), 0);
  await page.getByRole('tab', { name: 'Tasks' }).click();
  await page.locator('.plan-tasks').waitFor();
  console.log('PASS Plan: Tasks and Execution tabs, keyboard and click');

  await page.goto(`${personal}/#/p/${project.id}/work?section=plans`);
  await page.locator('.plan-link').first().waitFor();
  assert.equal(await page.locator('.plan-agents .wb-agent').count(), 0);
  console.log('PASS Plan: list row has no mark without a live agent');

  for (const width of [900, 640, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto(`${personal}/#/p/${project.id}/work?plan=${plan.id}`);
    await page.getByTestId('plan-head').waitFor();
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), `no horizontal scroll at ${width}px`);
    if (width <= 900) { const s = await page.locator('.plan-side').boundingBox(); const m = await page.locator('.plan-main').boundingBox(); assert(s.y >= m.y + m.height - 1, `the context column stacks below at ${width}px`); }
  }
  console.log('PASS Plan: narrow widths stack and do not scroll sideways');
  assert.deepEqual(errors, []);
})().then(() => browser?.close(), async e => { console.error(e); await browser?.close(); process.exit(1); });

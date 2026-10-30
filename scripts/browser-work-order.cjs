// Work queue cards and ordering against a disposable Personal API (test-browser.cjs).
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
  const project = await call('POST', '/projects', { name: 'Ordering', path: process.env.WERKBORD_BROWSER_REPO });
  const make = async title => call('POST', `/projects/${project.id}/tasks`, { title, workMode: 'human' });
  const standalone = {}; for (const title of ['Alpha', 'Bravo', 'Charlie', 'Delta']) standalone[title] = await make(title);
  const planned = {}; for (const title of ['Echo', 'Foxtrot', 'Golf']) planned[title] = await make(title);
  await call('POST', `/projects/${project.id}/work-plans`, { title: 'Release train', description: 'Ship it', taskIds: [planned.Echo.id, planned.Foxtrot.id, planned.Golf.id], decisions: '', questions: '' });
  const serverOrder = async names => (await call('GET', `/projects/${project.id}/tasks`)).tasks.filter(t => names.includes(t.title)).sort((a, b) => a.position - b.position).map(t => t.title);
  const plan = async () => (await call('GET', `/projects/${project.id}/work-plans`))[0];
  const idToTitle = Object.fromEntries([...Object.values(standalone), ...Object.values(planned)].map(t => [t.id, t.title]));

  browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } }); page.setDefaultTimeout(15000);
  const errors = []; page.on('pageerror', e => errors.push(e.message));
  await page.addInitScript(() => localStorage.setItem('werkbord.token', 'disposable-browser-credential'));
  const open = async () => { await page.goto(`${personal}/#/p/${project.id}/work`); await page.getByRole('heading', { name: 'Next', exact: true }).waitFor(); await page.locator('details.queue-section summary').first().waitFor(); };
  const shown = async scope => page.locator(`ul[data-order-scope="${scope}"] > [data-order-item] a.title`).allTextContents();
  const row = (scope, title) => page.locator(`ul[data-order-scope="${scope}"] > [data-order-item]`).filter({ hasText: title });
  async function drag(from, to, side = 'before', { release = true } = {}) {
    await from.hover(); const handle = from.locator('[data-order-handle]'); const h = await handle.boundingBox(); const r = await to.boundingBox();
    await page.mouse.move(h.x + h.width / 2, h.y + h.height / 2); await page.mouse.down();
    await page.mouse.move(h.x + h.width / 2, side === 'before' ? r.y + 4 : r.y + r.height - 4, { steps: 14 });
    if (release) await page.mouse.up();
  }

  await open();
  // One card per queue, projects and plans as quiet disclosures inside it, never cards of their own.
  assert.deepEqual(await page.locator('.queue-card > header h2').allTextContents(), ['Now', 'Next', 'Later']);
  assert.equal(await page.locator('.queue-card .queue-card').count(), 0, 'cards are never nested');
  assert.equal(await page.locator('.queue-section').count(), 1, 'the plan is one disclosure');
  assert.equal((await page.locator('.queue-section > summary').textContent()).replace(/\s+/g, ' ').trim(), 'Release train0 of 3 complete');
  assert.deepEqual(await shown('tasks:next'), ['Alpha', 'Bravo', 'Charlie', 'Delta']);
  assert.deepEqual(await shown(`plan:${(await plan()).id}`), ['Echo', 'Foxtrot', 'Golf']);
  assert.equal(await page.locator('[data-order-handle]').count(), 7);
  assert.equal(await page.locator('[data-board-column]').count(), 0);
  if (artifacts) await page.screenshot({ path: path.join(artifacts, 'work-cards-desktop.png') });
  console.log('PASS Work: queue cards, plan disclosure and handles');

  // A drag moves the row, stores the order and does not open the task.
  const url = page.url();
  await drag(row('tasks:next', 'Delta'), row('tasks:next', 'Alpha'), 'before');
  await page.waitForFunction(() => document.querySelector('ul[data-order-scope="tasks:next"] a.title')?.textContent === 'Delta');
  assert.equal(page.url(), url, 'a drag does not open the task');
  assert.deepEqual(await serverOrder(['Alpha', 'Bravo', 'Charlie', 'Delta']), ['Delta', 'Alpha', 'Bravo', 'Charlie']);
  await open(); assert.deepEqual(await shown('tasks:next'), ['Delta', 'Alpha', 'Bravo', 'Charlie'], 'the order survives a reload');
  console.log('PASS Work: pointer drag reorders and persists');

  // The keyboard alternative: Alt+Arrow on a row, with the move announced.
  await row('tasks:next', 'Alpha').locator('[data-order-handle]').focus();
  await page.keyboard.press('Alt+ArrowDown');
  await page.waitForFunction(() => [...document.querySelectorAll('ul[data-order-scope="tasks:next"] a.title')].map(a => a.textContent).join() === 'Delta,Bravo,Alpha,Charlie');
  assert.deepEqual(await serverOrder(['Alpha', 'Bravo', 'Charlie', 'Delta']), ['Delta', 'Bravo', 'Alpha', 'Charlie']);
  const said = async pattern => page.waitForFunction(source => [...document.querySelectorAll('[role=status][aria-live=polite]')].some(el => new RegExp(source).test(el.textContent)), pattern.source);
  await said(/Alpha moved to position 3 of 4/);
  await row('tasks:next', 'Delta').locator('[data-order-handle]').focus();
  await page.keyboard.press('Alt+ArrowUp');
  await said(/Delta is already first/);
  console.log('PASS Work: keyboard reorder with announcements');

  // Escape cancels a drag in progress and nothing is stored.
  await drag(row('tasks:next', 'Charlie'), row('tasks:next', 'Delta'), 'before', { release: false });
  assert.equal(await page.locator('.order-drop-before').count(), 1, 'the drop line shows where it would land');
  await page.keyboard.press('Escape'); await page.mouse.up();
  assert.deepEqual(await serverOrder(['Alpha', 'Bravo', 'Charlie', 'Delta']), ['Delta', 'Bravo', 'Alpha', 'Charlie']);
  assert.equal(await page.locator('.order-dragging, .order-drop-before, .order-drop-after').count(), 0);
  console.log('PASS Work: Escape cancels a drag');

  // Tasks of a plan reorder the plan, not the project queue.
  const planScope = `plan:${(await plan()).id}`;
  await drag(row(planScope, 'Golf'), row(planScope, 'Echo'), 'before');
  await page.waitForFunction(scope => document.querySelector(`ul[data-order-scope="${scope}"] a.title`)?.textContent === 'Golf', planScope);
  assert.deepEqual((await plan()).taskIds.map(id => idToTitle[id]), ['Golf', 'Echo', 'Foxtrot']);
  assert.deepEqual(await serverOrder(['Alpha', 'Bravo', 'Charlie', 'Delta']), ['Delta', 'Bravo', 'Alpha', 'Charlie'], 'the queue is untouched');
  await open(); assert.deepEqual(await shown(planScope), ['Golf', 'Echo', 'Foxtrot']);
  console.log('PASS Work: plan tasks reorder the plan and persist');

  // When two neighbours have no room between them the whole list is numbered again, in the order chosen.
  await call('PATCH', `/projects/${project.id}/tasks/${standalone.Bravo.id}`, { position: 100, version: (await call('GET', `/projects/${project.id}/tasks`)).tasks.find(t => t.id === standalone.Bravo.id).version });
  const fresh = async id => (await call('GET', `/projects/${project.id}/tasks`)).tasks.find(t => t.id === id);
  await call('PATCH', `/projects/${project.id}/tasks/${standalone.Alpha.id}`, { position: 100 + 1e-12, version: (await fresh(standalone.Alpha.id)).version });
  await open();
  await drag(row('tasks:next', 'Charlie'), row('tasks:next', 'Alpha'), 'before');
  await page.waitForFunction(() => [...document.querySelectorAll('ul[data-order-scope="tasks:next"] a.title')].map(a => a.textContent).join().includes('Bravo,Charlie,Alpha'));
  const stored = (await call('GET', `/projects/${project.id}/tasks`)).tasks.filter(t => ['Alpha', 'Bravo', 'Charlie', 'Delta'].includes(t.title)).sort((a, b) => a.position - b.position);
  assert.deepEqual(stored.map(t => t.title), ['Delta', 'Bravo', 'Charlie', 'Alpha']);
  assert.equal(new Set(stored.map(t => t.position)).size, 4, 'positions are distinct again');
  console.log('PASS Work: a list with no room left is numbered again');

  // Narrow window: cards stay inside the screen and the handles stay reachable.
  await page.setViewportSize({ width: 390, height: 844 }); await open();
  assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'no horizontal scrolling');
  if (artifacts) await page.screenshot({ path: path.join(artifacts, 'work-cards-narrow.png') });
  await page.setViewportSize({ width: 1440, height: 1000 }); await page.emulateMedia({ colorScheme: 'dark' }); await open();
  if (artifacts) await page.screenshot({ path: path.join(artifacts, 'work-cards-dark.png') });
  assert.deepEqual(errors, []);
  console.log('PASS Work: narrow and dark layouts, no page errors');
})().catch(async e => { console.error(e); process.exitCode = 1; }).finally(async () => { await browser?.close(); });

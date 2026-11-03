// Done -> archived -> deleted -> erased, for tasks and for projects, against a disposable Personal (test-browser.cjs).
const assert = require('node:assert/strict'), fs = require('node:fs'), path = require('node:path');
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || '../web/node_modules/playwright');
const personal = process.env.PERSONAL_BROWSER_URL;
const artifacts = process.env.BROWSER_ARTIFACT_DIR || path.join(__dirname, '..', 'output', 'playwright', 'archive');
fs.mkdirSync(artifacts, { recursive: true });
assert.equal(new URL(personal).hostname, '127.0.0.1');
async function call(method, route, body) {
  const r = await fetch(personal + '/api' + route, { method, headers: { Authorization: 'Bearer disposable-browser-credential', 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined });
  const text = await r.text();
  return { status: r.status, body: text ? JSON.parse(text) : null };
}
const ok = async (...args) => { const r = await call(...args); assert(r.status < 300, `${args[0]} ${args[1]} -> ${r.status} ${JSON.stringify(r.body)}`); return r.body; };
let browser;
(async () => {
  await ok('POST', '/onboarding/complete', {});
  for (const surface of ['desktop', 'settings', 'analytics']) await ok('POST', '/onboarding/tours/seen', { surface });
  const project = await ok('POST', '/projects', { name: 'Archive me', kind: 'work' });
  const base = `/projects/${project.id}/tasks`;
  const finished = await ok('POST', base, { title: 'Ship the launch page' });
  await ok('PATCH', `${base}/${finished.id}`, { version: finished.version, state: 'done' });
  const second = await ok('POST', base, { title: 'Write the changelog' });
  await ok('PATCH', `${base}/${second.id}`, { version: second.version, state: 'done' });

  browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
  page.setDefaultTimeout(15000);
  const errors = []; page.on('pageerror', e => errors.push(e.message));
  await page.addInitScript(() => localStorage.setItem('werkbord.token', 'disposable-browser-credential'));
  await page.goto(`${personal}/#/p/${project.id}/work?section=queue`);
  await page.getByRole('button', { name: /^Archive \(0\)/ }).click();
  const section = page.getByRole('region', { name: 'Task archive' });

  // Done tasks are archived by the button, and the project can do it by itself after a chosen number of days.
  await section.getByRole('button', { name: /^Archive done tasks \(2\)/ }).click();
  await section.getByText('Ship the launch page').waitFor();
  await section.getByLabel('Archive done tasks automatically').selectOption('30');
  await section.getByText('Done tasks are archived 30 days after they last change.').waitFor();
  assert.equal((await ok('GET', `/projects/${project.id}/archive-settings`)).afterDays, 30);
  await page.screenshot({ path: path.join(artifacts, 'archived.png') });

  // Deleting asks, then moves the task to Recently deleted with its time left, and it can be restored from there.
  const row = section.locator('li', { hasText: 'Ship the launch page' });
  await row.getByRole('button', { name: 'Delete' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Delete task' }).click();
  await section.getByRole('button', { name: /^Recently deleted/ }).click();
  const gone = section.locator('li', { hasText: 'Ship the launch page' });
  await gone.getByText(/Erased for good in 30 days/).waitFor();
  await page.screenshot({ path: path.join(artifacts, 'recently-deleted.png') });
  await gone.getByRole('button', { name: 'Restore' }).click();
  await section.getByText('Nothing deleted.').waitFor();
  await section.getByRole('button', { name: /^Archived/ }).click();
  await section.getByText('Ship the launch page').waitFor();

  // Erasing now is a second, plainly worded confirmation, and the task and its runs are gone afterwards.
  await section.locator('li', { hasText: 'Ship the launch page' }).getByRole('button', { name: 'Delete' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Delete task' }).click();
  await section.getByRole('button', { name: /^Recently deleted/ }).click();
  await section.locator('li', { hasText: 'Ship the launch page' }).getByRole('button', { name: 'Erase now' }).click();
  await page.getByRole('dialog').getByText(/cannot be undone/).waitFor();
  await page.getByRole('dialog').getByRole('button', { name: 'Erase now' }).click();
  await section.getByText('Nothing deleted.').waitFor();
  const left = (await ok('GET', `${base}`)).tasks.map(t => t.title);
  assert.deepEqual(left, ['Write the changelog']);

  // A project is archived before it can be deleted, and only a deleted one can be erased.
  assert.equal((await call('PUT', `/projects/${project.id}/lifecycle`, { state: 'deleted' })).status, 409);
  assert.equal((await call('DELETE', `/projects/${project.id}`)).status, 409);
  await page.goto(`${personal}/#/settings?history`);
  const history = page.locator('section', { has: page.getByRole('heading', { name: 'Project history' }) }).first();
  const card = history.locator('li', { hasText: 'Archive me' });
  await card.getByRole('button', { name: 'Archive project' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Archive project' }).click();
  await card.getByText(/^Archived/).first().waitFor();
  await card.getByRole('button', { name: 'Delete project' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Delete project' }).click();
  await history.getByRole('button', { name: 'Recently deleted' }).click();
  await card.getByText(/Erased for good in 30 days/).waitFor();
  await page.screenshot({ path: path.join(artifacts, 'project-history.png') });
  await card.getByRole('button', { name: 'Erase now' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Erase now' }).click();
  await history.getByText('No deleted projects yet.').waitFor();
  assert.equal((await call('GET', `/projects/${project.id}`)).status, 404);
  assert.deepEqual(errors, []);
  console.log('PASS Done, archived, deleted and erased: tasks and projects');
})().catch(e => { console.error(e); process.exitCode = 1; }).finally(() => browser && browser.close());

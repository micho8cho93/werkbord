// Team Work queue cards, claims and ordering against a disposable Team service (test-browser.cjs).
const assert = require('node:assert/strict'), fs = require('node:fs'), path = require('node:path');
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || '../web/node_modules/playwright');
const team = process.env.TEAM_BROWSER_URL;
assert.equal(new URL(team).hostname, '127.0.0.1', 'Use the disposable test-browser harness');
const ownerToken = fs.readFileSync(process.env.TEAM_BROWSER_TOKEN_FILE, 'utf8').trim();
const artifacts = process.env.BROWSER_ARTIFACT_DIR; if (artifacts) fs.mkdirSync(artifacts, { recursive: true });
async function call(token, method, route, body) {
  const r = await fetch(team + '/api/team/v1' + route, { method, headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined });
  const text = await r.text(); assert(r.ok, method + ' ' + route + ' ' + r.status + ' ' + text); return text ? JSON.parse(text) : null;
}
const as = token => (method, route, body) => call(token, method, route, body);
let browser;
(async () => {
  const owner = as(ownerToken);
  const project = await owner('POST', '/projects', { name: 'Queue order' });
  const ticket = title => owner('POST', `/projects/${project.id}/tickets`, { title, status: 'available' });
  const A = await ticket('Alpha'), B = await ticket('Bravo'), C = await ticket('Charlie'), D = await ticket('Delta'), E = await ticket('Echo');
  await owner('POST', '/initiatives', { title: 'Launch', objective: 'Ship the launch', projectIds: [project.id], tickets: [{ projectId: project.id, ticketId: D.id }, { projectId: project.id, ticketId: E.id }], decisions: '', questions: '' });
  const people = {};
  for (const name of ['Bo Builder', 'Cy Coder']) {
    const made = await owner('POST', '/members', { name, role: 'member' });
    await owner('PUT', `/projects/${project.id}/members/${made.member.id}`, { role: 'member' });
    people[name] = { id: made.member.id, token: made.token, api: as(made.token) };
  }
  const bo = people['Bo Builder'], cy = people['Cy Coder'];
  const queue = async () => (await owner('GET', `/projects/${project.id}/board`)).tickets.map(t => t.title);

  browser = await chromium.launch({ headless: true });
  const errors = [];
  const open = async (token, viewport = { width: 1440, height: 1000 }) => {
    const context = await browser.newContext({ viewport });
    await context.addInitScript(value => { sessionStorage.setItem('werkbord-team-token', value); localStorage.setItem('werkbord-team-tour', 'seen'); }, token);
    const page = await context.newPage(); page.setDefaultTimeout(15000); page.on('pageerror', e => errors.push(e.message));
    await page.goto(`${team}/?tab=work&project=${project.id}`); await page.getByRole('heading', { name: 'Next', exact: true }).waitFor();
    return page;
  };
  const titles = (page, scope) => page.locator(`ul[data-order-scope="${scope}"] > [data-order-item] .title`).allTextContents();
  const row = (page, scope, title) => page.locator(`ul[data-order-scope="${scope}"] > [data-order-item]`).filter({ hasText: title });
  async function drag(page, from, to, side = 'before') {
    await from.hover(); const h = await from.locator('[data-order-handle]').boundingBox(); const r = await to.boundingBox();
    await page.mouse.move(h.x + h.width / 2, h.y + h.height / 2); await page.mouse.down();
    await page.mouse.move(h.x + h.width / 2, side === 'before' ? r.y + 4 : r.y + r.height - 4, { steps: 14 }); await page.mouse.up();
  }

  // An editor sees cards, a plan disclosure and handles on every row; dragging stores one order everyone shares.
  const ownerPage = await open(ownerToken);
  assert.deepEqual(await ownerPage.locator('.queue-card > header h2').allTextContents(), ['Now', 'Next', 'Later']);
  assert.deepEqual(await titles(ownerPage, 'queue:next'), ['Alpha', 'Bravo', 'Charlie']);
  const planScope = (await ownerPage.locator('ul[data-order-scope^="plan:"]').getAttribute('data-order-scope'));
  assert.deepEqual(await titles(ownerPage, planScope), ['Delta', 'Echo']);
  assert.match((await ownerPage.locator('.queue-section > summary').first().textContent()).replace(/\s+/g, ' '), /Launch0 of 2 complete/);
  assert.equal(await ownerPage.locator('[data-order-handle]').count(), 5);
  if (artifacts) await ownerPage.screenshot({ path: path.join(artifacts, 'team-work-cards-owner.png') });
  await drag(ownerPage, row(ownerPage, 'queue:next', 'Charlie'), row(ownerPage, 'queue:next', 'Alpha'));
  await ownerPage.waitForFunction(() => document.querySelector('ul[data-order-scope="queue:next"] .title')?.textContent === 'Charlie');
  assert.deepEqual((await queue()).filter(t => ['Alpha', 'Bravo', 'Charlie'].includes(t)), ['Charlie', 'Alpha', 'Bravo']);
  await drag(ownerPage, row(ownerPage, planScope, 'Echo'), row(ownerPage, planScope, 'Delta'));
  await ownerPage.waitForFunction(scope => document.querySelector(`ul[data-order-scope="${scope}"] .title`)?.textContent === 'Echo', planScope);
  const plans = await owner('GET', '/initiatives');
  assert.deepEqual(plans[0].initiative.tickets.map(r => r.ticketId), [E.id, D.id]);
  console.log('PASS Team Work: cards, plan disclosure, shared order and plan order for an editor');

  // A contributor cannot set the shared order of unclaimed work: rows show why, not a handle.
  const boPage = await open(bo.token);
  assert.equal(await boPage.locator('[data-order-handle]').count(), 0, 'no handles on work nobody holds');
  assert((await boPage.locator('.order-lock').count()) >= 5);
  assert.match(await boPage.locator('.order-lock').first().getAttribute('aria-label'), /Only people who can edit tickets set the shared order/);
  if (artifacts) await boPage.screenshot({ path: path.join(artifacts, 'team-work-cards-contributor.png') });

  // Claiming moves the ticket into Now, shows who holds it, and gives its owner a handle.
  await row(boPage, 'queue:next', 'Alpha').getByRole('button', { name: 'Claim', exact: true }).click();
  await boPage.locator('ul[data-order-scope="queue:now"] .title', { hasText: 'Alpha' }).waitFor();
  await row(boPage, 'queue:next', 'Bravo').getByRole('button', { name: 'Claim', exact: true }).click();
  await boPage.waitForFunction(() => document.querySelectorAll('ul[data-order-scope="queue:now"] > [data-order-item]').length === 2);
  const mine = row(boPage, 'queue:now', 'Alpha');
  assert.equal(await mine.locator('.owner-chip[data-me]').textContent(), 'BB');
  assert.equal(await mine.locator('[data-order-handle]').count(), 1, 'the owner of a claimed ticket may order it');
  await drag(boPage, row(boPage, 'queue:now', 'Alpha'), row(boPage, 'queue:now', 'Bravo'), 'after');
  await boPage.waitForFunction(() => [...document.querySelectorAll('ul[data-order-scope="queue:now"] .title')].map(e => e.textContent).join() === 'Bravo,Alpha');
  assert.deepEqual((await queue()).filter(t => ['Alpha', 'Bravo'].includes(t)), ['Bravo', 'Alpha']);
  console.log('PASS Team Work: contributors see locks, claiming moves work to Now with an owner chip, owners order their own');

  // Another contributor sees whose work it is and cannot move it; the claimed work is no longer on offer.
  const cyPage = await open(cy.token);
  const held = row(cyPage, 'queue:now', 'Alpha');
  assert.equal(await held.locator('[data-order-handle]').count(), 0);
  assert.match(await held.locator('.order-lock').getAttribute('aria-label'), /Held by Bo Builder\. Only they or an editor can reorder it\./);
  assert.equal(await held.locator('.owner-chip').textContent(), 'BB');
  assert.equal(await held.locator('.owner-chip[data-me]').count(), 0);
  assert.equal(await held.getByRole('button', { name: 'Claim', exact: true }).count(), 0, 'held work cannot be claimed');
  // A move the service refuses leaves the order alone and says so.
  const refused = await fetch(`${team}/api/team/v1/projects/${project.id}/tickets/${A.id}/position`, { method: 'PUT', headers: { Authorization: 'Bearer ' + cy.token, 'Content-Type': 'application/json' }, body: JSON.stringify({ position: 99 }) });
  assert.equal(refused.status, 403);
  assert.deepEqual((await queue()).filter(t => ['Alpha', 'Bravo'].includes(t)), ['Bravo', 'Alpha']);
  // An editor still sees and may move what Bo holds.
  await ownerPage.reload(); await ownerPage.getByRole('heading', { name: 'Now', exact: true }).waitFor();
  assert.equal(await row(ownerPage, 'queue:now', 'Alpha').locator('[data-order-handle]').count(), 1);
  assert.equal(await row(ownerPage, 'queue:now', 'Alpha').locator('.owner-chip[data-me]').count(), 0);
  console.log('PASS Team Work: held work shows its owner and a lock to other contributors; editors may still move it');

  // Narrow and dark.
  const narrow = await open(ownerToken, { width: 390, height: 844 });
  assert(await narrow.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 2), 'no horizontal scrolling');
  if (artifacts) await narrow.screenshot({ path: path.join(artifacts, 'team-work-cards-narrow.png') });
  await ownerPage.emulateMedia({ colorScheme: 'dark' }); await ownerPage.reload(); await ownerPage.getByRole('heading', { name: 'Now', exact: true }).waitFor();
  if (artifacts) await ownerPage.screenshot({ path: path.join(artifacts, 'team-work-cards-dark.png') });
  assert.deepEqual(errors, []);
  console.log('PASS Team Work: narrow and dark layouts, no page errors');
})().catch(e => { console.error(e); process.exitCode = 1; }).finally(async () => { await browser?.close(); });

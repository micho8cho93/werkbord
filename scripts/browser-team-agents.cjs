// Team Work shows which agent a teammate's work is on, read only, with a ring that turns only for a fresh running report
// (test-browser.cjs). The report itself is stood in for by the board's `agents`, which the service tests prove (peer_agents_test.go);
// this checks what the console draws from it: marks, rings, stale, holders, plan steps, participants, and that nothing says anyone is online.
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
  const project = await owner('POST', '/projects', { name: 'Agent visibility' });
  const ticket = title => owner('POST', `/projects/${project.id}/tickets`, { title, status: 'available' });
  const [first, second, third] = [await ticket('Build the login'), await ticket('Write the tests'), await ticket('Nobody has this')];
  await owner('POST', '/initiatives', { title: 'Auth overhaul', objective: 'Replace sign-in', projectIds: [project.id], tickets: [first, second].map(t => ({ projectId: project.id, ticketId: t.id })), decisions: '', questions: '' });
  const people = {};
  for (const name of ['Maya Maker', 'Lucas Looker']) {
    const made = await owner('POST', '/members', { name, role: 'member' });
    await owner('PUT', `/projects/${project.id}/members/${made.member.id}`, { role: 'member' });
    people[name] = { id: made.member.id, token: made.token, api: as(made.token) };
  }
  const maya = people['Maya Maker'], lucas = people['Lucas Looker'];
  await maya.api('POST', `/projects/${project.id}/tickets/${first.id}/claim`);
  await maya.api('POST', `/projects/${project.id}/tickets/${second.id}/claim`);

  // What the service would say of Maya's agent, as Lucas's board receives it.
  let agents = {};
  browser = await chromium.launch({ headless: true });
  const errors = [];
  const open = async (viewport = { width: 1440, height: 1000 }, reducedMotion = 'no-preference') => {
    const context = await browser.newContext({ viewport, reducedMotion });
    await context.addInitScript(value => { sessionStorage.setItem('werkbord-team-token', value); localStorage.setItem('werkbord-team-tour', 'seen'); }, lucas.token);
    const page = await context.newPage(); page.setDefaultTimeout(15000); page.on('pageerror', e => errors.push(e.message));
    await page.route(/\/projects\/[^/]+\/board$/, async route => {
      const response = await route.fetch(); const board = await response.json();
      await route.fulfill({ response, json: { ...board, agents } });
    });
    await page.goto(`${team}/?tab=work&project=${project.id}`); await page.getByRole('heading', { name: 'Now', exact: true }).waitFor();
    return page;
  };
  const card = (page, title) => page.locator('.work-card').filter({ hasText: title });
  const ring = (page, title) => card(page, title).locator('.wb-agent[data-ring]');
  const animation = (locator) => locator.evaluate(el => getComputedStyle(el, '::before').animationName);
  const now = () => new Date().toISOString();

  // A fresh running report: the agent's mark with a turning ring, beside who holds the work.
  agents = { [first.id]: { id: 'codex', state: 'running', observedAt: now(), fresh: true } };
  const page = await open();
  const mark = ring(page, 'Build the login');
  assert.equal(await mark.getAttribute('data-ring'), 'running');
  assert.match(await card(page, 'Build the login').locator('.wb-assign').innerText(), /Codex running[\s\S]*Held by Maya Maker/);
  assert.equal(await animation(mark), 'wb-agent-turn', 'a fresh running report turns the ring');
  // Her other task has no report: her avatar alone, and no ring.
  assert.equal(await ring(page, 'Write the tests').count(), 0);
  assert.match(await card(page, 'Write the tests').locator('.wb-assign').innerText(), /Held by Maya Maker/);
  // Nobody holds the third, so it is on offer.
  assert.equal(await card(page, 'Nobody has this').locator('.wb-assign').count(), 0);
  assert.match(await card(page, 'Nobody has this').innerText(), /Available/);
  if (artifacts) await page.screenshot({ path: path.join(artifacts, 'team-agents-running.png') });
  console.log('PASS Team Work: a fresh running report shows the agent with a turning ring and its holder');

  // Reduced motion keeps the ring still.
  const calm = await open(undefined, 'reduce');
  assert.equal(await animation(ring(calm, 'Build the login')), 'none');
  await calm.context().close();

  // A stale report says so, and nothing turns: never done, never offline.
  agents = { [first.id]: { id: 'codex', state: 'running', observedAt: new Date(Date.now() - 6 * 60000).toISOString(), fresh: false } };
  await page.reload(); await page.getByRole('heading', { name: 'Now', exact: true }).waitFor();
  const stale = ring(page, 'Build the login');
  assert.equal(await stale.getAttribute('data-ring'), 'stale');
  assert.equal(await animation(stale), 'none');
  assert.match(await card(page, 'Build the login').locator('.wb-assign').innerText(), /Codex last reported 6m ago/);
  assert.doesNotMatch(await card(page, 'Build the login').locator('.wb-assign').innerText(), /offline|done|completed/i);
  console.log('PASS Team Work: a stale report does not animate and reads as last reported');

  // A neutral mark: no agent named, the true state shown.
  agents = { [first.id]: { state: 'running', observedAt: now(), fresh: true } };
  await page.reload(); await page.getByRole('heading', { name: 'Now', exact: true }).waitFor();
  assert.match(await card(page, 'Build the login').locator('.wb-assign').innerText(), /Agent running/);
  assert.equal(await ring(page, 'Build the login').locator('.wb-agent-tile.letter').count(), 1);

  // Plan steps show a holder and agent each, and Participants shows roles and tasks held, with nothing about being around.
  agents = { [first.id]: { id: 'claude-code', state: 'running', observedAt: now(), fresh: true } };
  await page.reload(); await page.getByRole('heading', { name: 'Now', exact: true }).waitFor();
  await page.getByRole('button', { name: 'Plans', exact: true }).click();
  await page.locator('.plan-link', { hasText: 'Auth overhaul' }).click();
  await page.locator('.plan-tasks').waitFor();
  assert.match(await page.locator('.plan-tasks > li').first().innerText(), /Step 1 of 2/);
  assert.match(await page.locator('.plan-tasks > li').first().innerText(), /Claude Code running[\s\S]*Held by Maya Maker/);
  assert.match(await page.locator('.plan-tasks > li').nth(1).innerText(), /Step 2 of 2/);
  const people$ = await page.locator('.plan-people').innerText();
  assert.match(people$, /Maya Maker/); assert.match(people$, /member · 2 tasks held/);
  assert.doesNotMatch(await page.locator('main, body').first().innerText(), /\b(online|offline|last seen|available now)\b/i, 'no presence anywhere');
  if (artifacts) await page.screenshot({ path: path.join(artifacts, 'team-agents-plan.png') });
  console.log('PASS Team plan: steps show holder and agent, participants show roles, no presence');

  // The phone width holds, with no page errors.
  const phone = await open({ width: 390, height: 800 });
  assert((await phone.evaluate(() => document.documentElement.scrollWidth)) <= 391, 'no sideways scrolling at 390px');
  assert.deepEqual(errors, []);
  console.log('PASS Team Work: agents at 390px, no page errors');
  await browser.close();
})().catch(async error => { console.error(error); if (browser) await browser.close(); process.exit(1); });

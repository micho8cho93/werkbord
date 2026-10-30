// Disposable Team console regression; invoked by test-browser.cjs.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || '../web/node_modules/playwright');
const fs = require('node:fs'), path = require('node:path'), assert = require('node:assert/strict');
const base = process.env.TEAM_BROWSER_URL;
const token = fs.readFileSync(process.env.TEAM_BROWSER_TOKEN_FILE, 'utf8').match(/wbt_[a-zA-Z0-9]+/)[0];
const artifacts = process.env.BROWSER_ARTIFACT_DIR;
async function api(method, route, body, credential = token) {
  const r = await fetch(base + '/api/team/v1' + route, { method, headers: { Authorization: 'Bearer ' + credential, 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined });
  const raw = await r.text(); assert(r.ok, `${method} ${route}: ${r.status} ${raw}`); return raw ? JSON.parse(raw) : null;
}
(async () => {
  const browser = await chromium.launch({ headless: true });
  try {
    const projects = [];
    for (const name of ['Frontend', 'Backend', 'Shared auth']) projects.push(await api('POST', '/projects', { name, repository: 'https://github.com/acme/' + name.toLowerCase().replaceAll(' ', '-') }));
    const tickets = [];
    for (const p of projects) tickets.push(await api('POST', `/projects/${p.id}/tickets`, { title: p.name + ' auth changes', status: 'available' }));
    const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
    const errors = []; page.on('pageerror', e => errors.push(e.message));
    await page.goto(base + '/?tab=work&project=' + projects[0].id + '#token=' + token);
    await page.getByRole('button', { name: 'New plan', exact: true }).click();
    await page.getByLabel('Title', { exact: true }).fill('Authentication Upgrade');
    await page.getByLabel('Objective', { exact: true }).fill('Coordinate compatible authentication across three repositories.');
    for (const k of tickets) await page.getByLabel(k.key + ' · ' + k.title, { exact: true }).check();
    await page.getByLabel('Prerequisite ticket').selectOption(tickets[2].id);
    await page.getByLabel('Dependent ticket').selectOption(tickets[0].id);
    await page.getByRole('button', { name: 'Add dependency', exact: true }).click();
    await page.getByRole('button', { name: 'Save plan', exact: true }).click();
    await page.getByText('Authentication Upgrade · 0/3 complete', { exact: true }).waitFor();
    await page.getByText('Authentication Upgrade · 0/3 complete', {exact:true}).click();
    await page.getByText('0 of 3 tickets done · 0 reported open PRs', { exact: true }).waitFor();
    await page.getByText('Dependencies and context',{exact:true}).click();
    await page.getByText(tickets[0].key + ' waits for ' + tickets[2].key, { exact: true }).waitFor();
    await page.getByRole('button', { name: 'Edit plan', exact: true }).click();
    await page.getByLabel('Status', { exact: true }).selectOption('paused', { timeout: 5000 }).catch(async e => { console.error(await page.locator('main').innerText()); throw e; });
    await page.getByRole('button', { name: 'Save plan', exact: true }).click();
    assert.equal((await api('GET','/initiatives'))[0].initiative.status,'paused');
    await page.reload();
    await page.getByText('Authentication Upgrade · 0/3 complete', { exact: true }).waitFor();
    for (const [name, width, height] of [['desktop', 1440, 1000], ['phone', 390, 844]]) {
      await page.setViewportSize({ width, height }); await page.evaluate(() => document.fonts.ready);
      await page.screenshot({ path: path.join(artifacts, 'initiatives-' + name + '.png'), fullPage: true });
      assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), name + ' overflow');
    }
    const restricted = await api('POST', '/members', { name: 'Restricted member', role: 'member' });
    await api('PUT', `/projects/${projects[0].id}/members/${restricted.member.id}`, { role: 'member' });
    const hidden = await api('GET', '/initiatives', null, restricted.token); assert.deepEqual(hidden, []);
    const context = await browser.newContext(); const other = await context.newPage();
    await other.goto(base + '/?tab=work&project=' + projects[0].id + '#token=' + restricted.token);
    await other.getByText(/No plans yet/).waitFor();
    assert.equal(await other.getByText('Authentication Upgrade', { exact: true }).count(), 0);
    assert.deepEqual(errors, []);
    console.log('PASS Initiative create/edit/dependency/deep-link, three repositories, restricted member, desktop/phone, no overflow');
  } finally { await browser.close(); }
})().catch(e => { console.error(e); process.exitCode = 1; });

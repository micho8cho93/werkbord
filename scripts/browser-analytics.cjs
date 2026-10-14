// Real API/database → browser verification against the disposable browser harness.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || '../web/node_modules/playwright');
const personal = process.env.PERSONAL_BROWSER_URL, team = process.env.TEAM_BROWSER_URL;
const teamToken = fs.readFileSync(process.env.TEAM_BROWSER_TOKEN_FILE, 'utf8').trim();
const artifacts = process.env.BROWSER_ARTIFACT_DIR;
if (!personal || !team || !artifacts) throw Error('Use the disposable test-browser harness');
for (const url of [personal, team]) assert.equal(new URL(url).hostname, '127.0.0.1');
async function api(base, token, method, url, body) {
  const response = await fetch(base + url, { method, signal: AbortSignal.timeout(15000), headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined }).catch(e => { throw Error(method + ' ' + url + ': ' + e.message); });
  const result = await response.json(); assert(response.ok, JSON.stringify(result)); return result;
}
let browser;
(async () => {
  const token = 'disposable-browser-credential';
  const individual = (method, url, body) => api(personal, token, method, '/api' + url, body);
  const project = await individual('POST', '/projects', { name: 'Analytics fixture', path: process.env.WERKBORD_BROWSER_REPO });
  const task = await individual('POST', `/projects/${project.id}/tasks`, { title: 'Analytics sample' });
  const run = await individual('POST', `/projects/${project.id}/tasks/${task.id}/runs`, { agentId: 'fixture' });
  // Wait for the disposable subprocess to be ready for a normal session finish.
  for (let n = 0; n < 80; n++) {
    const current = await individual('GET', `/projects/${project.id}/runs/${run.id}`);
    if (current.state === 'waiting_for_user') break;
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  await individual('POST', `/projects/${project.id}/runs/${run.id}/finish`, {});
  await individual('PUT', `/projects/${project.id}/runs/${run.id}/usage`, { inputTokens: 120, outputTokens: 0, costKind: 'usage_only', source: 'disposable fixture' });
  await individual('PATCH', `/projects/${project.id}/runs/${run.id}/assessment`, { acceptance: 'accepted' });
  const fresh = (await individual('GET', `/projects/${project.id}/tasks`)).tasks.find(t => t.id === task.id);
  await individual('PATCH', `/projects/${project.id}/tasks/${task.id}`, { state: 'done', version: fresh.version });
  let expected;
  for (let n = 0; n < 20; n++) {
    expected = await individual('GET', `/analytics?project=${project.id}&days=7`);
    if (expected.completed === 1 && expected.accepted === 1 && expected.inputTokens === 120) break;
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  assert.equal(expected.created, 1); assert.equal(expected.completed, 1); assert.equal(expected.inputTokens, 120); assert.equal(expected.outputTokens, 0); assert.equal(expected.actualCostUsd, null); assert.equal(expected.accepted, 1); assert.equal(expected.agentReportedCompletion, null); assert.equal(expected.normalSessionCompletions, 1); assert.equal(expected.verifiedCompletion, null);

  browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  page.setDefaultTimeout(15000);
  const errors = []; page.on('pageerror', e => errors.push(e.message));
  await page.addInitScript(() => localStorage.setItem('devboard.token', 'disposable-browser-credential'));
  await page.goto(`${personal}/#/p/${project.id}/analytics`);
  await page.getByText('Usage', { exact: true }).waitFor();
  const rangeLoaded = page.waitForResponse(r => r.url().includes('/api/analytics') && new URL(r.url()).searchParams.get('days') === '7');
  await page.getByLabel('Analytics time range').selectOption('7');
  await rangeLoaded;
  await page.getByRole('tab', { name: 'Completion', exact: true }).click();
  await page.getByText('Completion evidence', { exact: true }).click();
  await page.getByText('Agent-reported task completion', { exact: true }).waitFor();
  const reportedTaskCompletion = page.getByText('Agent-reported task completion', { exact: true });
  assert.equal(await reportedTaskCompletion.evaluate(el => el.nextElementSibling.textContent), 'Not recorded');
  await page.getByRole('tab', { name: 'Usage', exact: true }).click();
  await page.getByText(/Not reported/).first().waitFor();
  const providerLoaded = page.waitForResponse(r => r.url().includes('/api/analytics') && new URL(r.url()).searchParams.get('provider') === 'fixture');
  await page.getByLabel('Analytics agent provider').selectOption('fixture');
  await providerLoaded;
  await page.getByLabel('Usage measure').selectOption('cost');
  await page.getByRole('heading', { name: 'Reported and estimated API cost' }).waitFor();
  await page.getByRole('tab', { name: 'Activity', exact: true }).click();
  assert.equal(await page.getByLabel('Analytics time range').inputValue(), '7', 'Activity remembers its time range');
  assert.equal(await page.getByLabel('Analytics agent provider').count(), 0, 'Activity has only task-relevant filters');
  await page.getByText('Daily values', { exact: true }).click();
  await page.getByRole('columnheader', { name: 'UTC date', exact: true }).waitFor();
  await page.getByText('Daily values', { exact: true }).click();
  await page.getByRole('tab', { name: 'Execution', exact: true }).click();
  await page.getByRole('heading', { name: 'Run outcomes' }).waitFor();
  await page.getByLabel('Analytics runner').waitFor();
  await page.getByText('Breakdown', { exact: true }).click();
  for (const [width, height, name] of [[1440, 1000, 'analytics-desktop'], [390, 844, 'analytics-mobile']]) {
    await page.setViewportSize({ width, height }); await page.evaluate(() => document.fonts.ready);
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), name + ' page overflow');
    if (name === 'analytics-mobile') {
      const breakdown = page.getByRole('region', { name: 'Runner, provider and model execution breakdown' });
      await breakdown.focus();
      assert(await breakdown.evaluate(el => el === document.activeElement), 'execution breakdown must be keyboard-focusable');
      assert(await breakdown.evaluate(el => el.scrollWidth > el.clientWidth), 'execution breakdown must expose horizontal scrolling');
      await breakdown.evaluate(el => el.blur());
      await page.locator('.analytics').evaluate(el => el.scrollTo(0, 0));
      await page.evaluate(() => window.scrollTo(0, 0));
    }
    await page.screenshot({ path: path.join(artifacts, name + '.png'), fullPage: true });
  }
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.evaluate(() => document.documentElement.dataset.theme = 'dark');
  await page.screenshot({ path: path.join(artifacts, 'analytics-dark.png'), fullPage: true });
  for (const view of ['Activity', 'Usage', 'Completion']) {
    await page.getByRole('tab', { name: view, exact: true }).click();
    await page.getByText('Updating…', { exact: true }).waitFor({ state: 'hidden' });
    await page.screenshot({ path: path.join(artifacts, 'analytics-' + view.toLowerCase() + '.png'), fullPage: true });
    await page.setViewportSize({ width: 390, height: 844 });
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), view + ' mobile overflow');
    await page.screenshot({ path: path.join(artifacts, 'analytics-' + view.toLowerCase() + '-mobile.png'), fullPage: true });
    await page.setViewportSize({ width: 1440, height: 1000 });
  }
  await page.getByRole('tab', { name: 'Activity', exact: true }).focus();
  await page.keyboard.press('ArrowRight');
  assert.equal(await page.getByRole('tab', { name: 'Execution', exact: true }).getAttribute('aria-selected'), 'true');
  assert.equal(await page.getByRole('tab', { name: 'Execution', exact: true }).evaluate(el => el === document.activeElement), true);


  const coord = (method, url, body) => api(team, teamToken, method, '/api/team/v1' + url, body);
  const tp = await coord('POST', '/projects', { name: 'Analytics coordination' });
  const tpOther = await coord('POST', '/projects', { name: 'Analytics other project' });
  const ticket = await coord('POST', `/projects/${tp.id}/tickets`, { title: 'Coordinate sample', status: 'available' });
  await coord('POST', `/projects/${tpOther.id}/tickets`, { title: 'Other project sample', status: 'available' });
  await coord('POST', `/projects/${tp.id}/tickets/${ticket.id}/claim`, {});
  await coord('POST', `/projects/${tp.id}/tickets/${ticket.id}/submit`, {});
  await coord('POST', `/projects/${tp.id}/tickets/${ticket.id}/complete`, {});
  let expectedTeam;
  for (let n = 0; n < 20; n++) {
    expectedTeam = await coord('GET', `/analytics?project=${tp.id}&days=7`);
    if (expectedTeam.completed === 1 && expectedTeam.humanCompletionDecisions === 1) break;
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  assert.equal(expectedTeam.created, 1, 'Team ticket creation count');
  assert.equal(expectedTeam.completed, 1, 'Team completed ticket count');
  assert.equal(expectedTeam.humanCompletionDecisions, 1, 'Team human completion decision count');
  const tpPage = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  tpPage.on('pageerror', e => errors.push(e.message));
  await tpPage.addInitScript(tokenValue => sessionStorage.setItem('werkbord-team-token', tokenValue), teamToken);
  await tpPage.goto(`${team}/?tab=analytics`);
  await tpPage.getByRole('heading', { name: 'Created and completed over time' }).waitFor();
  const createdStat = tpPage.locator('.analytics-statistics dd').first();
  await createdStat.waitFor();
  const allCreated = Number(await createdStat.innerText());
  assert(allCreated >= 2, 'all-project coordination snapshot includes both fixture projects');
  await tpPage.getByLabel('Analytics project').selectOption(tp.id);
  await tpPage.getByLabel('Analytics time range').selectOption('7');
  assert.equal(Number(await createdStat.innerText()), allCreated, 'pending filters do not change the snapshot');
  await tpPage.getByRole('button', { name: 'Apply / refresh' }).click();
  await tpPage.waitForFunction(() => document.querySelector('.analytics-statistics dd')?.textContent === '1');
  assert.equal(await createdStat.innerText(), '1', 'applied project filter');
  for (const [width, height, name] of [[1440, 1000, 'analytics-team-desktop'], [390, 844, 'analytics-team-mobile']]) {
    await tpPage.setViewportSize({ width, height }); await tpPage.evaluate(() => document.fonts.ready);
    assert(await tpPage.evaluate(() => document.documentElement.scrollWidth <= innerWidth), name + ' page overflow');
    await tpPage.screenshot({ path: path.join(artifacts, name + '.png'), fullPage: true });
  }
  await tpPage.getByRole('tab', { name: 'Categories', exact: true }).click();
  await tpPage.getByLabel('Category measure').selectOption('created');
  await tpPage.getByRole('heading', { name: 'Work by category' }).waitFor();
  await tpPage.getByLabel('Work category').selectOption('__unlabelled');
  await tpPage.locator('.analytics-caption').getByText('Unlabelled', { exact: true }).waitFor();
  await tpPage.getByRole('tab', { name: 'Completion', exact: true }).click();
  await tpPage.getByRole('heading', { name: 'Recorded completion signals' }).waitFor();
  await tpPage.getByRole('tab', { name: 'Activity', exact: true }).click();
  await tpPage.getByRole('heading', { name: 'Created and completed over time' }).waitFor();
  assert.equal(await tpPage.getByLabel('Analytics project').inputValue(), tp.id, 'Team Activity remembers its project');
  await tpPage.setViewportSize({ width: 1440, height: 1000 });
  await tpPage.evaluate(() => document.documentElement.dataset.theme = 'dark');
  await tpPage.screenshot({ path: path.join(artifacts, 'analytics-team-dark.png'), fullPage: true });
  assert.deepEqual(errors, []);
  console.log('PASS analytics real API/database → desktop/mobile views, filters, missing usage and completion semantics');
})().catch(e => { console.error(e); process.exitCode = 1; }).finally(async () => { await browser?.close(); });

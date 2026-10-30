// Settings must expose the existing local memory editor without widening Team or phone access.
const assert = require('node:assert/strict');
const path = require('node:path');
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || '../web/node_modules/playwright');
const base = process.env.PERSONAL_BROWSER_URL;
if (!base || new URL(base).hostname !== '127.0.0.1') throw Error('Use the disposable test-browser harness');
async function api(method, route, body) {
  const response = await fetch(base + '/api' + route, { method, headers: { Authorization: 'Bearer disposable-browser-credential', 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined });
  const result = await response.json();
  assert(response.ok, JSON.stringify(result));
  return result;
}
(async () => {
  await api('POST', '/onboarding/complete', {});
  await api('POST', '/onboarding/tours/seen', {surface: 'settings'});
  const project = (await api('GET', '/projects')).projects.find(p => p.repoPath === process.env.WERKBORD_BROWSER_REPO) ?? await api('POST', '/projects', {name: 'Memory fixture', path: process.env.WERKBORD_BROWSER_REPO});
  const original = await api('GET', `/projects/${project.id}/memory`);
  await api('POST', `/projects/${project.id}/memory`, {summary: 'Phase seven inaccurate preference', kind: 'preference'});
  await api('POST', `/projects/${project.id}/memory`, {summary: 'Keep this unrelated decision', kind: 'decision'});
  const browser = await chromium.launch({headless: true});
  try {
    const page = await browser.newPage({viewport: {width: 1440, height: 1000}});
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    await page.addInitScript(() => localStorage.setItem('werkbord.token', 'disposable-browser-credential'));
    await page.goto(base + '/#/settings?memory');
    await page.getByRole('heading', {name: 'Local project memory', exact: true}).waitFor();
    await page.getByLabel('Memory project', {exact: true}).selectOption(project.id);
    const editor = page.locator('section.memory');
    await editor.getByText('Phase seven inaccurate preference', {exact: true}).waitFor();
    await editor.locator('li').filter({hasText: 'Phase seven inaccurate preference'}).getByRole('button', {name: 'Correct', exact: true}).click();
    await editor.getByLabel('What should this project remember?').fill('Phase seven corrected preference');
    await editor.getByRole('button', {name: 'Confirm and save', exact: true}).click();
    await editor.getByText('Phase seven corrected preference', {exact: true}).waitFor();
    await editor.getByLabel('Search memories').fill('Phase seven');
    const searched = page.waitForResponse(response => response.url().includes('/memory?') && new URL(response.url()).searchParams.get('q') === 'Phase seven');
    await editor.getByRole('button', {name: 'Search', exact: true}).click();
    await searched;
    await editor.getByRole('button', {name: 'Clear listed memories…', exact: true}).click();
    await editor.getByRole('button', {name: 'Delete listed memories', exact: true}).click();
    await editor.getByText(/No memories match this view/).waitFor();
    const remaining = await api('GET', `/projects/${project.id}/memory`);
    assert(remaining.records.some(record => record.summary === 'Keep this unrelated decision'));
    assert(!remaining.records.some(record => record.summary.startsWith('Phase seven')), 'clear deletes displayed correction history');
    for (const record of original.records) assert(remaining.records.some(saved => saved.id === record.id), 'unrelated existing memory is preserved');
    await editor.getByLabel('Search memories').fill('');
    await editor.getByRole('button', {name: 'Search', exact: true}).click();
    await editor.getByText('Keep this unrelated decision', {exact: true}).waitFor();
    for (const [width, height] of [[1440,1000], [390,844]]) {
      await page.setViewportSize({width,height});
      assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'Settings must fit the viewport');
      await page.screenshot({path:path.join(process.env.BROWSER_ARTIFACT_DIR, `memory-settings-${width}.png`),fullPage:true});
    }
    assert.deepEqual(errors, []);
    console.log('PASS Settings memory: inspect, correct, clear search matches, preserve unrelated memory, desktop and narrow layout');
  } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exit(1); });

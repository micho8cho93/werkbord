// Settings in the web app lists the sections the Mac window lists (web/src/lib/settings-sections.ts): General first (Appearance, Alerts,
// Keyboard shortcuts), then Individual's own, under one "Projects & workspaces" heading; the sections of the Mac app and the window's own
// are not offered in a browser. Run through the harness: BROWSER_SCRIPT=scripts/browser-settings-sections.cjs node scripts/test-browser.cjs
const assert = require('node:assert/strict');
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || require('node:path').join(__dirname, '../web/node_modules/playwright'));
const base = process.env.PERSONAL_BROWSER_URL, token = 'disposable-browser-credential';
const api = async (method, route, body) => {
  const r = await fetch(base + '/api' + route, { method, headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined });
  if (!r.ok) throw Error(method + route + ' ' + r.status);
};
const ok = msg => console.log('PASS ' + msg);

(async () => {
  await api('POST', '/onboarding/complete', {});
  for (const surface of ['desktop', 'settings', 'analytics', 'phone']) await api('POST', '/onboarding/tours/seen', { surface }).catch(() => {});
  const browser = await chromium.launch();
  const errors = [];
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, serviceWorkers: 'block' });
  await ctx.addInitScript(t => localStorage.setItem('werkbord.token', t), token);
  const page = await ctx.newPage(); page.setDefaultTimeout(15000);
  page.on('pageerror', e => errors.push(e.message));
  try {
    await page.goto(base + '/#/settings');
    const nav = page.getByRole('navigation', { name: 'Settings sections' });
    await page.getByRole('group', { name: 'Theme', exact: true }).waitFor();
    assert.equal(await nav.getByRole('button', { name: 'Appearance', exact: true }).getAttribute('aria-current'), 'page', 'Settings opens on Appearance, as the window does');
    const groups = await nav.getByRole('heading').allInnerTexts();
    assert.deepEqual(groups, ['General', 'Projects & workspaces', 'Work', 'Machines', 'Connections'], 'one list of groups, one Projects & workspaces');
    const links = await nav.getByRole('button').allInnerTexts();
    assert.deepEqual(links.slice(0, 3), ['Appearance', 'Alerts', 'Keyboard shortcuts']);
    for (const absent of ['Updates & about', 'Plan', 'Workspaces', 'Alerts and display']) assert(!links.includes(absent), absent + ' is not offered in a browser');
    assert(links.includes('Time format') && links.includes('Task defaults'));
    ok('Settings opens on Appearance with the shared list, and the Mac-only sections are not offered in a browser');

    await page.getByRole('group', { name: 'Theme', exact: true }).getByRole('button', { name: 'Dark', exact: true }).click();
    assert.equal(await page.locator('html').getAttribute('data-theme'), 'dark');
    await page.getByRole('group', { name: 'Style', exact: true }).getByRole('button', { name: 'Plum', exact: true }).click();
    assert.equal(await page.locator('html').getAttribute('data-style'), 'plum');
    ok('Appearance sets the theme and the style');

    await nav.getByRole('button', { name: 'Keyboard shortcuts', exact: true }).click();
    const keys = await page.getByTestId('shortcut-list').innerText();
    assert.match(keys, /(⌘|Ctrl\+)K/); assert.match(keys, /Fold or show the sidebar/);
    ok('Keyboard shortcuts lists the keys that work everywhere');

    await nav.getByRole('button', { name: 'Alerts', exact: true }).click();
    await page.getByRole('heading', { name: 'Alerts', exact: true }).first().waitFor();
    assert.equal(await page.getByRole('group', { name: 'Theme', exact: true }).count(), 0, 'Theme is Appearance’s, not Alerts’');
    await page.goto(base + '/#/settings?defaults');
    await page.getByRole('heading', { name: 'Defaults for every task' }).waitFor();
    ok('Alerts is its own section, and Task defaults is one address of the list');

    assert.deepEqual(errors, [], 'no page errors');
    console.log('PASS Settings sections in the web app match the window’s list');
  } finally { await browser.close(); }
})().catch(e => { console.error(e.stack || e.message); process.exitCode = 1; });

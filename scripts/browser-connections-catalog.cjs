// Connect a service: the catalog of hosted MCP servers fills in the connection form, and nothing is saved, signed in to or enabled until
// the person goes on. Run against the disposable fixture (test-browser.cjs).
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const assert = require('node:assert/strict');
const base = process.env.PERSONAL_BROWSER_URL;
const token = 'disposable-browser-credential';
assert(base, 'Requires a disposable fixture');
(async () => {
  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  const errors = []; page.on('pageerror', e => errors.push(e.message));
  try {
    await fetch(base + '/api/onboarding/complete', { method: 'POST', headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' }, body: '{}' });
    await fetch(base + '/api/onboarding/tours/seen', { method: 'POST', headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' }, body: JSON.stringify({ surface: 'settings' }) });
    await page.goto(base + '/#token=' + token);
    await page.getByRole('link', { name: 'Settings', exact: true }).waitFor();
    await page.goto(base + '/#/settings?mcp');
    await page.getByRole('button', { name: 'Add connection', exact: true }).click();
    const picker = page.getByRole('dialog', { name: 'Connect a service' });
    await picker.waitFor();
    const tiles = picker.locator('[data-catalog]');
    assert.equal(await tiles.count(), 9, 'eight services and "Add your own"');
    assert.deepEqual((await tiles.locator('strong').allInnerTexts()).slice(0, 3), ['Linear', 'Jira and Confluence', 'Notion']);
    assert.equal(new URL(await picker.getByRole('link', { name: 'Request a connector' }).getAttribute('href')).hostname, 'github.com', 'a request is a page the person writes');
    await page.screenshot({ path: (process.env.BROWSER_ARTIFACT_DIR || '/tmp') + '/connections-catalog.png' });

    // Choosing a service only fills in the form.
    await picker.locator('[data-catalog=linear]').click();
    await picker.waitFor({ state: 'detached' });
    assert.equal(await page.getByRole('textbox', { name: 'Name', exact: true }).inputValue(), 'Linear');
    assert.equal(await page.getByRole('textbox', { name: 'Service URL', exact: true }).inputValue(), 'https://mcp.linear.app/mcp');
    assert.equal(await page.getByLabel('Authentication').inputValue(), 'oauth');
    await page.getByTestId('catalog-note').waitFor();
    await page.getByRole('button', { name: 'Save connection', exact: true }).click();
    await page.getByRole('heading', { name: 'Linear', level: 4 }).waitFor();
    const row = page.locator('ul.connections > li').filter({ hasText: 'Linear' });
    assert.match(await row.innerText(), /Disabled/, 'a new connection starts disabled');
    assert.equal(await row.getByRole('checkbox', { name: 'Connection enabled' }).isChecked(), false, 'and is not turned on');
    assert.match(await page.getByRole('status').filter({ hasText: 'Connection saved' }).innerText(), /Test and discover to sign in/);

    // A service that is added is not offered again; a safer default is kept; "Add your own" is the plain form.
    await page.getByRole('button', { name: 'Add connection', exact: true }).click();
    await picker.waitFor();
    assert.equal(await picker.locator('[data-catalog=linear]').isDisabled(), true);
    await picker.locator('[data-catalog=supabase]').click();
    assert.match(await page.getByRole('textbox', { name: 'Service URL', exact: true }).inputValue(), /read_only=true/, 'Supabase is added read-only');
    await page.getByRole('button', { name: 'Cancel', exact: true }).click();
    await page.getByRole('button', { name: 'Add connection', exact: true }).click();
    await picker.locator('[data-catalog=github]').click();
    assert.equal(await page.getByLabel('Authentication').inputValue(), 'bearer', 'GitHub takes a token');
    await page.getByRole('button', { name: 'Cancel', exact: true }).click();
    await page.getByRole('button', { name: 'Add connection', exact: true }).click();
    await picker.locator('[data-catalog=custom]').click();
    assert.equal(await page.getByRole('textbox', { name: 'Name', exact: true }).inputValue(), '');
    await page.getByRole('button', { name: 'Cancel', exact: true }).click();

    // Clean up what this run saved.
    await row.getByRole('button', { name: 'Remove' }).click();
    await row.getByRole('button', { name: 'Remove?' }).click();
    await page.locator('ul.connections > li').filter({ hasText: 'Linear' }).waitFor({ state: 'detached' });
    assert.deepEqual(errors, [], 'no page errors');
    console.log('PASS connections: the catalog fills in the form, saves disabled, offers each service once, and keeps Supabase read-only');
  } catch (e) { await page.screenshot({ path: (process.env.BROWSER_ARTIFACT_DIR || '/tmp') + '/connections-catalog-failure.png' }).catch(() => {}); throw e; }
  finally { await browser.close(); }
})().catch(e => { console.error(e); process.exit(1); });

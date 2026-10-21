// Exercises the generated credential against the disposable fixture's actual MCP endpoint.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const assert = require('node:assert/strict');
const path = require('node:path');
const base = process.env.PERSONAL_BROWSER_URL;
const token = 'disposable-browser-credential';
assert(base, 'Requires a disposable fixture');
(async () => {
  assert.equal((await (await fetch(base + '/api/health')).json()).version, 'browser-fixture');
  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  const errors = [];
  page.on('pageerror', err => errors.push(err.message));
  try {
    await fetch(base + '/api/onboarding/complete', { method: 'POST', headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' }, body: '{}' });
    await page.goto(base + '/#token=' + token);
    await page.getByRole('link', { name: 'Settings', exact: true }).waitFor();
    await page.goto(base + '/#/settings?mcp');
    await page.getByText('Have an MCP configuration? Paste it here').click();
    await page.getByRole('textbox', { name: 'MCP configuration', exact: true }).fill(JSON.stringify({ mcpServers: { 'Fixture service': { url: base + '/mcp' } } }));
    await page.getByRole('button', { name: 'Fill in connection', exact: true }).click();
    assert.equal(await page.getByRole('textbox', { name: 'Name', exact: true }).inputValue(), 'Fixture service');
    assert.equal(await page.getByRole('textbox', { name: 'Service URL', exact: true }).inputValue(), base + '/mcp');
    await page.getByRole('button', { name: 'Let a client use Werkbord', exact: true }).click();
    await page.getByRole('textbox', { name: 'Client name', exact: true }).fill('Portable browser client');
    const projects = page.locator('.grant-form fieldset').filter({ has: page.locator('legend', { hasText: 'Projects' }) });
    // The full suite already registers repositories; standalone setup creates its own work project.
    if (await projects.getByRole('checkbox').count() === 0) {
      const res = await fetch(base + '/api/projects', { method: 'POST', headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' }, body: JSON.stringify({ name: 'MCP browser project', kind: 'work' }) });
      assert(res.ok);
      await page.reload();
      await page.getByRole('button', { name: 'Let a client use Werkbord', exact: true }).click();
      await page.getByRole('textbox', { name: 'Client name', exact: true }).fill('Portable browser client');
    }
    await projects.getByRole('checkbox').first().check();
    await page.locator('.grant-form fieldset').filter({ has: page.locator('legend', { hasText: 'Operations' }) }).getByRole('checkbox').first().check();
    await page.getByRole('button', { name: 'Create scoped client', exact: true }).click();
    await page.locator('.issued .config').waitFor();
    const config = JSON.parse(await page.locator('.issued .config').innerText()).mcpServers.werkbord;
    assert.equal(config.url, base + '/mcp');
    const headers = { ...config.headers, 'Content-Type': 'application/json', Accept: 'application/json, text/event-stream' };
    const initial = await fetch(config.url, { method: 'POST', headers, body: JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'initialize', params: { protocolVersion: '2025-03-26', capabilities: {}, clientInfo: { name: 'browser-fixture', version: '1' } } }) });
    assert.equal(initial.status, 200);
    const ready = await initial.json();
    assert(ready.result, JSON.stringify(ready));
    if (initial.headers.get('mcp-session-id')) headers['Mcp-Session-Id'] = initial.headers.get('mcp-session-id');
    headers['MCP-Protocol-Version'] = ready.result.protocolVersion;
    await fetch(config.url, { method: 'POST', headers, body: JSON.stringify({ jsonrpc: '2.0', method: 'notifications/initialized' }) });
    const response = await fetch(config.url, { method: 'POST', headers, body: JSON.stringify({ jsonrpc: '2.0', id: 2, method: 'tools/list', params: {} }) });
    assert.equal(response.status, 200, 'generated HTTP settings authenticate against the actual server');
    const result = await response.json();
    assert(result.result?.tools.length > 0, JSON.stringify(result));
    await page.getByRole('checkbox', { name: 'My client needs a local process (stdio)' }).check();
    const stdio = JSON.parse(await page.locator('.issued .config').innerText()).mcpServers.werkbord;
    assert.equal(stdio.command, 'werkbord');
    assert.deepEqual(stdio.args, ['mcp', '--url', base + '/mcp']);
    assert.equal('Bearer ' + stdio.env.WERKBORD_MCP_TOKEN, config.headers.Authorization);
    await page.getByRole('button', { name: 'I’ve saved it — hide credential' }).click();
    assert.equal(await page.locator('.issued').count(), 0);
    await page.screenshot({ path: path.join(process.env.BROWSER_ARTIFACT_DIR, 'mcp-setup-desktop.png') });
    await page.goto(base + '/#/help?article=mcp');
    await page.getByRole('heading', { name: 'Setting up an MCP server', exact: true }).waitFor();
    assert.equal(await page.locator('.guides > li').count(), 1);
    await page.setViewportSize({ width: 390, height: 844 });
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 2), false);
    await page.screenshot({ path: path.join(process.env.BROWSER_ARTIFACT_DIR, 'mcp-help-phone.png') });
    assert.deepEqual(errors, []);
    console.log('PASS MCP setup: import, generated HTTP authentication, stdio settings, credential hiding and phone Help');
  } finally { await browser.close(); }
})().catch(err => { console.error(err); process.exitCode = 1; });

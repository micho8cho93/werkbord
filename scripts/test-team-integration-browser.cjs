// Disposable real APIs + signed connector. No OS service or privileged network.
const { chromium } = require('../web/node_modules/playwright');
const { spawn } = require('node:child_process');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const assert = require('node:assert/strict');
const root = path.resolve(__dirname, '..');
const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'werkbord-integration-browser-'));
const artifacts = process.env.BROWSER_ARTIFACT_DIR || path.join(temp, 'screenshots');
fs.mkdirSync(artifacts, { recursive: true });
const fixture = spawn('go', ['test', './cmd/werkbord-team', '-run', '^TestConnectorBrowserFixture$', '-count=1', '-timeout=4m'], {
  cwd: root,
  env: { ...process.env, WERKBORD_SKIP_RQLITE: '1', WERKBORD_CONNECTOR_BROWSER_FIXTURE: temp },
  stdio: ['ignore', 'pipe', 'pipe'],
});
let diagnostic = '', browser;
fixture.stdout.on('data', d => { diagnostic += d; });
fixture.stderr.on('data', d => { diagnostic += d; });
const finished = new Promise(resolve => fixture.once('exit', code => resolve(code)));
const delay = ms => new Promise(resolve => setTimeout(resolve, ms));
async function capture(page, suffix) {
  for (const [name, width, height] of [['desktop', 1440, 1000], ['phone', 390, 844]]) {
    await page.setViewportSize({ width, height });
    await page.evaluate(() => document.fonts.ready);
    await page.locator('[aria-label="Individual execution"]').scrollIntoViewIfNeeded();
    await page.screenshot({ path: path.join(artifacts, name + suffix + '.png'), fullPage: true });
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'horizontal overflow: ' + name);
  }
}
(async () => {
  try {
    const ready = path.join(temp, 'ready.json');
    for (let n = 0; !fs.existsSync(ready); n++) {
      if (fixture.exitCode !== null || n > 600) throw Error('Fixture failed: ' + diagnostic);
      await delay(100);
    }
    const m = JSON.parse(fs.readFileSync(ready, 'utf8'));
    browser = await chromium.launch({ headless: true });
    const page = await browser.newPage();
    const errors = [];
    page.on('pageerror', e => errors.push(e.message));
    await page.goto(m.url + '/?tab=board&project=' + m.project + '&ticket=' + m.ticket + '#token=' + encodeURIComponent(m.token));
    await page.getByRole('heading', { name: 'Individual execution', exact: true }).waitFor();
    await page.getByText('Needs input', { exact: true }).waitFor();
    await capture(page, '');
    assert(await page.getByText('Execution completion leaves ticket review and approval to the team.', { exact: true }).isVisible());
    // Browser-only state fixtures exercise expiry, multiple reporters and empty
    // display; authoritative server ordering/revocation are covered in Go tests.
    await page.route('**/progress', async route => {
      const data = await (await route.fetch()).json();
      await route.fulfill({ json: [{ ...data[0], stale: true }, { ...data[0], deviceId: 'other_device_1234567890', execution: { ...data[0].execution, state: 'completed' } }] });
    });
    await page.reload();
    await page.getByText('Update overdue. Runner availability is unknown until the connector reconnects.').waitFor();
    await page.getByText('Was available in the owner’s Werkbord', { exact: true }).waitFor();
    assert.equal(await page.locator('.execution-progress').count(), 2);
    assert.equal(await page.locator('.execution-progress code').count(), 2);
    await capture(page, '-review');
    await page.unroute('**/progress');
    await page.route('**/progress', route => route.fulfill({ json: [] }));
    await page.reload();
    await page.getByText(/No execution reported/).waitFor();
    assert.equal(errors.length, 0, errors.join('\n'));
    console.log('PASS signed progress, stale/multiple-device/empty states, desktop/phone, no errors or overflow');
  } finally {
    if (browser) await browser.close();
    fs.writeFileSync(path.join(temp, 'stop'), '');
    const code = await finished;
    if (code !== 0) throw Error(diagnostic);
    if (!process.env.BROWSER_ARTIFACT_DIR) console.log('Screenshots: ' + artifacts);
    fs.rmSync(path.join(temp, 'ready.json'), { force: true });
  }
})().catch(e => { console.error(e); process.exitCode = 1; });

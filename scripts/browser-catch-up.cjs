// Real web navigation: visit cursors, a draft handed to Salie, remote refusal and blocked storage.
const assert = require('node:assert/strict');
const { chromium } = require('../web/node_modules/playwright');
const base = process.env.PERSONAL_BROWSER_URL, token = 'disposable-browser-credential';
const api = async (route, body, method = 'POST') => { const r = await fetch(base + '/api' + route, { method, headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' }, body: JSON.stringify(body) }); const raw = await r.text(); assert(r.ok, raw); return raw ? JSON.parse(raw) : null; };
(async () => {
  await api('/onboarding/complete', {});
  await api('/onboarding/tours/seen', { surface: 'desktop' });
  const project = await api('/projects', { kind: 'work', name: 'Catch-up fixture' });
  const task = await api(`/projects/${project.id}/tasks`, { title: 'A recorded completion', workMode: 'human' });
  const browser = await chromium.launch();
  const errors = [];
  try {
    const ctx = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
    await ctx.addInitScript(t => localStorage.setItem('werkbord.token', t), token);
    const page = await ctx.newPage(); page.on('pageerror', e => errors.push(e.message));
    await page.goto(base + '/#/control');
    await page.getByRole('heading', { name: 'Since you were last here' }).waitFor();
    await page.getByText('Your next visit will show what changed while you were away.').waitFor();
    // Leaving fires pagehide and records the first visit. No invented visit state.
    await page.goto(base + '/api/health');
    await api(`/projects/${project.id}/tasks/${task.id}`, { version: task.version, state: 'done' }, 'PATCH');
    await page.goto(base + '/#/control');
    await page.getByRole('button', { name: '0 pull requests merged · 1 item completed', exact: true }).waitFor();
    let sent = 0;
    page.on('request', r => { if (r.method() === 'POST' && /\/assistant\/.*\/(messages|turns)/.test(r.url())) sent++; });
    await page.getByRole('button', { name: 'Catch me up with Salie', exact: true }).click();
    const draft = page.getByRole('textbox', { name: 'Message to Salie' });
    await draft.waitFor();
    assert.match(await draft.inputValue(), /^Catch me up on the work recorded since/);
    assert.match(await draft.inputValue(), /Completed: A recorded completion/);
    await page.waitForTimeout(400);
    assert.equal(sent, 0, 'catch-up stays in the composer until the person sends');
    console.log('PASS first and second visits, catch-up facts, and Salie receives an unsent draft');
    await page.route('**/api/device', route => route.fulfill({ json: { remote: true } }));
    await page.reload();
    await page.getByRole('heading', { name: 'Running now', exact: false }).waitFor();
    assert.equal(await page.getByRole('button', { name: 'Catch me up with Salie' }).count(), 0, 'remote client cannot offer catch-up to Salie');
    console.log('PASS catch-up with Salie is hidden for a remote client');
    const privateContext = await browser.newContext();
    await privateContext.addInitScript(() => Object.defineProperty(window, 'localStorage', { get() { throw new DOMException('Storage unavailable', 'SecurityError'); } }));
    const privatePage = await privateContext.newPage(); privatePage.on('pageerror', e => errors.push(e.message));
    await privatePage.goto(base + '/#token=' + token);
    await privatePage.waitForFunction(() => !location.hash.includes('token='));
    await privatePage.goto(base + '/#/control');
    await privatePage.getByRole('heading', { name: 'Since you were last here' }).waitFor();
    assert.deepEqual(errors, []);
    console.log('PASS Overview and catch-up survive a blocked localStorage getter');
  } finally { await browser.close(); }
})().catch(e => { console.error(e); process.exitCode = 1; });

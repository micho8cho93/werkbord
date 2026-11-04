// The Calendar page in the web app (web/src/routes/Calendar.svelte, drawn by web/src/lib/calendar/CalendarPage.svelte, the page the Mac window
// draws too): its sidebar entry between Work and Git, #/calendar, planned items on their days, the No date tray, a date dragged onto a day,
// the edit sheet, and the project the picker names. Run through the harness: BROWSER_SCRIPT=scripts/browser-calendar.cjs node scripts/test-browser.cjs
const assert = require('node:assert/strict');
const fs = require('node:fs'), path = require('node:path');
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || path.join(__dirname, '../web/node_modules/playwright'));
const base = process.env.PERSONAL_BROWSER_URL, token = 'disposable-browser-credential';
const api = async (method, route, body) => {
  const r = await fetch(base + '/api' + route, { method, headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined });
  const t = await r.text(); if (!r.ok) throw Error(method + route + ' ' + r.status + ' ' + t); return t ? JSON.parse(t) : null;
};
const ok = msg => console.log('PASS ' + msg);
const day = n => { const d = new Date(); d.setDate(d.getDate() + n); return d.getFullYear() + '-' + String(d.getMonth() + 1).padStart(2, '0') + '-' + String(d.getDate()).padStart(2, '0'); };
const wait = async (check, label) => { for (let n = 0; n < 100; n++) { if (await check()) return; await new Promise(r => setTimeout(r, 100)); } throw Error('Timed out: ' + label); };
async function drag(page, from, to) {
  const a = await from.boundingBox(), b = await to.boundingBox(); assert(a && b, 'drag targets');
  await page.mouse.move(a.x + a.width / 2, a.y + Math.min(a.height / 2, 20)); await page.mouse.down();
  await page.mouse.move(b.x + b.width / 2, b.y + Math.min(b.height / 2, 60), { steps: 12 }); await page.mouse.up();
}

(async () => {
  await api('POST', '/onboarding/complete', {});
  for (const surface of ['desktop', 'settings', 'analytics', 'phone']) await api('POST', '/onboarding/tours/seen', { surface }).catch(() => {});
  const repo = fs.realpathSync(process.env.WERKBORD_BROWSER_REPO);
  const billing = await api('POST', '/projects', { path: repo, name: 'Billing app' });
  const mk = (project, title, extra = {}) => api('POST', `/projects/${project.id}/tasks`, { title, description: 'What to change.', execution: { agent: 'fixture' }, ...extra });
  const planned = await mk(billing, 'Write the launch announcement', { workMode: 'human' });
  const undated = await mk(billing, 'Refactor invoice export');
  const second = await api('POST', '/projects', { name: 'Release planning', kind: 'work' });
  await mk(second, 'Plan the offsite', { workMode: 'human' });
  const edit = (project, t, plan) => api('PATCH', `/projects/${project.id}/tasks/${t.id}`, { version: t.version, plan });
  const plannedNow = await edit(billing, planned, { start: day(1), end: day(1), time: '09:30' });
  const taskOf = async (project, id) => (await api('GET', `/projects/${project.id}/tasks`)).tasks.find(t => t.id === id);

  const browser = await chromium.launch();
  const errors = [];
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 1000 }, serviceWorkers: 'block' });
  await ctx.addInitScript(t => localStorage.setItem('werkbord.token', t), token);
  const page = await ctx.newPage(); page.setDefaultTimeout(15000);
  page.on('pageerror', e => errors.push(e.message));
  try {
    await page.goto(base + '/#/control');
    const side = page.getByTestId('sidebar');
    await side.getByRole('link', { name: 'Work', exact: true }).waitFor();
    const names = await side.locator('.destinations a').allInnerTexts();
    assert.deepEqual(names.map(n => n.replace(/\s*\d+$/, '').trim()), ['Work', 'Calendar', 'Git', 'Agents', 'Analytics'], 'Calendar sits between Work and Git');
    await side.getByRole('link', { name: 'Calendar', exact: true }).click();
    await page.waitForURL(/#\/calendar$/);
    await side.locator('a[aria-current="page"]', { hasText: 'Calendar' }).waitFor();
    await page.getByRole('heading', { name: 'Calendar', exact: true }).waitFor();
    assert.equal(await page.getByRole('heading', { name: 'Calendar', exact: true }).count(), 1, 'one heading, not a second from the page around it');
    ok('the sidebar lists Calendar between Work and Git, and #/calendar opens it');

    const item = page.locator(`[data-plan-item="personal/${planned.id}/plan"]`).first();
    await item.waitFor();
    assert.equal(await item.getAttribute('data-plan-start'), day(1));
    const tray = page.getByRole('complementary', { name: 'No date' });
    await tray.getByText('Refactor invoice export').waitFor();
    assert.equal(await tray.getByText('Plan the offsite').count(), 1, 'work with no date waits in the No date tray');
    ok('a planned item stands on its day and undated work waits in the No date tray');

    const trayItem = tray.locator(`[data-plan-item="personal/${undated.id}/plan"]`);
    const target = day(3);
    await drag(page, trayItem, page.locator(`[data-plan-day="${target}"]`));
    await wait(async () => (await taskOf(billing, undated.id)).plan?.start === target, 'a date dragged onto a day is saved');
    await page.locator(`[data-plan-item="personal/${undated.id}/plan"][data-plan-start="${target}"]`).first().waitFor();
    assert.equal(await tray.getByText('Refactor invoice export').count(), 0, 'it leaves the tray once it has a date');
    ok('dragging a No date item onto a day plans it, and it leaves the tray');

    await item.click();
    const sheet = page.getByRole('dialog', { name: 'Edit task' });
    await sheet.getByLabel('Title').fill('Write the launch notes');
    await sheet.getByRole('button', { name: 'Save changes' }).click();
    await wait(async () => (await taskOf(billing, planned.id)).title === 'Write the launch notes', 'the edit is saved');
    await page.locator(`[data-plan-item="personal/${planned.id}/plan"]`).first().waitFor();
    ok('an item opens the edit sheet and saves its words');

    await page.getByTestId('project-context').click();
    await page.getByTestId('project-list').getByRole('option').filter({ hasText: 'Release planning' }).first().click();
    await page.waitForURL(new RegExp('#/calendar\\?project=' + second.id));
    await tray.getByText('Plan the offsite').waitFor();
    await page.locator(`[data-plan-item="personal/${planned.id}/plan"]`).waitFor({ state: 'detached' });
    assert.equal(await tray.getByText('Refactor invoice export').count(), 0, 'only the chosen project is on the calendar');
    assert.equal(await page.getByTestId('project-context').innerText().then(t => t.includes('Release planning')), true, 'the picker names the project');
    ok('the project picker scopes the Calendar, as it does in the window');

    await page.goto(base + '/#/p/' + billing.id + '/calendar');
    await side.locator('a[aria-current="page"]', { hasText: 'Work' }).waitFor();
    ok('an old project calendar link lands on that project’s Work');

    assert.deepEqual(errors, [], 'no page errors');
    console.log('PASS the Calendar page in the web app');
  } finally { await browser.close(); }
})().catch(e => { console.error(e.stack || e.message); process.exitCode = 1; });

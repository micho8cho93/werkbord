// A phone and the computer share one copy of the generated workspaces. Real controller fixture (its embedded web app),
// a real enrolled-phone credential, a phone-sized touch browser. No model provider.
// Needs the web app built into the controller first: make web web-embed. Run: node scripts/browser-personal-phone.cjs
const fs = require('node:fs'), os = require('node:os'), path = require('node:path');
const cp = require('node:child_process'), net = require('node:net');
const { once } = require('node:events');
const assert = require('node:assert/strict');
const root = path.resolve(__dirname, '..');
const { chromium, webkit } = require(process.env.PLAYWRIGHT_MODULE || path.join(root, 'web/node_modules/playwright'));
const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'werkbord-personal-phone-'));
const artifacts = process.env.BROWSER_ARTIFACT_DIR || path.join(root, 'output/playwright/personal-phone');
fs.mkdirSync(artifacts, { recursive: true });
const owner = 'disposable-browser-credential';
const children = [];
const delay = ms => new Promise(r => setTimeout(r, ms));
const ok = text => console.log('PASS ' + text);
async function freePort() {
  const server = net.createServer(); server.listen(0, '127.0.0.1');
  await once(server, 'listening'); const port = server.address().port;
  await new Promise(resolve => server.close(resolve)); return port;
}
function start(binary, args, cwd, env) {
  const child = cp.spawn(binary, args, { cwd, env: { ...process.env, ...env }, stdio: ['ignore', 'pipe', 'pipe'] });
  child.diagnostics = '';
  for (const stream of [child.stdout, child.stderr]) stream.on('data', d => child.diagnostics += d);
  children.push(child); return child;
}
async function until(fn, message) {
  for (let i = 0; i < 150; i++) { if (await fn()) return; await delay(100); }
  throw new Error(message);
}
const collection = {
  name: 'Personal to-dos', schemaVersion: 1,
  fields: [
    { key: 'title', name: 'To-do', type: 'text', required: true },
    { key: 'done', name: 'Completed', type: 'boolean', required: true },
    { key: 'priority', name: 'Priority', type: 'select', required: true, options: ['Low', 'Normal', 'High'] },
  ],
};
const workspace = (name, collectionId) => {
  const fields = ['title', 'done', 'priority'];
  const all = ['create', 'update', 'delete'];
  return {
    name, icon: 'check', uiSchemaVersion: 1, visibility: { universal: true, individual: true }, collectionIds: [collectionId], readBindings: [],
    definition: {
      views: [
        { id: 'todos', name: 'To-dos', kind: 'checklist', collectionId, fields, checkedField: 'done', operations: all },
        { id: 'table', name: 'All records', kind: 'table', collectionId, fields, operations: all },
      ],
      layout: { type: 'stack', children: [{ type: 'view', viewId: 'todos' }, { type: 'view', viewId: 'table' }] },
    },
  };
};
(async () => {
  const binary = path.join(temp, 'fixture');
  cp.execFileSync('go', ['build', '-o', binary, './scripts/browser-fixture'], { cwd: root, stdio: 'inherit' });
  const port = await freePort();
  const base = `http://127.0.0.1:${port}`;
  const controller = start(binary, [], root, { WERKBORD_BROWSER_ADDR: `127.0.0.1:${port}`, WERKBORD_BROWSER_DIR: temp, WERKBORD_BROWSER_ASSISTANT: '1' });
  await until(async () => { if (controller.exitCode !== null) throw Error(controller.diagnostics); return fetch(base + '/api/health').then(r => r.ok).catch(() => false); }, 'the controller did not start: ' + controller.diagnostics);
  const call = async (token, method, route, data, expect) => {
    const res = await fetch(base + route, { method, headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' }, body: data === undefined ? undefined : JSON.stringify(data) });
    const raw = await res.text();
    if (expect !== undefined) { assert.equal(res.status, expect, `${method} ${route}: ${res.status} ${raw}`); return raw ? JSON.parse(raw) : null; }
    assert.ok(res.ok, `${method} ${route}: ${res.status} ${raw}`);
    return raw ? JSON.parse(raw) : null;
  };
  await call(owner, 'POST', '/api/onboarding/complete', {});
  const code = await call(owner, 'POST', '/api/phones/codes', {});
  const enrolled = await fetch(base + '/api/phones/enroll', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ code: code.code, name: 'Personal phone fixture' }) }).then(r => r.json());
  const phoneToken = enrolled.token;
  assert.match(phoneToken, /^wbp_/);
  const personal = '/api/personal/v1';
  const removeWorkspace = async id => {
    const saved = await call(owner, 'GET', `${personal}/workspaces/${id}`);
    const preview = await call(owner, 'POST', `${personal}/workspaces/${id}/deletion-preview`, { revision: saved.revision, mode: 'keep_data' });
    await call(owner, 'DELETE', `${personal}/workspaces/${id}`, { revision: preview.revision, mode: preview.mode, fingerprint: preview.fingerprint, confirmed: true });
  };

  // At the computer: a workspace and one record.
  const tasks = await call(owner, 'POST', personal + '/collections', collection);
  const first = await call(owner, 'POST', personal + '/workspaces', workspace('Made at the computer', tasks.id));
  await call(owner, 'POST', `${personal}/collections/${tasks.id}/records`, { values: { title: 'Plan the week', done: false, priority: 'High' } });

  const engine = process.env.BROWSER_ENGINE === 'webkit' ? webkit : chromium;
  const browser = await engine.launch({ headless: true });
  const context = await browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, serviceWorkers: 'block' });
  await context.addInitScript(token => {
    localStorage.setItem('werkbord.token', token);
    localStorage.setItem('werkbord.theme', 'light');
    localStorage.setItem('werkbord-tour-phone-deck-v2', 'fixture');
  }, phoneToken);
  const page = await context.newPage();
  page.setDefaultTimeout(10000);
  const errors = [];
  page.on('pageerror', e => errors.push(e.message));
  const overflow = () => page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  const shot = name => page.screenshot({ path: path.join(artifacts, name + '.png') });
  try {
    // 1. It is on the phone's deck without any step on the phone.
    await page.goto(base + '/#/deck');
    // The workspaces are in the Add button sheet with Create with Salie, not under the deck.
    const section = page.getByTestId('deck-workspaces');
    await page.getByRole('button', { name: 'Empty slot. Add a button' }).first().click();
    await section.getByRole('button', { name: 'Made at the computer' }).waitFor();
    await section.getByRole('button', { name: /^Create with Salie/ }).waitFor();
    assert.equal(await overflow(), 0, 'the deck must not scroll sideways');
    await delay(500); await shot('add-button-with-workspace');
    ok('a workspace made at the computer is in the phone’s Add button list, with Create with Salie');

    // 2. Another one made while the phone is looking appears without a reload.
    const second = await call(owner, 'POST', personal + '/workspaces', workspace('Made while you watched', tasks.id));
    await section.getByRole('button', { name: 'Made while you watched' }).waitFor();
    ok('a workspace made later shows up live, with no reload');
    await page.getByRole('button', { name: 'Close' }).click();
    await section.waitFor({ state: 'detached' });
    assert.equal(await page.locator('.workspace-row').count(), 0, 'the list is not under the deck');

    // 3. Opening it: Back, records, no owner-only controls.
    await page.goto(base + '/#/personal?id=' + first.id);
    await page.getByRole('heading', { name: 'Made at the computer', level: 1 }).waitFor();
    await page.getByTestId('phone-back').waitFor();
    assert.equal(await page.getByRole('button', { name: 'Data library' }).count(), 0, 'the data library is the owner’s');
    assert.equal(await page.getByText('Manage workspace').count() + await page.getByLabel('Manage workspace').count(), 0, 'no workspace management on a phone');
    assert.equal(await page.getByText('Saved on your computer').count(), 1);
    ok('the workspace opens on the phone with Back and without owner-only controls');

    // 4. A table reads as cards on the phone; nothing scrolls sideways.
    await page.locator('.table-cards').waitFor({ state: 'visible' });
    assert.equal(await page.locator('.table-scroll').isVisible(), false, 'the wide table is not shown on a phone');
    assert.equal(await overflow(), 0, 'the workspace must not scroll sideways');
    const card = page.locator('.table-cards li').first();
    assert.ok((await card.innerText()).includes('Plan the week'));
    await shot('workspace-phone');
    ok('tables read as labelled cards and the page does not scroll sideways');

    // 5. Ticking a to-do on the phone is what the computer reads.
    const checklist = page.getByRole('region', { name: 'To-dos', exact: true });
    await checklist.getByRole('checkbox', { name: 'Complete Plan the week', exact: true }).check();
    await until(async () => (await call(owner, 'GET', `${personal}/collections/${tasks.id}/records`)).items[0].values.done === true, 'the tick did not reach the computer');
    ok('a record changed on the phone is what the computer reads');

    // 6. What the computer changes arrives on the phone while it is open.
    const record = (await call(owner, 'GET', `${personal}/collections/${tasks.id}/records`)).items[0];
    await call(owner, 'PUT', `${personal}/collections/${tasks.id}/records/${record.id}`, { revision: record.revision, values: { ...record.values, title: 'Plan the week, edited at the computer' } });
    await page.getByText('Plan the week, edited at the computer').first().waitFor();
    ok('a record changed at the computer shows on the open phone page without a reload');

    // 7. Adding a record from the phone, as a form that fits.
    await checklist.getByRole('button', { name: 'Add record', exact: true }).click();
    const editor = checklist.getByRole('form', { name: 'Add record' });
    await editor.getByLabel('To-do (required)', { exact: true }).fill('Call the dentist');
    await editor.getByLabel('Priority (required)', { exact: true }).selectOption('Low');
    await editor.getByRole('button', { name: 'Save record', exact: true }).click();
    await editor.waitFor({ state: 'detached' });
    await until(async () => (await call(owner, 'GET', `${personal}/collections/${tasks.id}/records`)).items.some(r => r.values.title === 'Call the dentist'), 'the new record did not reach the computer');
    assert.equal(await overflow(), 0);
    ok('a record added on the phone is saved to the one copy');

    // 8. A button on the Control Deck.
    await page.getByTestId('workspace-deck-button').click();
    await page.getByText('Added to the Control Deck.').waitFor();
    await page.getByTestId('phone-back').click();
    const key = page.locator('[data-key]').filter({ hasText: 'Made at the computer' });
    await key.waitFor();
    assert.equal(await key.evaluate(el => el.tagName), 'A');
    const layout = await call(phoneToken, 'GET', '/api/deck');
    assert.ok(layout.slots.some(s => s && s.button === first.id), 'the deck keeps the button');
    await shot('deck-with-button');
    await key.click();
    await page.getByRole('heading', { name: 'Made at the computer', level: 1 }).waitFor();
    ok('the workspace can be a Control Deck button that opens it');

    // 9. What a phone may not do stays refused.
    await call(phoneToken, 'DELETE', `${personal}/workspaces/${first.id}`, { revision: first.revision }, 403);
    await call(phoneToken, 'POST', personal + '/workspaces', workspace('Made by the phone', tasks.id), 403);
    await call(phoneToken, 'PUT', `${personal}/workspaces/${second.id}`, { revision: second.revision, workspace: workspace('Renamed', tasks.id) }, 403);
    ok('a phone cannot create, change or delete a workspace');

    // 10. Deleted at the computer: the phone's list and button follow, and nothing breaks.
    await page.getByTestId('phone-back').click();
    await page.locator('[data-key]').filter({ hasText: 'Made at the computer' }).waitFor();
    await removeWorkspace(first.id);
    await page.locator('[data-key]').filter({ hasText: 'Removed workspace' }).waitFor();
    ok('a workspace deleted at the computer leaves the phone’s list and its button says so');
    // 11. Made with Salie on the phone: confirmed on the phone, then it is the computer's too, and on the deck at once.
    await removeWorkspace(second.id); // the fixture's model only proposes a workspace when there is none yet
    await page.getByRole('button', { name: 'Empty slot. Add a button' }).first().click();
    await section.waitFor();
    await section.locator(`[data-workspace-id="${first.id}"], [data-workspace-id="${second.id}"]`).waitFor({ state: 'detached' });
    await page.getByTestId('deck-create-workspace').click();
    const box = page.getByRole('textbox', { name: 'Message to Salie' });
    await box.waitFor();
    const creationRequest = await box.inputValue();
    assert.match(creationRequest, /create a new generative workspace/i, 'the phone starts from the shared creation request');
    assert.match(creationRequest, /ask me where it should appear/i, 'placement remains a person’s choice');
    await box.fill(creationRequest + 'A weekly to-do list with deadlines and priorities.');
    await page.getByRole('button', { name: 'Send', exact: true }).click();
    const proposal = page.locator('.confirm[data-state="waiting"]');
    await proposal.getByRole('button', { name: 'Confirm', exact: true }).waitFor();
    assert.equal((await call(owner, 'GET', personal + '/workspaces')).items.filter(w => w.name === 'Weekly to-dos').length, 0, 'nothing is saved before the person confirms');
    await shot('salie-confirm-phone');
    await proposal.getByRole('button', { name: 'Confirm', exact: true }).click();
    await page.getByRole('link', { name: 'Open saved workspace', exact: true }).waitFor();
    const made = (await call(owner, 'GET', personal + '/workspaces')).items.find(w => w.name === 'Weekly to-dos');
    assert.ok(made, 'the computer reads the workspace the phone confirmed');
    await page.getByTestId('nav-deck').click(); // on a phone the Control Deck tab closes the Assistant
    await page.getByRole('button', { name: 'Empty slot. Add a button' }).first().click();
    await section.locator(`[data-workspace-id="${made.id}"]`).waitFor();
    ok('a workspace made with Salie on the phone is saved once confirmed, and is in the Add button list and for the computer');
    await section.locator(`[data-workspace-id="${made.id}"]`).click();
    await page.getByTestId('edit-deck').click();
    await page.locator('[data-key]').filter({ hasText: 'Weekly to-dos' }).waitFor();
    assert.ok((await call(phoneToken, 'GET', '/api/deck')).slots.some(s => s && s.button === made.id), 'the button is saved');
    ok('the new workspace can be put on a button straight from that list');
    assert.deepEqual(errors, [], 'no browser exceptions');
  } catch (err) {
    console.error(await page.locator('body').ariaSnapshot());
    console.error('Fixture diagnostics:',controller.diagnostics);
    await shot('failure').catch(() => {});
    throw err;
  } finally {
    await browser.close();
  }
  console.log('Screenshots: ' + artifacts);
})().catch(err => { console.error(err); process.exitCode = 1; }).finally(() => { for (const c of children) c.kill('SIGTERM'); });

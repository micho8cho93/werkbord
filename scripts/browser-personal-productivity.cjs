// Productivity views of a generated workspace: a word processor, pages, boards, calendars, snippets, a file viewer and
// shortcuts. Real Svelte, real controller and SQLite, real key presses. No model provider.
// Run: node scripts/browser-personal-productivity.cjs
const fs = require('node:fs'), os = require('node:os'), path = require('node:path');
const cp = require('node:child_process'), net = require('node:net');
const { once } = require('node:events');
const assert = require('node:assert/strict');
const root = path.resolve(__dirname, '..');
const { chromium } = require(path.join(root, 'web/node_modules/playwright'));
const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'werkbord-productivity-'));
const artifacts = process.env.BROWSER_ARTIFACT_DIR || path.join(root, 'output/playwright/personal-productivity');
fs.mkdirSync(artifacts, { recursive: true });
const token = 'disposable-browser-credential';
const children = [];
const delay = ms => new Promise(r => setTimeout(r, ms));
let browser, lastPage;
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
async function stop(child) {
  if (child.exitCode !== null) return;
  child.kill('SIGTERM');
  await Promise.race([once(child, 'exit'), delay(5000)]);
  if (child.exitCode === null) child.kill('SIGKILL');
}
async function until(fn, message, tries = 120) {
  for (let i = 0; i < tries; i++) { if (await fn()) return; await delay(100); }
  throw new Error(message);
}
async function ready(url, child) {
  await until(async () => { if (child.exitCode !== null) throw Error(child.diagnostics); return fetch(url).then(r => r.ok).catch(() => false); }, 'Server did not start: ' + child.diagnostics);
}
const ok = text => console.log('PASS ' + text);
// The screen follows a save by a moment: wait for what it should show.
const shows = (locator, value) => until(async () => (await locator.inputValue().catch(() => null)) === value, `Expected ${value}`, 60);

(async () => {
  const binary = path.join(temp, 'fixture');
  cp.execFileSync('go', ['build', '-o', binary, './scripts/browser-fixture'], { cwd: root, stdio: 'inherit' });
  const apiPort = await freePort(), webPort = await freePort();
  const base = `http://127.0.0.1:${apiPort}`, web = `http://127.0.0.1:${webPort}`;
  const controller = start(binary, [], root, { WERKBORD_BROWSER_ADDR: `127.0.0.1:${apiPort}`, WERKBORD_BROWSER_DIR: temp,
    WERKBORD_BROWSER_TERMINALS: '1', WERKBORD_BROWSER_TERMINAL_CLI: '/usr/bin/false' });
  await ready(base + '/api/health', controller);
  const call = async (method, url, data, status) => {
    const res = await fetch(base + '/api/personal/v1' + url, { method, headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' }, body: data === undefined ? undefined : JSON.stringify(data) });
    const raw = await res.text();
    if (status) { assert.equal(res.status, status, `${method} ${url}: ${raw}`); return raw ? JSON.parse(raw) : undefined; }
    assert.ok(res.ok, `${method} ${url}: ${res.status} ${raw}`);
    return raw ? JSON.parse(raw) : undefined;
  };
  assert.ok((await fetch(base + '/api/onboarding/complete', { method: 'POST', headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' }, body: '{}' })).ok);
  const vite = start(process.execPath, [path.join(root, 'web/node_modules/vite/bin/vite.js'), '--host', '127.0.0.1', '--port', String(webPort), '--strictPort'], path.join(root, 'web'), { WERKBORD_URL: base });
  await ready(web, vite);

  const field = (key, name, type, extra = {}) => ({ key, name, type, required: false, ...extra });
  const ops = ['create', 'update', 'delete'];
  const collection = spec => call('POST', '/collections', { schemaVersion: 1, ...spec });
  const workspace = (name, views, layout) => call('POST', '/workspaces', {
    name, icon: 'auto', uiSchemaVersion: 1, readBindings: [], visibility: { universal: true, individual: true, teamWorkspaceIds: [] },
    collectionIds: [...new Set(views.map(v => v.collectionId))], definition: { views, layout },
  });
  const records = async cid => (await call('GET', `/collections/${cid}/records?limit=100`)).items || [];

  browser = await chromium.launch({ headless: true, ...(process.env.BROWSER_EXECUTABLE ? { executablePath: process.env.BROWSER_EXECUTABLE } : {}) });
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, permissions: ['clipboard-read', 'clipboard-write'] });
  await context.addInitScript(token => { localStorage.setItem('devboard.token', token); localStorage.setItem('werkbord.theme', 'light'); }, token);
  const page = await context.newPage(); lastPage = page;
  const errors = [], outside = [];
  page.on('pageerror', e => errors.push(e.message));
  page.on('console', m => { if (m.type() === 'error') errors.push(`${m.location().url}: ${m.text()}`); });
  page.on('request', r => { const u = new URL(r.url()); if (!['127.0.0.1', 'localhost'].includes(u.hostname) && u.protocol.startsWith('http')) outside.push(r.url()); });
  page.setDefaultTimeout(10000);
  const open = async w => { await page.goto(`${web}/#/personal?id=${w.id}`); await page.reload(); };

  // ---- Documents -------------------------------------------------------------------------------------------------
  const notes = await collection({ name: 'Notes', fields: [field('title', 'Title', 'text', { required: true }), field('body', 'Body', 'longtext'), field('parent', 'Parent', 'text'), field('tags', 'Tags', 'multiselect', { options: ['idea', 'plan'] })] });
  const docs = await workspace('Writing', [{ id: 'doc', name: 'Documents', kind: 'document', collectionId: notes.id, fields: ['title', 'tags'], bodyField: 'body', operations: ops }], { type: 'view', viewId: 'doc' });
  await open(docs);
  const region = page.getByRole('region', { name: 'Documents', exact: true });
  await region.getByText('No documents yet.', { exact: false }).first().waitFor();
  await region.getByTestId('document-new').click();
  const title = region.getByRole('textbox', { name: 'Title', exact: true });
  await title.waitFor();
  assert.equal(await title.inputValue(), 'Untitled');
  await title.fill('Launch plan');
  await page.keyboard.press('Enter');
  const text = region.getByRole('textbox', { name: 'Document text' });
  await text.click();
  // Real key presses: the Markdown shortcuts work as they are typed.
  await page.keyboard.type('# Goals');
  await page.keyboard.press('Enter');
  await page.keyboard.type('Ship the **editor** and the `board`.');
  await page.keyboard.press('Enter');
  await page.keyboard.type('- [ ] write the spec');
  await page.keyboard.press('Enter');
  await page.keyboard.type('review it');
  await page.keyboard.press('Enter');
  await page.keyboard.press('Enter');
  await page.keyboard.type('1. first');
  await page.keyboard.press('Enter');
  await page.keyboard.type('second');
  await page.keyboard.press('Enter');
  await page.keyboard.press('Enter');
  await page.keyboard.type('> a quote');
  await page.keyboard.press('Enter');
  await page.keyboard.press('Enter');
  await page.keyboard.type('See [[Other page]] soon.');
  assert.equal(await text.locator('h1').innerText(), 'Goals');
  assert.equal(await text.locator('strong').innerText(), 'editor');
  assert.equal(await text.locator('code').first().innerText(), 'board');
  assert.equal(await text.locator('li[data-task]').count(), 2);
  assert.equal(await text.locator('ol li').count(), 2);
  assert.equal(await text.locator('blockquote').count(), 1);
  assert.equal(await text.locator('.wiki-link').innerText(), '↗ Other page'.replace('↗ ', '') );
  await until(async () => (await records(notes.id))[0]?.values.body?.includes('Other page'), 'The document was not saved as it was typed');
  let saved = (await records(notes.id))[0];
  assert.equal(saved.values.title, 'Launch plan');
  assert.equal(saved.values.body, '# Goals\n\nShip the **editor** and the `board`.\n\n- [ ] write the spec\n- [ ] review it\n\n1. first\n2. second\n\n> a quote\n\nSee [[Other page]] soon.');
  await region.getByText(/^Saved/).first().waitFor();
  ok('A document is written with real key presses, formatted by Markdown shortcuts and saved as Markdown');

  await text.locator('li[data-task] input').first().check();
  await until(async () => (await records(notes.id))[0].values.body.includes('- [x] write the spec'), 'A checked task was not saved');
  ok('A task box can be ticked and the tick is saved');

  // The toolbar formats the selection and undo takes it back.
  await text.locator('h1').click({ clickCount: 3 });
  await region.getByRole('button', { name: 'Italic', exact: true }).click();
  assert.equal(await text.locator('h1 em').count(), 1);
  await region.getByRole('button', { name: 'Undo', exact: true }).click();
  assert.equal(await text.locator('h1 em').count(), 0);
  await page.keyboard.press('Meta+Z');
  await region.getByRole('button', { name: 'Bulleted list', exact: true }).waitFor();
  ok('The toolbar formats a selection and undo reverses it');

  // A link needs a safe address.
  await text.locator('p', { hasText: 'Ship the' }).click();
  await page.keyboard.press('Home');
  await page.keyboard.press('Shift+End');
  await region.getByRole('button', { name: 'Link', exact: true }).click();
  const link = region.getByRole('form', { name: 'Link' });
  await link.getByLabel('Web address').fill('javascript:alert(1)');
  await link.getByRole('button', { name: 'Apply' }).click();
  await link.getByRole('alert').waitFor();
  assert.equal(await text.locator('a').count(), 0);
  await link.getByLabel('Web address').fill('example.com/spec');
  await link.getByRole('button', { name: 'Apply' }).click();
  assert.equal(await text.locator('a').first().getAttribute('href'), 'https://example.com/spec');
  assert.equal(await text.locator('a').first().getAttribute('rel'), 'noopener noreferrer');
  ok('Links accept web addresses only and open safely');

  // The document survives a reload, and a second one can be made and chosen.
  await until(async () => (await records(notes.id))[0].values.body.includes('example.com/spec'), 'The link was not saved');
  await page.reload();
  const again = page.getByRole('region', { name: 'Documents', exact: true });
  await again.getByRole('textbox', { name: 'Document text' }).locator('h1').waitFor();
  await shows(again.getByRole('textbox', { name: 'Title', exact: true }), 'Launch plan');
  await again.getByTestId('document-new').click();
  await again.getByRole('textbox', { name: 'Title', exact: true }).fill('Second note');
  await until(async () => (await records(notes.id)).some(r => r.values.title === 'Second note'), 'The second title was not saved');
  await again.getByRole('button', { name: /Launch plan/ }).click();
  await shows(again.getByRole('textbox', { name: 'Title', exact: true }), 'Launch plan');
  await again.getByRole('searchbox', { name: 'Search documents' }).fill('second note');
  assert.equal(await again.locator('aside li button').count(), 1);
  await again.getByRole('searchbox', { name: 'Search documents' }).fill('');
  await again.getByRole('button', { name: 'Copy as Markdown' }).click();
  assert.match(await page.evaluate(() => navigator.clipboard.readText()), /^# Launch plan\n\n# Goals/);
  ok('Documents persist, can be searched and chosen, and copy out as Markdown');

  await again.getByRole('button', { name: 'Delete', exact: true }).first().click();
  await again.getByRole('button', { name: 'Delete', exact: true }).last().click();
  await until(async () => (await records(notes.id)).length === 1, 'The document was not deleted');
  ok('A document can be deleted');

  // Stored text is never trusted: raw HTML, scripts and unsafe links are shown as text.
  await call('POST', `/collections/${notes.id}/records`, { values: { title: 'Hostile', body: '<script>window.__pwned=1</script> <img src=x onerror="window.__pwned=2"> [x](javascript:window.__pwned=3) ![i](https://evil.example/p.png)\n\n<div onclick="window.__pwned=4">click</div>' } });
  await page.reload();
  await page.getByRole('region', { name: 'Documents', exact: true }).getByRole('button', { name: /Hostile/ }).click();
  const hostile = page.getByRole('region', { name: 'Documents', exact: true }).getByRole('textbox', { name: 'Document text' });
  await hostile.waitFor();
  assert.equal(await page.evaluate(() => window.__pwned), undefined);
  assert.equal(await hostile.locator('script, img, div').count(), 0);
  assert.equal(await hostile.locator('a[href^="javascript"]').count(), 0);
  assert.deepEqual(outside, []);
  ok('Hostile stored text is inert: no script, picture, handler or unsafe link, and nothing is fetched');

  await page.screenshot({ path: path.join(artifacts, 'documents.png') });

  // ---- Pages -----------------------------------------------------------------------------------------------------
  const wiki = await collection({ name: 'Wiki', fields: [field('title', 'Title', 'text', { required: true }), field('body', 'Body', 'longtext'), field('parent', 'Parent', 'text'), field('tags', 'Tags', 'multiselect', { options: ['guide', 'plan'] })] });
  const pagesWorkspace = await workspace('Knowledge', [{ id: 'pg', name: 'Handbook', kind: 'pages', collectionId: wiki.id, fields: ['title', 'tags'], bodyField: 'body', parentField: 'parent', operations: ops }], { type: 'view', viewId: 'pg' });
  await open(pagesWorkspace);
  const handbook = page.getByRole('region', { name: 'Handbook', exact: true });
  await handbook.getByText('Start a knowledge base').waitFor();
  await handbook.getByTestId('page-new').click();
  await handbook.getByRole('textbox', { name: 'Title', exact: true }).fill('Home');
  await page.keyboard.press('Enter');
  await page.keyboard.type('Start with [[Child page]] or [[Nowhere]].');
  await handbook.getByRole('button', { name: 'Add a sub-page', exact: true }).click();
  await until(async () => (await records(wiki.id)).length === 2, 'The sub-page was not created');
  await handbook.getByRole('textbox', { name: 'Title', exact: true }).fill('Child page');
  await until(async () => (await records(wiki.id)).some(r => r.values.title === 'Child page'), 'The sub-page title was not saved');
  const [home, child] = [(await records(wiki.id)).find(r => r.values.title === 'Home'), (await records(wiki.id)).find(r => r.values.title === 'Child page')];
  assert.equal(child.values.parent, home.id);
  assert.equal(await handbook.locator('[role=treeitem]').count(), 2);
  assert.match(await handbook.locator('nav[aria-label="Where this page sits"]').innerText(), /Home\s*\/\s*Child page/);
  ok('Pages nest: a sub-page keeps its parent and the tree and breadcrumb show it');

  await handbook.getByRole('button', { name: 'Home', exact: true }).first().click();
  await shows(handbook.getByRole('textbox', { name: 'Title', exact: true }), 'Home');
  await handbook.locator('.wiki-link.wiki-missing').waitFor();
  assert.equal(await handbook.locator('.wiki-link.wiki-missing').count(), 1);
  await handbook.locator('.wiki-link:not(.wiki-missing)').click();
  await shows(handbook.getByRole('textbox', { name: 'Title', exact: true }), 'Child page');
  await handbook.getByRole('region', { name: 'Linked from' }).getByRole('button', { name: 'Home' }).waitFor();
  await handbook.getByRole('region', { name: 'Linked from' }).getByRole('button', { name: 'Home' }).click();
  await handbook.locator('.wiki-missing').click();
  await handbook.getByText('There is no page called “Nowhere”.').waitFor();
  await handbook.getByRole('button', { name: 'Create it', exact: true }).click();
  await until(async () => (await records(wiki.id)).some(r => r.values.title === 'Nowhere'), 'The missing page was not created');
  await shows(handbook.getByRole('textbox', { name: 'Title', exact: true }), 'Nowhere');
  ok('[[Links]] open the page they name, show backlinks, and offer to write a page that is missing');

  await handbook.getByRole('searchbox', { name: 'Search pages' }).fill('start with');
  assert.equal(await handbook.locator('aside .row.flat').count(), 1);
  await handbook.getByRole('searchbox', { name: 'Search pages' }).fill('');
  await handbook.getByRole('button', { name: 'Child page', exact: true }).first().click();
  await handbook.getByLabel('Move under').selectOption({ label: 'Top level' });
  await until(async () => (await records(wiki.id)).find(r => r.id === child.id).values.parent === undefined, 'The page was not moved');
  await handbook.getByRole('button', { name: 'Home', exact: true }).first().click();
  await handbook.getByLabel('Move under').selectOption({ label: 'Child page' });
  await until(async () => (await records(wiki.id)).find(r => r.id === home.id).values.parent === child.id, 'The page was not moved under another');
  assert.equal(await handbook.getByLabel('Move under').locator('option', { hasText: 'Home' }).count(), 0);
  await handbook.getByRole('button', { name: 'Delete', exact: true }).first().click();
  await handbook.getByRole('button', { name: 'Delete', exact: true }).last().click();
  await until(async () => (await records(wiki.id)).every(r => r.id !== home.id), 'The page was not deleted');
  assert.deepEqual((await records(wiki.id)).map(r => r.values.title).sort(), ['Child page', 'Nowhere']);
  ok('Pages can be searched and moved, never under themselves, and deleting one keeps its neighbours');
  await page.screenshot({ path: path.join(artifacts, 'pages.png') });

  // ---- Board, calendar, snippets, files and shortcuts in one workspace -----------------------------------------------
  const today = new Date(); const day = d => { const x = new Date(today.getFullYear(), today.getMonth(), d); return `${x.getFullYear()}-${String(x.getMonth() + 1).padStart(2, '0')}-${String(x.getDate()).padStart(2, '0')}`; };
  const plan = await collection({ name: 'Plan', fields: [field('title', 'Title', 'text', { required: true }), field('status', 'Status', 'select', { options: ['To do', 'Doing', 'Done'] }), field('due', 'Due', 'date'), field('link', 'Link', 'url')] });
  const snippets = await collection({ name: 'Snippets', fields: [field('title', 'Title', 'text', { required: true }), field('lang', 'Language', 'select', { options: ['Shell', 'Go'] }), field('body', 'Body', 'longtext')] });
  const material = await collection({ name: 'Material', fields: [field('title', 'Title', 'text', { required: true }), field('tags', 'Tags', 'multiselect', { options: ['brochure', 'draft'] }), field('notes', 'Notes', 'longtext')] });
  const a = await call('POST', `/collections/${plan.id}/records`, { values: { title: 'Write spec', status: 'To do', due: day(10), link: 'https://example.com/spec' } });
  await call('POST', `/collections/${plan.id}/records`, { values: { title: 'Review spec', status: 'Doing', due: day(12) } });
  await call('POST', `/collections/${snippets.id}/records`, { values: { title: 'Run checks', lang: 'Shell', body: 'make check\nmake tag' } });
  await call('POST', `/collections/${snippets.id}/records`, { values: { title: 'Table test', lang: 'Go', body: 'func TestX(t *testing.T) {}' } });
  const mix = await workspace('Cockpit', [
    { id: 'board', name: 'Board', kind: 'board', collectionId: plan.id, fields: ['title', 'due', 'link'], groupBy: 'status', operations: ops },
    { id: 'cal', name: 'Calendar', kind: 'calendar', collectionId: plan.id, fields: ['title'], dateField: 'due', operations: ops },
    { id: 'snip', name: 'Snippets', kind: 'snippets', collectionId: snippets.id, fields: ['title', 'lang'], bodyField: 'body', groupBy: 'lang', operations: ops },
    { id: 'files', name: 'Material', kind: 'files', collectionId: material.id, fields: ['title', 'tags'], bodyField: 'notes', operations: ops },
  ], { type: 'stack', children: [{ type: 'grid', columns: 2, children: [{ type: 'shortcut', target: 'projects' }, { type: 'shortcut', target: 'calendar', text: 'Schedule' }] }, { type: 'view', viewId: 'board' }, { type: 'view', viewId: 'cal' }, { type: 'view', viewId: 'snip' }, { type: 'view', viewId: 'files' }] });
  await open(mix);

  const board = page.getByRole('region', { name: 'Board', exact: true });
  await board.getByRole('group', { name: /^To do, 1 card$/ }).waitFor();
  await board.getByRole('button', { name: 'Move Write spec to the next column' }).click();
  await until(async () => (await records(plan.id)).find(r => r.id === a.id).values.status === 'Doing', 'The card was not moved');
  await board.getByRole('group', { name: /^Doing, 2 cards$/ }).waitFor();
  await board.locator('.card', { hasText: 'Write spec' }).dragTo(board.getByRole('group', { name: /^Done/ }));
  await until(async () => (await records(plan.id)).find(r => r.id === a.id).values.status === 'Done', 'Dragging a card did not move it');
  await board.getByRole('group', { name: /^To do/ }).getByRole('button', { name: 'Add a card' }).click();
  await page.keyboard.type('Fresh idea');
  await page.keyboard.press('Enter');
  await until(async () => (await records(plan.id)).some(r => r.values.title === 'Fresh idea' && r.values.status === 'To do'), 'A card was not added to its column');
  await board.getByRole('link', { name: 'https://example.com/spec' }).waitFor();
  ok('A board moves cards with buttons and by dragging, and adds cards to a column');

  // The whole month must be on screen for a drag from one day to another: a page that scrolls under the pointer ends the drag.
  await page.setViewportSize({ width: 1440, height: 2000 });
  const calendar = page.getByRole('region', { name: 'Calendar', exact: true });
  const reviewChip = calendar.getByRole('button', { name: 'Review spec', exact: true });
  await reviewChip.waitFor();
  const target = calendar.locator('[role=gridcell]:not(.out)').nth(19);
  await reviewChip.dragTo(target);
  await until(async () => (await records(plan.id)).find(r => r.values.title === 'Review spec').values.due === day(20), 'Dragging an entry did not reschedule it');
  await calendar.getByRole('button', { name: /^\w+, \w+ 15,/ }).click();
  await calendar.getByRole('button', { name: 'Add', exact: true }).click();
  await page.keyboard.type('Dentist');
  await page.keyboard.press('Enter');
  await until(async () => (await records(plan.id)).some(r => r.values.title === 'Dentist' && r.values.due === day(15)), 'An entry was not added to its day');
  await calendar.getByRole('button', { name: 'Next month' }).click();
  await calendar.getByRole('button', { name: 'Today', exact: true }).click();
  ok('A calendar shows records on their day, reschedules by dragging and adds to a day');
  await page.setViewportSize({ width: 1440, height: 1000 });

  const snip = page.getByRole('region', { name: 'Snippets', exact: true });
  await snip.getByRole('button', { name: 'Copy Run checks' }).click();
  assert.equal(await page.evaluate(() => navigator.clipboard.readText()), 'make check\nmake tag');
  await snip.getByRole('button', { name: 'Go', exact: true }).click();
  assert.equal(await snip.locator('li.snippet').count(), 1);
  await snip.getByRole('button', { name: 'All', exact: true }).click();
  await snip.getByTestId('snippet-new').click();
  const form = snip.getByRole('form', { name: 'Add record' });
  await form.getByLabel('Title (required)').fill('Release');
  await form.getByLabel('Body').fill('git tag -a v1\n\tgit push');
  await form.getByRole('button', { name: 'Save record' }).click();
  await until(async () => (await records(snippets.id)).some(r => r.values.title === 'Release' && r.values.body === 'git tag -a v1\n\tgit push'), 'A snippet was not saved with its tabs and lines');
  ok('Snippets copy, filter by category and keep their exact text');

  const files = page.getByRole('region', { name: 'Material', exact: true });
  const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==', 'base64');
  await files.getByTestId('files-add').setInputFiles([
    { name: 'Spring_brochure.md', mimeType: 'text/markdown', buffer: Buffer.from('# Spring\n\nFresh **colours** this season.') },
    { name: 'logo.png', mimeType: 'image/png', buffer: png },
    { name: 'notes.pdf', mimeType: 'application/pdf', buffer: Buffer.from('%PDF-1.1\n1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj\n3 0 obj<</Type/Page/MediaBox[0 0 100 100]/Parent 2 0 R>>endobj\ntrailer<</Root 1 0 R>>\n%%EOF') },
    { name: 'archive.zip', mimeType: 'application/zip', buffer: Buffer.from('PK') },
  ]);
  await until(async () => (await records(material.id)).length === 3, 'Files were not catalogued');
  assert.deepEqual((await records(material.id)).map(r => r.values.title).sort(), ['Spring brochure', 'logo', 'notes']);
  await files.getByRole('alert').filter({ hasText: 'archive.zip cannot be shown here' }).waitFor();
  await files.getByRole('button', { name: /Spring brochure/ }).click();
  await files.locator('.stage strong', { hasText: 'colours' }).waitFor();
  await files.getByRole('button', { name: /logo/ }).click();
  await files.locator('.stage img').waitFor();
  assert.match(await files.locator('.stage img').getAttribute('src'), /^blob:/);
  await files.getByRole('button', { name: /notes/ }).click();
  await files.locator('.stage iframe').waitFor();
  assert.match(await files.locator('.stage iframe').getAttribute('src'), /^blob:/);
  await page.reload();
  const kept = page.getByRole('region', { name: 'Material', exact: true });
  await kept.getByRole('button', { name: /logo/ }).click();
  await kept.locator('.stage img').waitFor();
  await kept.getByRole('button', { name: 'Remove file', exact: true }).click();
  await kept.getByText('Attach the file to read it here.').waitFor();
  assert.equal((await records(material.id)).length, 3);
  await kept.getByRole('button', { name: 'Delete', exact: true }).click();
  await kept.getByRole('button', { name: 'Delete', exact: true }).last().click();
  await until(async () => (await records(material.id)).length === 2, 'A catalogue entry was not deleted');
  assert.deepEqual(outside, []);
  ok('Files are catalogued and shown from this device (Markdown, picture, PDF), kept across a reload, refused when unsupported');

  const shortcuts = page.locator('[data-shortcut]');
  assert.equal(await shortcuts.count(), 2);
  assert.equal(await page.locator('[data-shortcut="calendar"]').isDisabled(), true);
  assert.match(await page.locator('[data-shortcut="calendar"]').innerText(), /Schedule[\s\S]*Not available from here/);
  await page.locator('[data-shortcut="projects"]').click();
  await page.waitForURL(/#\/projects/);
  ok('Shortcuts open the app’s own pages and say when a place cannot be opened from here');
  await open(mix);
  await page.screenshot({ path: path.join(artifacts, 'cockpit.png'), fullPage: true });

  // ---- A phone ------------------------------------------------------------------------------------------------------
  await page.setViewportSize({ width: 390, height: 844 });
  for (const w of [docs, pagesWorkspace, mix]) {
    await open(w);
    await page.getByRole('region').first().waitFor();
    await delay(300);
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    assert.ok(overflow <= 1, `${w.name} overflows a phone by ${overflow}px`);
  }
  await page.screenshot({ path: path.join(artifacts, 'phone.png'), fullPage: true });
  ok('Every productivity view fits a phone without sideways scrolling');
  await page.setViewportSize({ width: 1440, height: 1000 });

  assert.deepEqual(errors, []);
  console.log('All productivity checks passed.');
})().catch(async error => {
  console.error(error);
  try { if (lastPage) await lastPage.screenshot({ path: path.join(artifacts, 'failure.png') }); } catch { /* the page may be gone */ }
  process.exitCode = 1;
}).finally(async () => {
  await browser?.close().catch(() => {});
  for (const child of children) await stop(child);
  fs.rmSync(temp, { recursive: true, force: true });
});

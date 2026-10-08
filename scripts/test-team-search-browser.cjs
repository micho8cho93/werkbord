// Runs against a disposable Team workspace; exercises real search/membership APIs.
const {chromium} = require('../web/node_modules/playwright');
const assert = require('node:assert/strict');
const base = process.env.TEAM_BROWSER_URL, token = process.env.TEAM_BROWSER_TOKEN;
if (!base || !token) throw Error('Set TEAM_BROWSER_URL and TEAM_BROWSER_TOKEN for a disposable workspace.');
async function api(method, path, data) {
  const r = await fetch(base + '/api/team/v1' + path, {method, headers: {Authorization: 'Bearer ' + token, 'Content-Type': 'application/json'}, body: data ? JSON.stringify(data) : undefined});
  if (r.status === 204) return null;
  const result = await r.json(); if (!r.ok) throw Error(JSON.stringify(result)); return result;
}
(async () => {
  const browser = await chromium.launch({headless: true});
  try {
    const ctx = await browser.newContext({viewport: {width: 1440, height: 1000}, colorScheme: 'dark'});
    const page = await ctx.newPage(); page.setDefaultTimeout(10000);
    const errors = []; page.on('pageerror', e => errors.push(e.message));
    const project = await api('POST', '/projects', {name: 'Search project ' + Date.now()});
    const other = await api('POST', '/projects', {name: 'Other search project ' + Date.now()});
    const ticket = await api('POST', `/projects/${project.id}/tickets`, {title: 'Improve quick navigation', description: 'Keyboard discoverability', status: 'available'});
    const remote = await api('POST', `/projects/${other.id}/tickets`, {title: 'Search across shared projects', status: 'available'});
    const archived = await api('POST', `/projects/${other.id}/tickets`, {title: 'Retired navigation experiment', status: 'available'});
    await api('POST', `/projects/${other.id}/tickets/${archived.id}/archive`, {version: archived.version, archived: true});
    const member = await api('POST', '/members', {name: 'Directory colleague ' + Date.now(), email: 'colleague@example.test'});
    await page.goto(base + '/#token=' + token);
    await page.getByRole('button', {name: 'Switch to light mode', exact: true}).waitFor();
    assert.equal(await page.evaluate(() => getComputedStyle(document.body).backgroundColor), 'rgb(11, 11, 12)', 'system theme applies initially');
    await page.getByRole('button', {name: 'Switch to light mode', exact: true}).click();
    await page.reload(); await page.getByRole('button', {name: 'Switch to dark mode', exact: true}).waitFor();
    assert.equal(await page.evaluate(() => getComputedStyle(document.body).backgroundColor), 'rgb(245, 245, 242)', 'saved light choice overrides the dark system');
    await page.getByRole('button', {name: 'Switch to dark mode', exact: true}).click();
    await page.emulateMedia({colorScheme: 'light'}); await page.reload();
    await page.getByRole('button', {name: 'Switch to light mode', exact: true}).waitFor();
    assert.equal(await page.evaluate(() => document.querySelector('.brand img').getAttribute('src')), 'mark-dark.svg');
    assert.equal(await page.locator('meta[name=theme-color]').getAttribute('content'), '#0b0b0c');
    console.log('PASS system theme, explicit light/dark choices, reload persistence and matching brand/chrome');

    const input = page.getByRole('combobox', {name: 'Search projects, tickets and sections'});
    const dialog = page.getByRole('dialog', {name: 'Search Werkbord Team'});
    const finished = query => page.waitForFunction(q => searchSession?.query === q && !searchSession.loading, query);
    await page.getByRole('button', {name: 'Search', exact: true}).focus();
    await page.keyboard.press('Meta+k'); await input.waitFor(); assert(await input.evaluate(el => el === document.activeElement));
    await input.press('ArrowDown'); await input.press('Enter');
    await page.getByRole('heading', {name: 'Projects', exact: true}).waitFor();
    await page.keyboard.press('Control+k'); await input.fill(remote.key); await finished(remote.key);
    assert.equal(await dialog.getByRole('option').first().locator('code').textContent(), remote.key);
    await input.press('Enter'); await page.waitForFunction(id => state.ticketId === id && !!document.querySelector('.ticket'), remote.id);
    assert.equal(await page.locator('[name=project-switch]').inputValue(), other.id);
    await page.keyboard.press('Meta+k'); await input.press('Escape');
    assert.equal(await page.evaluate(() => state.ticketId), remote.id, 'Escape from search keeps background ticket open');
    await page.keyboard.press('Escape'); await page.waitForFunction(() => !state.ticketId);
    await page.getByRole('button', {name: 'Search', exact: true}).click(); await input.fill(archived.key); await finished(archived.key);
    await dialog.getByRole('option').filter({hasText: archived.title}).click();
    await page.waitForFunction(id => state.ticketId === id && !!document.querySelector('.ticket'), archived.id);
    assert.equal(await page.evaluate(() => state.showArchived), true);
    await page.keyboard.press('Escape');
    console.log('PASS Cmd/Ctrl K, arrows/Enter, cross-project tickets, archived tickets and nested Escape');

    await page.goto(base + `/?tab=board&project=${project.id}&ticket=${ticket.id}`);
    await page.getByText('Edit this ticket', {exact: true}).click();
    const draft = page.locator(`[name=e-title-${ticket.id}]`); await draft.fill('Unsent navigation draft');
    await page.keyboard.press('Meta+k'); await input.fill('keyboard'); await finished('keyboard');
    await api('POST', `/projects/${project.id}/tickets`, {title: 'A concurrent change', status: 'available'});
    await page.waitForFunction(() => state.data?.board.tickets.some(k => k.title === 'A concurrent change'));
    assert.equal(await input.inputValue(), 'keyboard'); assert(await input.evaluate(el => el === document.activeElement));
    await input.press('Shift+Tab'); assert(await page.getByRole('button', {name: 'Close search'}).evaluate(el => el === document.activeElement), 'modal traps focus');
    await page.keyboard.press('Escape'); assert.equal(await draft.inputValue(), 'Unsent navigation draft');
    assert(await draft.evaluate(el => el === document.activeElement), 'search returns focus to the original field');
    console.log('PASS search while editing, live updates, focus trap, draft and focus restoration');

    let delayed = false;
    await page.route('**/api/team/v1/search?**', async route => {
      if (new URL(route.request().url()).searchParams.get('q') === 'navigation') {
        delayed = true; await new Promise(r => setTimeout(r, 450));
      }
      await route.continue().catch(() => {});
    });
    await page.keyboard.press('Meta+k');
    const navigationRequest = page.waitForRequest(r => new URL(r.url()).pathname === '/api/team/v1/search' && new URL(r.url()).searchParams.get('q') === 'navigation');
    await input.fill('navigation'); await navigationRequest;
    await input.fill(remote.key); await finished(remote.key); assert(delayed);
    await page.waitForTimeout(550);
    assert.equal(await dialog.getByRole('option').first().locator('code').textContent(), remote.key, 'late query must not replace current results');
    await page.unroute('**/api/team/v1/search?**');
    await input.fill('zz-no-such-team-result'); await finished('zz-no-such-team-result'); await dialog.getByText(/No results for/).waitFor();
    await page.route('**/api/team/v1/search?**', route => route.fulfill({status: 503, contentType: 'application/json', body: JSON.stringify({error: {code: 'unavailable', message: 'Search is temporarily unavailable.'}})}), {times: 1});
    await input.fill('keyboard'); await dialog.getByRole('button', {name: 'Try again'}).waitFor();
    await dialog.getByRole('button', {name: 'Try again'}).click(); await finished('keyboard');
    await dialog.getByRole('option').filter({has: page.getByText(ticket.key, {exact: true})}).waitFor();
    await input.fill('Members'); await finished('Members'); await input.press('Enter');
    await page.getByRole('heading', {name: 'Members', exact: true}).waitFor();
    const filter = page.getByRole('searchbox', {name: 'Find a member'}); await filter.fill(member.member.name);
    assert.equal(await page.locator('.member-row:visible').count(), 1);
    await filter.fill('missing colleague'); await page.getByText('No members found.', {exact: true}).waitFor();
    console.log('PASS cancellation, empty/error/retry states, section search and directory filtering');

    const memberCtx = await browser.newContext({viewport: {width: 390, height: 844}});
    const memberPage = await memberCtx.newPage(); memberPage.on('pageerror', e => errors.push(e.message));
    await memberPage.goto(base + '/?tab=members#token=' + member.token);
    await memberPage.getByRole('heading', {name: 'Members', exact: true}).waitFor();
    assert(await memberPage.locator('.member-row').count() >= 2, 'plain member sees the company directory');
    assert.equal(await memberPage.getByRole('button', {name: 'Remove', exact: true}).count(), 0);
    assert.equal(await memberPage.getByRole('button', {name: 'Add', exact: true}).count(), 0);
    await memberPage.getByRole('button', {name: 'Search', exact: true}).click();
    const memberInput = memberPage.getByRole('combobox'); await memberInput.fill('navigation');
    await memberPage.waitForFunction(() => searchSession && !searchSession.loading && searchSession.query === 'navigation');
    assert.equal(await memberPage.getByRole('option').count(), 0, 'no private project results in browser');
    await memberInput.press('Escape');
    await memberPage.getByRole('button', {name: 'Switch to dark mode', exact: true}).click();
    assert.equal(await memberPage.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, 'mobile does not overflow');
    await memberPage.setViewportSize({width: 320, height: 780});
    await memberPage.waitForFunction(() => { const button = document.querySelector('.rail-primary [aria-current="page"]'); const r = button.getBoundingClientRect(); return r.left >= 8 && r.right <= innerWidth; });
    await memberPage.getByRole('button', {name: 'Search', exact: true}).click();
    await api('DELETE', '/members/' + member.member.id);
    await memberInput.fill('revoked session');
    await memberPage.getByRole('heading', {name: 'Sign in', exact: true}).waitFor();
    assert.equal(await memberPage.getByRole('dialog').count(), 0, 'losing membership closes the palette');
    assert.equal(errors.length, 0, errors.join('\n'));
    console.log('PASS regular-member permissions, phone controls/resizing, revoked session cleanup, no overflow or browser errors');
  } finally { await browser.close(); }
})().catch(e => { console.error(e); process.exitCode = 1; });

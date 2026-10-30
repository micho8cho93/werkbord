// Comments and @mentions in the Team console, with two members in two browsers: write with the keyboard-driven picker, a live
// notification, opening it at the highlighted comment, marking it read in one window and watching the count clear in another,
// and a refresh in the middle of writing. Runs only against a disposable workspace (scripts/test-browser.cjs makes one:
// BROWSER_COMMENTS_ONLY=1 node scripts/test-browser.cjs); it creates projects and members in it.
const { chromium, webkit } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const fs = require('node:fs');
const assert = require('node:assert/strict');
const path = require('node:path');
const os = require('node:os');
const base = process.env.TEAM_BROWSER_URL || 'http://127.0.0.1:17430';
const artifacts = process.env.BROWSER_ARTIFACT_DIR || fs.mkdtempSync(path.join(os.tmpdir(), 'werkbord-comments-'));
fs.mkdirSync(artifacts, { recursive: true });
const tokenFile = process.env.TEAM_BROWSER_TOKEN_FILE;
const token = process.env.TEAM_BROWSER_TOKEN || (tokenFile && fs.readFileSync(tokenFile, 'utf8').match(/wbt_[a-zA-Z0-9]+/)[0]);
if (!token) throw Error('Set TEAM_BROWSER_TOKEN or TEAM_BROWSER_TOKEN_FILE for a disposable workspace.');

async function api(method, p, data, credential = token) {
    const res = await fetch(base + '/api/team/v1' + p, { method, headers: { Authorization: 'Bearer ' + credential, 'Content-Type': 'application/json' }, body: data ? JSON.stringify(data) : undefined });
    const text = await res.text();
    let body; try { body = JSON.parse(text); } catch { }
    if (res.status >= 400) throw Error(method + ' ' + p + ' ' + res.status + ' ' + text);
    return body;
}
const stamp = Date.now();
const started = Date.now();
const pass = (what) => console.log('PASS ' + what + ' (' + Math.round((Date.now() - started) / 1000) + 's)');
const say = (...parts) => parts.map(p => typeof p === 'string' ? { type: 'text', text: p } : { type: 'mention', memberId: p.id });
let counter = 0;
const clientId = () => 'browser-' + stamp + '-' + (++counter);

(async () => {
    const executable = process.env.BROWSER_EXECUTABLE ? { executablePath: process.env.BROWSER_EXECUTABLE } : {};
    const browser = await chromium.launch({ headless: true, ...executable });
    const errors = [];
    const watch = (page, who) => { page.on('pageerror', e => errors.push(who + ': ' + e.message)); page.on('console', m => { if (m.type() === 'error' && !/Failed to load resource/.test(m.text())) errors.push(who + ' console: ' + m.text()); }); };

    // The world: Ada owns the workspace; Bo and Cy are on one project.
    const ada = (await api('GET', '/me')).member;
    const project = await api('POST', '/projects', { name: 'Comments ' + stamp, repository: 'https://github.com/acme/shop' });
    const boJoin = await api('POST', '/members', { name: 'Bo ' + stamp });
    const cyJoin = await api('POST', '/members', { name: 'Cy ' + stamp });
    const bo = boJoin.member, cy = cyJoin.member;
    for (const m of [bo, cy]) await api('PUT', `/projects/${project.id}/members/${m.id}`, {});
    const ticket = await api('POST', `/projects/${project.id}/tickets`, { title: 'Checkout flow', description: 'Make it faster', status: 'available' });
    const T = `/projects/${project.id}/tickets/${ticket.id}`;
    const ticketAddress = `${base}/?tab=work&project=${project.id}&ticket=${ticket.id}`;
    // Enough conversation that the ticket has to scroll.
    for (let i = 1; i <= 8; i++) await api('POST', T + '/comments', { clientId: clientId(), segments: say(`Earlier note ${i}: the checkout has to stay fast on slow phones, and the cart must survive a reload. `.repeat(2)) }, cyJoin.token);

    const desktop = { viewport: { width: 1440, height: 900 } };
    // The first-visit tour is its own test (and covers the composer); these flows start with it already seen.
    const seenTour = () => { try { localStorage.setItem('werkbord-team-tour', 'seen'); } catch (_) { } };
    const adaCtx = await browser.newContext(desktop), boCtx = await browser.newContext(desktop), bo2Ctx = await browser.newContext(desktop);
    for (const c of [adaCtx, boCtx, bo2Ctx]) await c.addInitScript(seenTour);
    const adaPage = await adaCtx.newPage(), boPage = await boCtx.newPage(), bo2Page = await bo2Ctx.newPage();
    [[adaPage, 'ada'], [boPage, 'bo'], [bo2Page, 'bo-2']].forEach(([p, w]) => { watch(p, w); p.setDefaultTimeout(10000); });
    await adaPage.goto(base + '/#token=' + token);
    await boPage.goto(base + '/#token=' + boJoin.token);
    await bo2Page.goto(base + '/#token=' + boJoin.token);
    for (const p of [adaPage, boPage, bo2Page]) await p.locator('.app-shell').waitFor();
    // Bo's two windows rest on Needs you, with nothing waiting.
    for (const p of [boPage, bo2Page]) await p.getByText('Nothing needs your attention.').waitFor();
    const navBadge = p => p.locator('.rail-primary button[aria-label="Workspace"] .count');
    assert.equal(await navBadge(boPage).count(), 0, 'nothing is counted before anyone is mentioned');

    // ---- Ada writes, choosing Bo with the keyboard ----
    await adaPage.goto(ticketAddress);
    const composer = adaPage.locator(`textarea[name="comment-new-${ticket.id}"]`);
    await composer.waitFor();
    await composer.scrollIntoViewIfNeeded();
    await composer.click();
    await composer.pressSequentially('Can you take this, @');
    const picker = adaPage.getByRole('listbox', { name: 'People on this project' });
    await picker.waitFor();
    assert.equal(await picker.getByRole('option').count(), 3, 'every person on the project is offered');
    await composer.pressSequentially('bo');
    assert.equal(await picker.getByRole('option').count(), 1, 'typing narrows the list');
    assert.match(await picker.getByRole('option').first().innerText(), /Bo /);
    // Escape closes the list and keeps the words; a keystroke brings it back.
    await composer.press('Escape');
    await picker.waitFor({ state: 'hidden' });
    assert.match(await composer.inputValue(), /@bo$/);
    await composer.press('Backspace'); await composer.pressSequentially('o');
    await picker.waitFor();
    await composer.press('ArrowDown'); // one match: wraps to itself
    assert.equal(await composer.getAttribute('aria-activedescendant') !== null, true, 'the listbox is announced to assistive technology');
    await composer.press('Enter');
    await picker.waitFor({ state: 'hidden' });
    assert.equal(await composer.inputValue(), `Can you take this, @${bo.name} `);
    assert.deepEqual(await adaPage.locator('.mention-chips .mention-chip').allInnerTexts().then(t => t.map(x => x.replace(/\s*×$/, ''))), ['@' + bo.name]);
    await composer.pressSequentially('please?');
    await adaPage.screenshot({ path: path.join(artifacts, 'comments-composer-desktop-light.png') });
    const posted = adaPage.waitForResponse(r => r.request().method() === 'POST' && r.url().endsWith('/comments'));
    await adaPage.getByRole('button', { name: 'Post comment' }).click();
    assert.equal((await posted).status(), 201);
    await adaPage.locator('.comment .mention', { hasText: '@' + bo.name }).waitFor();
    const commentId = await adaPage.locator('.comment', { hasText: 'Can you take this' }).getAttribute('data-comment-id');
    assert.equal(await composer.inputValue(), '', 'the box is empty after posting');
    pass('writing a comment with the keyboard-driven mention picker');

    // ---- Bo is told, live, without reloading ----
    const row = boPage.locator('.mention-row');
    await row.waitFor();
    assert.match(await row.innerText(), new RegExp(ada.name + ' .*mentioned you on WB-' + ticket.number));
    assert.match(await row.innerText(), /Can you take this, @Bo/);
    assert.equal(await row.getByRole('button', { name: /Open comment/ }).count(), 1);
    assert.equal(await row.getByRole('button', { name: /Mark the mention .* read/ }).count(), 1);
    assert.equal((await navBadge(boPage).innerText()).trim(), '1');
    await boPage.locator('.toast', { hasText: 'mentioned you' }).waitFor();
    // Bo's other window agrees; Ada and Cy have nothing.
    await bo2Page.locator('.mention-row').waitFor();
    assert.equal((await navBadge(bo2Page).innerText()).trim(), '1');
    await boPage.screenshot({ path: path.join(artifacts, 'comments-needs-you-desktop-light.png') });
    pass('the mention arrives live in Needs you, with its count, in every window');

    // ---- opening it: the ticket, the comment highlighted, nothing read ----
    await row.getByRole('button', { name: /Open comment/ }).click();
    const target = boPage.locator(`#comment-${commentId}`);
    await target.waitFor();
    await boPage.waitForFunction(id => document.activeElement && document.activeElement.id === 'comment-' + id, commentId);
    assert.match(await target.getAttribute('class'), /is-referenced/);
    assert.match(await target.getAttribute('class'), /mentions-me/);
    assert.match(await target.getAttribute('class'), /is-unread/);
    const inView = await target.evaluate(el => { const r = el.getBoundingClientRect(); return r.top >= 0 && r.bottom <= innerHeight; });
    assert.equal(inView, true, 'the referenced comment is scrolled into view');
    assert.equal(await boPage.getByRole('button', { name: 'Mark read', exact: true }).count(), 1, 'Mark read sits beside the unread mention');
    assert.match(await boPage.locator('.comments-head').innerText(), /1 unread mention/);
    assert.equal(await api('GET', '/notifications', null, boJoin.token).then(l => l.unread), 1, 'opening a comment does not read it');
    assert.equal((await navBadge(bo2Page).innerText()).trim(), '1');
    await boPage.screenshot({ path: path.join(artifacts, 'comments-ticket-highlight-desktop-light.png') });
    pass('Open comment shows the ticket at the highlighted comment and leaves it unread');

    // ---- a refresh in the middle of writing keeps the draft, the focus, the caret, the open picker and the scroll ----
    const draft = boPage.locator(`textarea[name="comment-new-${ticket.id}"]`);
    await draft.scrollIntoViewIfNeeded();
    await draft.click();
    await draft.pressSequentially('Looking now. @');
    await draft.pressSequentially('cy');
    await boPage.getByRole('listbox', { name: 'People on this project' }).waitFor();
    await draft.evaluate(el => el.setSelectionRange(el.value.length, el.value.length));
    const ticketBox = boPage.locator('.ticket');
    const scrolled = await ticketBox.evaluate(el => { el.scrollTop = Math.max(0, el.scrollHeight - el.clientHeight - 40); return el.scrollTop; });
    assert.ok(scrolled > 0, 'the ticket panel has to scroll for this to mean anything');
    const before = { value: await draft.inputValue(), caret: await draft.evaluate(el => el.selectionStart) };
    await boPage.evaluate(() => { window.__redraws = 0; new MutationObserver(() => window.__redraws++).observe(document.getElementById('app'), { childList: true }); });
    await api('POST', T + '/comments', { clientId: clientId(), segments: say('Meanwhile, from Ada: the numbers are in.') });
    await boPage.locator('.comment', { hasText: 'the numbers are in' }).waitFor();
    assert.ok(await boPage.evaluate(() => window.__redraws) > 0, 'the page was redrawn');
    const after = boPage.locator(`textarea[name="comment-new-${ticket.id}"]`);
    assert.equal(await after.inputValue(), before.value, 'the unsent comment survived the refresh');
    assert.equal(await after.evaluate(el => document.activeElement === el), true, 'the focus is still in the box');
    assert.equal(await after.evaluate(el => el.selectionStart), before.caret, 'the caret is where it was');
    await boPage.getByRole('listbox', { name: 'People on this project' }).waitFor(); // still open: the redraw reopens it at the caret
    assert.ok(Math.abs(await ticketBox.evaluate(el => el.scrollTop) - scrolled) <= 80, 'the scroll position was kept (a new comment may add its own height)');
    // Choosing with the keyboard still works after the redraw, and the mention is bound to Cy.
    await after.press('Enter');
    assert.equal(await after.inputValue(), `Looking now. @${cy.name} `);
    assert.deepEqual(await boPage.locator('.mention-chips .mention-chip').allInnerTexts().then(t => t.map(x => x.replace(/\s*×$/, ''))), ['@' + cy.name]);
    // A draft also survives a reload of the page.
    await after.pressSequentially('thanks');
    await boPage.reload();
    await boPage.locator(`textarea[name="comment-new-${ticket.id}"]`).waitFor();
    assert.equal(await boPage.locator(`textarea[name="comment-new-${ticket.id}"]`).inputValue(), `Looking now. @${cy.name} thanks`);
    assert.equal(await boPage.locator('.mention-chips .mention-chip').count(), 1, 'its mention is still a mention after a reload');
    pass('refreshes keep an unsent comment, focus, caret, open picker and scroll; a reload keeps the draft');

    // ---- marking read in one window clears the count in the other ----
    await boPage.locator(`#comment-${commentId}`).scrollIntoViewIfNeeded();
    await boPage.locator(`[name="mark-read-${commentId}"]`).click();
    await boPage.waitForFunction(id => /is-read/.test(document.getElementById('comment-' + id)?.className || ''), commentId);
    assert.equal(await api('GET', '/notifications', null, boJoin.token).then(l => l.unread), 0);
    await bo2Page.getByText('Nothing needs your attention.').waitFor();
    assert.equal(await navBadge(bo2Page).count(), 0, 'the other window clears its count without being touched');
    assert.equal(await bo2Page.locator('.mention-row').count(), 0);
    assert.match(await boPage.locator(`#comment-${commentId}`).getAttribute('class'), /mentions-me/, 'the comment is still marked as one that mentions Bo');
    pass('Mark read persists and clears counts across sessions');

    // ---- editing and deleting; a stale edit is refused with the draft kept ----
    await adaPage.goto(ticketAddress);
    const mine = adaPage.locator(`#comment-${commentId}`);
    await mine.getByRole('button', { name: 'Edit', exact: true }).click();
    const editor = adaPage.locator(`textarea[name="comment-edit-${commentId}"]`);
    await editor.waitFor();
    await editor.fill('Can you take this, please? (edited)');
    await api('PATCH', `${T}/comments/${commentId}`, { version: 1, segments: say('Changed by another window') });
    await adaPage.getByText(/Someone changed this comment while you were editing/).waitFor();
    assert.equal(await editor.inputValue(), 'Can you take this, please? (edited)', 'the draft is kept');
    await adaPage.getByRole('button', { name: 'Load latest version' }).click();
    await adaPage.locator(`#comment-${commentId}`, { hasText: 'Changed by another window' }).waitFor();
    assert.match(await adaPage.locator(`#comment-${commentId}`).innerText(), /edited/);
    adaPage.once('dialog', d => d.accept());
    await adaPage.locator(`#comment-${commentId}`).getByRole('button', { name: 'Delete', exact: true }).click();
    await adaPage.locator(`#comment-${commentId} .comment-gone`).waitFor();
    assert.match(await adaPage.locator(`#comment-${commentId}`).innerText(), /deleted/);
    pass('editing keeps a draft through a conflict; deleting leaves a tombstone');

    // ---- markup is only ever text ----
    await api('POST', T + '/comments', { clientId: clientId(), segments: say('<img src=x onerror="window.__pwned=1"> <script>window.__pwned=1</script> **not bold**') });
    await adaPage.locator('.comment', { hasText: '**not bold**' }).waitFor();
    assert.equal(await adaPage.evaluate(() => window.__pwned), undefined, 'a comment ran no script');
    assert.equal(await adaPage.locator('.comment-body img, .comment-body script, .comment-body b, .comment-body strong').count(), 0, 'a comment made no elements');
    pass('markup in a comment is shown as text');

    // ---- a long conversation is read in pages, and opening an old comment reads around it ----
    const long = await api('POST', `/projects/${project.id}/tickets`, { title: 'Long thread', status: 'available' });
    const L = `/projects/${project.id}/tickets/${long.id}`;
    const longComments = [];
    for (let i = 1; i <= 130; i++) longComments.push(await api('POST', L + '/comments', { clientId: clientId(), segments: i === 5 ? say('Old question for ', bo, ': is this ready?') : say(`Message number ${i}`) }, cyJoin.token));
    await boPage.goto(base + '/?tab=workspace');
    const old = boPage.locator('.mention-row', { hasText: 'Old question' });
    await old.waitFor();
    await old.getByRole('button', { name: /Open comment/ }).click();
    await boPage.locator(`#comment-${longComments[4].id}`).waitFor();
    await boPage.waitForFunction(id => document.activeElement && document.activeElement.id === 'comment-' + id, longComments[4].id);
    assert.match(await boPage.locator(`#comment-${longComments[4].id}`).getAttribute('class'), /is-referenced/);
    assert.equal(await boPage.getByRole('button', { name: 'Load earlier comments' }).count(), 0, 'the conversation starts a few comments before it');
    await boPage.getByRole('button', { name: 'Load newer comments' }).waitFor();
    assert.ok(await boPage.locator('.comment').count() <= 50, 'only a page is loaded');
    assert.match(await boPage.locator('.comments-head').innerText(), /130/);
    await boPage.getByRole('button', { name: 'Load newer comments' }).click();
    await boPage.locator('.comment', { hasText: 'Message number 70' }).waitFor();
    assert.equal(await boPage.locator(`#comment-${longComments[4].id}`).count(), 1, 'what was loaded stays loaded');
    // A refresh while reading an old stretch keeps the stretch (and the highlight) where it is.
    await api('POST', L + '/comments', { clientId: clientId(), segments: say('A new message at the end') }, cyJoin.token);
    await boPage.locator('.comments-head', { hasText: '131' }).waitFor();
    assert.equal(await boPage.locator(`#comment-${longComments[4].id}`).count(), 1, 'a refresh did not snap the conversation to its end');
    assert.match(await boPage.locator(`#comment-${longComments[4].id}`).getAttribute('class'), /is-referenced/);
    // The same ticket opened plainly shows the latest page, with the way back.
    await boPage.goto(`${base}/?tab=work&project=${project.id}&ticket=${long.id}`);
    await boPage.locator('.comment', { hasText: 'A new message at the end' }).waitFor();
    assert.equal(await boPage.locator('.comment', { hasText: 'Message number 5' }).count(), 0);
    await boPage.getByRole('button', { name: 'Load earlier comments' }).click();
    await boPage.locator('.comment', { hasText: 'Message number 60' }).waitFor(); // before the latest page, which starts near 82
    assert.ok(await boPage.locator('.comment').count() > 50, 'earlier comments were added to the page');
    // Ctrl+Enter posts.
    const quick = boPage.locator(`textarea[name="comment-new-${long.id}"]`);
    await quick.click();
    await quick.pressSequentially('Posted from the keyboard');
    await quick.press('Control+Enter');
    await boPage.locator('.comment', { hasText: 'Posted from the keyboard' }).waitFor();
    assert.equal(await quick.inputValue(), '');
    pass('long conversations are paged, opened around a comment, and kept steady by refreshes; Ctrl+Enter posts');
    // Bo has dealt with the old question; what is left to count is the next mention.
    const oldQuestion = (await api('GET', '/notifications', null, boJoin.token)).notifications.find(n => /Old question/.test(n.excerpt));
    await api('POST', `/notifications/${oldQuestion.id}/read`, null, boJoin.token);

    // ---- a project's own Overview, and the same on a phone, in both themes ----
    await api('POST', T + '/comments', { clientId: clientId(), segments: say('Overview, ', bo, ' and ', cy, ': see this.') }); // by Ada, mentions Bo and Cy
    await boPage.goto(`${base}/?tab=overview&project=${project.id}`);
    await boPage.locator('.overview-mentions .mention-row').waitFor();
    assert.equal(await boPage.locator('.overview-mentions .mention-row').count(), 1);
    assert.equal((await boPage.locator('.sub-tabs button[name="project-tab-overview"] .count').innerText()).trim(), '1', 'the Overview tab counts the project\'s unread mentions');
    for (const scheme of ['light', 'dark']) {
        await boPage.emulateMedia({ colorScheme: scheme });
        await boPage.screenshot({ path: path.join(artifacts, `comments-overview-desktop-${scheme}.png`) });
    }
    await boPage.emulateMedia({ colorScheme: 'light' });

    for (const [engineName, engine] of [['chromium', chromium], ['webkit', webkit]]) {
        const phoneBrowser = engineName === 'chromium' ? browser : await engine.launch({ headless: true });
        const phone = await phoneBrowser.newContext({ viewport: { width: 390, height: 844 }, isMobile: engineName === 'chromium', hasTouch: true, deviceScaleFactor: 2 });
        await phone.addInitScript(seenTour);
        const page = await phone.newPage(); watch(page, 'phone-' + engineName); page.setDefaultTimeout(10000);
        await page.goto(base + '/#token=' + boJoin.token);
        await page.locator('.app-shell').waitFor();
        await page.goto(ticketAddress + '&comment=' + commentId);
        await page.locator('.comments').waitFor();
        for (const scheme of ['light', 'dark']) {
            await page.emulateMedia({ colorScheme: scheme });
            await page.locator('.comments').scrollIntoViewIfNeeded();
            const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
            assert.ok(overflow <= 0, `${engineName}/${scheme}: the page scrolls sideways by ${overflow}px`);
            await page.screenshot({ path: path.join(artifacts, `comments-ticket-phone-${engineName}-${scheme}.png`) });
        }
        // The picker on a phone: the @ button, then a person.
        const box = page.locator(`textarea[name="comment-new-${ticket.id}"]`);
        await box.scrollIntoViewIfNeeded();
        await page.getByRole('button', { name: 'Mention someone' }).click();
        const people = page.getByRole('listbox', { name: 'People on this project' });
        await people.waitFor();
        const option = people.getByRole('option').first();
        const size = await option.boundingBox();
        assert.ok(size.height >= 36, `${engineName}: a person in the list is ${size.height}px tall`);
        await page.screenshot({ path: path.join(artifacts, `comments-picker-phone-${engineName}-light.png`) });
        await option.click();
        assert.match(await box.inputValue(), /^@\S/);
        await page.goto(base + '/?tab=workspace');
        await page.locator('.mention-row').first().waitFor();
        const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
        assert.ok(overflow <= 0, `${engineName}: Needs you scrolls sideways by ${overflow}px`);
        await page.screenshot({ path: path.join(artifacts, `comments-needs-you-phone-${engineName}-light.png`) });
        await phone.close();
        if (engineName !== 'chromium') await phoneBrowser.close();
    }
    pass('Overview mentions, and the ticket, picker and Needs you on a phone in Chromium and WebKit, light and dark');

    assert.deepEqual(errors, [], 'page errors:\n' + errors.join('\n'));
    await browser.close();
    pass('comments and mentions in the browser');
})().catch(e => { console.error(e); process.exit(1); }); // exit: a browser left open must not keep a failed run alive

// Configurable deck integration against the disposable controller and real runner fixture.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const assert = require('node:assert/strict');
const path = require('node:path');
const fs = require('node:fs');
const base = process.env.PERSONAL_BROWSER_URL;
const owner = 'disposable-browser-credential';
async function request(token, method, route, body) {
    const response = await fetch(base + route, { method, headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body) });
    const raw = await response.text();
    assert(response.ok, `${method} ${route}: ${response.status} ${raw}`);
    return raw ? JSON.parse(raw) : null;
}
(async () => {
    let project = (await request(owner, 'GET', '/api/projects')).projects.find(p => p.repoPath === fs.realpathSync(process.env.WERKBORD_BROWSER_REPO));
    if (!project) project = await request(owner, 'POST', '/api/projects', { path: fs.realpathSync(process.env.WERKBORD_BROWSER_REPO), name: 'Deck fixture' });
    const code = await request(owner, 'POST', '/api/phones/codes', {});
    const phone = await request('', 'POST', '/api/phones/enroll', { code: code.code, name: 'Deck integration fixture' });
    const browser = await chromium.launch({ headless: true });
    const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });
    const errors = [];
    await ctx.addInitScript(token => {
        localStorage.setItem('devboard.token', token);
        localStorage.setItem('werkbord-tour-phone-deck-v2', 'fixture');
    }, phone.token);
    const page = await ctx.newPage();
    page.setDefaultTimeout(10000);
    page.on('pageerror', e => errors.push(e.message));
    let starts = 0;
    page.on('request', r => { if (new URL(r.url()).pathname === '/api/deck/workflows' && r.method() === 'POST') starts++; });
    const socket = i => page.locator(`[data-deck-position="${i}"] .key-slot`);
    const dialog = () => page.getByRole('dialog');
    const edit = () => page.getByRole('button', { name: 'Edit Deck', exact: true });
    const done = () => page.getByRole('button', { name: 'Done', exact: true });
    try {
        await page.goto(base + '/#/deck');
        await edit().waitFor();
        await edit().click();
        await socket(6).click();
        await dialog().getByRole('button', { name: /^Find Bugs/ }).click();
        await dialog().getByRole('combobox', { name: 'Repository target', exact: true }).selectOption('selected');
        await dialog().getByRole('checkbox', { name: project.name, exact: true }).check();
        await dialog().getByRole('combobox', { name: 'Agent', exact: true }).selectOption('fixture');
        await dialog().getByRole('button', { name: 'Save configuration', exact: true }).click();
        assert.equal(starts, 0, 'configuration never executes');
        await done().click();
        await edit().waitFor();
        await page.reload(); await edit().waitFor();
        await page.waitForFunction(() => document.querySelector('[data-deck-position="6"] [data-key="find-bugs"]'));
        assert((await socket(6).innerText()).includes(project.name), 'workflow shows its repository target');

        // Holding configures, releasing never launches. Escape restores keyboard focus.
        const box = await socket(6).boundingBox();
        await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
        await page.mouse.down(); await page.waitForTimeout(650); await page.mouse.up();
        await dialog().getByRole('heading', { name: 'Configure Find Bugs' }).waitFor();
        assert.equal(starts, 0, 'long press does not execute');
        await page.keyboard.press('Escape');
        await socket(6).focus(); await page.keyboard.press('Shift+F10');
        await dialog().getByRole('heading', { name: 'Configure Find Bugs' }).waitFor();
        await page.keyboard.press('Escape');
        assert.equal(starts, 0, 'keyboard configuration does not execute');

        // Pointer dragging preserves empty positions. The menu supplies a keyboard alternative.
        await edit().click();
        const from = await socket(6).boundingBox(), to = await socket(8).boundingBox();

        await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2);
        await page.mouse.down(); await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, { steps: 8 }); await page.mouse.up();
        await page.waitForFunction(() => document.querySelector('[data-deck-position="8"] [data-key="find-bugs"]'));
        assert.equal(starts, 0, 'drag never executes');
        await socket(8).click();
        await dialog().getByRole('combobox', { name: 'Move to position' }).selectOption({ label: '8' });
        await page.waitForFunction(() => document.querySelector('[data-deck-position="7"] [data-key="find-bugs"]'));
        await done().click(); await edit().waitFor();
        await page.reload(); await edit().waitFor();
        assert.equal(await socket(6).getAttribute('data-key'), null);
        assert.equal(await socket(8).getAttribute('data-key'), null);
        await page.waitForFunction(() => document.querySelector('[data-deck-position="7"] [data-key="find-bugs"]'));

        // A genuine tap starts one ordinary run and stays on the deck.
        await socket(7).click();
        const executions = page.getByRole('region', { name: 'Workflow executions' });
        const banner = page.getByTestId('workflow-banner');
        await executions.getByRole('link', { name: 'Find Bugs', exact: true }).waitFor();
        await banner.getByRole('button', {name:'Open Find Bugs execution details'}).waitFor();
        assert.equal(new URL(page.url()).hash, '#/deck', 'start stays on the Control Deck');
        assert.equal(starts, 1, 'tap executes once');
        const history = await request(phone.token, 'GET', '/api/deck/history');
        const runs = await request(phone.token, 'GET', `/api/projects/${project.id}/tasks/${history[0].taskId}/runs`);
        assert.equal(runs.runs.length, 1);
        const firstRun = runs.runs[0];
        await request(phone.token, 'POST', `/api/projects/${project.id}/runs/${firstRun.id}/input`, {text:'workflow-verify'});
        await banner.getByText(/Verify potential findings/).waitFor();
        await banner.getByRole('button', {name:'Open Find Bugs execution details'}).click();
        await dialog().getByRole('heading', {name:'Find Bugs execution'}).waitFor();
        await dialog().locator('summary').filter({hasText:'Verify potential findings'}).waitFor();
        await dialog().getByText(/Fixture verification evidence/).waitFor();
        if (process.env.BROWSER_ARTIFACT_DIR) await page.screenshot({path:path.join(process.env.BROWSER_ARTIFACT_DIR,'workflow-execution-detail.png'),fullPage:true});
        await page.keyboard.press('Escape');
        // Navigation retains the banner, and tapping the key again only opens this execution.
        await socket(7).click(); await dialog().getByRole('heading', {name:'Find Bugs execution'}).waitFor();
        assert.equal(starts, 1, 'tapping an active workflow does not duplicate the job');
        await page.keyboard.press('Escape');
        await page.locator('[data-key="agents"]').click();
        await banner.getByRole('button', {name:'Open Find Bugs execution details'}).waitFor();
        await ctx.setOffline(true);
        // The authorised process progresses while its browser is disconnected.
        await request(phone.token, 'POST', `/api/projects/${project.id}/runs/${firstRun.id}/input`, {text:'workflow-report'});
        await ctx.setOffline(false);
        await page.reload();
        await banner.getByText(/6 of 6 stages reported complete/).waitFor();
        assert.equal(starts, 1, 'reconnection never starts another job');
        // Stop in the persistent banner reaches the process and is confirmed by durable state.
        await banner.getByRole('button', {name:/Stop Find Bugs in/}).click();
        await banner.getByText('Cancelled', {exact:true}).waitFor();
        const stopped = await request(phone.token,'GET',`/api/projects/${project.id}/runs/${firstRun.id}`);
        assert.equal(stopped.state,'stopped');
        await page.goto(base + '/#/deck'); await edit().waitFor();

        // A one-off picker changes this invocation without changing the saved target.
        await socket(7).focus(); await page.keyboard.press('Shift+F10');
        await dialog().getByRole('combobox', { name: 'Repository target', exact: true }).selectOption('ask');
        await dialog().getByRole('combobox', { name: 'Output', exact: true }).selectOption('report');
        await dialog().getByRole('button', { name: 'Run Once With Different Settings', exact: true }).click();
        await dialog().getByRole('heading', { name: 'Choose workflow repositories' }).waitFor();
        assert.equal(starts, 1, 'picker does not launch before a repository is selected');
        await dialog().getByRole('checkbox', { name: project.name, exact: true }).check();
        const oneOffResponse = page.waitForResponse(r => new URL(r.url()).pathname === '/api/deck/workflows' && r.request().method() === 'POST');
        await dialog().getByRole('button', { name: 'Run workflow', exact: true }).click();
        const response = await oneOffResponse; assert(response.ok());
        const oneOff = await response.json();
        assert.equal(oneOff.tasks.length, 1); assert.equal(oneOff.task.projectId, project.id);
        const persisted = await request(phone.token, 'GET', '/api/deck');
        assert.equal(persisted.slots[7].target, 'selected', 'one-off scope is not saved');
        assert.notEqual(persisted.slots[7].output, 'report', 'one-off output is not saved');
        await request(phone.token, 'POST', `/api/projects/${project.id}/runs/${oneOff.run.id}/input`, {text:'fail'});
        await banner.getByText('Failed', {exact:true}).waitFor();
        await banner.getByRole('button', {name:'Open Find Bugs execution details'}).click();
        await dialog().getByText(/Partial completion/).waitFor();
        await dialog().getByRole('link', {name:'Change Settings'}).waitFor();
        await page.keyboard.press('Escape');
        await page.reload(); await edit().waitFor();

        // Workflow labels, targets, edit controls and sheets fit both themes and phone sizes.
        for (const width of [320, 430, 844]) {
            await page.setViewportSize({ width, height: width === 844 ? 390 : 844 });
            for (const colorScheme of ['light', 'dark']) {
                await page.emulateMedia({ colorScheme });
                assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), true, `workflow deck ${width} ${colorScheme}`);
                for (const target of [page.getByRole('button', { name: 'Jump to', exact: true }), page.getByRole('link', { name: 'Help and shortcuts', exact: true }), edit()]) {
                    const r = await target.boundingBox(); assert(r.width >= 44 && r.height >= 44, '44px controls');
                }
                if (process.env.BROWSER_ARTIFACT_DIR) await page.screenshot({ path: path.join(process.env.BROWSER_ARTIFACT_DIR, `configured-deck-${width}-${colorScheme}.png`), fullPage: true, animations: 'disabled' });
                await socket(7).focus(); await page.keyboard.press('Shift+F10');
                await dialog().getByRole('heading', { name: 'Configure Find Bugs' }).waitFor();
                assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), true, `configuration ${width} ${colorScheme}`);
                if (process.env.BROWSER_ARTIFACT_DIR) await page.screenshot({ path: path.join(process.env.BROWSER_ARTIFACT_DIR, `workflow-settings-${width}-${colorScheme}.png`), fullPage: true, animations: 'disabled' });
                await page.keyboard.press('Escape');
            }
        }
        // Removing everything persists an intentionally empty deck.
        await page.setViewportSize({ width: 390, height: 844 });
        await edit().click();
        for (const i of [0,1,2,3,4,5,7]) { await socket(i).click(); await dialog().getByRole('button', { name: 'Remove button', exact: true }).click(); }
        await done().click(); await edit().waitFor(); await page.reload(); await edit().waitFor();
        await page.waitForFunction(() => document.querySelectorAll('[data-key]').length === 0);
        assert.equal(await page.getByRole('button', { name: 'Empty slot. Add a button', exact: true }).count(), 9);
        assert.deepEqual(errors, []);
        console.log('PASS configurable deck: persistence, touch/keyboard configuration, dragging, explicit empties, normal execution, progress and detail, themes and 44px targets');
    } catch (e) {
        console.error('Browser errors:', errors);
        console.error('Deck positions:', await page.locator('[data-deck-position]').evaluateAll(els => els.map(el => el.innerText)));
        if (process.env.BROWSER_ARTIFACT_DIR) await page.screenshot({ path: path.join(process.env.BROWSER_ARTIFACT_DIR, 'deck-failure.png'), fullPage: true, animations: 'disabled' });
        throw e;
    } finally {
        await browser.close();
        await request(owner, 'DELETE', '/api/phones/' + phone.id);
    }
})().catch(e => { console.error(e); process.exitCode = 1; });

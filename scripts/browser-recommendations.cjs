// Suggested next steps, in a real browser, against the real controller, rules, Git and memory (scripts/browser-fixture) in
// disposable storage on loopback, with a scripted provider in place of Claude Code or Codex.
//
// It checks what the unit tests cannot: that the cards sit in the Git overview, the Control Deck and Salie without moving what was
// already there, that "Why this" shows the evidence and the rule behind a suggestion, that a policy outranks a habit on screen, that
// a verdict is kept in memory on the desktop and only on the device on a phone, that a suggestion starts nothing, and that
// every control has a name and a tap target a thumb can hit.
const fs = require('node:fs'), os = require('node:os'), path = require('node:path');
const cp = require('node:child_process'), net = require('node:net');
const { once } = require('node:events');
const assert = require('node:assert/strict');
const root = path.resolve(__dirname, '..');
const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'werkbord-recommendations-e2e-'));
const artifacts = process.env.BROWSER_ARTIFACT_DIR || path.join(temp, 'artifacts');
fs.mkdirSync(artifacts, { recursive: true });
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || path.join(root, 'web/node_modules/playwright'));
const token = 'disposable-browser-credential';
const children = [];
const POLICY = 'Every merge to main needs review.';

async function freePort() {
    const server = net.createServer();
    server.listen(0, '127.0.0.1'); await once(server, 'listening');
    const n = server.address().port; await new Promise(resolve => server.close(resolve)); return n;
}
function start(binary, env) {
    const child = cp.spawn(binary, [], { cwd: root, env: { ...process.env, ...env }, stdio: ['ignore', 'ignore', 'pipe'] });
    child.diagnostics = ''; child.stderr.on('data', d => child.diagnostics += d);
    children.push(child); return child;
}
async function stop(child) {
    if (child.exitCode !== null) return;
    child.kill('SIGTERM');
    await Promise.race([once(child, 'exit'), new Promise(r => setTimeout(r, 5000))]);
    if (child.exitCode === null) child.kill('SIGKILL');
}
async function ready(url, child) {
    for (let i = 0; i < 100; i++) {
        if (child.exitCode !== null) throw Error('fixture exited: ' + child.diagnostics);
        if (await fetch(url + '/api/health').then(r => r.ok).catch(() => false)) return;
        await new Promise(r => setTimeout(r, 100));
    }
    throw Error('fixture never became healthy: ' + child.diagnostics);
}
async function api(base, method, p, data, bearer = token) {
    const res = await fetch(base + p, { method, headers: { Authorization: 'Bearer ' + bearer, 'Content-Type': 'application/json' }, body: data ? JSON.stringify(data) : undefined });
    const raw = await res.text();
    if (res.status >= 400) throw Error(method + ' ' + p + ' ' + res.status + ' ' + raw);
    return raw ? JSON.parse(raw) : null;
}
const ok = msg => console.log('PASS ' + msg);
const git = (repo, ...args) => cp.execFileSync('git', ['-C', repo, '-c', 'user.name=F', '-c', 'user.email=f@example.test', '-c', 'core.hooksPath=/dev/null', ...args], { stdio: 'ignore' });

(async () => {
    const fixture = path.join(temp, 'fixture');
    cp.execFileSync('go', ['build', '-o', fixture, './scripts/browser-fixture'], { cwd: root, stdio: 'inherit' });
    // A repository with history, committed straight to main, and a file changed and not committed.
    const repo = path.join(temp, 'repo'); fs.mkdirSync(repo);
    git(repo, 'init', '-b', 'main');
    for (let i = 0; i < 7; i++) { fs.writeFileSync(path.join(repo, `f${i}.txt`), `${i}\n`); git(repo, 'add', '-A'); git(repo, 'commit', '-m', `tweak ${i}`); }
    fs.writeFileSync(path.join(repo, 'f0.txt'), 'changed\n');

    const dataDir = path.join(temp, 'data'); fs.mkdirSync(dataDir);
    const port = await freePort();
    const base = 'http://127.0.0.1:' + port;
    const server = start(fixture, { WERKBORD_BROWSER_ADDR: '127.0.0.1:' + port, WERKBORD_BROWSER_ASSISTANT: '1', WERKBORD_BROWSER_EXECUTION: '1', WERKBORD_BROWSER_DIR: dataDir, GIT_CONFIG_GLOBAL: '/dev/null', GIT_CONFIG_NOSYSTEM: '1' });
    await ready(base, server);
    await api(base, 'POST', '/api/onboarding/complete', {});
    const project = await api(base, 'POST', '/api/projects', { path: repo, name: 'Shop' });
    const pid = project.id;
    await api(base, 'POST', `/api/projects/${pid}/tasks`, { title: 'Write the README', description: '' });
    await api(base, 'POST', `/api/projects/${pid}/memory`, { kind: 'policy', summary: POLICY });

    const browser = await chromium.launch({ headless: true, ...(process.env.BROWSER_EXECUTABLE ? { executablePath: process.env.BROWSER_EXECUTABLE } : {}) });
    const errors = [];
    async function newPage(opts = {}, storage = {}) {
        const ctx = await browser.newContext({ viewport: { width: 1280, height: 860 }, ...opts });
        await ctx.addInitScript(s => { for (const [k, v] of Object.entries(s)) localStorage.setItem(k, v); }, { 'werkbord.token': token, 'werkbord.project': pid, ...storage });
        const page = await ctx.newPage();
        page.setDefaultTimeout(12000);
        page.on('pageerror', e => errors.push(e.message));
        page.on('console', m => { if (m.type() === 'error' && !/Failed to load resource|net::ERR/.test(m.text())) errors.push(m.text()); });
        return { ctx, page };
    }
    const cards = page => page.locator('article.rec');

    try {
        // The rules' answer, as the controller serves it to its owner.
        const set = await api(base, 'GET', `/api/projects/${pid}/recommendations`);
        assert.equal(set.feedbackStoredIn, 'memory');
        assert.ok(set.items.length >= 1 && set.items.length <= 2, 'at most two suggestions');
        assert.ok(set.items.every(i => i.action.startsWith === undefined && i.action.startsExecution === false), 'no suggestion starts anything');
        const first = set.items[0];
        assert.equal(first.intent, 'continue_work');
        assert.equal(first.basis, 'required', 'a policy makes moving changes off main the first step');
        assert.ok(first.applied.length > 0 && JSON.stringify(set.standing).includes(POLICY), 'the policy is cited');
        ok('the controller answers with at most two suggestions, each starting nothing, the policy cited');

        // ======== the desktop: Git overview ========
        for (const scheme of ['light', 'dark']) {
            const { ctx, page } = await newPage({ colorScheme: scheme });
            await page.goto(base + `/#/p/${pid}/git`);
            await cards(page).first().waitFor();
            assert.equal(await page.locator('section.recs').count(), 1);
            assert.ok(await cards(page).count() <= 2);
            const text = await cards(page).first().innerText();
            assert.match(text, /Needed first/);
            assert.match(text, /Move your changes off main/);
            assert.match(text, /Nothing changes until you confirm there|You start it yourself|Open changes/);
            // The Git overview is otherwise as it was.
            await page.getByRole('heading', { name: 'Needs attention' }).waitFor();
            // "Why this" says what it rests on and which rule shaped it.
            const why = cards(page).first().locator('details.why');
            assert.equal(await why.getAttribute('open'), null, 'the reasons start collapsed');
            await why.locator('summary').click();
            const reasons = await why.innerText();
            assert.match(reasons, /what shaped it/i);
            assert.match(reasons, /what it rests on/i);
            assert.ok(reasons.includes(POLICY), 'the owner sees which saved policy applied');
            assert.match(reasons, /never becomes a rule/i);
            const box = await cards(page).first().boundingBox();
            assert.ok(box.x >= 0 && box.x + box.width <= 1280, 'the card fits the window');
            await page.screenshot({ path: path.join(artifacts, `recommendations-git-${scheme}.png`) });
            await ctx.close();
        }
        ok('Git overview shows the suggestions at the top, with the evidence and the policy behind them, in both themes');

        // ======== the phone: the Control Deck ========
        {
            const code = await api(base, 'POST', '/api/phones/codes', {});
            const phone = await api(base, 'POST', '/api/phones/enroll', { code: code.code, name: 'Suggestion fixture' }, '');
            const deck = await api(base, 'GET', '/api/deck', undefined, phone.token);
            const slots = deck.slots.map(s => s ? { ...s } : null);
            slots[6] = { id: 'wf-find-bugs', button: 'find-bugs', target: 'ask' };
            await api(base, 'PUT', '/api/deck', { version: deck.version, slots }, phone.token);
            const { ctx, page } = await newPage({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, colorScheme: 'light' },
                { 'werkbord.token': phone.token, 'werkbord-tour-phone-deck-v2': 'fixture' });
            const posts = [];
            page.on('request', r => { if (r.method() === 'POST' && (r.url().includes('/recommendations/feedback') || r.url().includes('/deck/workflows'))) posts.push(r.url()); });
            await page.goto(base + '/#/deck');
            await page.locator('[data-testid="deck-grid"]').waitFor();
            await cards(page).first().waitFor();
            assert.equal(await page.locator('[data-deck-position]').count(), 9, 'the nine positions are unchanged');
            // The card is below the keys: nothing the deck already had moved above it.
            const keys = await page.locator('section.housing').boundingBox();
            const card = await cards(page).first().boundingBox();
            assert.ok(card.y >= keys.y + keys.height - 1, 'suggestions sit below the keys');
            assert.ok(card.x >= 0 && card.x + card.width <= 390, 'the card fits a phone');
            // A phone is not shown what saved memory said, and keeps its own verdicts.
            await cards(page).first().locator('details.why summary').click();
            assert.ok(!(await page.content()).includes(POLICY), 'saved text is not served to a phone');
            const phoneSet = await api(base, 'GET', `/api/projects/${pid}/recommendations`, undefined, phone.token);
            assert.equal(phoneSet.feedbackStoredIn, 'device');
            // Scrolled to the end, the last card is clear of the bottom bar.
            await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight));
            await page.waitForTimeout(150);
            const last = await cards(page).last().boundingBox();
            const bar = await page.locator('[data-phone-nav]').first().boundingBox();
            assert.ok(!bar || last.y + last.height <= bar.y + 1, 'the last suggestion is not hidden behind the bottom bar');
            await page.screenshot({ path: path.join(artifacts, 'recommendations-deck-phone-end.png') });
            await page.evaluate(() => window.scrollTo(0, 0));
            // Tap targets and names.
            for (const b of await cards(page).first().getByRole('button').all()) {
                const bb = await b.boundingBox();
                assert.ok(bb.height >= 36, 'a thumb can hit it: ' + (await b.innerText()));
                assert.ok((await b.innerText()).trim().length > 0 || await b.getAttribute('aria-label'), 'every button has a name');
            }
            await page.screenshot({ path: path.join(artifacts, 'recommendations-deck-phone.png'), fullPage: true });
            // A suggestion that points at a workflow marks its key; it does not press it.
            const marked = await page.locator('.position.suggested').count();
            assert.equal(marked, 1, 'the Find Bugs suggestion marks its key');
            {
                assert.ok(await page.locator('[data-deck-position="6"].suggested [data-key="find-bugs"]').count() === 1, 'the Find Bugs key is the one marked');
                await page.locator('article.rec').filter({ hasText: /Scan for bugs/ }).getByRole('button', { name: 'Show on Deck' }).click();
                await page.getByText(/is marked on the Deck/).first().waitFor({ state: 'attached' });
            }
            // Dismissing on a phone is remembered on the phone, and asks memory for nothing.
            const title = await cards(page).first().locator('h3').innerText();
            await cards(page).first().getByRole('button', { name: 'Dismiss', exact: true }).click();
            await page.waitForFunction(t => ![...document.querySelectorAll('article.rec h3')].some(h => h.textContent === t), title);
            assert.deepEqual(posts, [], 'nothing was sent to memory, and no workflow was started');
            assert.ok(await page.evaluate(() => Object.keys(JSON.parse(localStorage.getItem('werkbord.recommendations.hidden') || '{}')).length) >= 1);
            await page.reload();
            await page.locator('[data-testid="deck-grid"]').waitFor();
            await page.waitForTimeout(400);
            assert.ok(!(await page.locator('article.rec h3').allInnerTexts()).includes(title), 'still hidden after a reload');
            ok('the Control Deck keeps its nine keys, shows the suggestions below them, marks but never presses a workflow key, and keeps a phone’s verdicts on the phone');
            await ctx.close();
        }

        // ======== a verdict on the desktop is kept in memory ========
        {
            const { ctx, page } = await newPage();
            const posts = [];
            page.on('request', r => { if (r.method() === 'POST' && r.url().includes('/recommendations/feedback')) posts.push(r.postData()); });
            await page.goto(base + `/#/p/${pid}/git`);
            await cards(page).first().waitFor();
            await cards(page).first().getByRole('button', { name: 'Useful', exact: true }).click();
            await cards(page).first().getByText('Noted as useful.').waitFor();
            assert.equal(posts.length, 1);
            const wasSecond = cards(page).nth(1);
            if (await wasSecond.count()) {
                const title = await wasSecond.locator('h3').innerText();
                await wasSecond.getByRole('button', { name: 'Dismiss', exact: true }).click();
                await page.waitForFunction(t => ![...document.querySelectorAll('article.rec h3')].some(h => h.textContent === t), title);
                await page.reload();
                await cards(page).first().waitFor();
                assert.ok(!(await page.locator('article.rec h3').allInnerTexts()).includes(title), 'a dismissed suggestion stays dismissed');
            }
            const saved = await api(base, 'GET', `/api/projects/${pid}/memory?type=recommendation_feedback&limit=50`);
            assert.ok(saved.records.length >= 1);
            for (const m of saved.records) {
                assert.equal(m.authority, 'inferred_preference');
                assert.equal(m.sharingEligible, false);
                assert.ok(!m.verifiedAt, 'feedback is never verified knowledge');
            }
            const policies = await api(base, 'GET', `/api/projects/${pid}/memory?authority=approved_policy&limit=50`);
            assert.equal(policies.records.length, 1, 'feedback did not become a policy');
            ok('Useful and Dismiss are kept as bounded private preferences in memory, never as a policy');
            await ctx.close();
        }

        // ======== Salie ========
        {
            const { ctx, page } = await newPage({ colorScheme: 'light' });
            await page.goto(base + `/#/p/${pid}/git`);
            await page.getByTestId('assistant-button').first().click();
            await page.getByTestId('assistant').waitFor();
            await page.waitForFunction(() => document.querySelector('[data-testid="assistant"]')?.dataset.phase === 'open');
            // Nothing said yet: what the rules would suggest for the project last worked on, beside the usual three questions.
            await page.getByTestId('assistant-empty').waitFor();
            assert.equal(await page.getByRole('group', { name: 'Things to ask' }).getByRole('button').count(), 3, 'the starting questions are kept');
            await page.screenshot({ path: path.join(artifacts, 'recommendations-assistant-empty.png') });
            const box = page.getByRole('textbox', { name: 'Message to Salie' });
            await box.fill('What should I do next? ' + pid);
            await page.getByRole('button', { name: 'Send', exact: true }).click();
            const region = page.getByTestId('assistant-suggestions');
            await region.waitFor();
            assert.ok(await region.locator('article.rec').count() >= 1 && await region.locator('article.rec').count() <= 2);
            await page.waitForFunction(() => document.querySelector('article.reply')?.dataset.status === 'done');
            assert.equal(await page.getByTestId('assistant-confirm-all').count(), 0);
            assert.equal(await page.getByRole('button', { name: 'Confirm', exact: true }).count(), 0, 'a suggestion asks for no confirmation: it changes nothing');
            await page.screenshot({ path: path.join(artifacts, 'recommendations-assistant-reply.png') });
            ok('Salie shows the rules’ answer as cards beside the starting questions and in a reply, and proposes nothing');
            await ctx.close();
        }

        // Nothing started anywhere.
        const runs = await api(base, 'GET', `/api/projects/${pid}/runs`);
        assert.equal((runs.runs ?? runs).length, 0, 'no run was started');
        const tasks = await api(base, 'GET', `/api/projects/${pid}/tasks`);
        assert.equal((tasks.tasks ?? tasks).length, 1, 'no task was created');
        assert.deepEqual(errors, [], 'no page errors');
        ok('nothing was started, nothing was created, and the pages raised no errors');
    } catch (e) {
        console.error(e);
        console.error(server.diagnostics);
        process.exitCode = 1;
    } finally {
        await browser.close();
        for (const c of children) await stop(c);
        console.log('Browser artifacts: ' + artifacts);
    }
})();

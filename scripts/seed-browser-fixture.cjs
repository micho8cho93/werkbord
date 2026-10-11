// Seeds a running disposable browser fixture (scripts/browser-fixture, started with WERKBORD_BROWSER_EXECUTION=1) with the
// state the screens need to be looked at: a repository project with one run waiting on a question, a second repository with a
// finished run in Review, and a work project. Nothing here touches a real controller: the fixture's own throwaway storage and
// its scripted agent only.
//
//   PERSONAL_BROWSER_URL=http://127.0.0.1:17499 SEED_DIR=/tmp/seed node scripts/seed-browser-fixture.cjs
//
// SEED_DIR is where the two throwaway Git repositories are made (created if missing). Prints the project ids as JSON.
const { execFileSync } = require('node:child_process');
const fs = require('node:fs');
const path = require('node:path');
const base = process.env.PERSONAL_BROWSER_URL || 'http://127.0.0.1:17421';
const dir = process.env.SEED_DIR;
if (!dir) throw Error('SEED_DIR is required: a scratch directory for two throwaway Git repositories');
const token = 'disposable-browser-credential';

async function api(method, route, data) {
    const res = await fetch(base + route, { method, headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' }, body: data ? JSON.stringify(data) : undefined });
    const raw = await res.text();
    if (res.status >= 400) throw Error(`${method} ${route}: ${res.status} ${raw}`);
    return raw ? JSON.parse(raw) : null;
}
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const git = (cwd, ...args) => execFileSync('git', ['-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.test', '-c', 'core.hooksPath=/dev/null', ...args], { cwd, stdio: 'ignore' });

function repo(name) {
    fs.mkdirSync(dir, { recursive: true });
    const p = path.join(fs.realpathSync(dir), name);
    fs.rmSync(p, { recursive: true, force: true });
    fs.mkdirSync(p, { recursive: true });
    git(p, 'init', '-q', '-b', 'main');
    fs.writeFileSync(path.join(p, 'README.md'), name + '\n');
    git(p, 'add', '.');
    git(p, 'commit', '-q', '-m', 'init');
    return p;
}

(async () => {
    const url = new URL(base);
    if (url.protocol !== 'http:' || !['127.0.0.1', 'localhost', '[::1]'].includes(url.hostname)) throw Error('Seeding requires a loopback browser fixture');
    const health = await api('GET', '/api/health');
    if (health.version !== 'browser-fixture') throw Error('Refusing to seed anything except the disposable browser fixture');
    await api('POST', '/api/onboarding/complete', {});
    // Two runs at once, one in each repository.
    const local = (await api('GET', '/api/runners')).runners.find((r) => r.kind === 'local');
    await api('PUT', '/api/runners/' + local.id, { name: local.name, automatic: true, capacity: 3 });

    const waiting = await api('POST', '/api/projects', { path: repo('waiting-repo'), name: 'Fixture repo' });
    const review = await api('POST', '/api/projects', { path: repo('review-repo'), name: 'Second repo' });
    const work = await api('POST', '/api/projects', { kind: 'work', name: 'Venue search' });
    for (const p of [waiting, review]) await api('PUT', `/api/projects/${p.id}/execution`, { agent: 'fixture' });

    // A run that asks a question and waits for the person.
    const asking = await api('POST', `/api/projects/${waiting.id}/tasks`, { title: 'Add retry to the sync client' });
    await api('POST', `/api/projects/${waiting.id}/tasks`, { title: 'Dark mode audit' });
    const askRun = await api('POST', `/api/projects/${waiting.id}/tasks/${asking.id}/runs`, {});
    await sleep(1500);
    await api('POST', `/api/projects/${waiting.id}/runs/${askRun.id}/input`, { text: 'ask' });

    // A run that commits and finishes, and its task in Review.
    const done = await api('POST', `/api/projects/${review.id}/tasks`, { title: 'Tidy the settings copy' });
    const doneRun = await api('POST', `/api/projects/${review.id}/tasks/${done.id}/runs`, {});
    await sleep(1500);
    await api('POST', `/api/projects/${review.id}/runs/${doneRun.id}/input`, { text: 'commit' });
    await sleep(2500);
    await api('POST', `/api/projects/${review.id}/runs/${doneRun.id}/finish`, {});
    const fresh = (await api('GET', `/api/projects/${review.id}/tasks`)).tasks.find((t) => t.id === done.id);
    if (fresh.state !== 'review') await api('PATCH', `/api/projects/${review.id}/tasks/${done.id}`, { state: 'review', version: fresh.version });

    for (const title of ['Book the venue', 'Send the invitations', 'Order catering']) await api('POST', `/api/projects/${work.id}/tasks`, { title });

    const runs = (await api('GET', `/api/projects/${waiting.id}/runs`)).runs;
    if (!runs.some((r) => r.state === 'waiting_for_user')) throw Error('the question did not arrive: the fixture needs WERKBORD_BROWSER_EXECUTION=1');
    console.log(JSON.stringify({ waiting: waiting.id, review: review.id, work: work.id }));
})().catch((e) => { console.error(e.message); process.exit(1); });

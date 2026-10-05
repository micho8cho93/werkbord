// Disposable live bridge check: imports a ticket, commits in the fixture repository and reports metadata.
const fs = require('node:fs'), cp = require('node:child_process'), assert = require('node:assert/strict');
const local = process.env.PERSONAL_BROWSER_URL || 'http://127.0.0.1:17421';
const team = process.env.TEAM_BROWSER_URL || 'http://127.0.0.1:17430';
const token = process.env.TEAM_BROWSER_TOKEN || fs.readFileSync(process.env.TEAM_BROWSER_TOKEN_FILE, 'utf8').match(/wbt_[a-zA-Z0-9]+/)[0];
const binary = process.env.WERKBORD_TEAM_BINARY || require('node:path').resolve('bin/werkbord-team');
async function api(base, credential, method, path, body) { const r = await fetch(base + path, { method, headers: { Authorization: 'Bearer ' + credential, 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined }); const b = await r.json(); if (!r.ok)
    throw Error(r.status + ' ' + JSON.stringify(b)); return b; }
(async () => {
    const projects = await api(local, 'disposable-browser-credential', 'GET', '/api/projects');
    const lp = projects.projects[0];
    const p = await api(team, token, 'POST', '/api/team/v1/projects', { name: 'Bridge integration ' + Date.now(), repository: 'https://github.com/acme/bridge' });
    let k = await api(team, token, 'POST', `/api/team/v1/projects/${p.id}/tickets`, { title: '界'.repeat(200), description: '界'.repeat(12000), requirements: 'Context preserved', status: 'available' });
    k = await api(team, token, 'POST', `/api/team/v1/projects/${p.id}/tickets/${k.id}/claim`);
    await api(team, token, 'PUT', `/api/team/v1/projects/${p.id}/tickets/${k.id}/git`, { state: { baseBranch: 'main' } });
    const args = ['handoff', '--server', team, '--project', p.id, '--ticket', k.id, '--runner', local, '--local-project', lp.id];
    function cli(extra = []) { const r = cp.spawnSync(binary, [...args, ...extra], { encoding: 'utf8', env: { ...process.env, WERKBORD_TEAM_TOKEN: token, DEVBOARD_TOKEN: 'disposable-browser-credential' } }); assert.equal(r.status, 0, r.stderr); return r.stdout; }
    cli();
    let tasks = await api(local, 'disposable-browser-credential', 'GET', `/api/projects/${lp.id}/tasks`);
    let task = tasks.tasks.find(x => x.workBranch === k.branch);
    assert(task);
    assert.equal([...task.title].length, 200);
    assert(task.description.includes('界'.repeat(200)));
    assert(task.description.includes('界'.repeat(12000)));
    assert.equal(task.baseBranch, 'main');
    await api(local, 'disposable-browser-credential', 'PATCH', `/api/projects/${lp.id}/tasks/${task.id}`, { version: task.version, title: 'Locally edited imported task' });
    cli();
    tasks = await api(local, 'disposable-browser-credential', 'GET', `/api/projects/${lp.id}/tasks`);
    assert.equal(tasks.tasks.filter(x => x.sourceRef === task.sourceRef).length, 1);
    assert.equal(tasks.tasks.find(x => x.id === task.id).title, 'Locally edited imported task');
    const repo = lp.repoPath;
    function git(...a) { const r = cp.spawnSync('git', ['-C', repo, '-c', 'core.hooksPath=/dev/null', ...a], { encoding: 'utf8', env: { ...process.env, GIT_AUTHOR_NAME: 'Bridge test', GIT_AUTHOR_EMAIL: 'test@example.test', GIT_COMMITTER_NAME: 'Bridge test', GIT_COMMITTER_EMAIL: 'test@example.test', GIT_CONFIG_GLOBAL: '/dev/null' } }); assert.equal(r.status, 0, r.stderr); return r.stdout.trim(); }
    git('checkout', '-b', k.branch, 'main');
    fs.writeFileSync(repo + '/bridge.txt', 'Verified on the developer machine\n');
    git('add', 'bridge.txt');
    git('commit', '-m', 'Actual branch commit for Team metadata');
    const sha = git('rev-parse', 'HEAD');
    git('checkout', 'main');
    cli(['--report']);
    k = await api(team, token, 'GET', `/api/team/v1/projects/${p.id}/tickets/${k.id}`);
    assert.equal(k.commits.length, 1);
    assert.equal(k.commits[0].sha, sha);
    assert.equal(k.branch, task.workBranch);
    console.log('PASS live Team → individual import, 200-code-point Unicode title, long context, durable association, intended branch/base, re-import retains edits, real Git commit reported to Team');
})().catch(e => { console.error(e.message); process.exit(1); });

// Disposable live bridge check: import → subprocess agent → Git worktree → report → review → Done.
// The import and the report are the two calls the Team service's synchronization makes on a member's computer, made here by hand
// against the real APIs of both servers (there is no command line for them any more).
const fs = require('node:fs'), cp = require('node:child_process'), assert = require('node:assert/strict');
const local = process.env.PERSONAL_BROWSER_URL || 'http://127.0.0.1:17421';
const team = process.env.TEAM_BROWSER_URL || 'http://127.0.0.1:17430';
const token = process.env.TEAM_BROWSER_TOKEN || fs.readFileSync(process.env.TEAM_BROWSER_TOKEN_FILE, 'utf8').match(/wbt_[a-zA-Z0-9]+/)[0];
async function api(base, credential, method, path, body) { const r = await fetch(base + path, { method, headers: { Authorization: 'Bearer ' + credential, 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined }); const raw = await r.text(); const b = raw ? JSON.parse(raw) : null; if (!r.ok)
    throw Error(method + ' ' + path + ' ' + r.status + ' ' + JSON.stringify(b)); return b; }
(async () => {
    const projects = await api(local, 'disposable-browser-credential', 'GET', '/api/projects');
    const lp = projects.projects[0];
    const p = await api(team, token, 'POST', '/api/team/v1/projects', { name: 'Bridge integration ' + Date.now(), repository: 'https://github.com/acme/bridge' });
    let k = await api(team, token, 'POST', `/api/team/v1/projects/${p.id}/tickets`, { title: '界'.repeat(200), description: '界'.repeat(12000), requirements: 'Context preserved', status: 'available' });
    k = await api(team, token, 'POST', `/api/team/v1/projects/${p.id}/tickets/${k.id}/claim`);
    await api(team, token, 'PUT', `/api/team/v1/projects/${p.id}/tickets/${k.id}/git`, { state: { baseBranch: 'main' } });
    const L = 'disposable-browser-credential';
    // Import: the ticket's handoff from Team becomes a task in the member's own Werkbord. Importing again reuses the task.
    async function importTicket() {
        const h = await api(team, token, 'POST', `/api/team/v1/projects/${p.id}/tickets/${k.id}/handoff`);
        assert.equal(h.schema, 'werkbord-team.handoff/v1');
        const title = [...(h.ticket.key + ': ' + h.ticket.title)].slice(0, 200).join('');
        const sourceRef = team + '/?tab=board&project=' + encodeURIComponent(p.id) + '&ticket=' + encodeURIComponent(k.id);
        return api(local, L, 'POST', `/api/projects/${lp.id}/tasks`, { title, description: h.prompt, sourceRef, workBranch: h.git.branch, baseBranch: h.git.baseBranch });
    }
    // Report: the branch, its commits and its pull request, as the member's Werkbord sees them, to Team.
    async function report(taskId) {
        const base = `/api/projects/${lp.id}`;
        const runs = (await api(local, L, 'GET', `${base}/tasks/${taskId}/runs`)).runs;
        const last = runs[runs.length - 1];
        const branch = (last && last.branch) || k.branch, scope = last && last.remote ? 'remote' : 'local';
        const c = await api(local, L, 'GET', `${base}/git/compare?scope=${scope}&branch=${encodeURIComponent(branch)}&limit=200&commitLimit=200&target=main`);
        const body = { branch, commits: c.unique.items.map(i => ({ sha: i.sha, subject: i.subject, author: i.author, committedAt: i.date })), state: { headSha: c.branchSha, baseBranch: c.target, ahead: c.ahead, behind: c.behind, files: c.files.map(f => f.path) } };
        await api(team, token, 'PUT', `/api/team/v1/projects/${p.id}/tickets/${k.id}/git`, body);
    }
    await importTicket();
    let tasks = await api(local, 'disposable-browser-credential', 'GET', `/api/projects/${lp.id}/tasks`);
    let task = tasks.tasks.find(x => x.workBranch === k.branch);
    assert(task);
    assert.equal([...task.title].length, 200);
    assert(task.description.includes('界'.repeat(200)));
    assert(task.description.includes('界'.repeat(12000)));
    assert.equal(task.baseBranch, 'main');
    await api(local, 'disposable-browser-credential', 'PATCH', `/api/projects/${lp.id}/tasks/${task.id}`, { version: task.version, title: 'Locally edited imported task' });
    await importTicket();
    tasks = await api(local, 'disposable-browser-credential', 'GET', `/api/projects/${lp.id}/tasks`);
    assert.equal(tasks.tasks.filter(x => x.sourceRef === task.sourceRef).length, 1);
    assert.equal(tasks.tasks.find(x => x.id === task.id).title, 'Locally edited imported task');
    const repo = lp.repoPath;
    function git(...a) { const r = cp.spawnSync('git', ['-C', repo, '-c', 'core.hooksPath=/dev/null', ...a], { encoding: 'utf8', env: { ...process.env, GIT_AUTHOR_NAME: 'Bridge test', GIT_AUTHOR_EMAIL: 'test@example.test', GIT_COMMITTER_NAME: 'Bridge test', GIT_COMMITTER_EMAIL: 'test@example.test', GIT_CONFIG_GLOBAL: '/dev/null' } }); assert.equal(r.status, 0, r.stderr); return r.stdout.trim(); }
    const runners = await api(local, 'disposable-browser-credential', 'GET', '/api/runners');
    const runnerId = runners.runners.find(r => r.kind === 'local').id;
    const projectPath = `/api/projects/${lp.id}`;
    const run = await api(local, 'disposable-browser-credential', 'POST', `${projectPath}/tasks/${task.id}/runs`, { agentId: 'fixture', runnerId });
    assert.equal(run.remote, false);
    assert.equal(run.agentId, 'fixture');
    assert(run.worktreeId, 'fixture must execute in a recorded worktree');
    async function waitFor(get, check) {
        let value;
        for (let i = 0; i < 100; i++) { value = await get(); if (check(value)) return value; await new Promise(r => setTimeout(r, 100)); }
        throw Error('Integration state did not settle: ' + JSON.stringify(value));
    }
    const getRun = () => api(local, 'disposable-browser-credential', 'GET', `${projectPath}/runs/${run.id}`);
    await waitFor(getRun, r => r.state === 'waiting_for_user');
    await api(local, 'disposable-browser-credential', 'POST', `${projectPath}/runs/${run.id}/input`, { text: 'ask' });
    const qs = await waitFor(() => api(local, 'disposable-browser-credential', 'GET', `${projectPath}/questions`), v => v.questions.some(q => q.runId === run.id && q.state === 'pending'));
    const question = qs.questions.find(q => q.runId === run.id && q.state === 'pending');
    await api(local, 'disposable-browser-credential', 'POST', `${projectPath}/questions/${question.id}/answer`, { answer: 'Yes' });
    await waitFor(getRun, r => r.state === 'waiting_for_user');
    await api(local, 'disposable-browser-credential', 'POST', `${projectPath}/runs/${run.id}/finish`, {});
    await waitFor(getRun, r => r.state === 'completed');
    const sha = git('rev-parse', k.branch);
    assert.equal(git('show', k.branch + ':executed.txt'), 'Committed by the disposable runner agent');
    assert.equal(git('branch', '--show-current'), 'main', 'user checkout must be preserved');
    await report(task.id);
    k = await api(team, token, 'GET', `/api/team/v1/projects/${p.id}/tickets/${k.id}`);
    assert.equal(k.commits.length, 1);
    assert.equal(k.commits[0].sha, sha);
    assert.equal(k.branch, task.workBranch);
    const reviewer = await api(team, token, 'POST', '/api/team/v1/members', { name: 'Bridge reviewer' });
    await api(team, token, 'PUT', `/api/team/v1/projects/${p.id}/members/${reviewer.member.id}`, { role: 'reviewer' });
    const ticketPath = `/api/team/v1/projects/${p.id}/tickets/${k.id}`;
    const pr = { number: 71, url: 'https://github.com/acme/bridge/pull/71', state: 'open', mergeable: 'mergeable', baseBranch: 'main' };
    await api(team, token, 'PUT', ticketPath + '/git', { pullRequest: pr });
    await api(team, token, 'POST', ticketPath + '/submit', { reviewerId: reviewer.member.id });
    let reviews = await api(team, reviewer.token, 'GET', '/api/team/v1/reviews');
    assert.equal(reviews.items.find(x => x.ticket.id === k.id).canComplete, false);
    git('merge', '--ff-only', k.branch);
    await api(team, reviewer.token, 'PUT', ticketPath + '/git', { pullRequest: { ...pr, state: 'merged' } });
    const stale = await fetch(team + ticketPath + '/git', { method: 'PUT', headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' }, body: JSON.stringify({ pullRequest: pr }) });
    assert.equal(stale.status, 409, 'a stale report cannot reopen the merged PR');
    reviews = await api(team, reviewer.token, 'GET', '/api/team/v1/reviews');
    assert.equal(reviews.items.find(x => x.ticket.id === k.id).canComplete, true);
    k = await api(team, reviewer.token, 'POST', ticketPath + '/complete', {});
    assert.equal(k.status, 'done');
    assert.equal(k.commits[0].sha, sha);
    tasks = await api(local, 'disposable-browser-credential', 'GET', `${projectPath}/tasks`);
    await api(local, 'disposable-browser-credential', 'PATCH', `${projectPath}/tasks/${task.id}`, { version: tasks.tasks.find(x => x.id === task.id).version, state: 'done' });
    const failureTask = await api(local, 'disposable-browser-credential', 'POST', `${projectPath}/tasks`, { title: 'Agent failure recovery' });
    const failedRun = await api(local, 'disposable-browser-credential', 'POST', `${projectPath}/tasks/${failureTask.id}/runs`, { agentId: 'fixture', runnerId });
    const failedPath = `${projectPath}/runs/${failedRun.id}`;
    await waitFor(() => api(local, 'disposable-browser-credential', 'GET', failedPath), r => r.state === 'waiting_for_user');
    await api(local, 'disposable-browser-credential', 'POST', failedPath + '/input', { text: 'fail' });
    const failed = await waitFor(() => api(local, 'disposable-browser-credential', 'GET', failedPath), r => r.state === 'failed');
    assert(failed.reason.includes('intentional fixture agent failure'));
    const retried = await api(local, 'disposable-browser-credential', 'POST', `${projectPath}/tasks/${failureTask.id}/runs`, { agentId: 'fixture', runnerId });
    assert.equal(retried.worktreeId, failedRun.worktreeId, 'retry must retain the task workspace');
    await api(local, 'disposable-browser-credential', 'POST', `${projectPath}/runs/${retried.id}/stop`, {});
    await waitFor(() => api(local, 'disposable-browser-credential', 'GET', `${projectPath}/runs/${retried.id}`), r => r.state === 'stopped');
    console.log('PASS live Team import → actual subprocess/question → real Git worktree/commit → metadata → separate reviewer/merge → Done; failure/retry retains workspace');
    console.log('PASS Unicode/context limits, durable association, intended branch/base, re-import retains edits, merged PR resists stale report');
})().catch(e => { console.error(e.stack); process.exit(1); });

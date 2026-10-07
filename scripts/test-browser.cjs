// Complete browser/integration validation. All servers, credentials and Git work
// belong to this disposable directory; no production server URL is accepted.
const fs = require('node:fs'), os = require('node:os'), path = require('node:path');
const cp = require('node:child_process'), net = require('node:net');
const { once } = require('node:events');
const root = path.resolve(__dirname, '..');
const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'werkbord-e2e-'));
const artifacts = process.env.BROWSER_ARTIFACT_DIR || path.join(temp, 'artifacts');
fs.mkdirSync(artifacts, { recursive: true });
const children = [];
const env = { ...process.env, GIT_CONFIG_GLOBAL: '/dev/null', GIT_CONFIG_NOSYSTEM: '1' };
const commandTimeout = Number(process.env.BROWSER_COMMAND_TIMEOUT_MS) || 120000;
function run(binary, args, extra = {}, quiet = false) {
    const r = cp.spawnSync(binary, args, { cwd: root, env: { ...env, ...extra }, encoding: 'utf8', timeout: commandTimeout });
    if (r.status !== 0) throw Error(`${binary} failed: ${r.error || r.stderr}\n${r.stdout}`);
    if (!quiet) process.stdout.write(r.stdout);
    return r.stdout;
}
async function port() {
    const server = net.createServer();
    server.listen(0, '127.0.0.1'); await once(server, 'listening');
    const n = server.address().port; await new Promise(resolve => server.close(resolve)); return n;
}
function serve(binary, args, extra) {
    const child = cp.spawn(binary, args, { cwd: root, env: { ...env, ...extra }, stdio: ['ignore', 'ignore', 'pipe'] });
    child.diagnostics = ''; child.stderr.on('data', d => child.diagnostics += d);
    children.push(child); return child;
}
async function ready(child, url) {
    for (let i = 0; i < 100; i++) {
        if (child.exitCode !== null) throw Error('Disposable server exited: ' + child.diagnostics);
        if (await fetch(url).then(r => r.ok).catch(() => false)) return;
        await new Promise(resolve => setTimeout(resolve, 100));
    }
    throw Error('Disposable server failed to become healthy: ' + child.diagnostics);
}
async function cleanup() {
    for (const c of children) {
        if (c.exitCode === null) {
            c.kill('SIGTERM');
            await Promise.race([once(c, 'exit'), new Promise(resolve => setTimeout(resolve, 5000))]);
            if (c.exitCode === null) c.kill('SIGKILL');
        }
    }
    // Screenshots are useful after both success and failure. They contain only fixtures.
    console.log('Browser artifacts: ' + artifacts);
    fs.rmSync(path.join(artifacts, 'active-token.txt'), { force: true });
    for (const name of ['team-data', 'owner-token.txt', 'fixture', 'repo']) fs.rmSync(path.join(temp, name), { recursive: true, force: true });
}
(async () => {
    const repo = path.join(temp, 'repo'); fs.mkdirSync(repo);
    run('git', ['-C', repo, 'init', '-b', 'main'], {}, true);
    run('git', ['-C', repo, '-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.test', '-c', 'core.hooksPath=/dev/null', 'commit', '--allow-empty', '-m', 'Initial fixture commit'], {}, true);
    const fixture = path.join(temp, 'fixture');
    run('go', ['build', '-o', fixture, './scripts/browser-fixture'], {}, true);
    const personalURL = 'http://127.0.0.1:' + await port();
    const personal = serve(fixture, [], { WERKBORD_BROWSER_ADDR: new URL(personalURL).host, WERKBORD_BROWSER_EXECUTION: '1' });
    await ready(personal, personalURL + '/api/health');
    const teamURL = 'http://127.0.0.1:' + await port();
    const teamBinary = path.join(root, 'bin/werkbord-team');
    // This test is about the pages and the handoff, not where the workspace's data lives: one file needs no database program
    // (Team's own cluster tests cover the replicated storage).
    const teamEnv = { WERKBORD_TEAM_DATA_DIR: path.join(temp, 'team-data'), WERKBORD_TEAM_ADDR: new URL(teamURL).host, WERKBORD_TEAM_STORAGE: 'single-file' };
    const created = run(teamBinary, ['workspace', 'create', '--name', 'Disposable E2E', '--owner', 'Fixture owner'], teamEnv, true);
    const tokenFile = path.join(temp, 'owner-token.txt');
    fs.writeFileSync(tokenFile, created.match(/wbt_[a-zA-Z0-9]+/)[0], { mode: 0o600 });
    const team = serve(teamBinary, ['serve'], teamEnv);
    await ready(team, teamURL + '/api/team/v1/health');
    const testEnv = { PERSONAL_BROWSER_URL: personalURL, TEAM_BROWSER_URL: teamURL, TEAM_BROWSER_TOKEN: '', TEAM_BROWSER_TOKEN_FILE: tokenFile, WERKBORD_BROWSER_REPO: repo, BROWSER_ARTIFACT_DIR: artifacts, PLAYWRIGHT_MODULE: process.env.PLAYWRIGHT_MODULE || path.join(root, 'web/node_modules/playwright') };
    run(process.execPath, ['scripts/browser-personal.cjs'], testEnv);
    run(process.execPath, ['scripts/browser-team.cjs'], testEnv);
    testEnv.TEAM_BROWSER_TOKEN_FILE = path.join(artifacts, 'active-token.txt');
    run(process.execPath, ['scripts/browser-bridge.cjs'], testEnv);
    console.log('PASS disposable Individual + Team browser/integration suite');
})().catch(e => { console.error(e.message); process.exitCode = 1; }).finally(cleanup);

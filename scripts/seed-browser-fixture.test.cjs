// Run with node --test scripts/seed-browser-fixture.test.cjs. Refusals must precede any write or repository deletion.
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { execFile } = require('node:child_process');
const { promisify } = require('node:util');
const http = require('node:http');
const fs = require('node:fs/promises');
const os = require('node:os');
const path = require('node:path');
const run = promisify(execFile);

for (const version of ['v4.18.0-preview.1', undefined]) {
    test(`refuses controller version ${version ?? 'missing'} before writes`, async () => {
        const requests = [];
        const server = http.createServer((req, res) => {
            requests.push(`${req.method} ${req.url}`);
            res.setHeader('Content-Type', 'application/json');
            res.end(JSON.stringify({ status: 'ok', version }));
        });
        await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
        const dir = await fs.mkdtemp(path.join(os.tmpdir(), 'werkbord-seed-guard-'));
        const marker = path.join(dir, 'waiting-repo', 'keep.txt');
        await fs.mkdir(path.dirname(marker));
        await fs.writeFile(marker, 'keep');
        try {
            await assert.rejects(run(process.execPath, [path.join(__dirname, 'seed-browser-fixture.cjs')], {
                env: { ...process.env, PERSONAL_BROWSER_URL: `http://127.0.0.1:${server.address().port}`, SEED_DIR: dir },
            }), /Refusing to seed/);
            assert.deepEqual(requests, ['GET /api/health']);
            assert.equal(await fs.readFile(marker, 'utf8'), 'keep');
        } finally {
            await new Promise((resolve) => server.close(resolve));
            await fs.rm(dir, { recursive: true, force: true });
        }
    });
}

test('refuses non-loopback destinations before connecting', async () => {
    await assert.rejects(run(process.execPath, [path.join(__dirname, 'seed-browser-fixture.cjs')], {
        env: { ...process.env, PERSONAL_BROWSER_URL: 'http://example.invalid', SEED_DIR: '/unused' },
    }), /Seeding requires a loopback browser fixture/);
});

// Desktop frame regression: project lifecycle and connected agents against disposable real APIs.
// Run: node scripts/browser-project-actions-map.cjs
const fs = require('node:fs'), os = require('node:os'), path = require('node:path');
const cp = require('node:child_process'), net = require('node:net');
const { once } = require('node:events');
const assert = require('node:assert/strict');
const root = path.resolve(__dirname, '..');
const { chromium } = require(path.join(root, 'web/node_modules/playwright'));
const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'werkbord-personal-'));
const artifacts = process.env.BROWSER_ARTIFACT_DIR || path.join(root, 'output/playwright/project-actions-map');
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
async function until(fn, message) {
  for (let i = 0; i < 120; i++) { if (await fn()) return; await delay(100); }
  throw new Error(message);
}
async function ready(url, child) {
  await until(async () => {
    if (child.exitCode !== null) throw Error(child.diagnostics);
    return fetch(url).then(r => r.ok).catch(() => false);
  }, 'Server did not start: ' + child.diagnostics);
}
const ok = text => console.log('PASS ' + text);
(async () => {
  const binary = path.join(temp, 'fixture');
  cp.execFileSync('go', ['build', '-o', binary, './scripts/browser-fixture'], { cwd: root, stdio: 'inherit' });
  const apiPort = await freePort(), webPort = await freePort();
  const base = `http://127.0.0.1:${apiPort}`, web = `http://127.0.0.1:${webPort}`;
  const env = { WERKBORD_BROWSER_ADDR: `127.0.0.1:${apiPort}`, WERKBORD_BROWSER_DIR: temp, WERKBORD_BROWSER_TERMINALS: '1', WERKBORD_BROWSER_SHELL_ORIGIN: web };
  const controller = start(binary, [], root, env);
  await ready(base + '/api/health', controller);
  const completed = await fetch(base + '/api/onboarding/complete', { method: 'POST', headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' }, body: '{}' });
  assert.ok(completed.ok);
  const vite = start(process.execPath, [path.join(root, 'web/node_modules/vite/bin/vite.js'), '--host', '127.0.0.1', '--port', String(webPort), '--strictPort'], path.join(root, 'web'), { WERKBORD_URL: base });
  await ready(web, vite);
  browser = await chromium.launch({ headless: true, ...(process.env.BROWSER_EXECUTABLE ? { executablePath: process.env.BROWSER_EXECUTABLE } : {}) });
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  await context.addInitScript(token => { localStorage.setItem('werkbord.token', token); localStorage.setItem('werkbord.theme', 'light'); }, token);
  const page = await context.newPage(); lastPage = page;
  const errors = [];
  page.on('pageerror', e => errors.push(e.message));
  page.setDefaultTimeout(10000);


  const personal = async (method, route, body) => {
    const response = await fetch(base + '/api' + route, {method, headers:{Authorization:`Bearer ${token}`,'Content-Type':'application/json'},body:body===undefined?undefined:JSON.stringify(body)});
    const value = await response.json(); assert(response.ok, JSON.stringify(value)); return value;
  };
  await personal('POST','/onboarding/tours/seen',{surface:'desktop'});
  const repo = path.join(temp, 'repo'); fs.mkdirSync(repo);
  const git = (...args) => cp.execFileSync('git', ['-C',repo,...args], {env:{...process.env,GIT_CONFIG_NOSYSTEM:'1',GIT_CONFIG_GLOBAL:'/dev/null'}});
  git('init','-b','main'); git('config','user.name','Test');git('config','user.email','test@example.invalid');
  fs.writeFileSync(path.join(repo,'main.go'),'package main\nfunc main() {\n println("Hello")\n}\n'.replaceAll('\\n','\n'));
  git('add','.');git('commit','-m','Initial');git('branch','feature/planning');
  const project = await personal('POST','/projects',{name:'Lifecycle and map',path:repo});
  // Use the same framed page as the desktop shell, and reject every native confirm like WKWebView.
  await page.route('**/fixture-window', route => route.fulfill({contentType:'text/html',body:`<html><body style="margin:0"><iframe title="Personal" style="border:0;width:100vw;height:100vh" src="${web}/#/p/${project.id}/defaults"></iframe><script>addEventListener('message',event=>{if(event.data?.type==='werkbord.frame'&&event.data.event==='settings'){document.querySelector('iframe').src='${web}/?hosted=1'+event.data.place;}});</script></body></html>`}));
  await context.addInitScript(() => { window.confirm = () => false; });
  await page.goto(web+'/fixture-window');
  const frame = page.frameLocator('iframe');
  await frame.getByRole('button',{name:'Project history',exact:true}).click();
  for (const action of ['Archive','Delete']) {
    await frame.locator('main').getByRole('button',{name:action+' project',exact:true}).click();
    const dialog = frame.getByRole('dialog',{name:action+' '+project.name+'?',exact:true});
    await dialog.waitFor();
    await dialog.getByTestId('confirm-cancel').click();
    assert((await personal('GET','/projects')).projects.some(p=>p.id===project.id),'cancel preserves active project');
    await frame.locator('main').getByRole('button',{name:action+' project',exact:true}).click();
    await dialog.getByTestId('confirm-ok').click();
    await until(async()=>!(await personal('GET','/projects')).projects.some(p=>p.id===project.id),'project becomes inactive');
    await frame.getByRole('button',{name:'Restore project',exact:true}).click();
    await until(async()=>(await personal('GET','/projects')).projects.some(p=>p.id===project.id),'project restored');
    await page.locator('iframe').evaluate((el,url)=>{el.src=url;},`${web}/#/p/${project.id}/defaults`);
    await frame.getByRole('button',{name:'Project history',exact:true}).click();
  }
  ok('Framed Personal archive/delete shows actionable page confirmation, cancellation and restoration with native confirm disabled');
  await frame.locator('body').evaluate((el,id)=>{location.hash=`#/p/${id}/overview`;},project.id);
  await frame.getByText('Agents on the controller',{exact:true}).waitFor();
  assert.equal(await frame.locator('.agent').count(),2,'only ready fixture agents are shown');
  assert.deepEqual(errors,[]);
})().catch(async e=>{console.error(e); if(lastPage&&!lastPage.isClosed()) { for(const frame of lastPage.frames()) console.error(frame.url(),await frame.locator('body').innerText()); await lastPage.screenshot({path:path.join(artifacts,'failure.png')}); } process.exitCode=1;}).finally(async()=>{await browser?.close();for(const child of children.reverse())await stop(child);fs.rmSync(temp,{recursive:true,force:true});});

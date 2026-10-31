const { openDestination } = require('./test-support/shell-navigation.cjs');
// Regression coverage for workspace density and shell-to-Team navigation, using disposable services only.
const assert = require('node:assert/strict');
const fs = require('node:fs'), path = require('node:path'), http = require('node:http');
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || '../web/node_modules/playwright');
const personal = process.env.PERSONAL_BROWSER_URL, team = process.env.TEAM_BROWSER_URL;
for (const origin of [personal, team]) assert.equal(new URL(origin).hostname, '127.0.0.1');
const credential = fs.readFileSync(process.env.TEAM_BROWSER_TOKEN_FILE, 'utf8').trim();
const artifacts = process.env.BROWSER_ARTIFACT_DIR;
async function api(method, route, body) {
 const response = await fetch(team + '/api/team/v1' + route, { method, headers: { Authorization: 'Bearer ' + credential, 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined });
 if (response.status === 204) return null;
 const value = await response.json(); assert(response.ok, JSON.stringify(value)); return value;
}
let browser, server;
(async () => {
 const project = await api('POST', '/projects', {name: 'Orion Services delivery', repository: 'https://github.com/example/orion'});
 const ticket = await api('POST', `/projects/${project.id}/tickets`, {title: 'Keep ticket names out of the project heading'});
 browser = await chromium.launch({headless: true});
 const context = await browser.newContext({viewport: {width: 1440, height: 900}, colorScheme: 'dark'});
 await context.addInitScript(({credential}) => {
  try {sessionStorage.setItem('werkbord-team-token', credential); localStorage.setItem('werkbord-team.theme', 'light');} catch { /* Initial blank document. */ }
  window.confirm = () => false; // Native dialogs must not be needed for project archive.
 }, {credential});
 // This harness runs the coordination-only server, whose console forbids frames. The production
 // desktop daemon supplies these frame headers; substitute only that transport wrapper for this UI test.
 await context.route(team + '/', async route => {
  const response = await route.fetch(); const headers = {...response.headers()};
  delete headers['x-frame-options'];
  headers['content-security-policy'] = headers['content-security-policy'].replace("frame-ancestors 'none'", 'frame-ancestors ' + process.env.PLANNING_SHELL_URL);
  await route.fulfill({response, headers});
 });
 const page = await context.newPage(); page.setDefaultTimeout(15000); const errors = []; page.on('pageerror', e => errors.push(e.message));
 await page.goto(`${team}/?tab=work&project=${project.id}`);
 await page.locator(`[data-task-id="${ticket.id}"]`).waitFor();
 assert.equal((await page.locator('.project-where').innerText()).trim(), project.repository + '\n' + project.name);
 assert.equal(await page.locator('.label-filter, .lbls').count(), 0);
 await page.getByRole('button', {name: 'New ticket', exact: true}).click();
 const dialog = page.getByRole('dialog'); await dialog.waitFor();
 assert.equal(await dialog.getByText('Labels', {exact: true}).count(), 0);
 assert.equal(await dialog.getByText('Who', {exact: true}).count(), 0);
 await dialog.getByRole('button', {name: 'Cancel', exact: true}).click();
 assert.equal(await page.getByRole('button', {name: 'Product Map', exact: true}).count(), 0, 'Product Map is not offered');
 await page.getByRole('button', {name: 'Project settings: ' + project.name, exact: true}).click();
 await page.locator('.project-settings').waitFor();
 const panels = page.locator('.project-settings > *');
 const first = await panels.nth(0).boundingBox(), second = await panels.nth(1).boundingBox();
 assert(second.x > first.x, 'project settings use adjacent columns');
 await page.getByRole('button', {name: 'Archive project', exact: true}).click();
 await page.getByRole('button', {name: 'Cancel', exact: true}).click();
 assert.equal((await api('GET', '/projects')).find(p => p.id === project.id).archived, false);
 await page.getByRole('button', {name: 'Archive project', exact: true}).click();
 await page.getByRole('button', {name: 'Confirm archive', exact: true}).click();
 await page.getByRole('button', {name: 'Unarchive project', exact: true}).waitFor();
 assert.equal((await api('GET', '/projects')).find(p => p.id === project.id).archived, true);
 await page.getByRole('button', {name: 'Unarchive project', exact: true}).click();
 await page.getByRole('button', {name: 'Archive project', exact: true}).waitFor();
 await page.screenshot({path: path.join(artifacts, 'team-settings-density-desktop.png')});
 await page.setViewportSize({width: 390, height: 844});
 assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 2));
 await page.screenshot({path: path.join(artifacts, 'team-settings-density-mobile.png')});
 console.log('PASS Team identity, labels removed, no Product Map, settings columns and archive/cancel/restore without native dialogs');
 const items = [{id:'personal',kind:'personal',name:'Personal',state:'ready',source:'Fixture'},{id:'team:main',kind:'team',name:'Orion Services',state:'ready',source:'Fixture'}];
 const summary = {workspace:items[1], projects:[{id:project.id,name:project.name,href:`?tab=overview&project=${project.id}`}], counts:{}, attention:[], work:[], planned:[], schedule:[]};
 for (let i=0;i<9;i++) summary.attention.push({id:'review-'+i, kind:i<5?'review':'changes_requested',severity:'info',title:'Review customer portal change '+(i+1),project:project.name,href:`?tab=work&project=${project.id}`});
 for (let i=0;i<4;i++) summary.work.push({id:'mine-'+i,title:'Submitted customer portal change '+(i+1),project:project.name,status:'review',href:`?tab=work&project=${project.id}&ticket=mine-${i}`});
 const shellRoot = path.resolve(__dirname,'../desktop/frontend/dist/shell');
 server = http.createServer((req,res) => {
  const filename = path.resolve(shellRoot, '.' + req.url.split('?')[0]);
  if(!filename.startsWith(shellRoot + path.sep) || !fs.existsSync(filename) || !fs.statSync(filename).isFile()){res.writeHead(404);res.end();return;}
  res.setHeader('Content-Type', filename.endsWith('.js')?'text/javascript':filename.endsWith('.css')?'text/css':filename.endsWith('.html')?'text/html':'application/octet-stream');res.end(fs.readFileSync(filename));
 });
 await new Promise(resolve=>server.listen(Number(new URL(process.env.PLANNING_SHELL_URL).port),'127.0.0.1',resolve));
 await page.setViewportSize({width:1440,height:900});
 await page.addInitScript(({items,summary,personal,team})=>{
  if(window.parent!==window)return;
  window.runtime={EventsOn:()=>()=>{}};
  window.go={main:{App:{Workspaces:async()=>({items,selected:'personal',problems:[],team:{state:'ready'},teamInstaller:false,invitation:false}),OpenWorkspace:async id=>({id,kind:id==='personal'?'personal':'team',url:id==='personal'?personal:team,origin:id==='personal'?personal:team}),Overview:async()=>({entries:[{workspace:items[1],summary}],at:new Date().toISOString()}),RememberPlace:async()=>{}}}};
 },{items,summary,personal,team});
 await page.goto(process.env.PLANNING_SHELL_URL + '/shell/index.html');
 const side=page.getByRole('navigation',{name:'Werkbord',exact:true});
 await openDestination(page, 'page:reviews');
 await page.getByTestId('review-item').first().waitFor();
 assert.equal(await page.locator('.review-group').count(),3);
 const groups=await page.locator('.review-group').evaluateAll(nodes=>nodes.map(el=>({x:el.getBoundingClientRect().x,y:el.getBoundingClientRect().y})));
 assert(groups[1].x>groups[0].x && Math.abs(groups[1].y-groups[0].y)<2,'review groups use adjacent columns');
 assert.equal(await side.locator('.move-project').count(),0,'sidebar has no project moves or map shortcuts');
 await page.screenshot({path:path.join(artifacts,'reviews-density-desktop.png')});
 await page.setViewportSize({width:390,height:844});
 assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+2));
 await page.screenshot({path:path.join(artifacts,'reviews-density-mobile.png')});
 await page.setViewportSize({width:1440,height:900});
 await openDestination(page, 'project:team:main:' + project.id);
 const frame=page.frameLocator('iframe[data-workspace="team:main"]');
 await frame.getByRole('heading',{name:project.name,exact:true,level:1}).waitFor().catch(async error => { console.log('Frame diagnostics:', errors, await page.locator('body').innerText(), await frame.locator('body').innerText().catch(()=>'unavailable')); throw error; });
 await page.waitForFunction(()=>document.querySelector('iframe[data-workspace="team:main"]')?.contentWindow);
 await frame.locator('html[data-theme=dark]').waitFor();
 assert.equal(await frame.getByRole('button',{name:'Product Map',exact:true}).count(), 0, 'Team offers no Product Map tab');
 console.log('PASS grouped Reviews desktop/mobile, simplified sidebar, OS dark overrides stale Team light and Team has no Product Map tab');
 assert.deepEqual(errors,[]);
})().catch(error=>{console.error(error);process.exitCode=1;}).finally(async()=>{if(browser)await browser.close();if(server)await new Promise(resolve=>server.close(resolve));});

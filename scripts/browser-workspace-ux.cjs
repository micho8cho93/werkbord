// Workspace UX regressions against disposable real APIs; never accepts production origins.
const assert = require('node:assert/strict');
const fs = require('node:fs'), path = require('node:path'), http = require('node:http');
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || '../web/node_modules/playwright');
const individual = process.env.PERSONAL_BROWSER_URL, team = process.env.TEAM_BROWSER_URL;
const token = 'disposable-browser-credential';
const teamToken = fs.readFileSync(process.env.TEAM_BROWSER_TOKEN_FILE, 'utf8').trim();
const artifacts = process.env.BROWSER_ARTIFACT_DIR;
for (const url of [individual, team]) assert.equal(new URL(url).hostname, '127.0.0.1');
const call = async (base, credential, method, route, body) => {
 const res = await fetch(base + route, { method, headers: { Authorization: 'Bearer ' + credential, 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined });
 if (res.status === 204) return null;
 const value = await res.json(); assert(res.ok, JSON.stringify(value)); return value;
};
const personal = (method, route, body) => call(individual, token, method, '/api' + route, body);
const coord = (method, route, body) => call(team, teamToken, method, '/api/team/v1' + route, body);
const wait = async (fn, label) => { for (let i=0; i<100; i++) { if (await fn()) return; await new Promise(r=>setTimeout(r,100)); } throw Error('Timed out: ' + label); };
let browser, shellServer;
(async () => {
 await personal('POST', '/onboarding/complete', {});
 for (const surface of ['desktop','settings','analytics']) await personal('POST', '/onboarding/tours/seen', {surface});
 browser = await chromium.launch({ headless: true });
 const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
 page.setDefaultTimeout(15000);
 const errors = []; page.on('pageerror', e => errors.push(e.message));
 await page.addInitScript(() => localStorage.setItem('devboard.token','disposable-browser-credential'));
 const today = new Date().toISOString().slice(0,10);
 const project = await personal('POST', '/projects', { name: 'Northern European Venue Operations and Logistics', path: process.env.WERKBORD_BROWSER_REPO });
 const work = await personal('POST','/projects', { name: 'Launch planning', kind: 'work' });
 const task = await personal('POST',`/projects/${project.id}/tasks`, { title: 'Investigate an unusually long migration task name across all customer account workflows', workMode: 'human', plan: { start: today, end: today }, description: 'Coordinate the migration and preserve existing customer data.' });
 const planned = await personal('POST', `/projects/${work.id}/tasks`, { title: 'Plan the venue opening', plan: {start: today, end: today} });
 await page.goto(`${individual}/#/p/${project.id}/board`);
 await page.getByRole('heading', { name: project.name }).waitFor();
 const tabs = page.getByRole('navigation', { name: project.name + ' sections' });
 assert.deepEqual((await tabs.locator('a').allTextContents()).map(t=>t.trim()), ['Overview','Board','Calendar','Git','Runs']);
 assert.equal(await page.getByText('Who', { exact: true }).count(),0);
 assert.equal(await page.getByText('Human', { exact: true }).count(),0);
 const title = page.locator('[data-board-item="'+task.id+'"] .title');
 await title.dragTo(page.locator('[data-board-column="doing"]'));
 await wait(async()=> (await personal('GET',`/projects/${project.id}/tasks`)).tasks.find(t=>t.id===task.id).state === 'doing','native Individual drag');
 await wait(()=>page.locator('[data-board-item="'+task.id+'"] [data-drag-handle]').isEnabled(),'move acknowledgment');
 await page.locator('[data-board-item="'+task.id+'"] [data-drag-handle]').focus();
 await page.keyboard.press('ArrowRight');
 await wait(async()=> (await personal('GET',`/projects/${project.id}/tasks`)).tasks.find(t=>t.id===task.id).state === 'review','keyboard Individual move');
 console.log('PASS Individual native drag from full-card title and keyboard workflow move');
 const capture = async (name) => {
  await wait(async()=>await page.locator('.work-card[aria-busy=true]').count()===0,'settled card');
  await wait(async()=>await page.locator('.toast').count()===0,'transient feedback cleared');
  if (name === 'individual-detail') await wait(async()=>await page.getByText('Checking available review evidence…',{exact:true}).count()===0,'resolved review evidence');
  for (const [width,height,suffix] of [[1440,1000,'desktop'],[390,844,'mobile']]) {
   await page.setViewportSize({width,height}); await page.evaluate(()=>document.fonts.ready);
   await page.waitForTimeout(350);
   if (name.endsWith('board')) await page.locator('.work-card').first().scrollIntoViewIfNeeded();
   assert(await page.evaluate(()=>document.documentElement.scrollWidth <= innerWidth+2),name+' overflow');
   await page.screenshot({path:path.join(artifacts,name+'-'+suffix+'.png')});
  }
  await page.setViewportSize({width:1440,height:1000});
 };
 await capture('individual-board');
 await page.getByRole('button',{name:'New task',exact:true}).click();
 const dialog = page.getByRole('dialog');
 await dialog.getByText('Planning',{exact:false}).first().waitFor();
 assert(await dialog.locator('input[type=date]').first().isVisible(),'planning is directly visible');
 assert.equal(await dialog.getByText('Labels',{exact:true}).count(),0);
 await page.keyboard.press('Escape');
 await page.goto(`${individual}/#/p/${project.id}/calendar`);
 await page.getByRole('button',{name:'Week',exact:true}).waitFor();
 assert.equal(await page.getByRole('button',{name:'Week',exact:true}).getAttribute('aria-pressed'),'true');
 await page.getByRole('link',{name:'Timeline',exact:true}).click();
 await page.locator('.nrow .nt').waitFor();
 assert.equal(await page.locator('.nrow .when').count(),0);
 await page.locator('[data-tooltip]').first().hover();
 await page.getByRole('tooltip').waitFor();
 await capture('individual-timeline');
 await page.goto(`${individual}/#/analytics`);
 await page.getByRole('heading',{name:'Created and completed over time',exact:true}).waitFor();
 assert.equal(await page.getByLabel('Analytics project').inputValue(),'');
 assert.equal(await page.getByRole('button',{name:/Apply|refresh/i}).count(),0);
 await page.getByLabel('Analytics project').selectOption(work.id);
 await wait(async()=> (await page.locator('.statistics dd').first().innerText())==='1','automatic filter reload');
 await personal('POST',`/projects/${work.id}/tasks`,{title:'Live analytics update'});
 await wait(async()=> (await page.locator('.statistics dd').first().innerText())==='2','analytics follows live task event');
 await capture('individual-analytics');
 console.log('PASS analytics defaults to all projects, filters automatically and follows live task creation');
 // Exercise real assessment save, reload, and preserved task column.
 const agentTask = await personal('POST',`/projects/${project.id}/tasks`,{title:'Review finished fixture work'});
 const run = await personal('POST',`/projects/${project.id}/tasks/${agentTask.id}/runs`,{agentId:'fixture'});
 await wait(async()=> (await personal('GET',`/projects/${project.id}/runs/${run.id}`)).state==='waiting_for_user','agent ready');
 await personal('POST',`/projects/${project.id}/runs/${run.id}/finish`,{});
 await wait(async()=> !!(await personal('GET',`/projects/${project.id}/runs/${run.id}`)).endedAt,'agent finished');
 let fresh = (await personal('GET',`/projects/${project.id}/tasks`)).tasks.find(t=>t.id===agentTask.id);
 await personal('PATCH',`/projects/${project.id}/tasks/${agentTask.id}`,{version:fresh.version,state:'review'});
 await page.goto(`${individual}/#/p/${project.id}/task/${agentTask.id}`);
 await page.getByRole('button',{name:'Accept work',exact:true}).click();
 await page.getByText('Accepted',{exact:true}).waitFor();
 await page.reload(); await page.getByText('Accepted',{exact:true}).waitFor();
 assert.equal((await personal('GET',`/projects/${project.id}/runs/${run.id}`)).usage.acceptance,'accepted');
 await capture('individual-detail');
 console.log('PASS Accept work persists, survives parent/live updates and reload');
 // Permission denied remains an off preference; permission changes recover without reloading.
 await page.addInitScript(() => { window.__notificationPermission='denied'; class Alerts { static get permission(){return window.__notificationPermission;} static async requestPermission(){return window.__notificationPermission;} } window.Notification=Alerts; });
 await page.goto(`${individual}/#/control`);
 await page.reload();
 await page.getByRole('button',{name:'Alerts off',exact:true}).click();
 await page.getByText(/Allow alerts in your browser/).first().waitFor();
 await page.evaluate(()=> { window.__notificationPermission='granted'; window.dispatchEvent(new Event('focus')); });
 await page.getByRole('button',{name:'Alerts off',exact:true}).click();
 await page.getByRole('button',{name:'Alerts on',exact:true}).waitFor();
 await page.getByRole('button',{name:'Alerts on',exact:true}).click();
 await page.getByRole('button',{name:'Alerts off',exact:true}).waitFor();
 console.log('PASS alerts denied explanation and recovery after permission changes');
 // Team remains a coordinator. Exercise board transitions and actual member-report surfaces.
 const tp = await coord('POST','/projects',{name:'Workspace coordination'});
 const tk = await coord('POST',`/projects/${tp.id}/tickets`,{title:'Coordinate the migration with an unusually long project task title',description:'Keep members informed about the migration progress.',status:'available',plan:{start:today,end:today}});
 await page.addInitScript(value=>sessionStorage.setItem('werkbord-team-token',value),teamToken);
 await page.goto(`${team}/?tab=board&project=${tp.id}`);
 await page.locator('[data-board-item="'+tk.id+'"]').waitFor();
 const teamTabs = page.getByRole('navigation',{name:'Project sections'});
 assert.deepEqual((await teamTabs.locator('button').allTextContents()).map(t=>t.replace(/\d+/g,'').trim()),['Overview','Board','Calendar','Git','Runs']);
 await page.locator('[data-board-item="'+tk.id+'"] .title').dragTo(page.locator('[data-board-column=in_progress]'));
 await wait(async()=> (await coord('GET',`/projects/${tp.id}/tickets/${tk.id}`)).status==='in_progress','Team native claim drag');
 await page.locator('[data-board-item="'+tk.id+'"][data-work-state=in_progress]').waitFor();
 await page.locator('[data-board-item="'+tk.id+'"] [data-drag-handle]').focus(); await page.keyboard.press('ArrowRight');
 await wait(async()=> (await coord('GET',`/projects/${tp.id}/tickets/${tk.id}`)).status==='review','Team keyboard submit');
 await capture('team-board');
 assert.equal(await page.locator('.project-meta').count(), 0, 'a project without a repository has no identity subtitle');
 for (const [tab, name] of [['projects','team-projects'],['workspace','team-workspace'],['reviews','team-reviews']]) {
  await page.goto(`${team}/?tab=${tab}`);
  await page.locator(tab === 'projects' ? '.project-cards' : tab === 'workspace' ? '.cc-split' : '.reviews-grid').waitFor();
  await capture(name);
 }
 await page.goto(`${team}/?tab=board&project=${tp.id}`);
 await page.locator('[data-board-item="'+tk.id+'"]').waitFor();
 await teamTabs.getByRole('button',{name:'Calendar',exact:true}).click();
 await page.getByText(/Planned work/).first().waitFor();
 await page.getByRole('group',{name:'Calendar presentation'}).getByRole('button',{name:'Timeline',exact:true}).click();
 await page.locator('.tl-name .nt').waitFor(); assert.equal(await page.locator('.tl-name .when').count(),0);
 await capture('team-timeline');
 await teamTabs.getByRole('button',{name:'Runs',exact:true}).click();
 await page.getByText(/Execution reported by members/).waitFor();
 await teamTabs.getByRole('button',{name:'Git',exact:true}).click();
 console.log('PASS Team native claim drag, keyboard submit, integrated Calendar/Timeline and coordinated Runs');
 // Archiving a Done card preserves the record and leaves other completed work on the board.
 let doneA=await personal('POST',`/projects/${work.id}/tasks`,{title:'Archive this completed task',description:'Keep this completion evidence'});
 let doneB=await personal('POST',`/projects/${work.id}/tasks`,{title:'Keep this completed task'});
 for(const task of [doneA,doneB]) await personal('PATCH',`/projects/${work.id}/tasks/${task.id}`,{version:task.version,state:'done'});
 await page.goto(`${individual}/#/p/${work.id}/board`);
 await page.locator(`[data-board-item="${doneA.id}"]`).getByRole('button',{name:'Archive',exact:true}).click();
 await wait(async()=>!!(await personal('GET',`/projects/${work.id}/tasks`)).tasks.find(task=>task.id===doneA.id).archivedAt,'Individual card archive');
 await page.locator(`[data-board-item="${doneB.id}"]`).waitFor();
 assert.equal(await page.getByRole('button',{name:'Archive Done tasks',exact:true}).count(),0);
 assert.equal((await personal('GET',`/projects/${work.id}/tasks`)).tasks.find(task=>task.id===doneA.id).description,'Keep this completion evidence');
 await coord('POST',`/projects/${tp.id}/tickets/${tk.id}/complete`,{});
 await page.goto(`${team}/?tab=board&project=${tp.id}`);
 await page.locator(`[data-board-item="${tk.id}"]`).getByRole('button',{name:'Archive',exact:true}).click();
 await wait(async()=>!!(await coord('GET',`/projects/${tp.id}/tickets/${tk.id}`)).archivedAt,'Team card archive');
 assert.equal(await page.getByRole('button',{name:'Archive Done tickets',exact:true}).count(),0);
 const linked=await coord('POST','/projects',{name:'Linked repository project',repository:'https://github.com/example/workspace-layout'});
 await coord('POST',`/projects/${linked.id}/tickets`,{title:'Keep ticket branches out of the project header'});
 await page.goto(`${team}/?tab=board&project=${linked.id}`);
 await page.locator('.project-meta').waitFor();
 assert.equal((await page.locator('.project-where').innerText()).trim(),'https://github.com/example/workspace-layout\nLinked repository project');
 console.log('PASS individual Done-card archive preserves details and other Done cards; Team archive and repository-only project identity');

 // Real Individual summary feeds aggregate planning; shell alone has only neutral workspace DTOs.
 const summary = await personal('GET','/workspace/v1/summary');
 assert(summary.planned.some(p=>p.id===planned.id),'summary carries planned dates for unexecuted tasks');
 const shellRoot = path.resolve(__dirname,'../desktop/frontend/dist/shell');
 shellServer = http.createServer((req,res)=> {
  if (req.method === 'POST' && req.url === '/fixture/personal') {
   let raw=''; req.on('data', part=>raw+=part); req.on('end', async()=> {
    try { const {method,route,body}=JSON.parse(raw); const response=await fetch(individual+route,{method,headers:{Authorization:'Bearer '+token,'Content-Type':'application/json'},body:body===null?undefined:JSON.stringify(body)}); const value=await response.text(); res.setHeader('Content-Type','application/json'); res.end(JSON.stringify({status:response.status,body:value?JSON.parse(value):null})); }
    catch(error) { res.statusCode=500;res.end(JSON.stringify({error:error.message})); }
   }); return;
  }
  const file=path.join(shellRoot,decodeURIComponent(req.url.split('?')[0])); if (!file.startsWith(shellRoot) || !fs.existsSync(file) || !fs.statSync(file).isFile()) {res.writeHead(404);res.end();return;} res.setHeader('Content-Type', file.endsWith('.js')?'text/javascript':file.endsWith('.css')?'text/css':file.endsWith('.html')?'text/html':'application/octet-stream'); res.end(fs.readFileSync(file)); });
 await new Promise(resolve=>shellServer.listen(Number(new URL(process.env.PLANNING_SHELL_URL).port),'127.0.0.1',resolve));
 const shellPage = await browser.newPage({viewport:{width:1440,height:1000}}); shellPage.on('pageerror',e=>errors.push(e.message));
 const items = [{id:'personal',kind:'personal',name:'Individual',state:'ready',source:'Werkbord'},{id:'team:main',kind:'team',name:'Workspace coordination',state:'ready',source:'Fixture'}];
 await shellPage.addInitScript(({items,summary,individual,team,teamToken})=> {
  try { localStorage.setItem('devboard.token','disposable-browser-credential'); sessionStorage.setItem('werkbord-team-token',teamToken); } catch (_) { /* Initial about:blank frames have no storage. */ }
  if(window.parent!==window)return;
  window.runtime={EventsOn:()=>()=>{}};
  window.go={main:{App:{Workspaces:async()=>({items,selected:'personal',problems:[],team:{state:'ready'},teamInstaller:false,invitation:false}), OpenWorkspace:async id=>({id,kind:id==='personal'?'personal':'team',url:id==='personal'?individual:team,origin:id==='personal'?individual:team}), Overview:async()=>({entries:[{workspace:items[0],summary},{workspace:items[1],summary:{...summary,workspace:items[1],work:[],planned:[],schedule:[],attention:[],projects:[]}}],at:new Date().toISOString()}), RememberPlace:async()=>{}, PersonalWorkspaceCall:async(method,route,body)=>(await fetch('/fixture/personal',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({method,route,body})})).json()}}};
 },{items,summary,individual,team,teamToken});
 await shellPage.goto(`http://127.0.0.1:${shellServer.address().port}/shell/index.html`);
 const side=shellPage.getByRole('navigation',{name:'Werkbord',exact:true});
 await shellPage.locator('nav[aria-label="Werkbord"] [data-open="personal"]').click();
 await side.locator('[data-section=personal]').getByRole('button',{name:'Analytics',exact:true}).waitFor();
 await shellPage.getByTestId('sidebar-salie').waitFor();
 assert.equal(await shellPage.getByTestId('salie-button').count(),0,'Personal defaults to sidebar Salie');
 assert.equal(await side.getByRole('button',{name:'My Work',exact:true}).count(),0);
 await shellPage.locator('nav[aria-label="Werkbord"] [data-page=inbox]').click();
 await shellPage.getByRole('heading',{name:'Home',exact:true}).waitFor();
 await shellPage.getByTestId('salie-button').waitFor();
 assert.equal(await shellPage.getByTestId('sidebar-salie').count(),0,'Home defaults to floating Salie');
 const bottom=side.locator('.bottom [data-page]');
 assert.deepEqual(await bottom.evaluateAll(nodes=>nodes.map(el=>el.dataset.page)),['add-team','workspaces','personal-library','settings']);
 await shellPage.getByTestId('hub-workspace-personal').hover();
 const hoverFits=await shellPage.getByTestId('hub-workspace-personal').evaluate(el=>{const card=el.getBoundingClientRect(),shelf=el.closest('.shelf').getBoundingClientRect();return card.top-3>=shelf.top && card.bottom+3<=shelf.bottom;});
 assert(hoverFits,'hover lift and outline fit inside the shelf');
 assert.equal(await side.getByRole('button',{name:'My Work',exact:true}).count(),0);
 assert.equal(await side.getByRole('button',{name:'Projects',exact:true}).count(),0);
 assert.equal(await side.getByRole('button',{name:'Calendar',exact:true}).count(),1,'Calendar is in the one sidebar');
 try { await shellPage.getByRole('button').filter({hasText:'Plan the venue opening'}).waitFor(); } catch (error) { await shellPage.screenshot({path:path.join(artifacts,'universal-failure.png')}); console.log('Shell:', (await shellPage.locator('body').innerText()).slice(0,3000), 'errors:', errors); throw error; }
 await shellPage.getByRole('group',{name:'Calendar presentation'}).getByRole('button',{name:'Timeline',exact:true}).click();
 await shellPage.getByRole('region',{name:'Planning timeline'}).waitFor();
 await shellPage.waitForTimeout(350);
 await shellPage.screenshot({path:path.join(artifacts,'universal-timeline-desktop.png')});
 await shellPage.setViewportSize({width:900,height:800}); await shellPage.screenshot({path:path.join(artifacts,'universal-timeline-window.png')});
 await shellPage.setViewportSize({width:1440,height:1000});
 await shellPage.keyboard.press('Meta+k'); await shellPage.getByTestId('palette').waitFor();
 await shellPage.screenshot({path:path.join(artifacts,'universal-palette-desktop.png')});
 await shellPage.keyboard.press('Escape');
 for (const destination of ['Workspaces','Calendar']) {
  await shellPage.getByRole('group',{name:'Calendar presentation'}).getByRole('button',{name:destination === 'Calendar' ? 'Calendar' : 'Timeline',exact:true}).click();
  await shellPage.waitForTimeout(350);
  await shellPage.screenshot({path:path.join(artifacts,'universal-'+destination.toLowerCase().replaceAll(' ','-')+'-desktop.png')});
 }

 // Library and placement controls use the built shell and real personal storage.
 for(let i=0;i<6;i++) {
  const collection=await personal('POST','/personal/v1/collections',{name:'Library collection '+i,schemaVersion:1,fields:[{key:'title',name:'Title',type:'text',required:true}]});
  await personal('POST','/personal/v1/workspaces',{name:'Saved workspace '+i,icon:'auto',uiSchemaVersion:1,collectionIds:[collection.id],readBindings:[],visibility:{universal:true,individual:true,teamWorkspaceIds:[]},definition:{views:[{id:'records',name:'Records',kind:'table',collectionId:collection.id,fields:['title'],operations:['create','update','delete']}],layout:{type:'view',viewId:'records'}}});
 }
 await side.getByRole('button',{name:'Data library',exact:true}).click();
 const saved=shellPage.getByRole('region',{name:'Saved workspaces',exact:true}), collections=shellPage.getByRole('region',{name:'Personal collections',exact:true});
 await saved.locator('.card-row').first().waitFor();
 assert.equal(await saved.locator('.card-row').count(),4); assert.equal(await collections.locator('.card-row').count(),4);
 const [savedBox,collectionBox]=await Promise.all([saved.boundingBox(),collections.boundingBox()]);
 assert(Math.abs(savedBox.y-collectionBox.y)<2 && collectionBox.x>savedBox.x,'library lists sit side by side');
 await collections.getByRole('button',{name:'Show all 6 collections'}).click(); assert.equal(await collections.locator('.card-row').count(),6);
 await collections.getByRole('button',{name:'Show fewer collections'}).click(); assert.equal(await collections.locator('.card-row').count(),4);
 for(const [width,height,suffix] of [[1440,1000,'desktop'],[390,844,'narrow']]) {
  await shellPage.setViewportSize({width,height}); await shellPage.screenshot({path:path.join(artifacts,'data-library-'+suffix+'.png'),fullPage:true});
  assert(await shellPage.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+2),'library fits window');
 }
 await shellPage.setViewportSize({width:1440,height:1000});
 await side.getByRole('button',{name:'Settings',exact:true}).click();
 const universalPlacement=shellPage.getByRole('group',{name:'Salie placement in Home and the window’s own pages',exact:true});
 const workspacePlacement=shellPage.getByRole('group',{name:'Salie placement in Personal and team projects',exact:true});
 await universalPlacement.getByRole('button',{name:'Both',exact:true}).click();
 await shellPage.getByTestId('sidebar-salie').waitFor(); await shellPage.getByTestId('salie-button').waitFor();
 await workspacePlacement.getByRole('button',{name:'Floating',exact:true}).click();
 await shellPage.reload();
 await shellPage.getByTestId('sidebar-salie').waitFor(); await shellPage.getByTestId('salie-button').waitFor();
 await shellPage.locator('nav[aria-label="Werkbord"] [data-open="personal"]').click();
 await shellPage.getByTestId('salie-button').waitFor(); assert.equal(await shellPage.getByTestId('sidebar-salie').count(),0);
 console.log('PASS library previews/expansion, Universal sidebar order, unclipped hover and persisted per-surface Salie placements');

 // New-user setup offers the same preference without prompting existing users.
 await personal('POST','/onboarding/reset',{});
 await personal('POST','/onboarding/variant',{variant:'local'});
 for(const step of ['github','ticket','assistant','publish','phone']) await personal('POST','/onboarding/step',{step,state:'skipped'});
 await shellPage.reload();
 await shellPage.locator('nav[aria-label="Werkbord"] [data-open="personal"]').click();
 const onboarding=shellPage.frameLocator('iframe[data-workspace=personal]');
 await onboarding.getByRole('heading',{name:'Welcome to Werkbord',exact:true}).waitFor().catch(async error=>{await shellPage.screenshot({path:path.join(artifacts,'onboarding-failure.png')}); console.log('Onboarding frame:',(await onboarding.locator('body').innerText()).slice(0,4000));throw error;});
 if(await onboarding.getByRole('button',{name:'Start guided setup',exact:true}).isVisible()) await onboarding.getByRole('button',{name:'Start guided setup',exact:true}).click();
 const onboardingPlacement=onboarding.getByRole('group',{name:'Salie placement in Personal and team projects',exact:true});
 await onboardingPlacement.getByRole('button',{name:'Sidebar',exact:true}).click();
 await shellPage.getByTestId('sidebar-salie').waitFor();
 assert.equal(await shellPage.getByTestId('salie-button').count(),0,'onboarding changes the shell placement immediately');
 assert.equal(await onboardingPlacement.getByRole('button',{name:'Sidebar',exact:true}).getAttribute('aria-pressed'),'true');
 await shellPage.screenshot({path:path.join(artifacts,'salie-onboarding-desktop.png')});
 console.log('PASS onboarding reads and updates the same saved Salie placement as Settings');

 assert.deepEqual(errors,[]);
 console.log('PASS Universal peer selector, scoped sidebar and aggregated planning timeline');
})().catch(e=>{console.error(e);process.exitCode=1;}).finally(async()=>{if(browser)await browser.close();if(shellServer)await new Promise(r=>shellServer.close(r));});

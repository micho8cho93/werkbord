// Real API regressions for board moves, date gestures, contextual editors, and the Universal frame bridge.
const assert = require('node:assert/strict');
const fs = require('node:fs'), path = require('node:path'), http = require('node:http');
const { chromium, webkit } = require('../web/node_modules/playwright');
const personalURL = process.env.PERSONAL_BROWSER_URL, teamURL = process.env.TEAM_BROWSER_URL;
const token = 'disposable-browser-credential', teamToken = fs.readFileSync(process.env.TEAM_BROWSER_TOKEN_FILE, 'utf8').trim();
const artifacts = process.env.BROWSER_ARTIFACT_DIR;
for (const url of [personalURL, teamURL]) assert.equal(new URL(url).hostname, '127.0.0.1');
const call = async (base, credential, method, route, body) => {
  const res = await fetch(base + route, { method, headers: { Authorization: 'Bearer ' + credential, 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined });
  if (res.status === 204) return null;
  const out = await res.json(); assert(res.ok, JSON.stringify(out)); return out;
};
const personal = (method, route, body) => call(personalURL, token, method, '/api' + route, body);
const team = (method, route, body) => call(teamURL, teamToken, method, '/api/team/v1' + route, body);
const add = (date, n) => new Date(Date.parse(date + 'T12:00Z') + n * 86400000).toISOString().slice(0,10);
const today = new Date().toISOString().slice(0,10);
const monday = add(today, -((new Date(today + 'T12:00Z').getUTCDay()+6)%7));
const wait = async (check, label) => { for (let n=0;n<100;n++) { if(await check())return; await new Promise(r=>setTimeout(r,100)); } throw Error('Timed out: '+label); };
async function drag(page, from, to) { const a=await from.boundingBox(), b=await to.boundingBox(); assert(a && b,'drag targets'); await page.mouse.move(a.x+a.width/2,a.y+Math.min(a.height/2,20)); await page.mouse.down(); await page.mouse.move(b.x+b.width/2,b.y+Math.min(b.height/2,60),{steps:12}); await page.mouse.up(); }
let browser, server, debugPage;
(async()=>{
  // First-visit tours are covered elsewhere and must not overlay the controls under test.
  for (const surface of ['desktop', 'settings', 'analytics']) await personal('POST','/onboarding/tours/seen',{surface});
  const work = await personal('POST','/projects',{name:'Release planning',kind:'work'});
  let task = await personal('POST',`/projects/${work.id}/tasks`,{title:'Plan the launch',description:'Planning context',plan:{start:monday,end:add(monday,2)}});
  const tp = await team('POST','/projects',{name:'Analytics Platform',repository:'https://github.com/acme/analytics'});
  let ticket = await team('POST',`/projects/${tp.id}/tickets`,{title:'Coordinate launch',description:'Team planning context',status:'available',plan:{start:monday,end:add(monday,2)}});
  await personal('POST', '/onboarding/complete', {});
  const getTask=async()=> (await personal('GET',`/projects/${work.id}/tasks`)).tasks.find(t=>t.id===task.id);
  const getTicket=()=>team('GET',`/projects/${tp.id}/tickets/${ticket.id}`);
  for (const engine of (process.env.BROWSER_PLANNING_ENGINES || 'chromium,webkit').split(',')) {
    browser = await (engine==='webkit'?webkit:chromium).launch({headless:true});
    const page=await browser.newPage({viewport:{width:1440,height:1000}}); page.setDefaultTimeout(10000);
    debugPage=page; const errors=[]; page.on('pageerror',e=>errors.push(e.message));
    await page.addInitScript(({token,teamToken})=>{localStorage.setItem('werkbord.token',token);sessionStorage.setItem('werkbord-team-token',teamToken);},{token,teamToken});
    task=await getTask(); if(task.state!=='backlog') task=await personal('PATCH',`/projects/${work.id}/tasks/${task.id}`,{version:task.version,state:'backlog'});
    await page.goto(`${personalURL}/#/p/${work.id}/work`);
    const card=page.locator(`[data-task-id="${task.id}"]`); await card.waitFor();
    await page.goto(`${personalURL}/#/p/${work.id}/calendar`);
    const event=page.locator(`[data-plan-item="${task.id}"]`).first(); await event.waitFor();
    const original=await getTask(), target=add(original.plan.start,1);
    if (await page.locator(`[data-plan-day="${target}"]`).count()) { await drag(page,event,page.locator(`[data-plan-day="${target}"]`)); await wait(async()=> (await getTask()).plan.start===target,'Individual work calendar drag'); }
    await page.goto(`${teamURL}/?tab=work&project=${tp.id}`);
    const teamCard=page.locator(`[data-task-id="${ticket.id}"]`); await teamCard.waitFor(); ticket=await getTicket();
    const tabs=page.getByRole('navigation',{name:'Project sections'});
    assert.deepEqual((await tabs.locator('button').allTextContents()).map(t=>t.replace(/\d+/g,'').trim()),['Overview','Work','Calendar','Git','Runs','Product Map']);
    await page.getByRole('button',{name:/^Project settings/}).waitFor();
    assert.equal(await page.locator('.project-details, .project-people').count(),0,'the project header has no people list or details dropdown');
    await page.getByRole('button',{name:/^Project settings/}).click();
    await page.getByRole('heading',{name:/^People on this project/}).waitFor();
    assert.equal(new URL(page.url()).searchParams.get('tab'),'project-settings','project membership stays accessible in Settings');
    await tabs.getByRole('button',{name:'Calendar',exact:true}).click();
    const te=page.locator(`[data-plan-item="${ticket.id}"]`).first(); await te.waitFor(); ticket=await getTicket();
    await drag(page,te,page.locator(`[data-plan-day="${add(ticket.plan.start,1)}"]`));
    await wait(async()=> (await getTicket()).plan.start===add(ticket.plan.start,1),'Team calendar move');
    const tb=page.locator(`[data-plan-item="${ticket.id}"]`).first();
    await tb.click(); await page.locator(`form[data-ticket-id="${ticket.id}"]`).waitFor();
    assert(new URL(page.url()).searchParams.get('tab')==='calendar','Team stays on calendar');
    await page.locator(`[name="e-title-${ticket.id}"]`).fill('Coordinate launch '+engine);
    await page.getByRole('button',{name:'Save changes',exact:true}).click();
    await wait(async()=> (await getTicket()).title==='Coordinate launch '+engine,'Team contextual edit saves');
    await page.getByRole('button',{name:'Close',exact:true}).click(); await page.locator('.calendar-with-ticket .ticket').waitFor({state:'detached'});
    assert.deepEqual(errors,[],engine+' page errors');
    if(engine==='chromium') {
      await page.screenshot({path:path.join(artifacts,'team-planning-desktop.png')});
      await page.setViewportSize({width:390,height:844}); await page.screenshot({path:path.join(artifacts,'team-planning-mobile.png')});
      await page.setViewportSize({width:1440,height:1000});
      await page.goto(`${personalURL}/#/p/${work.id}/calendar`);await page.locator('.event').first().waitFor(); await page.screenshot({path:path.join(artifacts,'individual-planning-desktop.png')});
    }
    console.log('PASS '+engine+' calendar dates and edits in place');
    await browser.close(); browser=null;
  }
  // An actual agent start and an unavailable-agent refusal use the normal controller workflow.
  browser=await chromium.launch({headless:true});
  const interaction=await browser.newPage({viewport:{width:1440,height:1000}});
  interaction.setDefaultTimeout(10000); debugPage=interaction;
  await interaction.addInitScript(({token,teamToken})=>{localStorage.setItem('werkbord.token',token);sessionStorage.setItem('werkbord-team-token',teamToken);},{token,teamToken});
  const repository=await personal('POST','/projects',{name:'Agent gestures',path:process.env.WERKBORD_BROWSER_REPO});
  const refused=await personal('POST',`/projects/${repository.id}/tasks`,{title:'Unavailable agent drop',execution:{agent:'codex'}});
  const runnable=await personal('POST',`/projects/${repository.id}/tasks`,{title:'Start from a board drop',execution:{agent:'fixture'}});
  await interaction.goto(`${personalURL}/#/p/${repository.id}/work`);
  await interaction.locator(`[data-task-id="${refused.id}"]`).waitFor();
  await interaction.locator(`[data-task-id="${refused.id}"]`).getByRole('button',{name:/Start an agent on/}).click();
  await interaction.getByRole('status').filter({hasText:/agent|Codex|available|installed/i}).first().waitFor();
  assert.equal((await personal('GET',`/projects/${repository.id}/tasks`)).tasks.find(t=>t.id===refused.id).state,'backlog','configuration refusal preserves Backlog');
  await interaction.locator(`[data-task-id="${runnable.id}"]`).getByRole('button',{name:/Start an agent on/}).click();
  await wait(async()=> (await personal('GET',`/projects/${repository.id}/tasks`)).tasks.find(t=>t.id===runnable.id).state==='doing','board drag starts configured agent');
  const execution=(await personal('GET',`/projects/${repository.id}/runs`)).runs.find(r=>r.taskId===runnable.id);assert(execution,'a real run exists');
  await personal('POST',`/projects/${repository.id}/runs/${execution.id}/stop`,{});
  const scheduledTask=await personal('POST',`/projects/${repository.id}/tasks`,{title:'Scheduled drag',execution:{agent:'fixture'},plan:{start:monday,end:add(monday,2)},orchestration:{enabled:true,scheduledAt:add(today,1)+'T10:00:00Z',timezone:'UTC',dependencies:[],expectedPaths:[]}});
  await interaction.goto(`${personalURL}/#/p/${repository.id}/calendar`);
  if ((new Date(today+'T12:00Z').getUTCDay()+6)%7===6) await interaction.getByRole('button',{name:'Next week',exact:true}).click();
  const scheduledEvent=interaction.locator(`[data-plan-item="${scheduledTask.id}/schedule"]`);await scheduledEvent.waitFor();
  const eventBounds=await scheduledEvent.boundingBox();await interaction.mouse.move(eventBounds.x+eventBounds.width/2,eventBounds.y+eventBounds.height/2);await interaction.mouse.down();await interaction.mouse.move(eventBounds.x+eventBounds.width/2,eventBounds.y+eventBounds.height/2+48,{steps:10});await interaction.mouse.up();
  const readScheduled=async()=> (await personal('GET',`/projects/${repository.id}/tasks`)).tasks.find(t=>t.id===scheduledTask.id);
  try { await wait(async()=> Date.parse((await readScheduled()).orchestration.scheduledAt)===Date.parse(scheduledTask.orchestration.scheduledAt)+3600000,'Individual hour drag reschedules agent'); } catch(error) { console.error('Scheduled before/after',scheduledTask.orchestration,(await readScheduled()).orchestration); throw error; }
  assert.deepEqual((await readScheduled()).plan,scheduledTask.plan,'rescheduling preserves planned dates');
  await team('POST',`/projects/${tp.id}/tickets/${ticket.id}/claim`,{});
  await scheduledEvent.click();
  await interaction.getByRole('dialog',{name:'Edit task'}).getByRole('button',{name:'Schedule and dependencies',exact:true}).click();
  const schedulePanel=interaction.getByRole('dialog',{name:'Schedule and dependencies',exact:true});
  await schedulePanel.getByRole('button',{name:'Save schedule and dependencies',exact:true}).waitFor();
  await schedulePanel.getByText('Run order affects priority; dependencies make a task wait.').waitFor();
  await schedulePanel.getByText('Recovery preferences', {exact:true}).click();
  await schedulePanel.getByLabel('When the runner reconnects').selectOption('manual');
  await schedulePanel.getByLabel('Provider usage limits').selectOption('retry_temporary');
  await schedulePanel.getByLabel('Retry limit (0–5)').fill('2');
  await schedulePanel.screenshot({path:path.join(artifacts,'execution-recovery-desktop.png')});
  await interaction.setViewportSize({width:390,height:844});
  await schedulePanel.getByLabel('Retry limit (0–5)').scrollIntoViewIfNeeded();
  assert(await schedulePanel.evaluate(el=>el.scrollWidth <= el.clientWidth + 1),'recovery settings fit mobile');
  await schedulePanel.screenshot({path:path.join(artifacts,'execution-recovery-mobile.png')});
  await interaction.setViewportSize({width:1440,height:1000});
  await schedulePanel.getByRole('button',{name:'Save schedule and dependencies',exact:true}).click();
  await wait(async()=> { const o=(await readScheduled()).orchestration; return o.reconnectPolicy==='manual' && o.usageLimitBehavior==='retry_temporary' && o.retryLimit===2; },'recovery preferences persist through real API');
  console.log('PASS recovery preferences save and fit desktop and mobile');
  await schedulePanel.waitFor({state:'detached'});
  await interaction.screenshot({path:path.join(artifacts,'calendar-execution-desktop.png')});
  await interaction.setViewportSize({width:390,height:844});
  await interaction.screenshot({path:path.join(artifacts,'calendar-execution-mobile.png')});
  await interaction.setViewportSize({width:1440,height:1000});
  const request=await team('PUT' ,`/projects/${tp.id}/tickets/${ticket.id}/schedule`,{version:0,at:add(today,1)+'T10:00:00Z',timezone:'UTC',missedPolicy:'run_late',graceSeconds:60,priority:1,order:0});
  await interaction.goto(`${teamURL}/?tab=calendar&project=${tp.id}`);
  if ((new Date(today+'T12:00Z').getUTCDay()+6)%7===6) await interaction.getByRole('button',{name:'Next week',exact:true}).click();
  const teamEvent=interaction.locator(`[data-plan-item="${ticket.id}/schedule"]`);await teamEvent.waitFor();const sourceDay=await teamEvent.getAttribute('data-plan-start'),step=new Date(sourceDay+'T12:00Z').getUTCDay()===0?-1:1;
  const previousPlan=(await getTicket()).plan;await drag(interaction,teamEvent,interaction.locator(`[data-plan-day="${add(sourceDay,step)}"]`));
  await wait(async()=> (await team('GET',`/projects/${tp.id}/schedules`)).find(s=>s.ticketId===ticket.id).at !== request.at,'Team calendar reschedules shared request');
  assert.deepEqual((await getTicket()).plan,previousPlan,'Team rescheduling preserves planned dates');
  console.log('PASS Individual hour dragging and Team calendar day dragging reschedule execution without changing planned work dates');
  console.log('PASS configured agent starts on drop and unavailable agent stays in Backlog');
  await browser.close();browser=null;
  // Universal uses real authenticated workspace pages; the shell's native registry is substituted for this focused test.
  const items=[{id:'personal',kind:'personal',name:'Individual',state:'ready',source:'Fixture'},{id:'team:main',kind:'team',name:'Team',state:'ready',source:'Fixture'}];
  const shellRoot=path.resolve(__dirname,'../desktop/frontend/dist/shell');
  server=http.createServer(async(req,res)=>{
    if(req.url==='/fixture-overview'){
      const summary=await personal('GET','/workspace/v1/summary'),b=await team('GET',`/projects/${tp.id}/board`),schedules=await team('GET',`/projects/${tp.id}/schedules`);
      res.setHeader('Content-Type','application/json');res.end(JSON.stringify({entries:[{workspace:items[0],summary},{workspace:items[1],summary:{schema:summary.schema,workspace:items[1],projects:[{id:tp.id,name:tp.name,href:`?tab=work&project=${tp.id}`}],work:[],attention:[],schedule:schedules.map(s=>({id:s.id,title:b.tickets.find(k=>k.id===s.ticketId)?.title,project:tp.name,at:s.at,state:s.state,href:`?tab=calendar&project=${tp.id}&ticket=${s.ticketId}`})),planned:b.tickets.filter(k=>k.plan?.start).map(k=>({id:k.id,title:k.title,project:tp.name,start:k.plan.start,end:k.plan.end||k.plan.start,href:`?tab=calendar&project=${tp.id}&ticket=${k.id}`})),at:new Date().toISOString()}}],at:new Date().toISOString()}));return;
    }
    const file=path.join(shellRoot,decodeURIComponent(req.url.split('?')[0]));if(!file.startsWith(shellRoot)||!fs.existsSync(file)||!fs.statSync(file).isFile()){res.writeHead(404);res.end();return;}
    res.setHeader('Content-Type',file.endsWith('.js')?'text/javascript':file.endsWith('.css')?'text/css':file.endsWith('.html')?'text/html':'application/octet-stream');res.end(fs.readFileSync(file));
  }); await new Promise(resolve=>server.listen(Number(new URL(process.env.PLANNING_SHELL_URL).port),'127.0.0.1',resolve));
  browser=await chromium.launch({headless:true});const page=await browser.newPage({viewport:{width:1440,height:1000}});page.setDefaultTimeout(15000);
  debugPage=page;const errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.addInitScript(({items,personalURL,teamURL,token,teamToken})=>{
    localStorage.setItem('werkbord.token',token);sessionStorage.setItem('werkbord-team-token',teamToken);
    if(window.parent!==window)return;
    const load=async id=>({id,kind:id==='personal'?'personal':'team',url:id==='personal'?personalURL:teamURL,origin:id==='personal'?personalURL:teamURL});
    window.go={main:{App:{Workspaces:async()=>({items,selected:'personal',problems:[],team:{state:'ready'},teamInstaller:false,invitation:false}),OpenWorkspace:load,LoadWorkspace:load,Overview:async()=>fetch('/fixture-overview').then(r=>r.json()),RememberPlace:async()=>{},Relay:async()=>({version:'fixture',platform:'darwin'}),Info:async()=>({version:'fixture',platform:'darwin'})}}};window.runtime={EventsOn:()=>()=>{}};
  },{items,personalURL,teamURL,token,teamToken});
  await page.goto('http://127.0.0.1:'+server.address().port+'/shell/index.html');
  const side=page.getByRole('navigation',{name:'Werkbord',exact:true});
  await side.getByLabel('Project context').selectOption('');
  await side.getByRole('button',{name:'Calendar',exact:true}).click();
  await page.getByRole('heading',{name:'Calendar',exact:true}).waitFor();
  for (const [workspace,id,getter] of [['personal',task.id,getTask]]) {
    const row=page.locator(`[data-plan-item="${workspace}/${id}/plan"]`).first();await row.waitFor();
    await row.click();const panel=page.getByRole('dialog',{name:'Edit task'});await panel.locator('input[maxlength="200"]').waitFor();await panel.locator('input[maxlength="200"]').fill('Universal '+workspace);await panel.getByRole('button',{name:'Save changes',exact:true}).click();
    await wait(async()=> (await getter()).title==='Universal '+workspace,'Universal edit persists');assert.equal(await page.getByRole('heading',{name:'Calendar',exact:true}).count(),1,'All Projects Calendar stays visible');
  }
  await page.locator('nav[aria-label="Werkbord"] [data-page=calendar]').click();
  const scheduledRow=page.locator(`[data-plan-item="personal/${scheduledTask.id}/schedule"]`).first();
  await scheduledRow.waitFor(); await scheduledRow.click();
  const executionPanel=page.getByRole('dialog',{name:'Edit task'});
  await executionPanel.getByLabel(/Agent start time/).waitFor();
  await executionPanel.getByRole('button',{name:'Open task and execution controls',exact:true}).waitFor();
  await executionPanel.getByRole('button',{name:'Close',exact:true}).last().click();
  console.log('PASS All Projects scheduled task opens through the authenticated planning bridge');
  await page.screenshot({path:path.join(artifacts,'universal-planning-desktop.png')});await page.setViewportSize({width:900,height:800});await page.screenshot({path:path.join(artifacts,'universal-planning-window.png')});
  await page.evaluate(()=>document.documentElement.dataset.theme='dark');await page.screenshot({path:path.join(artifacts,'universal-planning-dark.png')});
  assert.deepEqual(errors,[],'Universal page errors');console.log('PASS All Projects edits through real APIs without navigation');
})().catch(async e=>{console.error(e); if(debugPage && !debugPage.isClosed()) { console.error('Alerts',await debugPage.locator('[role=alert]').allTextContents()); console.error('Rows',await debugPage.locator('[data-plan-item]').evaluateAll(items=>items.map(el=>({id:el.dataset.planItem,start:el.dataset.planStart,end:el.dataset.planEnd,busy:el.getAttribute('aria-busy'),left:el.getBoundingClientRect().left,right:el.getBoundingClientRect().right})))); await debugPage.screenshot({path:path.join(artifacts,'planning-failure.png')}); } process.exitCode=1;}).finally(async()=>{await browser?.close();await new Promise(resolve=>server?server.close(resolve):resolve());});

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
async function shift(page, item, delta) { await wait(async()=>await item.evaluate(el=>el.closest('[data-plan-item]')?.getAttribute('aria-busy') !== 'true'),'date gesture acknowledgement'); await wait(async()=>{try{await item.scrollIntoViewIfNeeded({timeout:1000});return true;}catch(error){if(!/not attached|detached|not stable/i.test(String(error)))throw error;return false;}},'date handle attached after refresh'); await item.evaluate(el=>{const grid=el.closest('[data-plan-grid]');if(!grid)return;const r=el.getBoundingClientRect(),g=grid.getBoundingClientRect();if(r.right>g.right-100 || r.left<g.left+300)grid.scrollLeft+=(r.left+r.right)/2-(g.left+g.right+260)/2;}); const r=await item.boundingBox(); assert(r); await page.mouse.move(r.x+r.width/2,r.y+r.height/2); await page.mouse.down(); await page.mouse.move(r.x+r.width/2+delta,r.y+r.height/2,{steps:10}); await page.mouse.up(); }
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
  for (const engine of ['chromium','webkit']) {
    browser = await (engine==='webkit'?webkit:chromium).launch({headless:true});
    const page=await browser.newPage({viewport:{width:1440,height:1000}}); page.setDefaultTimeout(10000);
    debugPage=page; const errors=[]; page.on('pageerror',e=>errors.push(e.message));
    await page.addInitScript(({token,teamToken})=>{localStorage.setItem('devboard.token',token);sessionStorage.setItem('werkbord-team-token',teamToken);},{token,teamToken});
    task=await getTask(); if(task.state!=='backlog') task=await personal('PATCH',`/projects/${work.id}/tasks/${task.id}`,{version:task.version,state:'backlog'});
    await page.goto(`${personalURL}/#/p/${work.id}/board`);
    const card=page.locator(`[data-board-item="${task.id}"]`); await card.waitFor();
    await drag(page,card.locator('.work-detail').first(),page.locator('[data-board-column=doing]'));
    await wait(async()=> (await getTask()).state==='doing',engine+' full card move');
    await wait(()=>page.locator('[data-drag-handle]').first().isEnabled(),'Individual move acknowledgement');
    await page.locator('[data-drag-handle]').first().focus(); await page.keyboard.press('ArrowRight');
    await wait(async()=> (await getTask()).state==='review',engine+' keyboard board move');
    await page.goto(`${personalURL}/#/p/${work.id}/timeline`);
    const bar=page.locator(`.rb[data-plan-item="${task.id}"]`); await bar.waitFor(); task=await getTask();
    await shift(page,bar,56); await wait(async()=> (await getTask()).plan.start===add(task.plan.start,1),engine+' timeline move');
    task=await getTask(); await shift(page,bar.locator('[data-plan-edge=end]'),56);
    await wait(async()=> (await getTask()).plan.end===add(task.plan.end,1),engine+' timeline resize');
    await bar.click(); const panel=page.getByRole('dialog',{name:'Edit task'}); await panel.waitFor();
    assert(page.url().endsWith('/timeline'),'timeline route stays open');
    await panel.locator('input[maxlength="200"]').fill('Plan the launch '+engine);
    await panel.getByRole('button',{name:'Save changes',exact:true}).click();
    await wait(async()=> (await getTask()).title==='Plan the launch '+engine,'Individual in-place save');
    await page.getByRole('button',{name:'Day',exact:true}).click(); assert.equal(await page.getByRole('button',{name:'Day',exact:true}).getAttribute('aria-pressed'),'true');
    await page.getByRole('button',{name:'Quarter',exact:true}).click();
    await page.goto(`${personalURL}/#/p/${work.id}/calendar`);
    const event=page.locator(`[data-plan-item="${task.id}"]`).first(); await event.waitFor();
    const original=await getTask(), target=add(original.plan.start,1);
    if (await page.locator(`[data-plan-day="${target}"]`).count()) { await drag(page,event,page.locator(`[data-plan-day="${target}"]`)); await wait(async()=> (await getTask()).plan.start===target,'Individual work calendar drag'); }
    await page.goto(`${teamURL}/?tab=board&project=${tp.id}`);
    const teamCard=page.locator(`[data-board-item="${ticket.id}"]`); await teamCard.waitFor(); ticket=await getTicket();
    await drag(page,teamCard.locator('.title'),page.locator('[data-board-column=in_progress]'));
    await wait(async()=> (await getTicket()).status==='in_progress',engine+' Team full title claim');
    await wait(()=>page.locator('[data-drag-handle]').first().isEnabled(),'Team settled');
    await page.locator('[data-drag-handle]').first().focus(); await page.keyboard.press('ArrowRight');
    await wait(async()=> (await getTicket()).status==='review','Team submit');
    await page.locator(`[data-board-item="${ticket.id}"][data-work-state=review]`).waitFor();
    await wait(()=>page.locator(`[data-board-item="${ticket.id}"] [data-drag-handle]`).isEnabled(),'Team submit acknowledgement');
    await drag(page,page.locator(`[data-board-item="${ticket.id}"] .title`),page.locator('[data-board-column=available]'));
    await page.getByText(/This move needs the ticket’s normal workflow action/).waitFor(); assert.equal((await getTicket()).status,'review','refused drop preserves original status');
    const tabs=page.getByRole('navigation',{name:'Project sections'});
    assert.deepEqual((await tabs.locator('button').allTextContents()).map(t=>t.replace(/\d+/g,'').trim()),['Overview','Board','Calendar','Git','Runs']);
    await page.getByRole('button',{name:/^Project settings/}).waitFor();
    assert.equal(await page.locator('.project-details, .project-people').count(),0,'the project header has no people list or details dropdown');
    await tabs.getByRole('button',{name:'Calendar',exact:true}).click();
    const te=page.locator(`[data-plan-item="${ticket.id}"]`).first(); await te.waitFor(); ticket=await getTicket();
    await drag(page,te,page.locator(`[data-plan-day="${add(ticket.plan.start,1)}"]`));
    await wait(async()=> (await getTicket()).plan.start===add(ticket.plan.start,1),'Team calendar move');
    await page.getByRole('group',{name:'Calendar presentation'}).getByRole('button',{name:'Timeline',exact:true}).click();
    const tb=page.locator(`.tl-bar[data-plan-item="${ticket.id}"]`);await tb.waitFor();ticket=await getTicket();
    await shift(page,tb,56);await wait(async()=> (await getTicket()).plan.start===add(ticket.plan.start,1),'Team timeline move');
    ticket=await getTicket();await shift(page,tb.locator('[data-plan-edge=end]'),56);await wait(async()=> (await getTicket()).plan.end===add(ticket.plan.end,1),'Team timeline resize');
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
      await page.goto(`${personalURL}/#/p/${work.id}/timeline`);await page.locator('.rb').waitFor(); await page.screenshot({path:path.join(artifacts,'individual-planning-desktop.png')});
    }
    console.log('PASS '+engine+' full-card and keyboard board moves, refused drops, calendar dates, timeline drag/resize and edits in place');
    await browser.close(); browser=null;
  }
  // An actual agent start and an unavailable-agent refusal use the normal controller workflow.
  browser=await chromium.launch({headless:true});
  const interaction=await browser.newPage({viewport:{width:1440,height:1000}});
  interaction.setDefaultTimeout(10000); debugPage=interaction;
  await interaction.addInitScript(({token,teamToken})=>{localStorage.setItem('devboard.token',token);sessionStorage.setItem('werkbord-team-token',teamToken);},{token,teamToken});
  const repository=await personal('POST','/projects',{name:'Agent gestures',path:process.env.WERKBORD_BROWSER_REPO});
  const refused=await personal('POST',`/projects/${repository.id}/tasks`,{title:'Unavailable agent drop',execution:{agent:'codex'}});
  const runnable=await personal('POST',`/projects/${repository.id}/tasks`,{title:'Start from a board drop',execution:{agent:'fixture'}});
  await interaction.goto(`${personalURL}/#/p/${repository.id}/board`);
  await interaction.locator(`[data-board-item="${refused.id}"]`).waitFor();
  await drag(interaction,interaction.locator(`[data-board-item="${refused.id}"] .title`),interaction.locator('[data-board-column=doing]'));
  await interaction.getByRole('status').filter({hasText:/agent|Codex|available|installed/i}).first().waitFor();
  assert.equal((await personal('GET',`/projects/${repository.id}/tasks`)).tasks.find(t=>t.id===refused.id).state,'backlog','configuration refusal preserves Backlog');
  await drag(interaction,interaction.locator(`[data-board-item="${runnable.id}"] .title`),interaction.locator('[data-board-column=doing]'));
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
  const request=await team('PUT',`/projects/${tp.id}/tickets/${ticket.id}/schedule`,{version:0,at:add(today,1)+'T10:00:00Z',timezone:'UTC',missedPolicy:'run_late',graceSeconds:60,priority:1,order:0});
  await interaction.goto(`${teamURL}/?tab=calendar&project=${tp.id}`);
  if ((new Date(today+'T12:00Z').getUTCDay()+6)%7===6) await interaction.getByRole('button',{name:'Next week',exact:true}).click();
  const teamEvent=interaction.locator(`[data-plan-item="${ticket.id}/schedule"]`);await teamEvent.waitFor();const sourceDay=await teamEvent.getAttribute('data-plan-start'),step=new Date(sourceDay+'T12:00Z').getUTCDay()===0?-1:1;
  const previousPlan=(await getTicket()).plan;await drag(interaction,teamEvent,interaction.locator(`[data-plan-day="${add(sourceDay,step)}"]`));
  await wait(async()=> (await team('GET',`/projects/${tp.id}/schedules`)).find(s=>s.ticketId===ticket.id).at !== request.at,'Team calendar reschedules shared request');
  assert.deepEqual((await getTicket()).plan,previousPlan,'Team rescheduling preserves planned dates');
  console.log('PASS Individual hour dragging and Team calendar day dragging reschedule execution without changing planned work dates');
  // Real touch input exercises Pointer Events rather than synthesising a product-specific drop event.
  await interaction.goto(`${personalURL}/#/p/${work.id}/timeline`);
  const touchBar=interaction.locator(`.rb[data-plan-item="${task.id}"]`);await touchBar.waitFor();task=await getTask();await touchBar.scrollIntoViewIfNeeded();
  const b=await touchBar.boundingBox(),cdp=await interaction.context().newCDPSession(interaction);
  await cdp.send('Input.dispatchTouchEvent',{type:'touchStart',touchPoints:[{x:b.x+b.width/2,y:b.y+b.height/2}]});
  await cdp.send('Input.dispatchTouchEvent',{type:'touchMove',touchPoints:[{x:b.x+b.width/2+56,y:b.y+b.height/2}]});
  await cdp.send('Input.dispatchTouchEvent',{type:'touchEnd',touchPoints:[]});
  await wait(async()=> (await getTask()).plan.start===add(task.plan.start,1),'touch timeline date move');
  console.log('PASS configured agent starts on drop, unavailable agent stays in Backlog, and real touch input moves planned work');
  await browser.close();browser=null;
  // Universal uses real authenticated workspace pages; the shell's native registry is substituted for this focused test.
  const items=[{id:'personal',kind:'personal',name:'Individual',state:'ready',source:'Fixture'},{id:'team:main',kind:'team',name:'Team',state:'ready',source:'Fixture'}];
  const shellRoot=path.resolve(__dirname,'../desktop/frontend/dist/shell');
  server=http.createServer(async(req,res)=>{
    if(req.url==='/fixture-overview'){
      const summary=await personal('GET','/workspace/v1/summary'),b=await team('GET',`/projects/${tp.id}/board`),schedules=await team('GET',`/projects/${tp.id}/schedules`);
      res.setHeader('Content-Type','application/json');res.end(JSON.stringify({entries:[{workspace:items[0],summary},{workspace:items[1],summary:{schema:summary.schema,workspace:items[1],projects:[{id:tp.id,name:tp.name,href:`?tab=board&project=${tp.id}`}],work:[],attention:[],schedule:schedules.map(s=>({id:s.id,title:b.tickets.find(k=>k.id===s.ticketId)?.title,project:tp.name,at:s.at,state:s.state,href:`?tab=calendar&project=${tp.id}&ticket=${s.ticketId}`})),planned:b.tickets.filter(k=>k.plan?.start).map(k=>({id:k.id,title:k.title,project:tp.name,start:k.plan.start,end:k.plan.end||k.plan.start,href:`?tab=calendar&project=${tp.id}&ticket=${k.id}`})),at:new Date().toISOString()}}],at:new Date().toISOString()}));return;
    }
    const file=path.join(shellRoot,decodeURIComponent(req.url.split('?')[0]));if(!file.startsWith(shellRoot)||!fs.existsSync(file)||!fs.statSync(file).isFile()){res.writeHead(404);res.end();return;}
    res.setHeader('Content-Type',file.endsWith('.js')?'text/javascript':file.endsWith('.css')?'text/css':file.endsWith('.html')?'text/html':'application/octet-stream');res.end(fs.readFileSync(file));
  }); await new Promise(resolve=>server.listen(Number(new URL(process.env.PLANNING_SHELL_URL).port),'127.0.0.1',resolve));
  browser=await chromium.launch({headless:true});const page=await browser.newPage({viewport:{width:1440,height:1000}});page.setDefaultTimeout(15000);
  debugPage=page;const errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.addInitScript(({items,personalURL,teamURL,token,teamToken})=>{
    localStorage.setItem('devboard.token',token);sessionStorage.setItem('werkbord-team-token',teamToken);
    if(window.parent!==window)return;
    const load=async id=>({id,kind:id==='personal'?'personal':'team',url:id==='personal'?personalURL:teamURL,origin:id==='personal'?personalURL:teamURL});
    window.go={main:{App:{Workspaces:async()=>({items,selected:'personal',problems:[],team:{state:'ready'},teamInstaller:false,invitation:false}),OpenWorkspace:load,LoadWorkspace:load,Overview:async()=>fetch('/fixture-overview').then(r=>r.json()),RememberPlace:async()=>{},Relay:async()=>({version:'fixture',platform:'darwin'}),Info:async()=>({version:'fixture',platform:'darwin'})}}};window.runtime={EventsOn:()=>()=>{}};
  },{items,personalURL,teamURL,token,teamToken});
  await page.goto('http://127.0.0.1:'+server.address().port+'/shell/index.html');
  const side=page.getByRole('navigation',{name:'Werkbord',exact:true});await page.getByTestId('workspace-switcher').click();await page.locator('[role=menuitem][data-workspace=personal]').click();await side.getByRole('button',{name:'Analytics',exact:true}).waitFor();
  const addTop=await side.getByRole('button',{name:'Add project',exact:true}).boundingBox(),analyticTop=await side.getByRole('button',{name:'Analytics',exact:true}).boundingBox();assert(analyticTop.y>addTop.y && analyticTop.y-addTop.y<50,'Analytics follows Add project');
  await page.getByTestId('workspace-switcher').click();await page.locator('[data-workspace=universal]').click();await side.getByRole('button',{name:'Calendar',exact:true}).click();
  await page.getByRole('group',{name:'Calendar presentation'}).getByRole('button',{name:'Timeline',exact:true}).click();
  for (const [workspace,id,getter] of [['personal',task.id,getTask]]) {
    const row=page.locator(`[data-plan-item="${workspace}/${id}/plan"]`);await row.waitFor();let before=await getter();
    await shift(page,row,56);await wait(async()=> (await getter()).plan.start===add(before.plan.start,1),'Universal '+workspace+' move');
    before=await getter();await shift(page,row.locator('[data-plan-edge=end]'),56);await wait(async()=> (await getter()).plan.end===add(before.plan.end,1),'Universal '+workspace+' resize');
    await row.click();const panel=page.getByRole('dialog',{name:'Edit task'});await panel.locator('input[maxlength="200"]').waitFor();await panel.locator('input[maxlength="200"]').fill('Universal '+workspace);await panel.getByRole('button',{name:'Save changes',exact:true}).click();
    await wait(async()=> (await getter()).title==='Universal '+workspace,'Universal edit persists');assert.equal(await side.getByRole('button',{name:'Calendar',exact:true}).getAttribute('aria-current'),'page','Universal stays visible');
  }
  await page.getByRole('button',{name:'Day',exact:true}).click();await page.getByRole('button',{name:'Quarter',exact:true}).click();
  await page.screenshot({path:path.join(artifacts,'universal-planning-desktop.png')});await page.setViewportSize({width:900,height:800});await page.screenshot({path:path.join(artifacts,'universal-planning-window.png')});
  await page.evaluate(()=>document.documentElement.dataset.theme='dark');await page.screenshot({path:path.join(artifacts,'universal-planning-dark.png')});
  assert.deepEqual(errors,[],'Universal page errors');console.log('PASS Universal Individual saves through real APIs, resizes and edits without navigation; sidebar order and zoom');
})().catch(async e=>{console.error(e); if(debugPage && !debugPage.isClosed()) { console.error('Alerts',await debugPage.locator('[role=alert]').allTextContents()); console.error('Rows',await debugPage.locator('[data-plan-item]').evaluateAll(items=>items.map(el=>({id:el.dataset.planItem,start:el.dataset.planStart,end:el.dataset.planEnd,busy:el.getAttribute('aria-busy'),left:el.getBoundingClientRect().left,right:el.getBoundingClientRect().right})))); await debugPage.screenshot({path:path.join(artifacts,'planning-failure.png')}); } process.exitCode=1;}).finally(async()=>{await browser?.close();await new Promise(resolve=>server?server.close(resolve):resolve());});

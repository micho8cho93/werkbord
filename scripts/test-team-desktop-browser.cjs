// Real Team device APIs/database + a disposable individual controller. No OS service or privileged tunnel is installed.
const fs=require('node:fs'), os=require('node:os'), path=require('node:path'), cp=require('node:child_process'), net=require('node:net'), assert=require('node:assert/strict');
const {once}=require('node:events');
const {chromium}=require('../web/node_modules/playwright');
const root=path.resolve(__dirname,'..'), temp=fs.mkdtempSync(path.join(os.tmpdir(),'team-desktop-browser-'));
const artifacts=process.env.BROWSER_ARTIFACT_DIR||path.join(temp,'screenshots');fs.mkdirSync(artifacts,{recursive:true});
const children=[]; let browser;
const delay=ms=>new Promise(r=>setTimeout(r,ms));
function run(cmd,args,extra={}){const r=cp.spawnSync(cmd,args,{cwd:root,env:{...process.env,...extra},encoding:'utf8',timeout:180000});if(r.status!==0)throw Error(r.stderr||r.stdout||String(r.error));return r.stdout;}
function spawn(cmd,args,extra={}){const p=cp.spawn(cmd,args,{cwd:root,env:{...process.env,...extra},stdio:['ignore','pipe','pipe']});p.diagnostics='';p.stdout.on('data',d=>p.diagnostics+=d);p.stderr.on('data',d=>p.diagnostics+=d);children.push(p);return p;}
async function port(){const s=net.createServer();s.listen(0,'127.0.0.1');await once(s,'listening');const n=s.address().port;await new Promise(r=>s.close(r));return n;}
async function wait(fn,label){for(let n=0;n<900;n++){for(const c of children)if(c.exitCode!==null)throw Error(c.diagnostics);if(await fn())return;await delay(100);}throw Error('Timed out: '+label);}
async function http(base,token,method,url,body){const r=await fetch(base+url,{method,headers:{Authorization:'Bearer '+token,...(body?{'Content-Type':'application/json'}:{})},body:body?JSON.stringify(body):undefined});if(r.status===204)return null;const d=await r.json();if(!r.ok)throw Error(r.status+': '+JSON.stringify(d));return d;}
async function shot(page,name,width=1440,height=1000){await page.setViewportSize({width,height});await page.evaluate(()=>document.fonts.ready);if(!process.env.BROWSER_SKIP_SCREENSHOTS)await page.screenshot({path:path.join(artifacts,name+'.png'),fullPage:true});assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'horizontal overflow: '+name);}
(async()=>{
 const personal=path.join(temp,'personal');run('go',['build','-o',personal,'./scripts/browser-fixture']);
 const personalURL='http://127.0.0.1:'+await port();const full='disposable-browser-credential';
 spawn(personal,[],{WERKBORD_BROWSER_ADDR:new URL(personalURL).host,WERKBORD_BROWSER_EXECUTION:'1'});
 await wait(async()=>{try{return(await fetch(personalURL+'/api/health')).ok;}catch{return false;}},'personal controller');
 fs.writeFileSync(path.join(temp,'runner-config.json'),JSON.stringify({addr:new URL(personalURL).host,token:full}),{mode:0o600});
 const pins=run('sh',['scripts/team-build-flags.sh']).trim();
 spawn('go',['test','-ldflags',pins,'-run','^TestTeamDesktopBrowserFixture$','-count=1','-timeout','12m','./internal/team/server'],{WERKBORD_TEAM_BROWSER_FIXTURE:temp,WERKBORD_SKIP_RQLITE:'0',WERKBORD_REQUIRE_RQLITE:'1'});
 await wait(()=>fs.existsSync(path.join(temp,'ready.json')),'fixture');const m=JSON.parse(fs.readFileSync(path.join(temp,'ready.json')));
 const dev=(which,method,url,body)=>http(m[which],m[which+'Key'],method,'/api/device/v1'+url,body);
 const team=(method,url,body)=>http(m.first,m.firstKey,method,'/api/team/v1'+url,body);
 browser=await chromium.launch({headless:true});const page=await browser.newPage({viewport:{width:1440,height:1000}});page.setDefaultTimeout(90000);
 const errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.goto(m.first+'/#token='+encodeURIComponent(m.firstKey));await page.getByRole('button',{name:'Create Team',exact:true}).waitFor();
 await shot(page,'onboarding');await shot(page,'onboarding-mobile',390,844);await page.setViewportSize({width:1440,height:1000});
 await page.getByRole('button',{name:'Create Team',exact:true}).click();await page.locator('input[type=file]').setInputFiles(path.join(temp,'license.json'));await page.getByRole('button',{name:'Activate license',exact:true}).click();
 await page.locator('[name=setup-team]').fill('Northstar Studio');await page.locator('[name=setup-owner]').fill('Ada');await page.getByRole('button',{name:'Create workspace',exact:true}).click();
 await page.waitForFunction(()=>state.me&&state.desktop&&state.device.enrolled,{},{timeout:90000});
 let storage;await wait(async()=>{storage=await team('GET','/storage');return storage.writable;},'workspace accepting changes');await page.reload();await page.waitForFunction(()=>state.me&&state.desktop);
 assert.equal(await page.getByRole('navigation',{name:'Team administration'}).count(),0,'administration stays behind Settings');
 assert.equal(await page.locator('.resilience').count(),0,'host configuration does not displace everyday work');
 console.log('PASS signed license import, create, real host/database bootstrap and focused workspace');
 await shot(page,'desktop');await shot(page,'mobile',390,844);await page.setViewportSize({width:1440,height:1000});
 for(const label of ['Projects','Board','My Work','Reviews','Activity','Members','Devices','Workspace Hosts','Connectivity','Backups','License','Settings']){
  if(['Devices','Workspace Hosts','Connectivity','Backups','License'].includes(label))await page.getByRole('navigation',{name:'Settings',exact:true}).getByRole('button',{name:'Settings',exact:true}).click();
  await page.locator('nav').getByRole('button',{name:label,exact:true}).click();await page.locator('nav button[aria-current=page]').filter({hasText:new RegExp('^'+label)}).waitFor();
  await wait(async()=>!(await page.locator('.error').count()),label+' no error');
  if(['Devices','Workspace Hosts'].includes(label))assert(await page.getByRole('button',{name:'Remove host',exact:true}).isDisabled(),'final Host removal must be disabled');
  if(label==='Workspace Hosts')await page.getByText(/^Your workspace currently has one host\. Add two more hosts for fault tolerance\./).waitFor();
  if(['Devices','Workspace Hosts','Connectivity','Settings'].includes(label))await shot(page,label.toLowerCase().replaceAll(' ','-'));
 }
 const plan=await dev('first','GET','/removal');assert.equal(plan.canRemoveData,false);
 let ds;await wait(async()=>{ds=await team('GET','/devices');return ds[0]?.capabilities.includes('runner');},'automatic local runner registration');assert.equal(ds.length,1);assert(ds[0].capabilities.includes('runner'));assert(ds[0].capabilities.includes('workspace_host'));
 const owner=(await team('GET','/me')).member;
 await page.getByRole('navigation',{name:'Werkbord Team',exact:true}).getByRole('button',{name:'Members',exact:true}).click();
 await page.locator('[name=enrollment-label]').fill('Ada’s office Mac');await page.locator('[name=enrollment-person]').selectOption(owner.id);await page.getByRole('button',{name:'Create invitation',exact:true}).click();
 await page.getByText('Your invitation is ready',{exact:true}).waitFor();const link=await page.evaluate(()=>createdInvitation.link);await shot(page,'invitation');assert(await page.locator('.invite-qr').isVisible());
 const second=await browser.newPage({viewport:{width:1440,height:1000}});second.setDefaultTimeout(90000);second.on('pageerror',e=>errors.push(e.message));
 await second.goto(m.second+'/#token='+encodeURIComponent(m.secondKey)+'&join='+encodeURIComponent(link));await second.getByRole('button',{name:'Verify invitation',exact:true}).click();
 await second.locator('[name=join-member]').fill('Ada');await second.locator('[name=join-device]').fill('Office Mac');await second.getByRole('button',{name:'Join workspace',exact:true}).click();await second.getByRole('heading',{name:'Waiting for your administrator',exact:true}).waitFor();await shot(second,'join-pending');
 await page.getByRole('navigation',{name:'Settings',exact:true}).getByRole('button',{name:'Settings',exact:true}).click();await page.getByRole('navigation',{name:'Team administration'}).getByRole('button',{name:'Devices',exact:true}).click();await page.getByRole('button',{name:'Approve device',exact:true}).click();
 await second.waitForFunction(()=>state.me&&state.desktop&&state.device.enrolled,{},{timeout:90000});
 const joined=await dev('second','GET','/state');assert(joined.enrolled);const registered=(await team('GET','/devices')).find(d=>d.id===joined.deviceId);assert(!registered.capabilities.includes('workspace_host'));
 await wait(async()=>{const s=await dev('second','GET','/state'),ds=await team('GET','/devices');return s.runner.connected&&ds.some(d=>d.id===joined.deviceId&&d.capabilities.includes('runner'));},'joined device runner registration');
 console.log('PASS invitation QR/link, verified join, admin approval, own runner detection, independent Host role');
 await dev('second','PUT','/settings',{...joined.settings,form:'desktop'});
 const project=await team('POST','/projects',{name:'Customer portal',repository:'https://github.com/acme/shop'});
 const ticket=await team('POST',`/projects/${project.id}/tickets`,{title:'Improve the sign-in experience',description:'Keep the session clear and accessible.',requirements:'All states readable',status:'available'});
 await team('POST',`/projects/${project.id}/tickets/${ticket.id}/claim`,{});
 await page.keyboard.press('Meta+k');const search=page.getByRole('combobox',{name:'Search projects, tickets and sections'});await search.fill(ticket.key);await page.getByRole('dialog').getByRole('option').filter({hasText:ticket.title}).waitFor();await search.press('Enter');await page.waitForFunction(id=>state.ticketId===id&&!!document.querySelector('.ticket'),ticket.id);await page.keyboard.press('Escape');
 await page.getByRole('button',{name:'Switch to dark mode',exact:true}).click();await page.reload();await page.getByRole('button',{name:'Switch to light mode',exact:true}).waitFor();await page.getByRole('button',{name:'Switch to light mode',exact:true}).click();
 console.log('PASS Cmd K through the enrolled device API and persistent desktop theme selection');
 const repo=path.join(temp,'repo');fs.mkdirSync(repo);run('git',['-C',repo,'init','-b','main']);run('git',['-C',repo,'-c','user.name=Fixture','-c','user.email=fixture@example.test','-c','core.hooksPath=/dev/null','commit','--allow-empty','-m','Initial fixture']);run('git',['-C',repo,'remote','add','origin','https://github.com/acme/shop']);
 await http(personalURL,full,'POST','/api/projects',{name:'Customer portal',path:repo});
 await wait(async()=>{const s=await dev('first','GET','/state');return (s.senders||[]).some(d=>d.deviceId===joined.deviceId);},'device sender discovery');
 const own=(await dev('first','GET','/state')).deviceId;
 await wait(async()=>{const s=await dev('second','GET','/state');return (s.senders||[]).some(d=>d.deviceId===own);},'target sender discovery');
 await second.locator('nav').getByRole('button',{name:'Settings',exact:true}).click();await second.getByRole('button',{name:'Trust this device',exact:true}).click();
 await wait(async()=>{const s=await dev('second','GET','/state');return s.senders?.some(d=>d.deviceId===own&&d.approved);},'local sender approval');
 const message=await dev('first','POST','/requests',{target:joined.deviceId,action:'open_ticket_on_runner',payload:{projectId:project.id,ticketId:ticket.id}});
 await wait(async()=>{const result=await team('GET','/messages/'+message.id);if(result.state==='refused')throw Error(JSON.stringify(result.result));return result.state==='done';},'signed semantic handoff');
 const opened=await dev('second','GET','/state');assert.equal(opened.opened.length,1);assert.equal((opened.approvals||[]).length,0);
 console.log('PASS signed cross-device handoff to the actual narrow individual bridge; task opened with no run start');
 // Phase 2: authenticated local controls, effective policy, one-shot launch,
 // privileged question answer and shared scheduling through the owner controller.
 await page.goto(m.first+`/?tab=board&project=${project.id}&ticket=${ticket.id}`);
 await page.getByRole('button',{name:'Agent controls',exact:true}).click();
 await wait(async()=>{const error=page.locator('.error');if(await error.count())throw Error(await error.innerText());return await page.getByRole('button',{name:'Review execution policy',exact:true}).count();},'local execution controls');
 await page.getByRole('button',{name:'Review execution policy',exact:true}).click();
 await page.getByRole('heading',{name:'Effective policy',exact:true}).waitFor();
 await shot(page,'agent-policy');await shot(page,'agent-policy-mobile',390,844);await page.setViewportSize({width:1440,height:1000});
 await page.getByRole('button',{name:'Approve and start my run',exact:true}).click();
 await page.getByRole('button',{name:'Stop my run',exact:true}).waitFor();
 const localProjects=await http(personalURL,full,'GET','/api/projects');
 const localProject=localProjects.projects.find(p=>p.name==='Customer portal');
 const localRuns=await http(personalURL,full,'GET',`/api/projects/${localProject.id}/runs`);
 const running=localRuns.runs.find(r=>['running','waiting_for_user'].includes(r.state));assert(running,'local run launched');
 await http(personalURL,full,'POST',`/api/projects/${localProject.id}/runs/${running.id}/input`,{text:'ask'});
 await wait(async()=>{const detail=await dev('first','GET',`/execution/detail?projectId=${project.id}&ticketId=${ticket.id}`);return detail.questions.length===1;},'agent question');
 await page.getByRole('button',{name:'Refresh progress',exact:true}).click();
 await page.getByText('Commit the fixture work?',{exact:true}).waitFor();
 await page.getByRole('button',{name:'Yes',exact:true}).click();
 await wait(async()=>{const detail=await dev('first','GET',`/execution/detail?projectId=${project.id}&ticketId=${ticket.id}`);return detail.questions.length===0;},'owner answer delivered');
 await page.getByRole('button',{name:'Stop my run',exact:true}).click();
 await wait(async()=>{const r=await http(personalURL,full,'GET',`/api/projects/${localProject.id}/runs/${running.id}`);return r.state==='stopped';},'owner stop');
 await page.waitForFunction(()=>!actionBusy);
 const scheduledAt=new Date(Date.now()+3600000).toISOString();
 await page.locator(`[name=schedule-at-${ticket.id}]`).fill(scheduledAt);
 await page.evaluate(()=>render());
 await wait(async()=>await page.locator(`[name=schedule-at-${ticket.id}]`).inputValue()===scheduledAt,'schedule draft survives refresh');
 const formStatus=await page.getByRole('button',{name:'Propose schedule',exact:true}).evaluate(b=>({valid:b.form.checkValidity(),fields:[...b.form.elements].map(e=>({name:e.name,value:e.value,valid:e.checkValidity()}))}));
 assert(formStatus.valid,JSON.stringify(formStatus));
 const scheduleReply=page.waitForResponse(r=>r.request().method()==='PUT'&&r.url().endsWith('/schedule'),{timeout:10000});
 await page.getByRole('button',{name:'Propose schedule',exact:true}).click();
 const createdSchedule=await scheduleReply;assert(createdSchedule.ok(),await createdSchedule.text());
 await wait(async()=>{const error=page.locator('.error');if(await error.count())throw Error(await error.innerText());return await page.getByRole('button',{name:'Cancel request',exact:true}).count();},'shared schedule created');
 await page.getByRole('button',{name:'Agent controls',exact:true}).click();
 await wait(async()=>{const error=page.locator('.error');if(await error.count())throw Error(await error.innerText());return await page.getByRole('button',{name:'Review execution policy',exact:true}).count();},'local execution controls');
 await page.getByRole('button',{name:'Review execution policy',exact:true}).click();
 await page.getByRole('button',{name:'Approve this execution once',exact:true}).click();
 await page.getByText(/Authorized once until/).waitFor();
 await shot(page,'scheduled-policy');await shot(page,'scheduled-policy-mobile',390,844);await page.setViewportSize({width:1440,height:1000});
 await page.getByRole('button',{name:'Cancel request',exact:true}).click();
 await wait(async()=>{const list=await team('GET',`/projects/${project.id}/schedules`);return list[0]?.state==='canceled';},'canceled schedule');
 console.log('PASS local agent controls, policy review, start, question, stop, scheduled preapproval and cancellation');

 // Presentation-only regression: a request remains observable through queued → delivered → done.
 let progressCalls=0;await page.route('**/api/team/v1/messages/ui-progress-fixture',route=>route.fulfill({status:200,contentType:'application/json',body:JSON.stringify({id:'ui-progress-fixture',state:++progressCalls===1?'queued':progressCalls===2?'delivered':'done',result:{}})}));
 await page.evaluate(async()=>{activeRequest={id:'ui-progress-fixture'};await pollRequest();});
 await page.waitForFunction(()=>activeRequest?.id==='ui-progress-fixture'&&activeRequest.state==='done');assert.equal(progressCalls,3);
 await page.unroute('**/api/team/v1/messages/ui-progress-fixture');console.log('PASS request progress continues after delivery and stops at its final result');
 // A project invite is a code. The page's own address is this computer's, so a link would be useless to anyone else.
 await page.locator('nav').getByRole('button',{name:'Projects',exact:true}).click();await page.getByRole('heading',{name:'Join a project with a code',exact:true}).waitFor();
 await page.getByRole('button',{name:'People & invites',exact:true}).click();await page.getByRole('button',{name:'Create invite code',exact:true}).click();
 const secret=page.locator('.secret');await secret.waitFor();const secretText=await secret.innerText();
 assert(/wbi_[0-9a-f]{20,}/.test(secretText),'the invite code is shown');assert(!secretText.includes('http')&&!secretText.includes('127.0.0.1'),'no loopback link is offered');assert.equal(await secret.getByRole('button',{name:'Copy link',exact:true}).count(),0);
 console.log('PASS project invite is a code, never a link to this computer');
 await page.goto(m.first+'/?tab=workspace');await page.waitForFunction(()=>state.ov?.projects.length===1);await shot(page,'desktop');await shot(page,'mobile',390,844);
 assert((await page.locator('#working-now').boundingBox()).y<844,'everyday work must appear in the first mobile viewport');
 assert.deepEqual(errors,[]);fs.writeFileSync(path.join(temp,'done'),'done');console.log('PASS no page errors or horizontal overflow; screenshots: '+artifacts);
})().catch(async e=>{if(browser){for(const [i,p]of browser.contexts().flatMap(c=>c.pages()).entries())await p.screenshot({path:path.join(artifacts,'failure-'+i+'.png'),fullPage:true}).catch(()=>{});}console.error(e);process.exitCode=1;}).finally(async()=>{
 if(browser)await browser.close();fs.writeFileSync(path.join(temp,'done'),'done');await delay(1500);for(const c of children)if(c.exitCode===null)c.kill('SIGTERM');fs.rmSync(temp,{recursive:true,force:true});
});

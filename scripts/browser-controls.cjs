// Button-state regressions against disposable services from test-browser.cjs.
const assert = require('node:assert/strict');
const fs = require('node:fs'), path = require('node:path');
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || '../web/node_modules/playwright');
const individual = process.env.PERSONAL_BROWSER_URL, team = process.env.TEAM_BROWSER_URL;
const token = 'disposable-browser-credential';
const teamToken = fs.readFileSync(process.env.TEAM_BROWSER_TOKEN_FILE, 'utf8').trim();
for (const base of [individual, team]) assert.equal(new URL(base).hostname, '127.0.0.1');
async function call(base, credential, method, route, body) {
 const res = await fetch(base + route, { method, headers: { Authorization: 'Bearer ' + credential, 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined });
 const text = await res.text(); assert(res.ok, text); return text ? JSON.parse(text) : null;
}
const personal = (method, route, body) => call(individual, token, method, '/api' + route, body);
const coord = (method, route, body) => call(team, teamToken, method, '/api/team/v1' + route, body);
let browser, activePage;
(async () => {
 browser = await chromium.launch({headless:true});
 const page = await browser.newPage({viewport:{width:1440,height:1000}}); activePage=page;
 const errors=[]; page.on('pageerror', e => errors.push(e.message));
 const repo=fs.realpathSync(process.env.WERKBORD_BROWSER_REPO);
 const p = (await personal('GET','/projects')).projects.find(p=>p.repoPath===repo) || await personal('POST','/projects',{name:'Control audit',path:repo});
 await personal('POST','/onboarding/complete',{});
 // Start on the route under test. A token-link bootstrap first resolves its
 // default project; navigating during that startup can race its replacement URL.
 await page.addInitScript(({origin,token}) => {
  if(location.origin === origin) localStorage.setItem('werkbord.token',token);
 }, {origin:new URL(individual).origin,token});
 await page.goto(individual+'/#/settings?runners');
 await page.waitForURL(url => url.hash === '#/settings?runners');
 const runners = page.locator('.pn.runners');
 const local = (await personal('GET','/runners')).runners.find(r => r.kind === 'local');
 assert(local && local.online && !local.disabled, 'the local fixture runner is connected and accepts work');
 const machine = runners.locator('.runner').filter({hasText:' · Controller'});
 await machine.getByText('Connected',{exact:true}).waitFor();
 assert(!/\d+\s*[\/]\s*\d+/.test(await machine.locator('.state').innerText()),'runner connection is not a capacity fraction');
 await machine.locator('summary').click();
 const saveRunner = machine.getByRole('button',{name:'Save runner',exact:true});
 assert(await saveRunner.isDisabled(),'unchanged runner settings cannot be saved again');
 const name = machine.getByLabel('Name',{exact:true}); const initialName = await name.inputValue();
 await name.fill(initialName+' audit'); assert(await saveRunner.isEnabled());
 await saveRunner.click(); await saveRunner.waitFor({state:'visible'});
 await page.waitForFunction(() => [...document.querySelectorAll('button')].some(b=>b.textContent==='Save runner'&&b.disabled));
 assert((await personal('GET','/runners')).runners.some(r=>r.name===initialName+' audit'),'runner edit persisted');
 await name.fill(initialName); await saveRunner.click();
 await page.waitForFunction(() => [...document.querySelectorAll('button')].some(b=>b.textContent==='Save runner'&&b.disabled));
 await runners.getByRole('button',{name:'Add runner',exact:true}).click();
 await runners.getByRole('checkbox',{name:p.name,exact:true}).check();
 await runners.getByRole('button',{name:'Generate pairing code',exact:true}).click();
 await runners.getByLabel('Run on the other machine').waitFor();
 assert(await runners.getByRole('button',{name:'Pairing code ready',exact:true}).isDisabled(),'live pairing cannot be generated twice');
 await runners.getByRole('button',{name:'Close pairing',exact:true}).click();
 console.log('PASS runner connection words, dirty save, persisted edits and single active pairing code');
 await page.goto(individual+`/#/p/${p.id}/defaults`);
 await page.getByRole('button',{name:'Capacity',exact:true}).click();
 const capacity = page.getByLabel('Concurrent sessions in this project',{exact:true});
 const saveCapacity = page.getByRole('button',{name:'Save capacity',exact:true});
 assert(await saveCapacity.isDisabled());
 const originalCapacity=Number(await capacity.inputValue());
 const changedCapacity=originalCapacity===2?1:2;
 await capacity.fill(String(changedCapacity)); assert(await saveCapacity.isEnabled()); await saveCapacity.click();
 await page.getByText('Saved',{exact:true}).waitFor(); assert(await saveCapacity.isDisabled());
 assert.equal((await personal('GET',`/projects/${p.id}/orchestration`)).concurrencyLimit,changedCapacity);
 await capacity.fill(''); assert(await saveCapacity.isDisabled());
 await capacity.fill('17'); assert(await saveCapacity.isDisabled());
 await capacity.fill(String(originalCapacity)); await saveCapacity.click(); await page.getByText('Saved',{exact:true}).waitFor();
 console.log('PASS capacity saves only changed, loaded, valid values and persists them');
 const t=await personal('POST',`/projects/${p.id}/tasks`,{title:'Audit task controls'});
 await page.goto(individual+`/#/p/${p.id}/task/${t.id}`);
 await page.getByRole('heading',{name:t.title,exact:true}).waitFor();
 assert.equal(await page.getByRole('button',{name:'Edit task',exact:true}).count(),1);
 assert.equal(await page.getByRole('button',{name:/^Change plan$|^Change how it runs$/}).count(),0);
 await page.getByRole('button',{name:'Edit task',exact:true}).click();
 await page.getByRole('dialog',{name:'Edit task',exact:true}).waitFor();
 const edit = page.getByRole('dialog',{name:'Edit task',exact:true});
 assert(await edit.getByRole('button',{name:'Save',exact:true}).isDisabled(),'unchanged task cannot be saved again');
 await edit.getByRole('textbox',{name:'Title',exact:true}).fill(t.title+' changed');
 assert(await edit.getByRole('button',{name:'Save',exact:true}).isEnabled());
 await page.keyboard.press('Escape');
 await edit.waitFor({ state: 'detached' });
 await page.setViewportSize({width:390,height:844}); await page.goto(individual+`/#/p/${p.id}/board`);
 await page.getByRole('button',{name:'Switch project, current: '+p.name,exact:true}).waitFor();
 await page.locator('.head.deck').waitFor();
 await page.getByRole('button',{name:'Jump to',exact:true}).waitFor({state:'hidden'});
 await page.getByRole('button',{name:'Switch project, current: '+p.name,exact:true}).click();
 const jump = page.getByRole('dialog', { name: 'Jump to', exact: true });
 await jump.waitFor(); await page.keyboard.press('Escape');
 await jump.waitFor({ state: 'detached' });
 console.log('PASS one task edit entry and one working phone project switcher');
 // Presentation fixtures cover connected/disconnected/legacy/offline directory states.
 // Real profile storage, ordinary-member visibility, revocation and freshness are covered by Go tests.
 await page.setViewportSize({width:1440,height:1000});
 await page.addInitScript(value=>sessionStorage.setItem('werkbord-team-token',value),teamToken);
 const members=await coord('GET','/members');
 let connection={runnersConnected:2,runnersDisconnected:1,runnersUnknown:0,phonesConnected:1,phonesPaired:1,phonesUnknown:false};
 await page.route('**/api/team/v1/members',route=>route.fulfill({status:200,contentType:'application/json',body:JSON.stringify(members.map((m,i)=>({...m,connections:i===0?connection:m.connections})))}));
 await page.goto(team+'/?tab=members');
 const member=page.locator('[data-member-id="'+members[0].id+'"]');
 await member.getByText('2 runners connected · 1 runner disconnected',{exact:true}).waitFor();
 await member.getByText('1 phone connected',{exact:true}).waitFor();
 const screenshots=process.env.BROWSER_ARTIFACT_DIR;
 for(const theme of ['light','dark'])for(const [size,width,height]of [['desktop',1440,1000],['phone',390,844]]){
  await page.evaluate(theme=>document.documentElement.dataset.theme=theme,theme);await page.setViewportSize({width,height});
  assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+2),'member directory fits '+size);
  await page.screenshot({path:path.join(screenshots,'member-connections-'+size+'-'+theme+'.png')});
 }
 connection={runnersConnected:0,runnersDisconnected:2,runnersUnknown:0,phonesConnected:0,phonesPaired:1,phonesUnknown:false};
 await page.reload(); await member.getByText('2 runners disconnected',{exact:true}).waitFor();
 await member.getByText('Phone disconnected · access paired',{exact:true}).waitFor();
 connection={runnersConnected:0,runnersDisconnected:0,runnersUnknown:2,phonesConnected:0,phonesPaired:0,phonesUnknown:true};
 await page.reload(); await member.getByText('2 runners · connection unknown',{exact:true}).waitFor();
 await member.getByText('Phone connection unknown',{exact:true}).waitFor();
 await page.evaluate(()=>setOnline(false));
 await member.getByText('Runner connection unknown',{exact:true}).waitFor();
 const touchProject=await coord('POST','/projects',{name:'Touch controls'});
 await page.goto(team+'/?tab=calendar&project='+touchProject.id);
 await page.getByRole('group',{name:'Calendar presentation'}).waitFor();
 const teamTargets=await page.locator('.seg > button:visible').evaluateAll(buttons=>buttons.map(b=>b.getBoundingClientRect().height));
 assert(teamTargets.length>0&&teamTargets.every(height=>height>=44),'Team compact segments retain 44px phone targets');
 await page.goto(individual+'/#/p/'+p.id+'/calendar');
 await page.locator('.seg > button').first().waitFor();
 const personalTargets=await page.locator('.seg > button:visible').evaluateAll(buttons=>buttons.map(b=>b.getBoundingClientRect().height));
 assert(personalTargets.length>0&&personalTargets.every(height=>height>=40),'Personal phone segments retain their touch targets');
 assert.deepEqual(errors,[]);
 console.log('PASS shared segmented controls retain phone touch targets in Personal and Team');
 console.log('PASS Team member connection rendering, stale/legacy states, both themes and phone/desktop widths');
})().catch(async e=>{
 console.error(e); process.exitCode=1;
 if(activePage){
  const dir=process.env.BROWSER_ARTIFACT_DIR;
  if(dir) await activePage.screenshot({path:path.join(dir,'controls-failure.png'),fullPage:true}).catch(()=>{});
  console.error('Control URL:',activePage.url());
  console.error('Control page:',await activePage.locator('body').innerText().catch(()=>'<unavailable>'));
 }
}).finally(async()=>{if(browser)await browser.close();});

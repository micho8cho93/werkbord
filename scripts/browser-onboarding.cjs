// Guided setup against a disposable real controller, including the desktop shell's trusted frame bridge.
const assert = require('node:assert/strict');
const fs = require('node:fs'), os = require('node:os'), path = require('node:path'), http = require('node:http'), net = require('node:net');
const cp = require('node:child_process');
const { once } = require('node:events');
const root = path.resolve(__dirname, '..');
const { chromium } = require(path.join(root, 'web/node_modules/playwright'));
const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'werkbord-onboarding-'));
const artifacts = process.env.BROWSER_ARTIFACT_DIR || path.join(temp, 'artifacts');
fs.mkdirSync(artifacts, {recursive:true});
const token = 'disposable-browser-credential';
const sleep = ms => new Promise(r => setTimeout(r, ms));
async function port() { const s = net.createServer(); s.listen(0, '127.0.0.1'); await once(s,'listening'); const p=s.address().port; await new Promise(r=>s.close(r)); return p; }
let child, browser, server, activePage;
(async () => {
  const base = 'http://127.0.0.1:' + await port(), shell = 'http://127.0.0.1:' + await port();
  const fixture = path.join(temp,'fixture'), repo = path.join(temp,'repo');
  fs.mkdirSync(repo);
  cp.execFileSync('git',['init','-b','main',repo],{stdio:'ignore'});
  cp.execFileSync('git',['-C',repo,'-c','user.name=Fixture','-c','user.email=f@example.test','commit','--allow-empty','-m','init'],{stdio:'ignore'});
  cp.execFileSync('go',['build','-o',fixture,'./scripts/browser-fixture'],{cwd:root,stdio:'inherit'});
  child=cp.spawn(fixture,[],{cwd:root,stdio:['ignore','ignore','inherit'],env:{...process.env,WERKBORD_BROWSER_EXECUTION:'1',WERKBORD_BROWSER_ADDR:new URL(base).host,WERKBORD_BROWSER_SHELL_ORIGIN:shell}});
  for(let i=0;i<150;i++){ if(await fetch(base+'/api/health').then(r=>r.ok).catch(()=>false))break; await sleep(100); }
  async function api(method,p,data){ const r=await fetch(base+'/api'+p,{method,headers:{Authorization:'Bearer '+token,'Content-Type':'application/json'},body:data?JSON.stringify(data):undefined});assert(r.ok, p+' '+r.status);return r.json(); }
  browser=await chromium.launch({headless:true,...(process.env.BROWSER_EXECUTABLE?{executablePath:process.env.BROWSER_EXECUTABLE}:{})});
  const errors=[];
  const ctx=await browser.newContext({viewport:{width:1440,height:1000}});
  const page=await ctx.newPage(); activePage=page;page.setDefaultTimeout(15000);page.on('pageerror',e=>errors.push(e.message));
  await page.goto(base+'/#token='+token);
  await page.getByRole('dialog',{name:'Let’s set up Werkbord',exact:true}).waitFor();
  const intro=page.getByRole('dialog',{name:'Let’s set up Werkbord',exact:true});
  await intro.getByRole('button',{name:'Start guided setup',exact:true}).focus();
  await page.keyboard.press('Tab');
  assert.equal(await intro.locator(':focus').count(),1,'keyboard focus stays inside the setup introduction');
  await page.getByRole('button',{name:'Start guided setup',exact:true}).click();
  const steps=()=>page.getByRole('list',{name:'Setup steps'});
  await page.getByRole('heading',{name:'Connect GitHub for the complete workflow',exact:true}).waitFor();
  assert(await steps().getByRole('button',{name:/9\. Finish/}).isDisabled(),'future steps are locked');
  await page.goto(base+'/#/projects');
  await page.waitForURL('**/#/onboarding');
  await page.goto(base+'/#/help');
  await page.getByRole('link',{name:'Resume setup',exact:true}).waitFor();
  await page.getByRole('link',{name:'Resume setup',exact:true}).click();
  await page.getByRole('button',{name:'Pause setup…',exact:true}).click();
  await page.getByRole('button',{name:'Pause and explore',exact:true}).click();
  await page.getByRole('link',{name:'Resume setup',exact:true}).waitFor();
  assert.equal((await api('GET','/onboarding')).completedAt,undefined,'pausing does not complete onboarding');
  await page.reload();
  await page.getByRole('dialog',{name:'Let’s set up Werkbord',exact:true}).waitFor();
  await page.getByRole('button',{name:'Start guided setup',exact:true}).click();
  await page.getByRole('button',{name:'Continue without GitHub…',exact:true}).click();
  await page.route('**/api/onboarding/variant',r=>r.fulfill({status:503,contentType:'application/json',body:JSON.stringify({error:{code:'unavailable',message:'Could not save local setup'}})}));
  await page.getByRole('button',{name:'Use local-only setup',exact:true}).click();
  await page.getByRole('dialog',{name:'Continue without GitHub?',exact:true}).getByRole('alert').filter({hasText:'Could not save local setup'}).waitFor();
  assert.notEqual((await api('GET','/onboarding')).steps?.github,'skipped','failed path save must not resolve the GitHub step');
  await page.unroute('**/api/onboarding/variant');
  await page.getByRole('button',{name:'Use local-only setup',exact:true}).click();
  await page.getByRole('heading',{name:'Choose where your work will live',exact:true}).waitFor();
  assert.equal(await page.getByRole('button',{name:'Complete setup',exact:true}).count(),0);
  const project=await api('POST','/projects',{path:repo,name:'Setup fixture'});
  await page.reload();
  await page.getByRole('button',{name:'Start guided setup',exact:true}).click();
  await page.getByRole('heading',{name:'Try your first task',exact:true}).waitFor();
  assert.equal((await api('GET','/onboarding')).completedAt,undefined,'adding a repository never silently completes started setup');
  await page.getByRole('button',{name:'Do the example later',exact:true}).click();
  await page.getByRole('dialog',{name:'Leave the example for later?',exact:true}).getByRole('button',{name:'Do the example later',exact:true}).click();
  await page.getByRole('button',{name:'Pair a phone later',exact:true}).click();
  await page.getByRole('heading',{name:'Your setup is ready to finish',exact:true}).waitFor();
  for(const theme of ['light','dark']){
    await page.evaluate(t=>document.documentElement.dataset.theme=t,theme);
    for(const [width,height] of [[1440,1000],[390,844]]){
      await page.setViewportSize({width,height});
      assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+2),false,'no horizontal overflow');
      await page.screenshot({path:path.join(artifacts,`summary-${width}-${theme}.png`),fullPage:true});
    }
  }
  await page.setViewportSize({width:1440,height:1000});
  // A stale success must not unlock completion when a runner recheck fails.
  await page.route('**/api/runners',r=>r.fulfill({status:503,contentType:'application/json',body:JSON.stringify({error:{code:'unavailable',message:'Runner check unavailable'}})}));
  await page.getByRole('button',{name:'Complete setup',exact:true}).click();
  await page.getByRole('alert').filter({hasText:'Runner check unavailable'}).waitFor();
  assert.equal((await api('GET','/onboarding')).completedAt,undefined);
  await page.unroute('**/api/runners');
  await page.getByRole('button',{name:'Check again',exact:true}).click();
  await page.getByRole('heading',{name:'Your setup is ready to finish',exact:true}).waitFor();
  await page.route('**/api/onboarding/complete',r=>r.fulfill({status:503,contentType:'application/json',body:JSON.stringify({error:{code:'unavailable',message:'Completion could not be saved'}})}));
  await page.getByRole('button',{name:'Complete setup',exact:true}).click();
  await page.getByRole('alert').filter({hasText:'Completion could not be saved'}).waitFor();
  await page.unroute('**/api/onboarding/complete');
  await page.getByRole('button',{name:'Complete setup',exact:true}).click();
  await page.getByRole('heading',{name:'You’re ready to use Werkbord',exact:true}).waitFor();
  assert((await api('GET','/onboarding')).completedAt);
  await page.getByRole('button',{name:'Open my workspace',exact:true}).click();
  await page.waitForURL('**/#/p/'+project.id+'/board');
  await page.reload();
  assert.equal(await page.getByRole('dialog',{name:'Let’s set up Werkbord',exact:true}).count(),0);
  console.log('PASS guided setup: ordering, explicit optional choices, pause/reload, repository persistence, readiness and completion errors, themes and narrow browser');

  // A native stand-in hosts the built desktop shell; only the trusted Individual frame may report setup.
  const entry={id:'personal',kind:'personal',name:'Individual',state:'ready',source:'local'};
  const target={id:'personal',kind:'personal',url:base+'/#token='+token,origin:base,place:''};
  const calls={Info:()=>({version:'fixture',platform:'darwin'}),Workspaces:()=>({items:[entry],selected:'personal',problems:[],team:{state:'ready'},teamInstaller:false,invitation:false}),Overview:()=>({entries:[{workspace:entry,summary:{schema:'workspace-summary/v1',workspace:entry,projects:[],work:[],attention:[],schedule:[],at:new Date().toISOString()}}],at:new Date().toISOString()}),LoadWorkspace:()=>target,OpenWorkspace:()=>target,RememberPlace:()=>null,PendingInvitation:()=>'',Relay:()=>null,OpenExternal:()=>null};
  const bridge=`if(window.parent===window)window.go={main:{App:new Proxy({},{get:(_,method)=>async(...args)=>{const r=await fetch('/call',{method:'POST',body:JSON.stringify({method,args})});return r.json()}})}};`;
  server=http.createServer((req,res)=>{
    if(req.url==='/call'){let raw='';req.on('data',b=>raw+=b);req.on('end',()=>{const {method,args}=JSON.parse(raw);res.setHeader('Content-Type','application/json');res.end(JSON.stringify(calls[method]?.(...args)??null));});return;}
    let file=path.join(root,'desktop/frontend/dist',decodeURIComponent(new URL(req.url,shell).pathname));
    if(!fs.existsSync(file)||fs.statSync(file).isDirectory()){res.statusCode=404;res.end();return;}
    const ext=path.extname(file);res.setHeader('Content-Type',({'.html':'text/html','.js':'application/javascript','.css':'text/css','.woff2':'font/woff2'})[ext]||'application/octet-stream');
    const data=fs.readFileSync(file);res.end(ext==='.html'?data.toString().replace('<head>','<head><script>'+bridge+'</script>'):data);
  });server.listen(new URL(shell).port,'127.0.0.1');await once(server,'listening');
  await api('POST','/onboarding/reset',{});
  const shellPage=await ctx.newPage();shellPage.on('pageerror',e=>errors.push(e.message));
  await shellPage.goto(shell+'/shell/shell/index.html');
  const personal=shellPage.frameLocator('iframe[data-workspace=personal]');
  await personal.getByRole('button',{name:'Start guided setup',exact:true}).click();
  await personal.getByRole('heading',{name:'Connect GitHub for the complete workflow',exact:true}).waitFor();
  await shellPage.getByRole('button',{name:'Devices & Hosting',exact:true}).click();
  await personal.getByRole('heading',{name:'Connect GitHub for the complete workflow',exact:true}).waitFor();
  await shellPage.getByRole('button',{name:'Settings',exact:true}).click();
  await shellPage.getByRole('button',{name:'Resume setup',exact:true}).click();
  await personal.getByRole('heading',{name:'Connect GitHub for the complete workflow',exact:true}).waitFor();
  await personal.getByRole('button',{name:'Pause setup…',exact:true}).click();
  await personal.getByRole('button',{name:'Pause and explore',exact:true}).click();
  await shellPage.getByRole('button',{name:'Devices & Hosting',exact:true}).click();
  await shellPage.getByRole('button',{name:'Resume setup',exact:true}).click();
  await personal.getByRole('heading',{name:'Connect GitHub for the complete workflow',exact:true}).waitFor();
  await shellPage.screenshot({path:path.join(artifacts,'desktop-shell-setup.png')});
  assert.deepEqual(errors,[]);
  console.log('PASS desktop shell: opens setup automatically, holds ordinary navigation, pauses honestly and resumes from the persistent reminder');
  await ctx.close();
})().catch(async e=>{console.error(e); if(activePage){console.error((await activePage.locator('body').innerText()).slice(0,3000));await activePage.screenshot({path:path.join(artifacts,'failure.png')});} process.exitCode=1;}).finally(async()=>{await browser?.close();server?.close();child?.kill('SIGTERM');console.log('Browser artifacts: '+artifacts);});

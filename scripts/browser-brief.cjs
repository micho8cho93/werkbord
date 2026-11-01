// Work's Overview, and the brief written inside its card by a frame of the person's own Werkbord (a stand-in for the native app, the
// real Individual page and the scripted Salie of the browser fixture).
const fs=require('node:fs'),http=require('node:http'),path=require('node:path'),os=require('node:os'),cp=require('node:child_process'),{once}=require('node:events');
const root=require('node:path').resolve(__dirname,'..');
const { chromium }=require(process.env.PLAYWRIGHT_MODULE||root+'/web/node_modules/playwright');
const assert=require('node:assert/strict');
const dist=root+'/desktop/frontend/dist', O=(process.env.BROWSER_ARTIFACT_DIR||require('node:os').tmpdir())+'/', token='disposable-browser-credential';
const shellPort=17733, indPort=17732, shellOrigin='http://127.0.0.1:'+shellPort, ind='http://127.0.0.1:'+indPort, api0=ind;
const sleep=ms=>new Promise(r=>setTimeout(r,ms));

const BRIDGE = `if (window.parent === window && !window.go) window.go = { main: { App: new Proxy({}, { get: (_, method) => async (...args) => { const r = await fetch('/fixture/call', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ Method: method, Args: args }) }); const d = await r.json(); if (!r.ok) throw Error(d.error); return d; } }) } };
window.__frameMessages = []; window.addEventListener('message', e => { try { window.__frameMessages.push(JSON.stringify(e.data)); } catch (_) {} });`;
(async()=>{
 const tmp=fs.mkdtempSync(path.join(os.tmpdir(),'shot-'));const repo=path.join(tmp,'repo');fs.mkdirSync(repo);
 cp.execFileSync('git',['-C',repo,'init','-b','main'],{stdio:'ignore'});cp.execFileSync('git',['-C',repo,'-c','user.name=F','-c','user.email=f@e.test','commit','--allow-empty','-m','init'],{stdio:'ignore'});
 const fx=path.join(tmp,'fixture');cp.execFileSync('go',['build','-o',fx,'./scripts/browser-fixture'],{cwd:root,stdio:'inherit'});
 const child=cp.spawn(fx,[],{cwd:root,stdio:'ignore',env:{...process.env,GIT_CONFIG_GLOBAL:'/dev/null',WERKBORD_BROWSER_ADDR:'127.0.0.1:'+indPort,WERKBORD_BROWSER_EXECUTION:'1',WERKBORD_BROWSER_ASSISTANT:'1',WERKBORD_BROWSER_SHELL_ORIGIN:shellOrigin}});
 for(let i=0;i<100&&!(await fetch(ind+'/api/health').then(r=>r.ok).catch(()=>false));i++)await sleep(100);
 const H={Authorization:'Bearer '+token,'Content-Type':'application/json'};
 await fetch(ind+'/api/onboarding/complete',{method:'POST',headers:H,body:'{}'});
 const P=await (await fetch(ind+'/api/projects',{method:'POST',headers:H,body:JSON.stringify({path:repo,name:'Window project'})})).json();
 for(const t of ['Remember the last email on sign-in','Add retry to the upload worker','Document the release checklist','Fix flaky calendar test'])await fetch(ind+'/api/projects/'+P.id+'/tasks',{method:'POST',headers:H,body:JSON.stringify({title:t,description:'Describe what the agent should change.'})});
 await fetch(ind+'/api/onboarding/tours/seen',{method:'POST',headers:H,body:JSON.stringify({surface:'desktop'})});

 const g=async p=>(await fetch(api0+p,{headers:{Authorization:'Bearer '+token}})).json();
 const projs=(await g('/api/projects')).projects||[];
 const now=new Date().toISOString();const pj=projs[0]||{id:'p',name:'Fixture repo'};
 const A=[{id:'a1',kind:'needs_input',severity:'attention',title:'Add retry to the sync client',detail:'Commit the fixture work?',project:pj.name,href:'#/p/'+pj.id+'/task/x1',at:now,question:{id:'q1',kind:'decision',prompt:'Commit the fixture work?',options:['Yes','No'],allowFreeText:true,askedAt:now,agent:'Codex'}},{id:'a2',kind:'failed',severity:'error',title:'Migrate the runs table',detail:'The agent stopped with an error.',project:pj.name,href:'#/p/'+pj.id+'/task/x2',at:now}];
 const Q=[{id:'t1',title:'Add retry to the sync client',project:pj.name,href:'#/p/'+pj.id+'/task/x3',group:'now',detail:'Needs your answer'},{id:'t2',title:'Migrate the runs table',project:pj.name,href:'#/p/'+pj.id+'/task/x4',group:'now',detail:'Failed'},{id:'t3',title:'Dark mode audit',project:pj.name,href:'#/p/'+pj.id+'/task/x5',group:'next'},{id:'t4',title:'Document the export format',project:pj.name,href:'#/p/'+pj.id+'/task/x6',group:'later',detail:'Waiting on Migrate the runs table'}];
 const entry=(id,kind,name)=>({id,kind,name,state:'ready'});
 const items=[{...entry('personal','personal','Individual'),source:'local'}];
 const summary=e=>({schema:'workspace-summary/v1',workspace:e,projects:projs.map(p=>({id:p.id,name:p.name,href:'#/p/'+p.id+'/work'})),work:[],queue:Q,attention:A});
 const calls={
  Premium:()=>({active:false,canCheck:false,buyUrl:''}), Info:()=>({version:'fixture',platform:'darwin'}),
  Workspaces:()=>({items,selected:'personal',problems:null,team:{state:'ready'},teamInstaller:false,invitation:false}),
  Overview:()=>({entries:items.map(i=>({workspace:entry(i.id,i.kind,i.name),summary:summary(entry(i.id,i.kind,i.name))})),at:new Date().toISOString()}),
  LoadWorkspace:id=>({id,kind:'personal',url:ind+'/#token='+token,origin:ind,place:''}),
  OpenWorkspace:id=>({id,kind:'personal',url:ind+'/#token='+token,origin:ind,place:''}),
  AssistantWorkspaces:()=>[], RememberPlace:()=>null, Relay:(_i,m)=>m==='Info'?calls.Info():null, PendingInvitation:()=>'', OpenExternal:()=>null };
     const types = { '.html': 'text/html; charset=utf-8', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml', '.woff2': 'font/woff2', '.woff': 'font/woff' };
    const server = http.createServer((req, res) => {
        const url = new URL(req.url, shellOrigin);
        if (req.method === 'POST' && url.pathname === '/fixture/call') {
            let body = ''; req.on('data', d => body += d); req.on('end', async () => {
                const { Method, Args } = JSON.parse(body || '{}');
                const f = calls[Method];
                res.setHeader('Content-Type', 'application/json');
                if (!f) { res.statusCode = 404; return res.end(JSON.stringify({ error: 'no ' + Method })); }
                try { res.end(JSON.stringify(await f(...(Args || [])) ?? null)); }
                catch (e) { res.statusCode = 500; res.end(JSON.stringify({ error: e instanceof Error ? e.message : String(e) })); }
            });
            return;
        }
        if (url.pathname === '/team-stub.html') {
            res.setHeader('Content-Type', 'text/html; charset=utf-8');
            return res.end('<!doctype html><meta charset=utf-8><title>Team</title><body style="font:14px system-ui;padding:24px"><h1>Northstar (a stand-in for Team)</h1><label>Draft <input id="draft" placeholder="a half-written note"></label><script>window.__lastPlace="";addEventListener("message",e=>{if(e.data?.type==="werkbord.navigate")window.__lastPlace=e.data.href});parent.postMessage({type:"werkbord.frame",event:"ready"},"*")</script>');
        }
        let file = path.join(dist, decodeURIComponent(url.pathname));
        if (!file.startsWith(dist)) { res.statusCode = 403; return res.end(); }
        if (fs.existsSync(file) && fs.statSync(file).isDirectory()) file = path.join(file, 'index.html');
        if (!fs.existsSync(file)) { res.statusCode = 404; return res.end('not found'); }
        let data = fs.readFileSync(file);
        res.setHeader('Content-Type', types[path.extname(file)] || 'application/octet-stream');
        if (url.pathname === '/shell/shell/index.html') data = Buffer.from(String(data).replace('<head>', '<head><script>' + BRIDGE + '</script>'));
        res.end(data);
    });
    server.listen(shellPort, '127.0.0.1'); await once(server, 'listening');


 fs.mkdirSync(O,{recursive:true});
 const br=await chromium.launch();
 const ctx=await br.newContext({viewport:{width:1440,height:900}});
 const page=await ctx.newPage(); const errors=[]; page.on('pageerror',e=>errors.push(e.message));
 try {
  await page.goto(shellOrigin+'/shell/shell/index.html');
  // Work opens on its Overview: where things stand, what waits for the person, and the catch-up.
  const tabs=page.getByRole('navigation',{name:'Work sections'});
  await tabs.getByRole('button',{name:'Overview',exact:true}).waitFor();
  assert.equal(await tabs.locator('[aria-current=page]').innerText(),'Overview');
  await page.getByTestId('work-stats').waitFor();
  assert.equal(await page.getByTestId('work-stats').locator('li').count(),5,'five numbers');
  const brief=page.getByTestId('work-brief'); await brief.scrollIntoViewIfNeeded();
  console.log('PASS Work opens on Overview with where things stand');

  // A brief is written in the card: Salie is not opened, and what she wrote never reaches this page.
  await brief.getByRole('button',{name:'Last day',exact:true}).click();
  const frame=page.frameLocator('[data-testid=brief-frame]');
  await frame.getByText('Reply finished.').waitFor({timeout:30000});
  assert.equal(await page.getByTestId('assistant-frame').isVisible().catch(()=>false),false,'Salie did not open');
  assert((await page.locator('iframe[data-testid=brief-frame]').evaluate(el=>el.getBoundingClientRect().height))>20,'the card grows to the write-up');
  const text=await page.evaluate(()=>document.body.innerText);
  assert(!text.includes('Reply finished'),'the window page never holds the write-up');
  await brief.getByRole('button',{name:'Last day',exact:true}).waitFor();
  assert.equal(await brief.getByRole('button',{name:'Last day',exact:true}).isDisabled(),false,'asking again is possible once it is written');
  await page.screenshot({path:O+'work-overview-brief.png'});
  console.log('PASS the brief is written inside the Work card, not in Salie, and stays in its frame');

  // Queue & Plans: the queue, and the plans beside it.
  await tabs.getByRole('button',{name:/^Queue/}).click();
  for (const name of ['Now','Next','Later']) await page.getByRole('heading',{name,exact:true}).waitFor();
  await page.getByRole('heading',{name:'Plans',exact:true}).waitFor();
  await page.screenshot({path:O+'work-queue-plans.png'});
  // The tab is remembered while the window stays open.
  await tabs.getByRole('button',{name:'Overview',exact:true}).click();
  await page.getByTestId('work-stats').waitFor();
  assert.deepEqual(errors,[],'no page errors');
  console.log('PASS Queue & Plans holds the queue with the plans beside it');
 } catch(e){ await page.screenshot({path:O+'work-brief-failure.png'}).catch(()=>{}); throw e; }
 finally { await br.close(); child.kill(); server.close(); }
 process.exit(0);
})().catch(e=>{console.error(e);process.exit(1)});

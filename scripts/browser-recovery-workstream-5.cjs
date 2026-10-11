// Recovery behavior uses only an explicitly identified, seeded disposable fixture.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || '../web/node_modules/playwright');
const base = process.env.PERSONAL_BROWSER_URL;
if (!base) throw Error('PERSONAL_BROWSER_URL must explicitly target the disposable fixture');
const artifacts = process.env.BROWSER_ARTIFACT_DIR;
if (!artifacts) throw Error('BROWSER_ARTIFACT_DIR is required');
fs.mkdirSync(artifacts, { recursive: true });
const token = 'disposable-browser-credential';
const api = async (route) => {
  const res = await fetch(base + route, { headers: { Authorization: `Bearer ${token}` } });
  assert(res.ok, `${route}: ${res.status}`); return res.json();
};
(async () => {
  assert.equal((await api('/api/health')).version, 'browser-fixture');
  const { projects } = await api('/api/projects');
  const work = projects.find(p => p.name === 'Venue search');
  const waiting = projects.find(p => p.name === 'Fixture repo');
  const review = projects.find(p => p.name === 'Second repo');
  assert(work && waiting && review, 'guarded seed is required');
  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, reducedMotion: 'reduce', serviceWorkers: 'block' });
  const page = await context.newPage(); page.setDefaultTimeout(10000);
  const errors = []; page.on('pageerror', e => errors.push(e.message));
  const capture = async name => {
    for (const theme of ['light', 'dark']) {
      await page.evaluate(t => document.documentElement.dataset.theme = t, theme);
      for (const [size, width, height] of [['desktop',1440,900], ['phone',375,812]]) {
        await page.setViewportSize({width,height});
        if (size==='phone') {
          const target = name.startsWith('health-') ? page.locator('.health .head .btn') : name==='task-boundary' ? page.getByRole('button',{name:'Try again',exact:true}) : name==='answer-uncertain' ? page.getByRole('button',{name:'Retry same answer'}) : null;
          if(target) assert((await target.boundingBox()).height>=44,`${name} phone recovery target`);
        }
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1), false, `${name}/${size}/${theme} overflow`);
        if (name === 'answer-uncertain') {
          const retry = page.getByRole('button',{name:'Retry same answer'});
          await retry.scrollIntoViewIfNeeded();
          const rect=await retry.boundingBox(); assert(rect.y>=0 && rect.y+rect.height<=height+1,'retry is fully reachable');
        }
        await page.screenshot({path:path.join(artifacts, `${name}-${size}-${theme}.png`)});
      }
    }
  };
  const openTask = async project => {
    await page.goto(`${base}/#/p/${project.id}/board`);
    await page.getByRole('button',{name:'New task',exact:true}).click();
    return page.getByRole('dialog',{name:`New task in ${project.name}`,exact:true});
  };
  try {
    await page.goto(`${base}/#token=${token}`); await page.waitForFunction(() => !location.hash.includes('token='));
    const healthPath = `**/projects/${review.id}/git/health{,/**}`;
    let healthState = 'success';
    const healthy = { projectId:review.id,summary:{state:'healthy',headline:'Healthy',counts:{info:0,attention:0,risk:0,critical:0},needsAttention:0,dismissed:0,score:100},findings:[],dismissed:[],resolved:[],check:{projectId:review.id,checkedAt:new Date().toISOString(),durationMs:1} };
    const longError = 'Zugriff verweigert — 接続できません — تعذر الاتصال. ' + 'Repository/path-without-spaces/'.repeat(14);
    await page.route(healthPath, route => {
      if (healthState === 'unreachable') return route.abort();
      const success = ['success','read-failure','changed'].includes(healthState);
      const body = success ? { ...healthy, ...(healthState === 'read-failure' ? {check:{...healthy.check,error:longError}} : {}), ...(healthState === 'changed' ? {summary:{...healthy.summary,headline:'Fresh controlled Git evidence',score:98}} : {}) } : {error:{code:healthState === 'disabled' ? 'unavailable' : 'http_error',message:healthState === 'disabled' ? 'repository health is not enabled' : longError}};
      return route.fulfill({status:success?200:503,contentType:'application/json',body:JSON.stringify(body)});
    });
    await page.goto(`${base}/#/p/${review.id}/git?health`); await page.reload();
    await page.locator('.health .headline').filter({hasText:'Healthy'}).waitFor();
    await capture('health-success');
    for (const state of ['read-failure','failure','disabled','unknown','unreachable','changed']) {
      healthState = state;
      await page.locator('.health').getByRole('button',{name:'Check now'}).click();
      await page.locator('.health').getByRole('button',{name:'Check now'}).waitFor();
      const expected = state === 'disabled' ? 'Checks are disabled' : state === 'changed' ? 'Fresh controlled Git evidence' : 'Current health is unknown';
      await page.locator('.health .headline').filter({hasText:expected}).waitFor();
      if(state==='disabled') { assert.match(await page.locator('.health').innerText(), /There is no switch/); assert.equal(await page.locator('.health a').count(),0); }
      await capture('health-'+state);
    }
    await page.unroute(healthPath);
    // Offline entry loads the local shell but never fabricates a current controller state.
    const offlineContext = await browser.newContext({serviceWorkers:'block',reducedMotion:'reduce'});
    const offlinePage = await offlineContext.newPage();
    await offlinePage.route('**/api/**', route => route.abort());
    await offlinePage.goto(`${base}/#token=${token}`);
    await offlinePage.getByRole('button',{name:'Reconnect',exact:true}).waitFor();
    assert.match(await offlinePage.locator('.connection-note').innerText(),/Displayed status may be outdated/);
    await offlinePage.screenshot({path:path.join(artifacts,'offline-entry.png')});
    await offlinePage.unroute('**/api/**');
    await offlinePage.getByRole('button',{name:'Reconnect',exact:true}).click();
    await offlinePage.waitForFunction(()=>!document.querySelector('.connection-note'));
    await offlineContext.close();
    // Render-boundary retry fetches fresh evidence and never repeats a mutation.
    const waitingTask=(await api(`/api/projects/${waiting.id}/tasks`)).tasks.find(t=>t.state==='doing');
    let corrupted=true, boundaryReads=0;
    const runList=`**/projects/${waiting.id}/runs`;
    await page.route(runList,async route=>{boundaryReads++;const response=await route.fetch();const data=await response.json();if(corrupted)for(const run of data.runs)run.pendingRequests='controlled malformed evidence';await route.fulfill({response,json:data});});
    await page.goto(`${base}/#/p/${waiting.id}/task/${waitingTask.id}`); await page.reload();
    await page.getByRole('heading',{name:'This task could not be shown'}).waitFor();
    await capture('task-boundary');
    const beforeRetry=boundaryReads;corrupted=false;
    await page.getByRole('button',{name:'Try again',exact:true}).click();
    await page.getByRole('heading',{name:waitingTask.title,exact:true}).waitFor();
    assert(boundaryReads>beforeRetry,'boundary retry refreshes controller evidence');
    await page.unroute(runList);
    // Draft survives disconnect, close and reload; validation rejection remains retryable.
    let dialog = await openTask(work);
    const title = () => dialog.locator('#new-task-form input.big');
    await title().fill('Recoverable brief'); await dialog.locator('textarea').first().fill(longError);
    await dialog.locator('#new-task-form > details > summary').click();
    await dialog.locator('#new-task-start').fill('2026-10-12');
    await dialog.locator('#new-task-end').fill('2026-10-14');
    await context.setOffline(true); await page.keyboard.press('Escape'); await context.setOffline(false);
    await page.reload(); dialog = await openTask(work);
    assert.equal(await title().inputValue(),'Recoverable brief'); assert.equal(await dialog.locator('textarea').first().inputValue(),longError);
    assert.equal(await dialog.locator('#new-task-start').inputValue(),'2026-10-12');
    assert.equal(await dialog.locator('#new-task-end').inputValue(),'2026-10-14');
    let saves = 0;
    const tasksPath = `**/projects/${work.id}/tasks`;
    await page.route(tasksPath, route => {
      if(route.request().method()!=='POST') return route.continue(); saves++;
      return route.fulfill({status:400,contentType:'application/json',body:JSON.stringify({error:{code:'invalid',message:longError}})});
    });
    await dialog.getByRole('button',{name:'Add to Backlog'}).click();
    await dialog.locator('.error').waitFor(); assert.equal(await dialog.getByRole('button',{name:'Add to Backlog'}).isDisabled(),false);
    await capture('new-task-rejected'); await page.unroute(tasksPath);
    await title().fill('Lost task response');
    await page.route(tasksPath, async route => {
      if(route.request().method()!=='POST') return route.continue(); saves++;
      await route.fetch(); return route.abort('failed'); // save committed, response lost
    });
    await dialog.getByRole('button',{name:'Add to Backlog'}).click();
    await dialog.getByText(/Could not confirm whether the task was saved/).waitFor();
    assert.equal(await dialog.getByRole('button',{name:'Add to Backlog'}).isDisabled(),true);
    await capture('new-task-uncertain'); await page.reload(); dialog = await openTask(work);
    assert.equal(await title().inputValue(),'Lost task response');
    assert.equal(await dialog.getByRole('button',{name:'Add to Backlog'}).isDisabled(),true);
    await page.keyboard.press('Control+Enter'); assert.equal(saves,2,'reload and shortcut never replay an uncertain save');
    assert.equal((await api(`/api/projects/${work.id}/tasks`)).tasks.filter(t=>t.title==='Lost task response').length,1);
    await dialog.getByRole('link',{name:'Check Board'}).click(); await page.unroute(tasksPath);
    // Quick add has the same guard and keeps its draft through reload.
    await page.getByRole('button',{name:'Add a task to Backlog'}).click();
    await page.locator('#quick-add').fill('Lost quick response');
    let quickSaves=0;
    await page.route(tasksPath,async route=>{if(route.request().method()!=='POST')return route.continue();quickSaves++;await route.fetch();return route.abort();});
    await page.locator('.quick button[type=submit]').click(); await page.locator('.quick .error').waitFor();
    await page.reload(); await page.locator('#quick-add').waitFor();
    assert.equal(await page.locator('#quick-add').inputValue(),'Lost quick response');
    assert.equal(await page.locator('.quick button[type=submit]').isDisabled(),true); assert.equal(quickSaves,1);
    await capture('quick-uncertain'); await page.unroute(tasksPath);
    // Controlled free-text availability exercises the real question with a typed draft.
    const q=(await api(`/api/projects/${waiting.id}/questions`)).questions[0]; assert(q);
    await page.route('**/questions',async route=>{const res=await route.fetch();const data=await res.json();for(const question of data.questions??[])question.allowFreeText=true;await route.fulfill({response:res,json:data});});
    await page.goto(`${base}/#/p/${waiting.id}/task/${q.taskId}`); await page.reload();
    const card=page.locator('.ask[data-kind]').first(); const answer=card.locator('input');
    await answer.fill('Typed answer preserved'); await page.reload(); assert.equal(await answer.inputValue(),'Typed answer preserved');
    await context.setOffline(true); await capture('answer-offline'); await context.setOffline(false);
    let answers=0;
    const answerPath=`**/questions/${q.id}/answer`;
    await page.route(answerPath,async route=>{answers++;return route.abort();});
    await card.getByRole('button',{name:'Yes',exact:true}).click();
    await card.getByRole('button',{name:'Retry same answer'}).waitFor();
    assert.equal(await card.getByRole('button',{name:'No',exact:true}).isDisabled(),true);
    await page.reload(); assert.equal(await answer.inputValue(),'Typed answer preserved');
    await card.getByRole('button',{name:'Retry same answer'}).focus(); await page.keyboard.press('Tab');
    assert.equal(await page.evaluate(()=>document.activeElement?.tagName!=='BODY'),true);
    await capture('answer-uncertain'); await page.unroute(answerPath);
    // A successful controller answer is deliberately lost, then the same answer is retried.
    await page.route(answerPath,async route=>{answers++;await route.fetch();return route.abort();});
    await card.getByRole('button',{name:'Retry same answer'}).click();
    await page.unroute(answerPath); await page.reload();
    const settled=await api(`/api/projects/${waiting.id}/questions/${q.id}`);
    assert.equal(settled.state,'answered'); assert.equal(settled.answer,'Yes'); assert.equal(answers,2);
    // Repeating the existing controller request returns the settled question; never enqueues another answer.
    const repeat=await fetch(`${base}/api/projects/${waiting.id}/questions/${q.id}/answer`,{method:'POST',headers:{Authorization:`Bearer ${token}`,'Content-Type':'application/json'},body:JSON.stringify({answer:'Yes'})});
    assert([200,409].includes(repeat.status));
    const repeated=await api(`/api/projects/${waiting.id}/questions/${q.id}`);
    assert.equal(repeated.answer,settled.answer);assert.equal(repeated.answeredAt,settled.answeredAt);
    await page.unroute('**/questions');
    // Coarse pointer + reduced motion is a browser emulation, not a physical-device observation.
    const touch=await browser.newContext({viewport:{width:375,height:812},hasTouch:true,isMobile:true,reducedMotion:'reduce',storageState:await context.storageState()});
    const phone=await touch.newPage(); await phone.goto(`${base}/#/p/${work.id}/board`); await phone.getByRole('button',{name:'New task',exact:true}).click();
    assert(await phone.evaluate(()=>matchMedia('(pointer: coarse)').matches && matchMedia('(prefers-reduced-motion: reduce)').matches));
    const target=await phone.getByRole('button',{name:'Add to Backlog'}).boundingBox();assert(target.height>=44);
    await phone.screenshot({path:path.join(artifacts,'coarse-new-task.png')});await touch.close();
    assert.deepEqual(errors,[]); console.log('PASS workstream 5 health, drafts, uncertain mutations, reconnect and duplicate guards');
  } finally { await browser.close(); }
})().catch(e=>{console.error(e);process.exitCode=1;});

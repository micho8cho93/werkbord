// First-use onboarding against a disposable controller and genuine scripted runner.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const base = process.env.PERSONAL_BROWSER_URL;
const owner = 'disposable-browser-credential';
async function request(token, method, route, body) {
  const response = await fetch(base + route, {method, headers:{Authorization:'Bearer '+token,'Content-Type':'application/json'},body:body === undefined ? undefined : JSON.stringify(body)});
  const raw = await response.text(); assert(response.ok, `${method} ${route}: ${raw}`); return raw ? JSON.parse(raw) : null;
}
(async () => {
  const repoPath = fs.realpathSync(process.env.WERKBORD_BROWSER_REPO);
  const project = (await request(owner,'GET','/api/projects')).projects.find(p => p.repoPath === repoPath) || await request(owner,'POST','/api/projects',{path:repoPath,name:'Onboarding test repository'});
  await request(owner,'POST','/api/onboarding/example',{projectId:project.id});
  const browser = await chromium.launch({headless:true});
  try {
    for (const mode of ['navigation','live','clean','preview','skip']) {
      const code = await request(owner,'POST','/api/phones/codes',{});
      const phone = await request('','POST','/api/phones/enroll',{code:code.code,name:'Onboarding '+mode});
      const ctx = await browser.newContext({viewport:{width:390,height:844},isMobile:true,hasTouch:true});
      await ctx.addInitScript(token => localStorage.setItem('devboard.token',token),phone.token);
      const page = await ctx.newPage(); page.setDefaultTimeout(10000);
      const errors = []; page.on('pageerror',e => errors.push(e.message));
      let starts = 0; page.on('request',r => { if (new URL(r.url()).pathname === '/api/deck/workflows' && r.method() === 'POST') starts++; });
      const guide = () => page.getByRole('region',{name:'Control Deck onboarding'});
      const dialog = () => page.getByRole('dialog');
      const socket = i => page.locator(`[data-deck-position="${i}"] .key-slot`);
      // A busy key is disabled and a disabled key cannot take focus, so wait until it is enabled and focused before pressing anything.
      const focusSocket = i => page.waitForFunction(i => { const el = document.querySelector(`[data-deck-position="${i}"] .key-slot`); if (!el || el.disabled) return false; el.focus(); return document.activeElement === el; }, i);
      try {
        await page.goto(base+'/#/deck');
        await guide().getByRole('heading',{name:'Make the Control Deck yours'}).waitFor();
        assert.equal(await page.locator('[data-deck-position] [data-key]').count(),6);
        assert.equal(await page.getByRole('button',{name:'Empty slot. Add a button',exact:true}).count(),3);
        if (mode === 'live') {
          for (const [width,colorScheme] of [[320,'light'],[430,'dark']]) {
            await page.setViewportSize({width,height:844}); await page.emulateMedia({colorScheme});
            assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1));
            if (process.env.BROWSER_ARTIFACT_DIR) await page.screenshot({path:path.join(process.env.BROWSER_ARTIFACT_DIR,`onboarding-intro-${width}-${colorScheme}.png`),fullPage:true});
          }
          await page.setViewportSize({width:390,height:844}); await page.emulateMedia({colorScheme:'light'});
        }
        await guide().getByRole('button',{name:'Choose buttons'}).click();
        assert.equal(await dialog().getByRole('button',{name:'Needs you',exact:true}).count(),0);
        await dialog().getByRole('button',{name:'Reviews',exact:true}).click();
        if (mode === 'navigation') {
          for (const name of ['Git','Runners']) {
            await guide().getByRole('button',{name:'Add a button',exact:true}).click();
            await dialog().getByRole('button',{name,exact:true}).click();
          }
          await guide().getByRole('button',{name:'Continue',exact:true}).click();
          await guide().getByRole('button',{name:'Finish onboarding',exact:true}).click();
          await page.waitForFunction(() => !document.querySelector('[data-testid=edit-deck]').disabled);
          await page.reload();
          await page.waitForFunction(() => !document.querySelector('[data-testid=edit-deck]').disabled);
          const saved = await request(phone.token,'GET','/api/deck');
          assert.deepEqual(saved.slots.slice(6).map(s=>s.button),['reviews','git','runners']);
          assert.equal(starts,0);
          assert.equal(await guide().count(),0);
          continue;
        }
        await page.waitForFunction(() => !document.querySelector('[aria-label="Control Deck onboarding"] .actions button:last-child').disabled);
        await guide().getByRole('button',{name:'Add a button',exact:true}).click();
        await dialog().getByRole('button',{name:/^Security Audit/}).click();
        await guide().getByRole('button',{name:'Continue',exact:true}).click();
        await page.reload();
        await guide().getByText('Press and hold a workflow button to change its settings.').waitFor();
        await focusSocket(7); await page.keyboard.press('Shift+F10');
        assert.equal(await dialog().getByRole('combobox',{name:'Repository target',exact:true}).inputValue(),'ask','new workflows ask on tap');
        await dialog().getByRole('button',{name:'Save configuration'}).click();
        assert.equal(starts,0,'saving never executes');
        if (mode === 'live' && process.env.BROWSER_ARTIFACT_DIR) {
          await guide().getByRole('button',{name:'Change repository or settings'}).click();
          await page.screenshot({path:path.join(process.env.BROWSER_ARTIFACT_DIR,'onboarding-settings.png'),fullPage:true});
          await page.keyboard.press('Escape');
        }
        if (mode !== 'skip') {
        await guide().getByRole('button',{name:'Choose and configure Find Bugs'}).click();
        await dialog().getByRole('heading',{name:'Configure Find Bugs'}).waitFor();
        assert.equal(await dialog().getByRole('combobox',{name:'Agent',exact:true}).inputValue(),'');
        assert.equal(await dialog().getByRole('combobox',{name:'Model',exact:true}).inputValue(),'');
        await dialog().getByRole('button',{name:'Save configuration'}).click();
        assert.equal(await page.locator('[data-deck-position] [data-key]').count(),9,'at most three additions');
        }
        if (mode === 'live' || mode === 'clean') {
          await socket(8).click();
          await dialog().getByRole('heading',{name:'Run Find Bugs',exact:true}).waitFor();
          assert.equal(starts,0,'tap opens the repository sheet without executing');
          await dialog().getByRole('checkbox',{name:new RegExp('^'+project.name)}).check();
          await dialog().getByRole('button',{name:'Run Find Bugs',exact:true}).click();
          await guide().getByRole('heading',{name:'Inspect execution'}).waitFor();
          assert.equal(starts,1);
          await ctx.setOffline(true); await ctx.setOffline(false); await page.reload();
          await guide().getByRole('heading',{name:'Inspect execution'}).waitFor();
          await page.getByTestId('workflow-banner').getByRole('button',{name:'Open Find Bugs execution details'}).click();
          await dialog().getByRole('heading',{name:'Find Bugs execution'}).waitFor();
          await dialog().getByRole('button',{name:'Stop workflow',exact:true}).waitFor();
          if (mode === 'clean') {
            const history = await request(phone.token,'GET','/api/deck/history');
            const runs = await request(phone.token,'GET',`/api/projects/${project.id}/tasks/${history[0].taskId}/runs`);
            await request(phone.token,'POST',`/api/projects/${project.id}/runs/${runs.runs[0].id}/input`,{text:'workflow-clean'});
            await dialog().getByText('Completed · '+project.name,{exact:true}).waitFor();
            await dialog().locator('summary').filter({hasText:'Generate findings report'}).click();
            await dialog().getByText(/No bugs found in this scan/).waitFor();
          } else {
            await dialog().getByRole('button',{name:'Stop workflow',exact:true}).click();
            await dialog().getByText('Cancelled · '+project.name,{exact:true}).waitFor();
          }
          await page.keyboard.press('Escape');
        } else if (mode === 'preview') {
          // Server refusal of the default provider is a real unavailable-execution path.
          await page.route('**/api/deck/workflows',route => route.fulfill({status:422,contentType:'application/json',body:JSON.stringify({error:{code:'model_unavailable',message:'Agent model unavailable'}})}));
          await socket(8).click();
          await dialog().getByRole('checkbox',{name:new RegExp('^'+project.name)}).check();
          await dialog().getByRole('button',{name:'Run Find Bugs',exact:true}).click();
          await guide().getByRole('button',{name:'Try interactive preview'}).click();
          await socket(8).click();
          await page.reload(); // preview identity survives browser reconnection
          await page.getByRole('button',{name:'Interactive preview · Open execution details'}).click();
          await dialog().getByText('Preview only. No agent is running and no repository has been scanned.').waitFor();
          assert.equal(starts,1,'preview never adds an execution request');
          if (process.env.BROWSER_ARTIFACT_DIR) await page.screenshot({path:path.join(process.env.BROWSER_ARTIFACT_DIR,'onboarding-preview-detail.png'),fullPage:true});
          await dialog().getByRole('button',{name:'Stop preview'}).click();
        } else {
          await guide().getByRole('button',{name:'Skip execution for now'}).click(); assert.equal(starts,0);
        }
        await guide().getByText('Tap to run. Hold to configure. Use the pencil icon at the top to edit your deck.').waitFor();
        if (process.env.BROWSER_ARTIFACT_DIR) await page.screenshot({path:path.join(process.env.BROWSER_ARTIFACT_DIR,`onboarding-${mode}.png`),fullPage:true});
        await guide().getByRole('button',{name:'Finish onboarding'}).click();
        await page.waitForFunction(() => { const button = document.querySelector('[data-testid="edit-deck"]'); return button && !button.disabled; });
        await page.reload();
        await page.waitForFunction(() => { const button = document.querySelector('[data-testid="edit-deck"]'); return button && !button.disabled; });
        assert.equal(await guide().count(),0,'returning users are not forced through onboarding');
        const saved = await request(phone.token,'GET','/api/deck');
        const savedWorkflow = saved.slots[mode === 'skip' ? 7 : 8];
        assert.equal(savedWorkflow.button,mode === 'skip' ? 'security-audit' : 'find-bugs'); assert.equal(savedWorkflow.target,'ask'); assert.equal(savedWorkflow.projectIds,undefined);
        assert.equal(savedWorkflow.agent ?? '',''); assert.equal(savedWorkflow.model ?? '','');
        if (mode === 'skip') assert.equal(saved.slots[8],null,'unused slot remains empty');
        assert.equal(await page.getByText('All clear',{exact:true}).count(),0);
        for (const [button,hash] of [['reviews','#/reviews']]) {
          await page.locator(`[data-key="${button}"]`).click(); await page.waitForURL('**/'+hash);
          await page.goto(base+'/#/deck');
        }
        assert.deepEqual(errors,[]);
      } finally { await ctx.close(); await request(owner,'DELETE','/api/phones/'+phone.id); }
    }
    // A phone with no registered repository can still learn the controls honestly.
    const code = await request(owner,'POST','/api/phones/codes',{});
    const phone = await request('','POST','/api/phones/enroll',{code:code.code,name:'No repository preview'});
    const ctx = await browser.newContext({viewport:{width:390,height:844}});
    try {
      await ctx.addInitScript(token => localStorage.setItem('devboard.token',token),phone.token);
      const page = await ctx.newPage();
      await page.route('**/api/projects',route => route.request().method() === 'GET' ? route.fulfill({json:{projects:[]}}) : route.continue());
      let firstDeck = true;
      await page.route('**/api/deck',route => {
        if (firstDeck && route.request().method() === 'GET') { firstDeck = false; return route.fulfill({status:503,json:{error:{code:'unavailable',message:'Connection unavailable'}}}); }
        return route.continue();
      });
      await page.goto(base+'/#/deck');
      await page.getByRole('button',{name:'Reload saved deck'}).click();
      await page.getByRole('button',{name:'Choose buttons'}).click();
      await page.getByRole('dialog').getByRole('button',{name:/^Find Bugs/}).click();
      await page.getByRole('button',{name:'Continue',exact:true}).click();
      await page.getByRole('button',{name:'Open workflow settings'}).click();
      await page.getByRole('button',{name:'Continue with interactive preview'}).click();
      await page.locator('[data-key="find-bugs"]').click();
      await page.getByRole('button',{name:'Interactive preview · Open execution details'}).click();
      await page.getByRole('dialog').getByText('No scan results.',{exact:false}).waitFor();
      await page.getByRole('button',{name:'Return to Control Deck',exact:true}).click();
      await page.getByRole('button',{name:'Finish onboarding'}).click();
      await page.waitForFunction(() => { const button = document.querySelector('[data-testid="edit-deck"]'); return button && !button.disabled; });
      const saved = await request(phone.token,'GET','/api/deck');
      assert.equal(saved.slots[6].button,'find-bugs'); assert.equal(saved.slots[7],null); assert.equal(saved.slots[8],null);
      assert.equal((await request(phone.token,'GET','/api/deck/history')).length,0,'missing repository preview never executes');
    } finally { await ctx.close(); await request(owner,'DELETE','/api/phones/'+phone.id); }
    console.log('PASS onboarding: first use, navigation-only choices, three programmable positions, configuration/defaults, live/preview/skip, reload and returning users');
  } finally { await browser.close(); }
})().catch(e => { console.error(e); process.exitCode=1; });

<script lang="ts">
  import { onDestroy, untrack } from 'svelte';
  import { ApiError, api } from '../api';
  import { taskHref } from '../location';
  import { app } from '../state.svelte';
  import type { GitMergePlan, GitOverview } from '../types';
  import { EXAMPLE_DESCRIPTION, EXAMPLE_TITLE, exampleStage, stageText } from './hello';
  import SetupCard from './SetupCard.svelte';

  let { step, variant, ondone, onstep }: {
    step: 'ticket' | 'assistant' | 'publish';
    variant: 'github' | 'local';
    ondone?: () => void;
    onstep?: (id: string) => void;
  } = $props();

  const example = $derived(app.onboarding?.example);
  const project = $derived(example ? app.project(example.projectId) : undefined);
  const scope = $derived(project ? app.scope(project.id) : undefined);
  const task = $derived(example?.taskId ? scope?.tasks.find((t) => t.id === example.taskId) : undefined);
  const run = $derived(task ? scope?.latestRun[task.id] : undefined);
  const stage = $derived(exampleStage(task, run));
  const usedTemplate = $derived(app.onboarding?.steps?.ticket === 'skipped');

  // The project's data is fetched without making it the current project.
  $effect(() => { if (project) untrack(() => app.warm(project.id)); });

  let title = $state(EXAMPLE_TITLE);
  let description = $state(EXAMPLE_DESCRIPTION);
  let busy = $state(false);
  let error = $state('');

  async function remember(taskId = '') {
    if (project) app.onboarding = await api.setOnboardingExample(project.id, taskId);
  }

  async function makeLocal() {
    busy = true; error = '';
    try {
      const p = await api.localExample();
      app.upsertProject(p);
      app.onboarding = await api.setOnboardingExample(p.id);
      app.onboarding = await api.setOnboardingVariant('local');
    } catch (err) { error = err instanceof Error ? err.message : String(err); } finally { busy = false; }
  }

  async function createAndStart() {
    if (!project || busy) return;
    busy = true; error = '';
    try {
      let t = task;
      if (!t) {
        t = await api.createTask(project.id, title.trim() || EXAMPLE_TITLE, description.trim(), undefined, { workMode: 'agent' });
        app.scope(project.id)?.upsertTask(t);
        await remember(t.id); // from here the setup resumes at this ticket, never creating a second one
      }
      app.onboarding = await api.setOnboardingStep('ticket', 'done');
      onstep?.('assistant');
      await start();
    } catch (err) { error = err instanceof Error ? err.message : String(err); } finally { busy = false; }
  }

  async function start() {
    if (!project || !example?.taskId) return;
    try {
      const r = await api.startRun(project.id, example.taskId);
      app.scope(project.id)?.upsertRun(r);
    } catch (err) {
      error = `The assistant could not start: ${err instanceof Error ? err.message : String(err)}`;
    }
  }

  async function retry() { busy = true; error = ''; try { await start(); } finally { busy = false; } }

  async function useTemplate() {
    if (!project || busy) return;
    busy = true; error = '';
    try {
      await api.scaffoldExample(project.id);
      app.onboarding = await api.setOnboardingStep('ticket', 'skipped');
      app.onboarding = await api.setOnboardingStep('assistant', 'skipped');
      onstep?.('publish');
    } catch (err) { error = err instanceof Error ? err.message : String(err); } finally { busy = false; }
  }

  // ---- publishing: merge what the assistant made, push it, wait for the page ----

  let ov = $state<GitOverview | null>(null);
  let plan = $state<GitMergePlan | null>(null);
  let phase = $state<'idle' | 'merging' | 'pushing' | 'waiting' | 'live' | 'nopages' | 'slow' | 'local-done'>('idle');
  let pageUrl = $state('');
  let repoName = $state('');
  let poll: ReturnType<typeof setInterval> | undefined;
  let polled = 0;
  onDestroy(() => clearInterval(poll));

  const target = $derived(ov?.branches.find((b) => b.scope === 'local' && b.target));
  const source = $derived(run?.branch ? ov?.branches.find((b) => b.scope === 'local' && b.name === run.branch) : undefined);
  const pushNeeded = $derived(variant === 'github' && !!target && (target.notPushed > 0 || target.upstream.state === 'none'));
  const canMerge = $derived(stage === 'review' && !!source && !!target && !!plan?.canMerge);

  async function loadGit() {
    if (!project) return;
    try {
      ov = await api.git.overview(project.id);
      const m = ov.remote.remotes.map((r) => /github\.com[:/]([^/\s]+\/[^/\s]+?)(?:\.git)?$/.exec(r.url)?.[1]).find(Boolean);
      repoName = m ?? '';
      if (stage === 'review' && source && target) {
        plan = await api.git.mergePlan(project.id, { branch: source.name, branchSha: source.sha, target: target.name, targetSha: target.sha, strategy: 'merge' });
      }
    } catch (err) { error = err instanceof Error ? err.message : String(err); }
  }

  $effect(() => {
    if (step !== 'publish' || !project) return;
    void [stage, project.id];
    untrack(() => void loadGit());
  });

  async function merge() {
    if (!project || !source || !target || !task || busy) return;
    busy = true; error = '';
    phase = 'merging';
    try {
      const res = await api.git.merge(project.id, { branch: source.name, branchSha: source.sha, target: target.name, targetSha: target.sha, strategy: 'merge' });
      if (!res.ok) { error = res.message; phase = 'idle'; return; }
      const done = await api.moveTask(task, 'done').catch(() => null);
      if (done) app.scope(project.id)?.upsertTask(done);
      plan = null;
      await loadGit();
      phase = variant === 'local' ? 'local-done' : 'idle';
      if (variant === 'local') await finishPublish();
    } catch (err) { error = err instanceof ApiError ? err.message : String(err); phase = 'idle'; } finally { busy = false; }
  }

  async function finishPublish() { app.onboarding = await api.setOnboardingStep('publish', 'done'); }

  async function push() {
    if (!project || !target || busy) return;
    busy = true; error = '';
    phase = 'pushing';
    try {
      const res = await api.git.push(project.id, target.name, target.sha);
      if (!res.ok) { error = res.message; phase = 'idle'; return; }
      await loadGit();
      watchPage();
    } catch (err) { error = err instanceof ApiError ? err.message : String(err); phase = 'idle'; } finally { busy = false; }
  }

  function watchPage() {
    if (!repoName) { phase = 'nopages'; return; }
    phase = 'waiting';
    polled = 0;
    clearInterval(poll);
    const check = async () => {
      polled++;
      try {
        const st = await api.githubPages(repoName);
        if (st.url) pageUrl = st.url;
        if (!st.enabled) { phase = 'nopages'; clearInterval(poll); await finishPublish(); return; }
        if (st.state === 'live') { phase = 'live'; clearInterval(poll); await finishPublish(); return; }
      } catch { /* try again on the next tick */ }
      if (polled >= 45) { phase = 'slow'; clearInterval(poll); } // about three minutes
    };
    void check();
    poll = setInterval(check, 4000);
  }

  const runLink = $derived(project && task ? taskHref(project.id, task.id) : '');
  const repoUrl = $derived(repoName ? `https://github.com/${repoName}` : '');
</script>

{#if !project}
  <SetupCard title="Your example project" status="todo" summary="Create one first">
    <p class="muted">The example needs a repository to work in. Create one in the previous step{variant === 'local' ? '' : ' (or skip GitHub and use one on this computer)'}.</p>
    <div class="row"><button class="btn primary" disabled={busy} onclick={makeLocal}>{busy ? 'Creating…' : 'Make a local example repository'}</button></div>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
  </SetupCard>
{:else if step === 'ticket'}
  <SetupCard title="Make a ticket" status={task ? 'done' : 'todo'} summary={project.name}>
    <p>A ticket is a task on your board. This one asks the assistant for a one-page site, so you can watch the whole path.</p>
    <label class="field"><span>Title</span><input bind:value={title} maxlength="200" disabled={!!task} /></label>
    <label class="field"><span>What it should do</span><textarea bind:value={description} rows="7" disabled={!!task}></textarea></label>
    <div class="row">
      <button class="btn primary" disabled={busy || !title.trim()} onclick={createAndStart}>{busy ? 'Starting…' : task ? 'Start the assistant' : 'Create ticket and start the assistant'}</button>
      <button class="btn quiet" disabled={busy || !!task} onclick={useTemplate}>Use a template instead</button>
    </div>
    <p class="muted small">The template writes the same page and workflow for you and commits them to {project.name}. Nothing else changes.</p>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
  </SetupCard>
{:else if step === 'assistant'}
  <SetupCard title="The assistant works" status={stage === 'review' || stage === 'done' ? 'done' : stage === 'failed' ? 'todo' : 'wait'} summary={task?.title ?? ''}>
    {#if usedTemplate}
      <p>You used the template, so there is nothing for the assistant to do. Continue to publish.</p>
    {:else}
      <p role="status" aria-live="polite">{stageText(stage, run)}</p>
      <div class="row">
        {#if runLink}<a class="btn" href={runLink}>Open the ticket</a>{/if}
        {#if stage === 'not-started' || stage === 'failed'}<button class="btn primary" disabled={busy} onclick={retry}>{stage === 'failed' ? 'Try again' : 'Start the assistant'}</button>{/if}
        {#if stage === 'failed'}<button class="btn quiet" disabled={busy} onclick={useTemplate}>Use the template instead</button>{/if}
        {#if stage === 'review' || stage === 'done'}<button class="btn primary" onclick={() => onstep?.('publish')}>Next</button>{/if}
      </div>
    {/if}
    {#if error}<p class="error" role="alert">{error}</p>{/if}
  </SetupCard>
{:else}
  <SetupCard title={variant === 'local' ? 'Merge the page' : 'Publish the page'} status={phase === 'live' || phase === 'nopages' || phase === 'local-done' ? 'done' : phase === 'idle' ? 'todo' : 'wait'} summary={repoName}>
    {#if stage === 'review'}
      <p>The assistant made changes on its own branch. Look at them, then merge them into <strong>{target?.name ?? 'main'}</strong>.</p>
      {#if plan}<p class="muted">{plan.filesChanged} {plan.filesChanged === 1 ? 'file' : 'files'} changed, +{plan.additions} −{plan.deletions}.{plan.canMerge ? '' : ' It cannot be merged yet.'}</p>{/if}
      {#if plan && !plan.canMerge}<ul>{#each plan.blockers as b (b.code)}<li>{b.message}</li>{/each}</ul>{/if}
      <div class="row">
        {#if runLink}<a class="btn" href={runLink}>Look at the changes</a>{/if}
        <button class="btn primary" disabled={busy || !canMerge} onclick={merge}>{phase === 'merging' ? 'Merging…' : 'Merge into ' + (target?.name ?? 'main')}</button>
      </div>
    {:else if variant === 'local' && (phase === 'local-done' || stage === 'done' || usedTemplate)}
      <p>The page is merged. It lives in <code>{project.repoPath}/index.html</code>; open that file in a browser to see it.</p>
      {#if app.onboarding?.steps?.publish !== 'done'}<div class="row"><button class="btn primary" onclick={finishPublish}>Done</button></div>{/if}
    {:else if phase === 'live'}
      <p>Your page is live: <a href={pageUrl} target="_blank" rel="noopener noreferrer">{pageUrl}</a></p>
      <p class="muted">That was a repository, a ticket, an assistant, a push and a GitHub Actions deploy, all from here.{repoUrl ? '' : ''}</p>
      <div class="row">{#if repoUrl}<a class="btn" href={repoUrl} target="_blank" rel="noopener noreferrer">Open the repository</a>{/if}<button class="btn primary" onclick={() => ondone?.()}>Continue</button></div>
    {:else if phase === 'nopages'}
      <p>Your code is on GitHub{repoUrl ? '' : ''}, but GitHub Pages is not turned on for this repository, so there is no page to open (private repositories need a paid plan).</p>
      <div class="row">{#if repoUrl}<a class="btn" href={repoUrl} target="_blank" rel="noopener noreferrer">Open the repository</a>{/if}<button class="btn primary" onclick={() => ondone?.()}>Continue</button></div>
    {:else if phase === 'waiting' || phase === 'slow'}
      <p role="status" aria-live="polite">{phase === 'waiting' ? 'Pushed. GitHub is building the page; this usually takes under a minute.' : 'Still building. You can wait, or look at the deploy on GitHub.'}</p>
      {#if phase === 'slow'}<div class="row">{#if repoUrl}<a class="btn" href="{repoUrl}/actions" target="_blank" rel="noopener noreferrer">Open the Actions tab</a>{/if}<button class="btn primary" onclick={watchPage}>Check again</button></div>{/if}
    {:else if pushNeeded}
      <p>The page is in <strong>{target?.name}</strong> on this computer. Push it to {repoName || 'GitHub'} to publish.</p>
      <div class="row"><button class="btn primary" disabled={busy} onclick={push}>{phase === 'pushing' ? 'Pushing…' : 'Push to GitHub'}</button></div>
    {:else if variant === 'github' && stage !== 'no-ticket' && !usedTemplate && stage !== 'done'}
      <p class="muted">Waiting for the assistant to finish.</p>
    {:else if variant === 'github' && repoName}
      <div class="row"><button class="btn primary" onclick={watchPage}>Check the page</button></div>
    {:else}
      <p class="muted">Nothing to publish yet.</p>
    {/if}
    {#if error}<p class="error" role="alert">{error}</p>{/if}
  </SetupCard>
{/if}

<style>
  .row { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; }
  .field { display: grid; gap: 4px; }
  textarea { font: inherit; resize: vertical; }
  .small { font-size: 0.82rem; }
  ul { margin: 0; padding-left: 20px; }
</style>

<script lang="ts">
  import { acknowledge, milestone, surface } from '../lib/motion';
  import Sheet from '../lib/Sheet.svelte';
  import { onMount } from 'svelte';
  import { api } from '../lib/api';
  import { globalHref, router } from '../lib/router.svelte';
  import AgentsCard from '../lib/setup/AgentsCard.svelte';
  import CreateRepoCard from '../lib/setup/CreateRepoCard.svelte';
  import HelloFlow from '../lib/setup/HelloFlow.svelte';
  import { exampleStage } from '../lib/setup/hello';
  import GitHubCard from '../lib/setup/GitHubCard.svelte';
  import PhoneStepCard from '../lib/setup/PhoneStepCard.svelte';
  import ReposCard from '../lib/setup/ReposCard.svelte';
  import SetupCard from '../lib/setup/SetupCard.svelte';
  import { localReadiness, setupCurrent, setupResolved } from '../lib/setup/readiness';
  import { app } from '../lib/state.svelte';
  import type { GitHubStatusInfo } from '../lib/types';
  import { embedded } from '../lib/embed';
  import SaliePlacement from '../lib/SaliePlacement.svelte';
  import Settling from '../lib/Settling.svelte';
  import { defaultSaliePlacements, isSaliePlacements, type SaliePlacements } from '../lib/salie-placement';

  const framed = embedded();
  let saliePlacements = $state(defaultSaliePlacements());
  let saliePlacementReady = $state(false);
  function setSaliePlacements(value: SaliePlacements) {
    window.parent.postMessage({ type: 'werkbord.frame', event: 'salie_placement', action: 'set', value }, '*');
  }

  let github = $state<GitHubStatusInfo | null>(null);
  let finishing = $state(false);
  let saving = $state(false);
  let dialog = $state<'intro' | 'pause' | 'github' | 'example' | null>(null);
  let completed = $state(false);
  let error = $state('');
  let readinessUnavailable = $state(false);
  let checking = $state(true);
  let checked = $state(false);
  let chosen = $state<string | null>(null);
  let phonePaired = $state(false);
  // Setup decides which step you are on from five answers (this computer, the agents, GitHub, your phone and the example's project).
  // It asks for all of them first and shows nothing about the steps until they are in, so no step is shown as to do or as unmet and then
  // taken back by the next answer. A computer that never answers is not waited for for ever.
  let githubChecked = $state(false);
  let phonesChecked = $state(false);
  let wasLive = $state(false);
  let waitedLongEnough = $state(false);
  $effect(() => { if (app.connection === 'live') wasLive = true; });
  /** Connecting and syncing are the passing states between two live ones; they do not make a verified computer unverified. */
  const liveForReadiness = $derived(app.connection === 'live' || (wasLive && (app.connection === 'connecting' || app.connection === 'syncing')));
  const connectionKnown = $derived(app.connection === 'live' || app.connection === 'offline' || app.connection === 'unauthorized');
  const local = $derived(localReadiness(app.runners, app.agents, app.projects, liveForReadiness && !readinessUnavailable));
  const stepStates = $derived(app.onboarding?.steps ?? {});
  const githubDone = $derived(github?.state === 'signed_in' && !github.missingScopes?.length);
  const example = $derived(app.onboarding?.example);
  const exampleScope = $derived(example ? app.scope(example.projectId) : undefined);
  $effect(() => { if (example) app.warm(example.projectId); });
  const exampleKnown = $derived(!example || (!!exampleScope && (exampleScope.loaded || !!exampleScope.error)));
  const computerKnown = $derived(checked && connectionKnown);
  const settled = $derived((computerKnown && githubChecked && phonesChecked && exampleKnown) || waitedLongEnough);
  const checklist = $derived([
    { label: 'This computer and its coding agents', done: computerKnown },
    { label: 'GitHub', done: githubChecked },
    { label: 'Your phone', done: phonesChecked },
    ...(example ? [{ label: 'Your example project', done: exampleKnown }] : []),
  ]);
  const exampleTask = $derived(example?.taskId ? exampleScope?.tasks.find((t) => t.id === example.taskId) : undefined);
  const stage = $derived(exampleStage(exampleTask, exampleTask ? exampleScope?.latestRun[exampleTask.id] : undefined));
  const stepDone = (id: string) => stepStates[id] === 'done';
  const variant = $derived<'github' | 'local'>(app.onboarding?.variant === 'local' ? 'local' : 'github');
  // GitHub sits between the agent and the repository. It is optional: skipping it keeps everything local.
  const steps = $derived([
    local.steps[0],
    local.steps[1],
    { id: 'github', label: 'GitHub', done: githubDone, optional: true },
    { ...local.steps[2], optional: false },
    { id: 'ticket', label: 'First task', done: stepDone('ticket') || !!exampleTask, optional: true },
    { id: 'assistant', label: 'Agent work', done: stepDone('assistant') || stage === 'review' || stage === 'done', optional: true },
    { id: 'publish', label: variant === 'local' ? 'Merge' : 'Publish', done: stepStates.publish === 'done', optional: true },
    { id: 'phone', label: 'Phone', done: phonePaired || stepStates.phone === 'done', optional: true },
    { id: 'review', label: 'Finish', done: false },
  ].map((x) => ({ optional: false, ...x })));
  // Resumes at the first step that is neither verified nor skipped, unless the person picked another.
  const current = $derived(setupCurrent(steps, stepStates, chosen));
  const exampleStep = $derived(current === 'ticket' || current === 'assistant' || current === 'publish');
  const currentIndex = $derived(steps.findIndex((x) => x.id === current));
  const currentStep = $derived(steps[currentIndex]);
  const frontier = $derived(steps.findIndex(s => !setupResolved(s, stepStates)));
  const resolvedCount = $derived(steps.filter(s => s.id !== 'review' && setupResolved(s, stepStates)).length);
  const canFinish = $derived(local.ready && steps.filter(s => s.id !== 'review').every(s => setupResolved(s, stepStates)));
  const explanations: Record<string, { title: string; text: string }> = {
    computer: { title: 'First, get this computer ready', text: 'The runner carries out work on this computer. It must be online and enabled before a coding agent can start.' },
    agents: { title: 'Connect a coding agent', text: 'Install and sign in to at least one supported agent. Werkbord uses that existing sign-in to carry out your tasks.' },
    github: { title: 'Connect GitHub', text: 'Connect GitHub to find repositories and work with pull requests, or choose to work locally.' },
    repository: { title: 'Choose where your work will live', text: 'Add a Git repository on this computer, or create a separate example repository. Werkbord needs a repository to keep agent changes on working branches.' },
    ticket: { title: 'Try your first task', text: 'Learn the workflow in a separate example repository: describe the work, let an agent make changes, then review them. Nothing is merged or published without your action.' },
    assistant: { title: 'Follow the agent’s work', text: 'Watch the task progress. If the agent asks a question, open the task and answer it, then return here to continue setup.' },
    publish: { title: 'Review the result before it leaves the branch', text: 'Inspect the changes, then choose whether to merge and publish. You stay in control of your repository.' },
    phone: { title: 'Decide how you’ll stay connected', text: 'Pair a phone to answer questions and review work away from your desk. This is optional; you can explicitly leave it for later.' },
    review: { title: 'Your setup is ready to finish', text: 'Review what is connected and what you chose to leave for later. Opening the workspace below is the final step.' },
  };
  const previous = $derived(steps.slice(0, currentIndex).reverse().find(s => !(s.done && ['computer', 'agents', 'repository'].includes(s.id))));

  let quietFailures = 0;
  async function recheck(quiet = false) {
    const refreshAgentOptions = !quiet && current === 'agents';
    if (!quiet) { checking = true; error = ''; }
    try {
      const [runners, agents] = await Promise.all([api.listRunners(), api.listAgents()]);
      app.runners = runners;
      app.agents = agents;
      if (refreshAgentOptions) {
        // A newly installed or updated agent may offer different models.
        await Promise.all(agents.filter(a => a.available).map(a => app.loadAgentOptions(a.id, true)));
      }
      readinessUnavailable = false;
      quietFailures = 0;
    } catch (err) {
      // Asked for by the person: say so at once. The background check every five seconds must fail twice running before the screen
      // says this computer cannot be verified; one slow answer is not an outage and is not worth a notice that the next answer retracts.
      if (!quiet || ++quietFailures >= 2) readinessUnavailable = true;
      if (!quiet) error = err instanceof Error ? err.message : String(err);
    } finally { if (!quiet) checking = false; checked = true; }
  }

  function go(id: string) {
    if (steps.findIndex(s => s.id === id) > frontier) return;
    if (['computer', 'agents', 'repository'].includes(id) && steps.find(s => s.id === id)?.done) return;
    chosen = id;
    if (id === 'computer' || id === 'agents') void recheck();
  }

  // The repository the setup just made is the example the rest of the path works on.
  async function madeRepo(projectId: string) {
    try {
      app.onboarding = await api.setOnboardingExample(projectId);
      go('ticket');
    } catch (err) { error = err instanceof Error ? err.message : String(err); }
  }

  async function phoneEnrolled() {
    chosen = 'phone';
    phonePaired = true;
    try { app.onboarding = await api.setOnboardingStep('phone', 'done'); } catch { /* the phone is paired either way */ }
  }

  async function skipStep() {
    if (saving || !currentStep.optional) return;
    saving = true; error = '';
    const id = current;
    const index = currentIndex;
    try {
      // Save the path before resolving the step so a failed save cannot advance with the wrong example path.
      if (id === 'github') app.onboarding = await api.setOnboardingVariant('local');
      app.onboarding = await api.setOnboardingStep(id, 'skipped');
      chosen = steps[index + 1]?.id ?? null;
    } catch (err) { error = err instanceof Error ? err.message : String(err); } finally { saving = false; }
  }

  async function skipExample() {
    if (saving) return;
    saving = true; error = '';
    try {
      for (const id of ['ticket', 'assistant', 'publish']) {
        if (!steps.find(s => s.id === id)?.done) app.onboarding = await api.setOnboardingStep(id, 'skipped');
      }
      chosen = null; dialog = null;
    } catch (err) { error = err instanceof Error ? err.message : String(err); } finally { saving = false; }
  }

  async function begin() {
    saving = true; error = '';
    try {
      app.onboarding = await api.setOnboardingVariant(variant);
      app.onboardingIntroduced = true;
      dialog = null;
    } catch (err) { error = err instanceof Error ? err.message : String(err); } finally { saving = false; }
  }

  // Connecting GitHub (or undoing a skip) puts the example back on the GitHub path.
  $effect(() => {
    if (githubDone && app.onboarding && app.onboarding.variant !== 'github') {
      void api.setOnboardingVariant('github').then((o) => (app.onboarding = o), () => {});
      if (stepStates.github) void api.setOnboardingStep('github', '').then((o) => (app.onboarding = o), () => {});
    }
  });

  $effect(() => {
    if (app.needsOnboarding && !app.onboardingIntroduced) {
      app.onboardingIntroduced = true;
      dialog = 'intro';
      if (!app.onboarding?.variant) void api.setOnboardingVariant(variant).then(o => { app.onboarding = o; }, err => { error = String(err); });
    }
  });

  onMount(() => {
    const placementMessage = (event: MessageEvent) => {
      if (!framed || event.source !== window.parent || event.data?.type !== 'werkbord.salie-placement' || !isSaliePlacements(event.data.value)) return;
      saliePlacements = event.data.value;
      saliePlacementReady = true;
    };
    window.addEventListener('message', placementMessage);
    if (framed) window.parent.postMessage({ type: 'werkbord.frame', event: 'salie_placement', action: 'get' }, '*');
    app.onboardingPaused = false;
    void recheck();
    const timer = setInterval(() => { if (document.visibilityState === 'visible') void recheck(true); }, 5000);
    api.github().then((g) => (github = g), () => {}).finally(() => (githubChecked = true));
    api.phones().then((p) => (phonePaired = p.length > 0), () => {}).finally(() => (phonesChecked = true));
    const patience = setTimeout(() => (waitedLongEnough = true), 8000);
    return () => { clearInterval(timer); clearTimeout(patience); window.removeEventListener('message', placementMessage); };
  });

  async function finish() {
    if (!canFinish || finishing || saving) return;
    finishing = true;
    error = '';
    try {
      // Recheck required capabilities immediately before recording completion.
      await recheck();
      if (!local.ready) { chosen = null; return; }
      const skipped: string[] = [];
      if (!phonePaired) skipped.push('network');
      if (!githubDone) skipped.push('github');
      app.onboarding = await api.completeOnboarding(skipped);
      completed = true; milestone();
    } catch (err) { error = err instanceof Error ? err.message : String(err); }
    finally { finishing = false; }
  }

  function openWorkspace() {
    const first = local.project;
    router.go(first ? { view: 'work', projectId: first.id, taskId: '' } : { view: 'projects', projectId: '', taskId: '' });
  }

</script>

{#if dialog}
  <Sheet title={dialog === 'intro' ? 'Let’s set up Werkbord' : dialog === 'pause' ? 'Pause setup?' : dialog === 'github' ? 'Continue without GitHub?' : 'Leave the example for later?'} onclose={() => { if (!saving) dialog = null; }}>
    {#if dialog === 'intro'}
      <p>Connect your tools, then try your first task. Your progress is saved.</p>
      <button class="btn primary big" disabled={saving} onclick={begin}>{saving ? 'Saving…' : 'Start guided setup'}</button>
    {:else if dialog === 'pause'}
      <p>Your progress will be kept, but setup will remain unfinished. Agent work needs a ready runner, a signed-in coding agent and a Git repository.</p>
      <p>A reminder stays visible while you explore. The next time you open Werkbord, setup resumes.</p>
      <div class="actions"><button class="btn primary big" onclick={() => (dialog = null)}>Continue setup</button><button class="btn big warn" onclick={() => { app.onboardingPaused = true; dialog = null; router.go({ view: 'projects', projectId: '', taskId: '' }); }}>Pause and explore</button></div>
    {:else if dialog === 'github'}
      <p>Without a GitHub connection, you can work in local repositories. GitHub repository discovery, pull requests, checks and publishing the example page will need a connection later.</p>
      <p>You can connect it under Settings → GitHub whenever you are ready.</p>
      <div class="actions"><button class="btn primary big" disabled={saving} onclick={() => (dialog = null)}>Connect GitHub instead</button><button class="btn big" disabled={saving} onclick={async () => { await skipStep(); if (!error) dialog = null; }}>{saving ? 'Saving…' : 'Use local-only setup'}</button></div>
    {:else}
      <p>The example teaches you how to create a task, follow an agent and review changes. You can leave it for later without changing or publishing any work.</p>
      <p>If an agent is already running, it continues. Pausing the example does not cancel it.</p>
      <div class="actions"><button class="btn primary big" disabled={saving} onclick={() => (dialog = null)}>Continue the example</button><button class="btn big" disabled={saving} onclick={skipExample}>{saving ? 'Saving…' : 'Do the example later'}</button></div>
    {/if}
    {#if error}<p class="error" role="alert">{error}</p>{/if}
  </Sheet>
{/if}

<div class="onboarding" inert={dialog !== null}>
  <header class="welcome">
    <h2 class="title">Welcome to Werkbord</h2>
    {#if settled}
    <p class="progress-label" role="status">{resolvedCount} of 8 setup steps resolved · {local.completed} of 3 requirements verified</p>
    <ol class="progress" aria-label="Setup steps">
      {#each steps as s, i (s.id)}
        <li>
          <button class="chip" disabled={completed || !checked || saving || i > frontier || (s.done && ['computer', 'agents', 'repository'].includes(s.id))} data-done={s.done} aria-current={s.id === current ? 'step' : undefined} onclick={() => go(s.id)}>
            <span class="dot" data-tone={s.done ? 'ok' : 'neutral'}></span>{i + 1}. {s.label}{s.id === 'github' || s.id === 'phone' ? ' (optional)' : ''}<span class="visually-hidden">{s.done ? ', done' : stepStates[s.id] === 'skipped' ? ', skipped' : ', to do'}</span>
          </button>
        </li>
      {/each}
    </ol>
    {/if}
    {#if error}<p class="error" role="alert">{error}</p>{/if}
  </header>

  {#if !settled}
    <Settling label="Checking this computer…" items={checklist} testid="setup-loading" />
  {:else if completed}
    <section class="step-intro" use:acknowledge={{ value: completed, success: completed, fresh: true }} in:surface={{ duration: 180 }} aria-labelledby="setup-complete">
      <h2 id="setup-complete">You’re ready to use Werkbord</h2>
      <p>Your runner, coding agent and repository are ready. Create tasks on your board, follow agent work and review changes before merging.</p>
      <p class="muted">Anything you left for later is listed below. You can return to setup from Settings → This computer.</p>
      <button class="btn primary big" onclick={openWorkspace}>Open my workspace</button>
    </section>
  {:else}
    <section class="step-intro" aria-live="polite" aria-atomic="true">
      <p class="muted">Step {currentIndex + 1} of {steps.length}</p>
      <h2>{explanations[current].title}</h2>
      <p>{explanations[current].text}</p>
    </section>
  {/if}

  {#if settled && (completed || current === 'review')}
    <section aria-label="Setup summary">
      <ul class="summary-list">{#each steps.filter(s => s.id !== 'review') as s (s.id)}<li><strong>{s.label}</strong><span>{s.done ? 'Verified' : stepStates[s.id] === 'skipped' ? 'Left for later' : 'Needs attention'}</span></li>{/each}</ul>
    </section>
    {#if framed && saliePlacementReady}
      <SetupCard title="Where would you like Salie?" status="done" summary="Choose how to open Salie">
        <p class="muted">Use the sidebar, a movable floating button, or both. You can change this later in Settings → Appearance.</p>
        <SaliePlacement value={saliePlacements} onchange={setSaliePlacements} />
      </SetupCard>
    {/if}
  {:else if !settled}
    <!-- Shown as the loading screen above. -->
  {:else if current === 'computer'}
  <SetupCard title="This computer" status={local.steps[0].done ? 'done' : checking ? 'wait' : 'todo'} summary={local.runner ? `${local.runner.name} · ${local.runner.os}/${local.runner.arch}` : 'No local runner reported'}>
    <p class="muted">
      {local.steps[0].done ? 'The local runner is online and enabled.' : !liveForReadiness ? 'Reconnect to verify local runner readiness.' : 'The local runner must be online, enabled and configured to run an agent.'} Your repositories, credentials and sessions stay on this computer.
    </p>
    {#if !local.steps[0].done}<p><a href="{globalHref('settings')}?runners">Open runner settings</a> to inspect or enable your local runner.</p>{/if}
    {#if local.runner && local.runner.currentRuns >= local.runner.capacity && local.runner.capacity > 0}<p class="muted">The runner is at capacity. You can create a task now and start it when capacity is available.</p>{/if}
  </SetupCard>
  {:else if current === 'agents'}
  <AgentsCard />
  {:else if current === 'github'}
  <p>Don’t have an account? <a href="https://github.com/signup" target="_blank" rel="noopener noreferrer">Create a GitHub account</a>, then return here to connect it.</p>
  <GitHubCard known={github} skipped={stepStates.github === 'skipped'} onchange={(s) => (github = s)} />
  {:else if current === 'repository'}
  {#if githubDone}<CreateRepoCard {github} onadded={madeRepo} />{/if}
  <ReposCard {github} />
  {:else if current === 'phone'}
  <PhoneStepCard paired={phonePaired} skipped={stepStates.phone === 'skipped'} onenrolled={phoneEnrolled} />
  {:else}
  <HelloFlow step={current as 'ticket' | 'assistant' | 'publish'} {variant} onstep={go} ondone={() => go('phone')} />
  {/if}

  {#if settled && !completed}
  <nav class="actions" aria-label="Setup navigation">
    {#if previous}<button class="btn big" onclick={() => go(previous.id)}>Back</button>{/if}
    {#if current === 'review'}
      <button class="btn primary big" disabled={!canFinish || finishing || checking} onclick={finish}>{finishing ? 'Verifying and saving…' : 'Complete setup'}</button>
    {:else if exampleStep}
      <button class="btn big" disabled={saving} onclick={() => (dialog = 'example')}>Do the example later</button>
    {:else if current === 'phone'}
      {#if currentStep.done || stepStates.phone === 'skipped'}<button class="btn primary big" onclick={() => go('review')}>Review setup</button>
      {:else}<button class="btn big" disabled={saving} onclick={skipStep}>{saving ? 'Saving…' : 'Pair a phone later'}</button>{/if}
    {:else if currentStep.done || stepStates[current] === 'skipped'}
      <button class="btn primary big" onclick={() => go(steps[currentIndex + 1].id)}>Continue</button>
    {:else if current === 'github'}
      <button class="btn big" disabled={saving} onclick={() => (dialog = 'github')}>Continue without GitHub…</button>
    {:else}
      <button class="btn primary big" disabled={checking} onclick={() => void recheck()}>{checking ? 'Checking…' : 'Check again'}</button>
    {/if}
    <a class="btn big" href={globalHref('help')}>Setup help</a>
    <button class="btn quiet big" disabled={saving || finishing} onclick={() => (dialog = 'pause')}>Pause setup…</button>
  </nav>
  {/if}

</div>

<style>
  .step-intro { display: grid; gap: 8px; padding-block: 12px; }
  .step-intro h2 { font-family: var(--font); font-size: 20px; font-weight: 600; text-transform: none; letter-spacing: -0.02em; color: var(--text); }
  .step-intro p { max-width: 68ch; }
  .summary-list { list-style: none; margin: 0; padding: 0; }
  .summary-list li { display: flex; flex-wrap: wrap; justify-content: space-between; gap: 8px; padding: 12px 0; border-bottom: 1px solid var(--border); }
  .summary-list span { color: var(--text-2); }

  .onboarding {
    display: grid;
    gap: 16px;
    max-width: 760px;
    margin: 0 auto;
    padding-top: 12px;
  }

  .welcome {
    display: grid;
    gap: 8px;
  }

  .welcome .title {
    font-family: var(--font);
    font-size: 28px;
    font-weight: 600;
    text-transform: none;
    letter-spacing: -0.02em;
    color: var(--text);
  }

  .progress {
    list-style: none;
    margin: 6px 0 0;
    padding: 0;
    display: flex;
    gap: 6px 8px;
    flex-wrap: wrap;
  }
  .progress-label { font-size: 13px; font-weight: 500; }
  .actions { display: flex; flex-wrap: wrap; gap: 8px; }

  .progress .chip { cursor: pointer; min-height: 36px; }
  .progress .chip[aria-current='step'] { border-color: var(--accent); }
  .progress .chip[data-done='true'] { color: var(--text); }
  .progress .chip:disabled { opacity: 1; cursor: default; }

  .progress .dot[data-tone='neutral'] {
    opacity: 0.5;
  }

  .big {
    min-height: 44px;
    padding: 0 20px;
    font-size: 14px;
  }

</style>

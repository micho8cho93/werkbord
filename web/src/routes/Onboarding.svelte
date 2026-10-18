<script lang="ts">
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
  import { localReadiness, setupCurrent } from '../lib/setup/readiness';
  import { app } from '../lib/state.svelte';
  import type { GitHubStatusInfo } from '../lib/types';

  let github = $state<GitHubStatusInfo | null>(null);
  let finishing = $state(false);
  let error = $state('');
  let readinessUnavailable = $state(false);
  let checking = $state(true);
  let checked = $state(false);
  let chosen = $state<string | null>(null);
  let phonePaired = $state(false);
  const local = $derived(localReadiness(app.runners, app.agents, app.projects, app.connection === 'live' && !readinessUnavailable));
  const stepStates = $derived(app.onboarding?.steps ?? {});
  const githubDone = $derived(github?.state === 'signed_in' && !github.missingScopes?.length);
  const example = $derived(app.onboarding?.example);
  const exampleScope = $derived(example ? app.scope(example.projectId) : undefined);
  const exampleTask = $derived(example?.taskId ? exampleScope?.tasks.find((t) => t.id === example.taskId) : undefined);
  const stage = $derived(exampleStage(exampleTask, exampleTask ? exampleScope?.latestRun[exampleTask.id] : undefined));
  const stepDone = (id: string) => stepStates[id] === 'done' || stepStates[id] === 'skipped';
  const variant = $derived<'github' | 'local'>(app.onboarding?.variant === 'local' ? 'local' : 'github');
  // GitHub sits between the agent and the repository. It is optional: skipping it keeps everything local.
  const steps = $derived([
    local.steps[0],
    local.steps[1],
    { id: 'github', label: 'GitHub', done: githubDone, optional: true },
    { ...local.steps[2], optional: false },
    { id: 'ticket', label: 'Ticket', done: stepDone('ticket') || !!exampleTask },
    { id: 'assistant', label: 'Assistant', done: stepDone('assistant') || stage === 'review' || stage === 'done' },
    { id: 'publish', label: variant === 'local' ? 'Merge' : 'Publish', done: stepStates.publish === 'done' },
    { id: 'phone', label: 'Phone', done: phonePaired || stepStates.phone === 'done', optional: true },
  ].map((x) => ({ optional: false, ...x })));
  // Resumes at the first step that is neither verified nor skipped, unless the person picked another.
  const current = $derived(setupCurrent(steps, stepStates, chosen));
  const exampleStep = $derived(current === 'ticket' || current === 'assistant' || current === 'publish');
  const currentIndex = $derived(steps.findIndex((x) => x.id === current));
  const currentStep = $derived(steps[currentIndex]);
  const previous = $derived(steps.slice(0, currentIndex).reverse().find(s => !(s.done && ['computer', 'agents', 'repository'].includes(s.id))));

  async function recheck() {
    checking = true;
    error = '';
    try {
      const [runners, agents] = await Promise.all([api.listRunners(), api.listAgents()]);
      app.runners = runners;
      app.agents = agents;
      readinessUnavailable = false;
    } catch (err) {
      readinessUnavailable = true;
      error = err instanceof Error ? err.message : String(err);
    } finally { checking = false; checked = true; }
  }

  function go(id: string) {
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
    phonePaired = true;
    try { app.onboarding = await api.setOnboardingStep('phone', 'done'); } catch { /* the phone is paired either way */ }
  }

  async function skipStep() {
    try {
      app.onboarding = await api.setOnboardingStep(current, 'skipped');
      // Without GitHub the example runs in a local repository.
      if (current === 'github') app.onboarding = await api.setOnboardingVariant('local');
      chosen = steps[currentIndex + 1]?.id ?? null;
    } catch (err) { error = err instanceof Error ? err.message : String(err); }
  }

  // Connecting GitHub (or undoing a skip) puts the example back on the GitHub path.
  $effect(() => {
    if (githubDone && app.onboarding && app.onboarding.variant !== 'github') {
      void api.setOnboardingVariant('github').then((o) => (app.onboarding = o), () => {});
      if (stepStates.github) void api.setOnboardingStep('github', '').then((o) => (app.onboarding = o), () => {});
    }
  });

  onMount(() => {
    void recheck();
    api.github().then((g) => (github = g), () => {});
    api.phones().then((p) => (phonePaired = p.length > 0), () => {});
  });

  async function finish(createTask = false) {
    if (createTask && !local.ready) return;
    finishing = true;
    error = '';
    try {
      const skipped: string[] = [];
      if (!phonePaired) skipped.push('network');
      if (github?.state !== 'signed_in') skipped.push('github');
      if (!app.agents.some((a) => a.available)) skipped.push('agents');
      if (!local.project) skipped.push('projects');
      app.onboarding = await api.completeOnboarding(skipped);
      const first = createTask ? local.project : app.projects[0];
      router.go(first ? { view: 'board', projectId: first.id, taskId: '' } : { view: 'projects', projectId: '', taskId: '' });
      if (createTask) app.newTaskOpen = true;
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      finishing = false;
    }
  }

</script>

<div class="onboarding">
  <header class="welcome">
    <h2 class="title">Welcome to Werkbord</h2>
    <p class="lede">
      Run your first local coding task. Your runner, agent sign-in and repository stay on your computer. You can also continue with planning only.
    </p>
    <p class="progress-label" role="status">{!checked ? 'Checking this computer…' : `${local.completed} of 3 requirements verified${local.ready ? ' · ready to create a task' : ''}`}</p>
    <ol class="progress" aria-label="Setup steps">
      {#each steps as s, i (s.id)}
        <li>
          <button class="chip" disabled={!checked || (s.done && ['computer', 'agents', 'repository'].includes(s.id))} data-done={s.done} aria-current={s.id === current ? 'step' : undefined} onclick={() => go(s.id)}>
            <span class="dot" data-tone={s.done ? 'ok' : 'neutral'}></span>{i + 1}. {s.label}{s.optional ? ' (optional)' : ''}<span class="visually-hidden">{s.done ? ', done' : stepStates[s.id] === 'skipped' ? ', skipped' : ', to do'}</span>
          </button>
        </li>
      {/each}
    </ol>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
  </header>

  {#if !checked}
    <p class="muted" role="status">Detecting your runner and available coding agents. Verified requirements will be marked done automatically.</p>
  {:else if current === 'computer'}
  <SetupCard title="This computer" status={local.steps[0].done ? 'done' : checking ? 'wait' : 'todo'} summary={local.runner ? `${local.runner.name} · ${local.runner.os}/${local.runner.arch}` : 'No local runner reported'}>
    <p class="muted">
      {local.steps[0].done ? 'The local runner is online and enabled.' : app.connection !== 'live' ? 'Reconnect to verify local runner readiness.' : 'The local runner must be online, enabled and configured to run an agent.'} Your repositories, credentials and sessions stay on this computer.
    </p>
    {#if !local.steps[0].done}<p><a href="{globalHref('settings')}?runners">Open runner settings</a> to inspect or enable your local runner.</p>{/if}
    {#if local.runner && local.runner.currentRuns >= local.runner.capacity && local.runner.capacity > 0}<p class="muted">The runner is at capacity. You can create a task now and start it when capacity is available.</p>{/if}
  </SetupCard>
  {:else if current === 'agents'}
  <AgentsCard />
  {:else if current === 'github'}
  <p class="muted">GitHub lets Werkbord create a repository for you and publish a page from it. Without it, everything still works on this computer.</p>
  <GitHubCard skipped={stepStates.github === 'skipped'} onskip={skipStep} onchange={(s) => (github = s)} />
  {:else if current === 'repository'}
  {#if githubDone}<CreateRepoCard {github} onadded={madeRepo} />{/if}
  <ReposCard {github} />
  {:else if current === 'phone'}
  <p class="muted">Last step: take Werkbord with you. Your phone becomes a remote for the runner on this computer, so you can answer the assistant and approve work from anywhere. It is optional and you can do it later in Settings.</p>
  <PhoneStepCard paired={phonePaired} skipped={stepStates.phone === 'skipped'} onenrolled={phoneEnrolled} />
  {#if phonePaired}<p role="status">Your phone is connected. Open Werkbord on it to take a short tour.</p>{/if}
  {:else}
  <HelloFlow step={current as 'ticket' | 'assistant' | 'publish'} {variant} onstep={go} ondone={() => go('phone')} />
  {/if}

  {#if checked}
  <nav class="actions" aria-label="Setup navigation">
    {#if previous}<button class="btn big" onclick={() => go(previous.id)}>Back</button>{/if}
    {#if current === 'phone'}
      <button class="btn primary big" disabled={finishing} onclick={() => finish(false)}>{finishing ? 'Opening…' : 'Finish setup'}</button>
      {#if !currentStep.done}<button class="btn big" onclick={skipStep}>Skip for now</button>{/if}
    {:else if exampleStep}
      <!-- The example steps carry their own buttons. -->
    {:else if (currentStep.done || stepStates[current] === 'skipped') && currentIndex < steps.length - 1}
      <button class="btn primary big" onclick={() => go(steps[currentIndex + 1].id)}>Next</button>
    {:else if local.ready}
      <button class="btn primary big" disabled={checking || finishing} onclick={() => finish(true)}>{finishing ? 'Opening…' : 'Create your first task'}</button>
    {:else if !currentStep.done}
      <button class="btn primary big" disabled={checking} onclick={() => void recheck()}>{checking ? 'Checking…' : 'Check again'}</button>
      {#if currentStep.optional}<button class="btn big" onclick={skipStep}>Skip this step</button>{/if}
    {/if}
    {#if current !== 'phone'}<button class="btn big" disabled={finishing} onclick={() => finish(false)}>Finish later</button>{/if}
  </nav>
  {/if}

  <footer class="done">
    <p class="muted small">Finish later keeps what you have done. You can come back to any of this under Settings.</p>
  </footer>
</div>

<style>
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

  .title {
    font-family: var(--font);
    font-size: 28px;
    font-weight: 600;
    text-transform: none;
    letter-spacing: -0.02em;
    color: var(--text);
  }

  .lede {
    color: var(--text-2);
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

  .done {
    display: grid;
    gap: 8px;
    justify-items: start;
    padding-bottom: 8px;
  }

  .big {
    min-height: 44px;
    padding: 0 20px;
    font-size: 14px;
  }

  .small {
    font-size: 0.82rem;
  }
</style>

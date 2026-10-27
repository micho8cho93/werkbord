<script lang="ts">
  import { tick, untrack } from 'svelte';
  import { readDraft, writeDraft, uncertainMutation } from './recovery';
  import { ApiError, api } from './api';
  import { BUSY_RUNNERS, agentLabel, compact, executionAgents, onlyBusy, resolveFor, sourceLabel } from './execution';
  import { agentWorkable } from './board';
  import { INTERACTION_OPTIONS, PERMISSIONS_NOTE } from './policy';
  import { globalHref } from './router.svelte';
  import ExecutionFields from './ExecutionFields.svelte';
  import { isPhone } from './phones/capability';
  import { router } from './router.svelte';
  import Sheet from './Sheet.svelte';
  import TaskPlanFields from './TaskPlanFields.svelte';
  import { app } from './state.svelte';
  import { validatePlanRange } from './planning';
  import type { ExecutionConfig, Plan, PlanIssue, Project, Task, WorkMode } from './types';

  // Essentials first; saved work survives a failed launch.

  let { project }: { project: Project } = $props();

  const draftKey = untrack(() => `new-task.${project.id}`);
  const restored = untrack(() => readDraft<{ title: string; description: string; execution: ExecutionConfig; labelIds: string[]; workMode: WorkMode; plan: Plan; saved: Task | null; uncertainSave: boolean; uncertainStart: boolean }>(draftKey));
  let title = $state(restored?.title ?? '');
  let description = $state(restored?.description ?? '');
  let execution = $state<ExecutionConfig>(restored?.execution ?? {});
  let labelIds = $state<string[]>(restored?.labelIds ?? []);
  // A work project has no repository for an agent, so its tasks start out as work for a person.
  let workMode = $state<WorkMode>(untrack(() => (restored?.workMode ?? (project.kind === 'work' ? 'human' : 'agent'))));
  let plan = $state<Plan>(restored?.plan ?? {});
  let busy = $state(false);
  let error = $state('');
  let planIssues = $state<PlanIssue[]>([]);
  let saved = $state<Task | null>(restored?.saved ?? null);
  let phase = $state('');
  let uncertainSave = $state(restored?.uncertainSave ?? false);
  let uncertainStart = $state(restored?.uncertainStart ?? false);
  let draftStored = $state(true);
  let cleared = false;
  function persist() { if (cleared) return; draftStored = writeDraft(draftKey, { title, description, execution, labelIds, workMode, plan, saved, uncertainSave, uncertainStart }); }
  $effect(() => { void title; void description; JSON.stringify(execution); JSON.stringify(labelIds); void workMode; JSON.stringify(plan); void saved; void uncertainSave; void uncertainStart; untrack(persist); });
  $effect(() => { void [plan.start, plan.end, plan.milestone]; planIssues = []; });

  const inherited = $derived(resolveFor({}, project.execution, app.globalExecution));
  const isWork = $derived(project.kind === 'work');
  const canAgent = $derived(agentWorkable({ workMode }, project));
  const effective = $derived(resolveFor(execution, project.execution, app.globalExecution));
  const agents = $derived(executionAgents(app.agents, app.runners, project.id, effective.runner));
  const agentReady = $derived(canAgent && app.connection === 'live' && (effective.agent ? agents.some(a => a.id === effective.agent) : agents.length > 0));
  const busyOnly = $derived(!agentReady && app.connection === 'live' && onlyBusy(app.agents, app.runners, project.id, effective.runner, effective.agent ?? ''));
  const interaction = $derived(INTERACTION_OPTIONS.find(o => o.value === effective.interaction)!);
  const runnerName = $derived(app.runners.find(r => r.id === effective.runner)?.name ?? (effective.runner && effective.runner !== 'automatic' ? 'Unavailable runner' : 'Automatic runner'));

  function close() {
    if (saved) { cleared = true; writeDraft(draftKey, null); }
    app.newTaskOpen = false;
  }

  async function add(start: boolean) {
    const t = title.trim();
    if (!t || busy || uncertainSave || uncertainStart || (start && !saved && !agentReady)) return;
    const invalidPlan = validatePlanRange(plan, t);
    if (invalidPlan.length) {
      planIssues = invalidPlan;
      error = 'Fix the highlighted planning issues.';
      await tick();
      document.getElementById(invalidPlan[0].field === 'plan.end' ? 'new-task-end' : 'new-task-start')?.focus();
      return;
    }
    busy = true;
    error = '';
    try {
      phase = saved ? 'Requesting agent…' : 'Saving task…';
      if (!saved) { uncertainSave = true; persist(); }
      const task = saved ?? await api.createTask(project.id, t, description.trim(), isWork ? undefined : compact(execution), {
        workMode,
        labelIds,
        plan: plan.start || plan.end ? { ...plan, ...(plan.milestone ? {} : { end: plan.end || undefined }) } : undefined,
      });
      saved = task;
      uncertainSave = false;
      persist();
      const scope = app.scope(project.id);
      scope?.upsertTask(task);
      if (start) {
        phase = 'Requesting agent…';
        // A lost launch response can have started or even finished a run. Recover it before retrying.
        const runs = await api.listTaskRuns(project.id, task.id);
        const existing = runs.at(-1); // task history is oldest first
        if (!existing && !agentReady) throw new Error('The selected agent or runner is unavailable. Retry when it is ready.');
        uncertainStart = true;
        persist();
        const run = existing ?? await api.startRun(project.id, task.id);
        uncertainStart = false;
        scope?.upsertRun(run);
        router.go({ view: 'task', projectId: project.id, taskId: task.id });
      } else {
        app.notify(`“${t}” saved.`, 4000);
      }
      cleared = true;
      writeDraft(draftKey, null);
      close();
    } catch (err) {
      planIssues = err instanceof ApiError ? (err.issues ?? []).filter((issue) => issue.field.startsWith('plan.')) : [];
      uncertainSave = !saved && uncertainMutation(err);
      uncertainStart = !!saved && uncertainStart && uncertainMutation(err);
      persist();
      error = planIssues.length ? 'Fix the highlighted planning issues.' : uncertainStart ? 'Task saved. Could not confirm whether the agent started. Open the saved task and check Runs before starting again.' : uncertainSave ? 'Could not confirm whether the task was saved. Check the Board before adding it again.' : `${saved ? 'Task saved. Agent could not start: ' : ''}${err instanceof Error ? err.message : String(err)}`;
      if (planIssues.length) {
        await tick();
        document.getElementById(planIssues[0].field === 'plan.end' ? 'new-task-end' : 'new-task-start')?.focus();
      }
    } finally {
      busy = false;
    }
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      void add(canAgent);
    }
  }
</script>

<svelte:window onkeydown={onKey} onpagehide={persist} />

<Sheet title="New task in {project.name}" onclose={close} width="40rem">
  <form
    id="new-task-form"
    class="form"
    onsubmit={(e) => {
      e.preventDefault();
      void add(false);
    }}
  >
    {#if !saved}
    <p class="draft-note">Drafts stay in this tab when closed or reloaded.</p>
    <label class="field">
      <span class="lab">{isWork ? 'What needs doing?' : 'What should an agent do?'}</span>
      <input class="input big" disabled={!!saved || uncertainSave} maxlength="200" placeholder={isWork ? 'Book the venue' : 'Fix the flaky auth test'} bind:value={title} data-autofocus required />
    </label>
    <label class="field">
      <span class="lab">Details <span class="opt">optional</span></span>
      <textarea class="input" disabled={!!saved || uncertainSave} rows="2" placeholder={isWork ? 'Context, links, who to ask…' : 'Context, constraints, where to look…'} bind:value={description}></textarea>
    </label>
    {#if canAgent}
      <section class="exec" aria-label="Run settings">
        <strong>{effective.agent ? agentLabel(app.agents, effective.agent) : 'Automatic agent'} on {runnerName} · {interaction.label}</strong>
        <p class="hint">{PERMISSIONS_NOTE}</p>
        {#if busyOnly}<p class="hint" role="status">{BUSY_RUNNERS} Add it to Backlog now and start it from its card.</p>
        {:else if !agentReady}<p class="hint">{app.connection !== 'live' ? 'Reconnect to confirm availability before starting.' : 'The selected agent or runner is unavailable.'} You can save this task in Backlog. <a href={globalHref('settings')}>Check Settings</a>.</p>{/if}
        {#if !isPhone()}<details><summary>Change run settings</summary><p class="hint">{execution.interaction ? '' : interaction.description} From {sourceLabel(effective.sources.interaction)}.</p><ExecutionFields bind:value={execution} {inherited} idPrefix="new-task" disabled={!!saved || uncertainSave} showPermissionsNote={false} columns /></details>{/if}
      </section>
    {/if}
    <fieldset disabled={!!saved || uncertainSave}><TaskPlanFields bind:plan repository={!isWork} idPrefix="new-task" issues={planIssues} /></fieldset>
    {/if}
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    {#if uncertainSave}<p role="status">Save status is unknown. Your brief is kept in this tab; check the Board before creating another task.</p><a class="btn" href={`#/p/${project.id}/board`} onclick={close}>Check Board</a>{/if}
    {#if uncertainStart && !error}<p role="status">Start status is unknown. Open the saved task to check Runs before starting again.</p>{/if}
    {#if !draftStored}<p role="status">This browser cannot keep the brief through reload. Copy it before closing or reloading.</p>{/if}
    {#if saved}<button class="btn open" type="button" onclick={() => { if (saved) router.go({ view: 'task', projectId: project.id, taskId: saved.id }); close(); }}>View task</button>{/if}
  </form>

  {#snippet footer()}
    {#if canAgent && !saved}<span class="tip"><kbd class="key">⌘</kbd><kbd class="key">↵</kbd> start</span>{/if}
    {#if !saved && !uncertainSave}<button class="btn quiet" type="button" onclick={() => { cleared = true; writeDraft(draftKey, null); close(); }}>Discard draft</button>{/if}
    <button class="btn" type="button" onclick={close}>{saved || uncertainSave ? 'Close' : 'Cancel'}</button>
    {#if !saved}<button class="btn" class:primary={!canAgent} type="submit" form="new-task-form" disabled={busy || !title.trim() || uncertainSave || uncertainStart}>Add to Backlog</button>{/if}
    {#if canAgent}
      <button class="btn primary" type="button" disabled={busy || !title.trim() || (!saved && !agentReady) || uncertainSave || uncertainStart} onclick={() => add(true)} title={agentReady ? '' : busyOnly ? 'Every runner that can take this task is busy' : 'No agent is available on an online runner'}>
        {busy ? phase : saved ? 'Retry start' : 'Add and start agent'}
      </button>
    {/if}
  {/snippet}
</Sheet>

<style>
  .draft-note { font-size: 12px; color: var(--text-2); }
  .form {
    display: grid;
    gap: 14px;
  }

  .field {
    display: grid;
    gap: 6px;
  }

  .lab {
    font-size: 13px;
    font-weight: 500;
  }

  .opt {
    font-family: var(--mono);
    font-size: 12px;
    font-weight: 400;
    color: var(--text-2);
    margin-left: 4px;
  }

  .big {
    min-height: 40px;
    font-size: 16px;
  }

  .exec {
    padding: 12px;
    border-radius: var(--radius-tray);
    background: var(--bg);
    border: 1px solid var(--border);
  }

  fieldset { border: 0; margin: 0; padding: 0; min-width: 0; display: grid; gap: 14px; }
  summary { cursor: pointer; min-height: 44px; display: flex; align-items: center; font-size: 13px; font-weight: 500; }
  .exec p { margin-top: 6px; font-size: 13px; }
  .exec .hint { font-size: 12px; color: var(--text-2); }

  .tip {
    margin-right: auto;
    align-self: center;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-family: var(--mono);
    font-size: 12px;
    color: var(--text-2);
  }

  @media (max-width: 719px) {
    fieldset { border: 0; margin: 0; padding: 0; min-width: 0; display: grid; gap: 14px; }
  summary { cursor: pointer; min-height: 44px; display: flex; align-items: center; font-size: 13px; font-weight: 500; }
  .exec p { margin-top: 6px; font-size: 13px; }
  .exec .hint { font-size: 12px; color: var(--text-2); }

  .tip {
      display: none;
    }
  }
</style>

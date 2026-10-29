<script lang="ts">
  import { validatePlanRange } from './planning';
  import type { PlanIssue } from './types';
  import type { Plan } from './types';

  // The planning side of a task: its labels, who is expected to do it, and when it is planned for.
  // None of this changes when or whether an agent runs; the schedule is set separately.

  let {
    plan = $bindable(),
    repository = true,
    idPrefix,
    issues = [],
  }: {
    plan: Plan;
    /** False in a work project, where no agent can work and so “agent” is only a description. */
    repository?: boolean;
    idPrefix: string;
    issues?: PlanIssue[];
  } = $props();

  const milestone = $derived(!!plan.milestone);
  const draftIssues = $derived(validatePlanRange(plan, 'This task'));
  const startError = $derived(issues.find((i) => i.field === 'plan.start')?.message ?? draftIssues.find((i) => i.field === 'plan.start')?.message ?? '');
  const endError = $derived(issues.find((i) => i.field === 'plan.end')?.message ?? draftIssues.find((i) => i.field === 'plan.end')?.message ?? '');

  function setMilestone(on: boolean) {
    // A milestone is one date: keep the start, or the end if that is all there is.
    plan = on ? { start: plan.start || plan.end || '', milestone: true } : { start: plan.start, end: '' };
  }
</script>

<fieldset class="group">
  <legend>Planning <span class="opt">optional</span></legend>
  <p class="hint muted">{repository ? 'Plan the work. These dates do not start an agent.' : 'These dates plan the work in Calendar. This project does not run agents.'}</p>
  <div class="dates">
    <label>
      <span>{milestone ? 'Date' : 'Start'}</span>
      <input class="input" class:invalid={!!startError} type="date" id="{idPrefix}-start" bind:value={plan.start} aria-invalid={!!startError} aria-describedby={startError ? `${idPrefix}-start-error` : undefined} />
      {#if startError}<small class="field-error" id="{idPrefix}-start-error" role="alert">{startError}</small>{/if}
    </label>
    {#if !milestone}
      <label>
        <span>End</span>
        <input class="input" class:invalid={!!endError} type="date" id="{idPrefix}-end" bind:value={plan.end} min={plan.start || undefined} aria-invalid={!!endError} aria-describedby={endError ? `${idPrefix}-end-error` : undefined} />
        {#if endError}<small class="field-error" id="{idPrefix}-end-error" role="alert">{endError}</small>{/if}
      </label>
    {/if}
    <label class="check"><input type="checkbox" checked={milestone} onchange={(e) => setMilestone(e.currentTarget.checked)} /><span>Milestone</span></label>
    {#if plan.start || plan.end}<button type="button" class="btn quiet small" onclick={() => (plan = {})}>Clear dates</button>{/if}
  </div>
</fieldset>

<style>
  .group {
    display: grid;
    gap: 8px;
    margin: 0;
    padding: 0;
    border: 0;
    min-width: 0;
  }

  legend {
    padding: 0;
    margin-bottom: 6px;
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

  .hint {
    margin: 0;
    font-size: 12px;
  }

  .dates {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    gap: 10px;
  }

  .dates label {
    display: grid;
    gap: 4px;
    font-size: 12px;
  }

  .dates .check {
    display: flex;
    align-items: center;
    gap: 6px;
    min-height: 34px;
  }

  .dates .input {
    width: 10.5rem;
  }

  .dates .input.invalid { border-color: var(--danger); box-shadow: 0 0 0 1px var(--danger); }
  .field-error { color: var(--danger-text); font-size: 12px; line-height: 1.35; }
</style>

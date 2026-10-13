<script lang="ts">
  import type { Plan } from './types';

  // The planning side of a task: its labels, who is expected to do it, and when it is planned for.
  // None of this changes when or whether an agent runs; the schedule is set separately.

  let {
    plan = $bindable(),
    repository = true,
    idPrefix,
  }: {
    plan: Plan;
    /** False in a work project, where no agent can work and so “agent” is only a description. */
    repository?: boolean;
    idPrefix: string;
  } = $props();

  const milestone = $derived(!!plan.milestone);
  const rangeError = $derived(!milestone && plan.start && plan.end && plan.end < plan.start ? 'The end is before the start.' : '');

  function setMilestone(on: boolean) {
    // A milestone is one date: keep the start, or the end if that is all there is.
    plan = on ? { start: plan.start || plan.end || '', milestone: true } : { start: plan.start, end: '' };
  }
</script>

<fieldset class="group">
  <legend>Planning <span class="opt">optional</span></legend>
  <p class="hint muted">{repository ? 'These dates plan the work on Timeline; they do not schedule an agent. Use Calendar to choose when an agent runs.' : 'These dates plan the work on Timeline. This project does not run agents.'}</p>
  <div class="dates">
    <label>
      <span>{milestone ? 'Date' : 'Start'}</span>
      <input class="input" type="date" id="{idPrefix}-start" bind:value={plan.start} />
    </label>
    {#if !milestone}
      <label>
        <span>End</span>
        <input class="input" type="date" id="{idPrefix}-end" bind:value={plan.end} min={plan.start || undefined} />
      </label>
    {/if}
    <label class="check"><input type="checkbox" checked={milestone} onchange={(e) => setMilestone(e.currentTarget.checked)} /><span>Milestone</span></label>
    {#if plan.start || plan.end}<button type="button" class="btn quiet small" onclick={() => (plan = {})}>Clear dates</button>{/if}
  </div>
  {#if rangeError}<p class="error" role="alert">{rangeError}</p>{/if}
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
    font-size: 11.5px;
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
</style>

<script lang="ts">
  import { api } from '../api';
  import { cardActivity, oneLine, runElapsed, runStatus } from '../format';
  import { agentLabel } from '../execution';
  import Icon from '../Icon.svelte';
  import RunBadge from '../RunBadge.svelte';
  import { taskHref } from '../router.svelte';
  import { app } from '../state.svelte';
  import type { AttentionRun } from '../types';
  import { usageLine } from './agents';

  // One agent session: who is working on what, how it stands, the last thing it did, and what the person can do from here, which is
  // open it (its output, questions and every other control are on the task) or stop it. Stopping asks twice, as it does on the task.
  let { x }: { x: AttentionRun } = $props();
  const run = $derived(x.run);
  const status = $derived(runStatus(run));
  const activity = $derived(oneLine(cardActivity(run), 140));
  const usage = $derived(usageLine(run));
  const href = $derived(taskHref(run.projectId, run.taskId, run.id));

  let armed = $state(false);
  let busy = $state(false);
  let timer: ReturnType<typeof setTimeout> | undefined;
  async function stop() {
    if (busy) return;
    if (!armed) {
      armed = true;
      clearTimeout(timer);
      timer = setTimeout(() => (armed = false), 5000);
      return;
    }
    armed = false;
    busy = true;
    try {
      app.scope(run.projectId)?.upsertRun(await api.stopRun(run.projectId, run.id));
      app.refreshOverview();
    } catch (err) {
      app.notify(err instanceof Error ? err.message : String(err));
    } finally {
      busy = false;
    }
  }
</script>

<li class="card session" data-testid="session" data-tone={status.tone}>
  <div class="top">
    <span class="agent">{agentLabel(app.agents, run.agentId)}</span>
    <RunBadge {run} />
  </div>
  <a class="task" {href}>{x.taskTitle}</a>
  <p class="meta mm">{x.projectName} · {status.active ? runElapsed(run, app.now) : 'ended'}{run.model ? ` · ${run.model}` : ''}{run.reasoning ? ` · ${run.reasoning}` : ''}</p>
  {#if activity}<p class="well">› {activity}</p>{/if}
  {#if usage}<p class="meta mm">Used {usage}</p>{/if}
  <div class="acts">
    <a class="btn small" class:primary={status.needsInput} {href}>
      {#if status.needsInput}<Icon name="reply" size={14} />Reply{:else}Open{/if}
    </a>
    {#if status.active}
      <button class="btn small danger" class:armed type="button" disabled={busy} onclick={stop}>
        <Icon name="stop" size={12} />{busy ? 'Stopping…' : armed ? 'Stop? Tap again' : 'Stop'}
      </button>
    {/if}
  </div>
</li>

<style>
  .session {
    display: grid;
    gap: 8px;
    padding: 14px;
    list-style: none;
  }

  .session[data-tone='ask'] {
    border-color: color-mix(in srgb, var(--warn) 55%, var(--border));
    box-shadow:
      var(--card-sh),
      0 0 0 3px var(--amber-ring);
  }

  .session[data-tone='bad'] {
    border-color: color-mix(in srgb, var(--danger) 40%, var(--border));
  }

  .top {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }

  .agent {
    font-family: var(--mono);
    font-size: 11.5px;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--text-2);
  }

  .task {
    font-size: 14px;
    font-weight: 500;
    color: var(--text);
    text-decoration: none;
    overflow-wrap: anywhere;
  }

  p {
    margin: 0;
  }

  .well {
    overflow-wrap: anywhere;
  }

  .acts {
    display: flex;
    gap: 8px;
  }

  .acts .btn {
    min-height: 44px;
    text-decoration: none;
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
</style>

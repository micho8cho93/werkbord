<script lang="ts">
  import { attentionItems, segmentCount, SEGMENTS } from '../lib/attention';
  import AttentionCard from '../lib/attention/AttentionCard.svelte';
  import { matchesScheduledExecution } from '../lib/control';
  import ControlInfrastructure from '../lib/ControlInfrastructure.svelte';
  import { agentName, cardActivity, oneLine, runElapsed } from '../lib/format';
  import Icon from '../lib/Icon.svelte';
  import { disableNotifications, enableNotifications, notificationPermission, notificationsEnabled } from '../lib/notifications';
  import { activitySummary, attentionCount } from '../lib/projects';
  import { projectHref, taskHref } from '../lib/router.svelte';
  import { schedulingLabels } from '../lib/scheduling';
  import { app } from '../lib/state.svelte';

  // The one place that looks across every project. On the left, what needs a person, most pressing
  // first, each answerable where it is shown; on the right, what is going on, kept quiet.

  // Clock and external Git changes can alter eligibility without a task event.
  $effect(() => {
    const timer = setInterval(() => {
      if (document.visibilityState === 'visible') app.refreshOverview();
    }, 10000);
    return () => clearInterval(timer);
  });

  let projectFilter = $state('');
  let runnerFilter = $state('');
  let agentFilter = $state('');
  let segment = $state('all');
  const filtering = $derived(!!(projectFilter || runnerFilter || agentFilter));

  const items = $derived(attentionItems(app.overview, app.questions, { project: projectFilter, runner: runnerFilter, agent: agentFilter }, app.runners));
  const kinds = $derived(SEGMENTS.find((s) => s.id === segment)?.kinds ?? SEGMENTS[0].kinds);
  const shown = $derived(items.filter((i) => kinds.includes(i.kind)));
  const loaded = $derived(app.overview !== null);

  const matchRun = (r: import('../lib/types').Run) =>
    (!projectFilter || r.projectId === projectFilter) && (!runnerFilter || r.runnerId === runnerFilter) && (!agentFilter || r.agentId === agentFilter);
  const working = $derived((app.overview?.runs ?? []).filter((x) => matchRun(x.run) && (x.run.state === 'running' || x.run.state === 'starting')));
  const scheduled = $derived(
    (app.overview?.orchestration ?? []).filter(
      (x) =>
        (!projectFilter || x.task.projectId === projectFilter) &&
        x.decision.state !== 'blocked' &&
        x.decision.state !== 'potentially_conflicting' &&
        matchesScheduledExecution(x.task, app.project(x.task.projectId)?.execution ?? {}, app.globalExecution, runnerFilter, agentFilter),
    ),
  );
  const projects = $derived((app.overview?.projects ?? []).filter((p) => !projectFilter || p.projectId === projectFilter));
  const agentIds = $derived([...new Set([...(app.overview?.usage ?? []).map((u) => u.agentId), ...app.agents.map((a) => a.id)])].sort());

  /** One line on how things stand, as the phone heading says it. */
  const summary = $derived.by(() => {
    const needs = segmentCount(items, ['question', 'idle', 'blocked', 'schedule']);
    const failed = segmentCount(items, ['failed']);
    const review = segmentCount(items, ['review']);
    const parts = [needs ? `${needs} need${needs === 1 ? 's' : ''} you` : '', failed ? `${failed} failed` : '', review ? `${review} to review` : '', working.length ? `${working.length} running` : ''];
    return parts.filter(Boolean).join(' · ') || 'All clear';
  });

  let permission = $state(notificationPermission());
  let optedIn = $state(notificationsEnabled());
  async function toggleNotifications() {
    if (optedIn) {
      disableNotifications();
      optedIn = false;
      return;
    }
    permission = await enableNotifications();
    optedIn = permission === 'granted';
  }
</script>

<div class="cc">
  <div class="bar">
    <p class="summary mm">{summary}{projectFilter ? '' : ' · all projects'}</p>
    <div class="seg segs" role="tablist" aria-label="What to show">
      {#each SEGMENTS as s (s.id)}
        {@const n = segmentCount(items, s.kinds)}
        {#if s.id === 'all' || n > 0}
          <button role="tab" aria-selected={segment === s.id} onclick={() => (segment = s.id)}>{s.label}<span class="mono n">{n}</span></button>
        {/if}
      {/each}
    </div>
    <div class="filters">
      {#if app.projects.length > 1}
        <label class="pick" class:on={projectFilter}>
          <span>Project</span>
          <select bind:value={projectFilter} aria-label="Filter by project">
            <option value="">all</option>
            {#each app.projects as p (p.id)}<option value={p.id}>{p.name}</option>{/each}
          </select>
        </label>
      {/if}
      {#if (app.overview?.runners ?? []).length > 1}
        <label class="pick" class:on={runnerFilter}>
          <span>Runner</span>
          <select bind:value={runnerFilter} aria-label="Filter by runner">
            <option value="">all</option>
            {#each app.overview?.runners ?? [] as r (r.id)}<option value={r.id}>{r.name}</option>{/each}
          </select>
        </label>
      {/if}
      {#if agentIds.length > 1}
        <label class="pick" class:on={agentFilter}>
          <span>Agent</span>
          <select bind:value={agentFilter} aria-label="Filter by agent">
            <option value="">all</option>
            {#each agentIds as id (id)}<option value={id}>{app.agents.find((a) => a.id === id)?.name ?? id}</option>{/each}
          </select>
        </label>
      {/if}
      {#if filtering}<button class="btn quiet small" onclick={() => ((projectFilter = ''), (runnerFilter = ''), (agentFilter = ''))}>Clear</button>{/if}
      {#if permission !== 'unsupported'}
        <button
          class="btn small alerts"
          class:on={optedIn}
          onclick={toggleNotifications}
          title="Browser alerts for questions, blocked or failed runs, review and repository risk, while this browser stays connected. There is no push relay."
        >
          <span class="dot" data-tone={optedIn ? 'ok' : 'neutral'}></span>{optedIn ? 'Alerts on' : permission === 'denied' ? 'Alerts blocked' : 'Alerts off'}
        </button>
      {/if}
    </div>
  </div>

  <div class="split">
    <div class="pane" aria-label="Needs you">
      {#if !loaded}
        <p class="empty">Loading…</p>
      {:else if shown.length === 0}
        <div class="clear">
          <span class="dot" data-tone="ok"></span>
          <div>
            <p class="clear-title">{segment === 'all' ? 'Nothing needs you.' : 'Nothing here.'}</p>
            <p class="muted">
              {working.length
                ? `${working.length} ${working.length === 1 ? 'agent is' : 'agents are'} working. Questions, blocked runs and finished work appear here the moment they happen.`
                : 'Questions, blocked runs, failures and finished work appear here the moment they happen.'}
            </p>
          </div>
        </div>
      {:else}
        <ul class="feed">
          {#each shown as item (item.key)}<AttentionCard {item} showProject={!projectFilter} />{/each}
        </ul>
      {/if}
    </div>

    <aside class="pane side" aria-label="What is going on">
      {#if projects.length}
        <section class="pn">
          <div class="ph">Projects<span class="chip">{projects.length}</span></div>
          <ul class="rows">
            {#each projects as p (p.projectId)}
              {@const n = attentionCount(p)}
              <li>
                <a class="prow" href={projectHref(p.projectId, 'overview')} onclick={() => app.enter(p.projectId)}>
                  <span class="sq"></span>
                  <span class="pname">{p.name}</span>
                  <span class="mm psub">{activitySummary(p) || 'quiet'}</span>
                  {#if n}<span class="num pend">{n}</span>{/if}
                </a>
              </li>
            {/each}
          </ul>
        </section>
      {/if}

      <section class="pn">
        <div class="ph">Running now<span class="chip">{working.length}</span></div>
        {#if working.length}
          <ul class="rows">
            {#each working as r (r.run.id)}
              <li class="run">
                <a href={taskHref(r.run.projectId, r.run.taskId)}><span class="dot" data-tone="work"></span>{r.taskTitle}</a>
                <p class="mm">{r.projectName} · {agentName(app.agents, r.run.agentId)} · {runElapsed(r.run, app.now)}</p>
                {#if cardActivity(r.run)}<p class="mm act">› {oneLine(cardActivity(r.run), 90)}</p>{/if}
              </li>
            {/each}
          </ul>
        {:else}
          <p class="muted small">No agent is working right now.</p>
        {/if}
      </section>

      {#if scheduled.length}
        <section class="pn">
          <div class="ph">Scheduled and queued<span class="chip">{scheduled.length}</span></div>
          <ul class="rows">
            {#each scheduled as s (s.task.id)}
              <li class="run">
                <a href={taskHref(s.task.projectId, s.task.id)}><Icon name="calendar" size={14} />{s.task.title}</a>
                <p class="mm">{s.projectName} · {schedulingLabels[s.decision.state]}</p>
              </li>
            {/each}
          </ul>
        </section>
      {/if}

      <ControlInfrastructure project={projectFilter} runner={runnerFilter} agent={agentFilter} />
    </aside>
  </div>
</div>

<style>
  .cc {
    height: 100%;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }

  .bar {
    flex: none;
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 10px 16px;
    padding: 14px 24px 0;
  }

  .summary {
    display: none;
  }

  .segs .n {
    font-size: 11px;
    color: var(--text-2);
  }

  .filters {
    margin-left: auto;
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
  }

  .alerts.on {
    background: var(--surface-2);
    box-shadow: var(--press-sh);
  }

  .split {
    flex: 1;
    --side: 360px;
  }

  .feed {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 12px;
    max-width: 52rem;
  }

  .clear {
    display: flex;
    gap: 14px;
    align-items: flex-start;
    max-width: 52rem;
    padding: 20px;
    border-radius: var(--radius-tray);
    background: var(--surface-2);
    box-shadow: var(--tray-sh);
  }

  .clear .dot {
    margin-top: 6px;
  }

  .clear-title {
    font-weight: 600;
    font-size: 15px;
    margin-bottom: 2px;
  }

  .rows {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .prow {
    display: flex;
    align-items: center;
    gap: 10px;
    min-height: 32px;
    padding: 0 6px;
    border-radius: var(--radius-sm);
    color: var(--text);
    text-decoration: none;
    font-size: 13px;
  }

  .prow:hover {
    background: var(--surface-2);
  }

  .sq {
    width: 7px;
    height: 7px;
    flex: none;
    background: var(--text);
  }

  .pname {
    font-weight: 500;
    flex: none;
    max-width: 45%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .psub {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .run {
    display: grid;
    gap: 2px;
    padding: 6px 0;
    border-top: 1px solid var(--border);
  }

  .run:first-child {
    border-top: 0;
  }

  .run a {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 13px;
    font-weight: 500;
    color: var(--text);
    text-decoration: none;
  }

  .run a .dot {
    animation: pulse 2.4s ease-in-out infinite;
  }

  .run .mm {
    padding-left: 16px;
  }

  .act {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .small {
    font-size: 13px;
  }

  @media (max-width: 899px) {
    .cc {
      height: auto;
    }

    .bar {
      padding: 0 16px;
      gap: 10px;
    }

    .summary {
      display: block;
      width: 100%;
      font-size: 12px;
    }

    .segs {
      width: 100%;
      overflow-x: auto;
      scrollbar-width: none;
    }

    .segs button {
      height: 36px;
      font-size: 14px;
    }

    .filters {
      margin-left: 0;
    }
  }
</style>

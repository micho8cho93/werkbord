<script lang="ts">
  import { layout } from '../lib/motion';
  import { attentionItems, segmentCount, SEGMENTS } from '../lib/attention';
  import AttentionCard from '../lib/attention/AttentionCard.svelte';
  import { matchesScheduledExecution } from '../lib/control';
  import WaitingRepository from '../lib/WaitingRepository.svelte';
  import { agentName, cardActivity, oneLine, runElapsed, runReportStale } from '../lib/format';
  import { formatDateTime } from '../lib/clock.svelte';
  import RunBadge from '../lib/RunBadge.svelte';
  import Icon from '../lib/Icon.svelte';
  import { disableNotifications, enableNotifications, notificationPermission, notificationsEnabled } from '../lib/notifications';
  import { taskHref } from '../lib/router.svelte';
  import { schedulingLabels } from '../lib/scheduling';
  import { app } from '../lib/state.svelte';
  import type { SchedulingDecision } from '../lib/types';

  const scheduleOrder: Record<SchedulingDecision['state'], number> = {
    runnable: 0,
    queued: 1,
    waiting_capacity: 2,
    waiting_dependency: 3,
    waiting_schedule: 4,
    potentially_conflicting: 5,
    blocked: 6,
  };

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
    ).sort((a, b) => {
      const byReadiness = scheduleOrder[a.decision.state] - scheduleOrder[b.decision.state];
      if (byReadiness) return byReadiness;
      const aAt = Date.parse(a.task.orchestration?.scheduledAt ?? '');
      const bAt = Date.parse(b.task.orchestration?.scheduledAt ?? '');
      if (Number.isFinite(aAt) && Number.isFinite(bAt) && aAt !== bAt) return aAt - bAt;
      return a.task.createdAt.localeCompare(b.task.createdAt);
    }),
  );
  const agentIds = $derived([...new Set([...(app.overview?.usage ?? []).map((u) => u.agentId), ...app.agents.map((a) => a.id)])].sort());

  /** One line on how things stand, as the phone heading says it. */
  const staleWork = $derived(app.connection !== 'live' || working.some(r => runReportStale(r.run, true, app.runners)));
  const summary = $derived.by(() => {
    const needs = segmentCount(items, ['question', 'idle', 'blocked', 'schedule']);
    const failed = segmentCount(items, ['failed']);
    const review = segmentCount(items, ['review']);
    const parts = [needs ? `${needs} need${needs === 1 ? 's' : ''} you` : '', failed ? `${failed} failed` : '', review ? `${review} to review` : '', working.length ? `${working.length} running` : ''];
    const known = parts.filter(Boolean).join(' · ');
    return app.connection === 'live' ? known || 'All clear' : `Status may be outdated${known ? ` · ${known}` : ''}`;
  });

  let permission = $state(notificationPermission());
  function syncAlerts() { permission = notificationPermission(); optedIn = notificationsEnabled() && permission === 'granted'; }
  let optedIn = $state(notificationsEnabled());
  async function toggleNotifications() {
    if (optedIn) {
      disableNotifications();
      optedIn = false;
      return;
    }
    permission = notificationPermission();
    if (permission === 'denied') { app.notify('Allow alerts in your browser or macOS notification settings, then return here.'); return; }
    permission = await enableNotifications();
    optedIn = permission === 'granted';
  }
</script>
<svelte:window onfocus={syncAlerts} />


<div class="cc" use:layout={{ revision: items.map(i => i.key).join('|'), enabled: app.overview !== null && app.connection === 'live', entries: true }}>
  <div class="bar">
    <p class="summary mm">{summary}{projectFilter ? '' : ' · all projects'}</p>
    <div class="seg segs" role="group" aria-label="What to show">
      {#each SEGMENTS as s (s.id)}
        {@const n = segmentCount(items, s.kinds)}
        {@const reviewCount = segmentCount(items, ['review'])}
        {#if s.id === 'all' || n > 0}
          <button aria-label="{s.label}: {n} {n === 1 ? 'item' : 'items'}{s.id === 'all' ? `, including ${reviewCount} ready for review` : ''}" aria-pressed={segment === s.id} onclick={() => (segment = s.id)}>{s.label}<span class="mono n" aria-hidden="true">{n}</span></button>
        {/if}
      {/each}
    </div>
    <p class="scope-note muted small">Workspace and project badges count interruptions. All also includes work ready for review.</p>
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
      {#if permission === 'denied'}<span class="muted small">Alerts are blocked in browser settings.</span>{/if}
      {#if permission !== 'unsupported'}
        <button
          class="btn small alerts"
          class:on={optedIn}
          onclick={toggleNotifications}
          title="Browser alerts for questions, blocked or failed runs, review and repository risk, while this page stays open in a browser. Werkbord has no push service."
        >
          <span class="dot" data-tone={optedIn ? 'ok' : 'neutral'}></span>{optedIn ? 'Alerts on' : 'Alerts off'}
        </button>
      {/if}
    </div>
  </div>

  <div class="split">
    <div class="pane" aria-label="Workspace">
      {#if app.waiting.length && segment === 'all'}
        <section class="waiting-list" aria-labelledby="waiting-h">
          <h2 id="waiting-h" class="ph">Team tickets waiting for a repository<span class="chip">{app.waiting.length}</span></h2>
          <ul>
            {#each app.waiting as item (item.sourceRef)}<WaitingRepository {item} />{/each}
          </ul>
        </section>
      {/if}
      {#if !loaded}
        <p class="empty">Loading…</p>
      {:else if shown.length === 0 && !(app.waiting.length && segment === 'all')}
        <div class="clear">
          <span class="dot" data-tone={staleWork ? 'neutral' : 'ok'}></span>
          <div>
            <p class="clear-title">{app.connection !== 'live' ? 'Waiting for current status.' : segment === 'all' ? 'Nothing needs you.' : 'Nothing here.'}</p>
            <p class="muted">
              {staleWork ? 'These are the last reported states. Reconnect or wait for the runner to report before relying on execution status.' : working.length
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
      {#if working.length}
        <section class="pn">
          <div class="ph">{staleWork ? 'Last reported work' : 'Running now'}<span class="chip">{working.length}</span></div>
          <ul class="rows">
            {#each working as r (r.run.id)}
              {@const activity = cardActivity(r.run)}
              <li class="run" class:live={!runReportStale(r.run, app.connection === 'live', app.runners)}>
                <a href={taskHref(r.run.projectId, r.run.taskId)}><span class="dot" data-tone="work"></span>{r.taskTitle}</a>
                <RunBadge run={r.run} />
                <p class="mm">{r.projectName} · {agentName(app.agents, r.run.agentId)} · {runElapsed(r.run, app.now)}</p>
                {#if activity.length > 90}
                  <details class="run-activity">
                    <summary class="mm act">Activity: {oneLine(activity, 90)}</summary>
                    <p class="mm">{activity}</p>
                  </details>
                {:else if activity}<p class="mm act">› {activity}</p>{/if}
              </li>
            {/each}
          </ul>
        </section>
      {/if}

      {#if scheduled.length}
        <section class="pn">
          <div class="ph">Next executions<span class="chip">{scheduled.length}</span></div>
          <ul class="rows">
            {#each scheduled as s (s.task.id)}
              {@const when = s.decision.state === 'waiting_schedule' ? s.task.orchestration?.scheduledAt : undefined}
              <li class="run">
                <a href={taskHref(s.task.projectId, s.task.id)}><Icon name="calendar" size={14} />{s.task.title}</a>
                <p class="mm">{s.projectName} · {schedulingLabels[s.decision.state]}{when ? ` · ${formatDateTime(when)}` : ''}</p>
              </li>
            {/each}
          </ul>
        </section>
      {/if}
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

  .scope-note {
    margin: 0;
    font-size: 12px;
  }

  .segs .n {
    font-size: 11.5px;
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

  .run.live a .dot {
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
      font-size: 14px;
    }

    .filters {
      margin-left: 0;
    }
  }

  @media (max-width: 899px), (pointer: coarse) {
    .filters .btn.small {
      min-height: 44px;
    }

    .segs button {
      min-height: 44px;
      height: auto;
    }

    .run-activity > summary {
      min-height: 44px;
      display: flex;
      align-items: center;
    }
  }
  .waiting-list {
    margin-bottom: 18px;
  }
  .waiting-list h2 {
    margin: 0 0 8px;
  }
  .waiting-list ul {
    display: flex;
    flex-direction: column;
    gap: 8px;
    margin: 0;
    padding: 0;
  }
</style>

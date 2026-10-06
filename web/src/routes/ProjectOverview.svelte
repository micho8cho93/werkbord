<script lang="ts">
  import { attentionItems } from '../lib/attention';
  import AttentionCard from '../lib/attention/AttentionCard.svelte';
  import { agentLabel } from '../lib/execution';
  import { runElapsed, runStatus } from '../lib/format';
  import FindingCard from '../lib/git/FindingCard.svelte';
  import { gitStore } from '../lib/git/store.svelte';
  import { headlineTone, splitFindings } from '../lib/health';
  import HistoryStrip from '../lib/HistoryStrip.svelte';
  import { globalHref, projectHref, taskHref } from '../lib/router.svelte';
  import { shortRunId } from '../lib/runs';
  import type { ProjectScope } from '../lib/scope.svelte';
  import { app } from '../lib/state.svelte';
  import type { Project, Run } from '../lib/types';

  // One project at a glance: what needs you here, what ran, how the repository stands, which
  // agents are ready, and what happened lately. Each panel leads to the page that has the rest.

  let { scope, project }: { scope: ProjectScope; project: Project } = $props();

  const git = $derived(gitStore(project.id));
  $effect(() => git.watch());

  const items = $derived(attentionItems(app.overview, app.questions, { project: project.id }, app.runners));
  const titleOf = (id: string) => scope.activeTasks.find((t) => t.id === id)?.title ?? 'Task';
  const day = $derived(app.now - 86_400_000);
  const recent = $derived(scope.history.filter((r) => !r.endedAt || Date.parse(r.endedAt) >= day || Date.parse(r.createdAt) >= day));
  const table = $derived(scope.history.slice(0, 8));
  const health = $derived(git.health);
  const findings = $derived(splitFindings(health).needs);
  const runnerName = (r: Run) => {
    const m = app.runners.find((x) => x.id === r.runnerId);
    return m ? (m.kind === 'local' ? 'this computer' : m.name) : r.remote ? (r.runnerId ?? 'remote') : 'this computer';
  };

  /** What happened lately, from the runs themselves: started, asked, finished. */
  const timeline = $derived.by(() => {
    const ev: { at: string; text: string; taskId: string; tone: string }[] = [];
    for (const r of scope.history.slice(0, 40)) {
      const t = titleOf(r.taskId);
      const agent = agentLabel(app.agents, r.agentId);
      ev.push({ at: r.createdAt, text: `${agent} started on “${t}”`, taskId: r.taskId, tone: 'work' });
      if (r.endedAt) {
        const s = runStatus(r);
        const what = r.state === 'completed' ? 'finished' : r.state === 'failed' ? 'failed' : 'was stopped';
        ev.push({ at: r.endedAt, text: `“${t}” ${what}`, taskId: r.taskId, tone: s.tone });
      } else if (r.state === 'waiting_for_user' && r.waiting === 'question') {
        ev.push({ at: r.updatedAt, text: `${agent} asked a question on “${t}”`, taskId: r.taskId, tone: 'ask' });
      } else if (r.state === 'blocked') {
        ev.push({ at: r.blocker?.raisedAt ?? r.updatedAt, text: `${agent} stopped on “${t}” rather than guess`, taskId: r.taskId, tone: 'block' });
      }
    }
    return ev.sort((a, b) => b.at.localeCompare(a.at)).slice(0, 8);
  });

  const when = (iso: string) => {
    const d = new Date(iso);
    return app.now - d.getTime() < 86_400_000
      ? d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' })
      : d.toLocaleDateString([], { weekday: 'short' });
  };

  const agentRuns = (id: string) => scope.history.filter((r) => r.agentId === id && runStatus(r).active).length;
</script>

<div class="split">
  <div class="pane">
    <section class="pn">
      <div class="ph">Needs you<span class="chip">{items.length}</span></div>
      {#if items.length}
        <ul class="feed">
          {#each items.slice(0, 3) as item (item.key)}<AttentionCard {item} showProject={false} />{/each}
        </ul>
        {#if items.length > 3}<a class="more" href={globalHref('control')}>{items.length - 3} more in the Control Center →</a>{/if}
      {:else}
        <p class="muted small">Nothing here needs you. Agents are working or waiting for work.</p>
      {/if}
    </section>

    <section class="pn">
      <div class="ph">Runs<span class="chip">{recent.length}</span><span class="mm end">last 24 hours</span></div>
      <HistoryStrip runs={scope.history} />
      {#if table.length}
        <div class="tbl" role="table" aria-label="Latest runs">
          <div class="tr th" role="row">
            <span role="columnheader">Run</span><span role="columnheader">Task</span><span role="columnheader">Agent</span><span role="columnheader">Runner</span><span role="columnheader">State</span><span role="columnheader">Time</span>
          </div>
          {#each table as r (r.id)}
            {@const s = runStatus(r)}
            <a class="tr" role="row" href={taskHref(project.id, r.taskId)}>
              <span class="mm" role="cell">{shortRunId(r.id)}</span>
              <span class="task" role="cell">{titleOf(r.taskId)}</span>
              <span role="cell">{agentLabel(app.agents, r.agentId)}</span>
              <span class="mm" role="cell">{runnerName(r)}</span>
              <span class="st" role="cell"><span class="dot" data-tone={s.tone}></span>{s.label}</span>
              <span class="mm" role="cell">{runElapsed(r, app.now)}</span>
            </a>
          {/each}
        </div>
        <a class="more" href={projectHref(project.id, 'runs')}>All runs →</a>
      {:else}
        <p class="muted small">{scope.loaded ? 'No agent has run in this project yet.' : 'Loading…'}</p>
      {/if}
    </section>
  </div>

  <aside class="pane" aria-label="Project">
    <section class="pn">
      <div class="ph">Git{#if project.repository?.currentBranch}<span class="chip">{project.repository.currentBranch}</span>{/if}<a class="end small" href={projectHref(project.id, 'git')}>Open →</a></div>
      {#if health}
        <p class="big"><span class="dot" data-tone={headlineTone(health)}></span>{health.summary.headline}</p>
        {#each findings.slice(0, 2) as f (f.id)}<FindingCard finding={f} projectId={project.id} store={git} compact />{/each}
        {#if findings.length > 2}<a class="more" href={projectHref(project.id, 'git')}>{findings.length - 2} more in Git →</a>{/if}
      {:else if git.overview}
        <p class="big"><span class="dot" data-tone="neutral"></span>{git.overview.summary.devboard} Werkbord branches</p>
      {:else}
        <p class="muted small">{git.error || 'Looking at the repository…'}</p>
      {/if}
      {#if git.overview}
        <p class="mm">
          {git.overview.summary.mergeable} ready to merge · {git.overview.summary.unpushed} unpushed · {git.overview.summary.cleanup} to clean up
        </p>
      {/if}
    </section>

    <section class="pn">
      <div class="ph">Agents</div>
      {#each app.agents as a (a.id)}
        <div class="agent">
          <span class="dot" data-tone={a.available ? 'ok' : 'bad'}></span>
          <span class="aname">{a.name}</span>
          <span class="mm">{a.available ? 'ready' : (a.detail ?? 'unavailable')}</span>
          <span class="mm end">{agentRuns(a.id)} active</span>
        </div>
      {:else}
        <p class="muted small">No agents found on this computer.</p>
      {/each}
    </section>

    <section class="pn">
      <div class="ph">Activity</div>
      {#if timeline.length}
        <ul class="tl">
          {#each timeline as e, i (i)}
            <li><span class="mm t">{when(e.at)}</span><a href={taskHref(project.id, e.taskId)}>{e.text}</a></li>
          {/each}
        </ul>
      {:else}
        <p class="muted small">Nothing has happened yet.</p>
      {/if}
    </section>
  </aside>
</div>

<style>
  .split {
    --side: 380px;
  }

  .feed {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .feed :global(.card) {
    box-shadow: none;
    background: var(--bg);
  }

  .small {
    font-size: 13px;
  }

  .end {
    margin-left: auto;
  }

  .ph a {
    font-weight: 500;
    font-size: 12px;
    text-decoration: none;
  }

  .more {
    font-size: 12.5px;
    text-decoration: none;
    align-self: flex-start;
  }

  .tbl {
    display: flex;
    flex-direction: column;
  }

  .tr {
    display: grid;
    grid-template-columns: 76px minmax(0, 1.6fr) minmax(0, 1fr) minmax(0, 1fr) 128px 64px;
    gap: 10px;
    align-items: center;
    min-height: 38px;
    border-top: 1px solid var(--border);
    font-size: 13px;
    color: var(--text);
    text-decoration: none;
  }

  a.tr:hover {
    background: color-mix(in srgb, var(--surface-2) 60%, transparent);
  }

  .tr > * {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .th {
    min-height: 26px;
    border-top: 0;
    font-family: var(--mono);
    font-size: 10px;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    color: var(--text-2);
  }

  .task {
    font-weight: 500;
  }

  .st {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 12.5px;
  }

  .big {
    display: flex;
    align-items: center;
    gap: 10px;
    font-size: 17px;
    font-weight: 600;
    letter-spacing: -0.01em;
  }

  .agent {
    display: flex;
    align-items: center;
    gap: 10px;
    font-size: 13px;
  }

  .aname {
    font-weight: 500;
  }

  .tl {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .tl li {
    display: flex;
    gap: 12px;
    align-items: baseline;
    font-size: 13px;
    line-height: 1.35;
  }

  .tl .t {
    width: 44px;
    flex: none;
  }

  .tl a {
    color: var(--text);
    text-decoration: none;
  }

  .tl a:hover {
    text-decoration: underline;
  }

  @media (max-width: 899px) {
    .th {
      display: none;
    }

    .tr {
      grid-template-columns: minmax(0, 1fr) auto !important;
      gap: 2px 12px;
      padding: 8px 0;
    }

    .tr > :nth-child(1),
    .tr > :nth-child(4) {
      display: none;
    }

    .tr > :nth-child(3) {
      display: block !important;
      font-size: 12px;
      color: var(--text-2);
    }
  }

  @media (max-width: 1320px) {
    .tr {
      grid-template-columns: 70px minmax(0, 1fr) 120px 56px;
    }

    .tr > :nth-child(3),
    .tr > :nth-child(4) {
      display: none;
    }
  }
</style>

<script lang="ts">
  import { agentName, cardActivity, runElapsed } from '../lib/format';
  import { interactionShort, isNotable } from '../lib/policy';
  import ProjectAvatar from '../lib/ProjectAvatar.svelte';
  import { activitySummary } from '../lib/projects';
  import QuestionCard from '../lib/QuestionCard.svelte';
  import { needsInputText } from '../lib/questions';
  import RunBadge from '../lib/RunBadge.svelte';
  import { projectHref, taskHref } from '../lib/router.svelte';
  import { app } from '../lib/state.svelte';
  import type { AttentionRun, Question } from '../lib/types';

  // The Control Center is the one place that looks across projects. Everything here is read from
  // the controller's overview, which names each item's project and task; none of it is filtered on this side.
  const runs = $derived(app.overview?.runs ?? []);
  const blocked = $derived(runs.filter((r) => r.run.state === 'blocked'));
  const idle = $derived(runs.filter((r) => r.run.state === 'waiting_for_user' && r.run.waiting === 'idle'));
  const working = $derived(runs.filter((r) => r.run.state === 'running' || r.run.state === 'starting'));
  const loaded = $derived(app.connection === 'live' && app.overview !== null);

  const where = (q: Question) => app.taskInfo(q.taskId, q.projectId);
  const agentOf = (q: Question) => {
    const r = runs.find((x) => x.run.id === q.runId);
    return r ? agentName(app.agents, r.run.agentId) : '';
  };
  const href = (r: AttentionRun) => taskHref(r.run.projectId, r.run.taskId);
</script>

<div class="sections">
  {#if app.overview && app.overview.projects.length > 1}
    <section aria-labelledby="projects-h">
      <h2 id="projects-h">Projects</h2>
      <ul class="strip">
        {#each app.overview.projects as p (p.projectId)}
          <li>
            <a class="card proj" href={projectHref(p.projectId)} onclick={() => app.enter(p.projectId)}>
              <ProjectAvatar id={p.projectId} name={p.name} size={30} />
              <span class="what">
                <span class="name">{p.name}</span>
                <span class="sub muted">{activitySummary(p) || 'Quiet'}</span>
              </span>
            </a>
          </li>
        {/each}
      </ul>
    </section>
  {/if}

  <!-- What an agent is blocked on comes first, and is hard to miss. -->
  <section class="needs" aria-labelledby="needs-input" data-count={app.questions.length}>
    <h2 id="needs-input">
      Needs input
      {#if app.questions.length}<span class="n" aria-label={needsInputText(app.questions.length)}>{app.questions.length}</span>{/if}
    </h2>
    {#if app.questions.length}
      <ul class="list">
        {#each app.questions as q (q.id)}
          {@const info = where(q)}
          <li class="card item ask">
            <div class="where">
              <a class="who" href={taskHref(q.projectId, q.taskId)}>{info?.title ?? 'Task'}</a>
              <span class="project-chip">
                <ProjectAvatar id={q.projectId} name={info?.projectName || '?'} size={16} />
                {info?.projectName ?? ''}{agentOf(q) ? ` · ${agentOf(q)}` : ''}
              </span>
            </div>
            <QuestionCard question={q} />
          </li>
        {/each}
      </ul>
    {:else}
      <p class="card empty">{loaded ? 'No agent is waiting for an answer.' : 'Loading…'}</p>
    {/if}
  </section>

  {#if blocked.length}
    <section class="blocked" aria-labelledby="blocked-h">
      <h2 id="blocked-h">
        Blocked
        <span class="n b" aria-label="{blocked.length} blocked">{blocked.length}</span>
      </h2>
      <ul class="list">
        {#each blocked as r (r.run.id)}
          <li class="card item block">
            <div class="where">
              <a class="who" href={href(r)}>{r.taskTitle}</a>
              <span class="project-chip">
                <ProjectAvatar id={r.run.projectId} name={r.projectName} size={16} />
                {r.projectName} · {agentName(app.agents, r.run.agentId)}
              </span>
            </div>
            <RunBadge run={r.run} />
            <p class="summary">{r.run.blocker?.summary}</p>
            {#if r.run.blocker?.detail}<p class="muted small detail">{r.run.blocker.detail}</p>{/if}
            <a class="btn small go" href={href(r)}>Settle it</a>
          </li>
        {/each}
      </ul>
    </section>
  {/if}

  {#if idle.length}
    <section aria-labelledby="ready">
      <h2 id="ready">Ready for your next message</h2>
      <ul class="list">
        {#each idle as r (r.run.id)}
          <li class="card item">
            <div class="where">
              <a class="who" href={href(r)}>{r.taskTitle}</a>
              <span class="project-chip">
                <ProjectAvatar id={r.run.projectId} name={r.projectName} size={16} />
                {r.projectName} · {agentName(app.agents, r.run.agentId)} · finished its turn
              </span>
            </div>
            {#if cardActivity(r.run)}<p class="activity">{cardActivity(r.run)}</p>{/if}
          </li>
        {/each}
      </ul>
    </section>
  {/if}

  <section>
    <h2>Running</h2>
    {#if working.length}
      <ul class="list">
        {#each working as r (r.run.id)}
          <li class="card item">
            <div class="where">
              <a class="who" href={href(r)}>{r.taskTitle}</a>
              <span class="project-chip">
                <ProjectAvatar id={r.run.projectId} name={r.projectName} size={16} />
                {r.projectName}
              </span>
            </div>
            <div class="line">
              <RunBadge run={r.run} />
              <span class="muted small">
                {agentName(app.agents, r.run.agentId)} · {runElapsed(r.run, app.now)}
                {#if isNotable(r.run.policy)}· {interactionShort(r.run.policy)}{/if}
              </span>
            </div>
            {#if cardActivity(r.run)}<p class="activity">{cardActivity(r.run)}</p>{/if}
          </li>
        {/each}
      </ul>
    {:else}
      <p class="card empty">{loaded ? 'No agent is working right now.' : 'Loading…'}</p>
    {/if}
  </section>

  <section>
    <h2>Agents</h2>
    {#if app.agents.length}
      <ul class="list">
        {#each app.agents as a (a.id)}
          <li class="card item row">
            <span>{a.name}{#if a.version}<span class="muted small"> {a.version}</span>{/if}</span>
            <span class="pill" data-ok={a.available}>{a.available ? 'Ready' : (a.detail ?? 'Unavailable')}</span>
          </li>
        {/each}
      </ul>
    {:else}
      <p class="card empty">{loaded ? 'No agents are configured.' : 'Loading…'}</p>
    {/if}
  </section>
</div>

<style>
  .sections {
    display: grid;
    gap: 20px;
    max-width: 760px;
  }

  section {
    display: grid;
    gap: 8px;
  }

  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 8px;
  }

  .item {
    padding: 12px;
    display: grid;
    gap: 6px;
  }

  .strip {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(210px, 1fr));
    gap: 8px;
  }

  .proj {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 12px;
    color: inherit;
    text-decoration: none;
  }

  .proj:hover {
    border-color: color-mix(in srgb, var(--accent) 40%, var(--border));
  }

  .proj .what {
    display: grid;
    min-width: 0;
  }

  .proj .name {
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .proj .sub {
    font-size: 0.78rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .project-chip {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 0.8rem;
    color: var(--text-2);
  }

  .blocked h2 {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--block);
  }

  .n.b {
    background: var(--block);
    color: #fff;
  }

  .item.block {
    gap: 8px;
    padding: 14px;
    border: 1px solid color-mix(in srgb, var(--block) 50%, var(--border));
    border-left: 4px solid var(--block);
    background: color-mix(in srgb, var(--block) 6%, var(--surface));
  }

  .summary {
    font-weight: 550;
    overflow-wrap: anywhere;
  }

  .detail {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .go {
    justify-self: start;
    text-decoration: none;
  }

  .needs h2 {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .needs[data-count]:not([data-count='0']) h2 {
    color: var(--warn);
  }

  .n {
    min-width: 20px;
    height: 20px;
    padding: 0 6px;
    border-radius: 999px;
    background: var(--warn);
    color: #1a1204;
    font-size: 0.75rem;
    font-weight: 700;
    line-height: 20px;
    text-align: center;
  }

  /* Hard to miss: a coloured edge and a tint, like an alert, not a list row. */
  .item.ask {
    gap: 12px;
    padding: 14px;
    border: 1px solid color-mix(in srgb, var(--warn) 55%, var(--border));
    border-left: 4px solid var(--warn);
    background: color-mix(in srgb, var(--warn) 7%, var(--surface));
  }

  .where {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 2px 10px;
  }

  .who {
    font-weight: 600;
    color: inherit;
    text-decoration: none;
    overflow-wrap: anywhere;
  }

  .who:hover {
    text-decoration: underline;
  }

  .line {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 2px 10px;
  }

  .activity {
    font-size: 0.82rem;
    color: var(--text-2);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }

  .small {
    font-size: 0.8rem;
  }

  .pill {
    flex: none;
    max-width: 60%;
    padding: 2px 8px;
    border-radius: 999px;
    background: var(--surface-2);
    font-size: 0.78rem;
    font-weight: 600;
    text-align: right;
    overflow-wrap: anywhere;
  }

  .pill[data-ok='true'] {
    color: var(--ok);
  }
</style>

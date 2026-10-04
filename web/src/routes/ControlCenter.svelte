<script lang="ts">
  import '../lib/git/git.css';
 import { matchesScheduledExecution } from '../lib/control';
 import ControlInfrastructure from '../lib/ControlInfrastructure.svelte';
  import { schedulingLabels } from '../lib/scheduling';
  import { agentName, cardActivity, runElapsed, timeAgo } from '../lib/format';
  import { overviewHref } from '../lib/gitroute';
  import { basisLabel, severityLabel, severityTone } from '../lib/health';
  import { interactionShort, isNotable } from '../lib/policy';
  import ProjectAvatar from '../lib/ProjectAvatar.svelte';
  import { activitySummary } from '../lib/projects';
  import QuestionCard from '../lib/QuestionCard.svelte';
  import { needsInputText } from '../lib/questions';
  import { disableNotifications, enableNotifications, notificationPermission, notificationsEnabled } from '../lib/notifications';
  import RunBadge from '../lib/RunBadge.svelte';
  import { projectHref, taskHref } from '../lib/router.svelte';
  import { app } from '../lib/state.svelte';
  import type { AttentionReview, AttentionRun, Question } from '../lib/types';

  // Clock and external Git changes can alter eligibility without a task event.
  $effect(() => {
    const timer = setInterval(() => {
      if (document.visibilityState === 'visible') app.refreshOverview();
    }, 10000);
    return () => clearInterval(timer);
  });
  // The Control Center is the one place that looks across projects. Everything here is read from
  // the controller's overview, which names each item's project and task; the filters below narrow this global view.
  let projectFilter=$state(''); let runnerFilter=$state(''); let agentFilter=$state('');
 const matchRun=(r:import('../lib/types').Run)=>(!projectFilter||r.projectId===projectFilter)&&(!runnerFilter||r.runnerId===runnerFilter)&&(!agentFilter||r.agentId===agentFilter);
 const matchProject=(id:string)=>(!projectFilter||id===projectFilter)&&(!runnerFilter||app.runners.some(r=>r.id===runnerFilter&&(r.kind==='local'||r.projects?.includes(id))));
 const runs = $derived((app.overview?.runs ?? []).filter(x=>matchRun(x.run)));
 const questions=$derived(app.questions.filter(q=>matchProject(q.projectId)&&(!runnerFilter&&!agentFilter||runs.some(r=>r.run.id===q.runId))));
  const blocked = $derived(runs.filter((r) => r.run.state === 'blocked'));
  const idle = $derived(runs.filter((r) => r.run.state === 'waiting_for_user' && r.run.waiting === 'idle'));
  const working = $derived(runs.filter((r) => r.run.state === 'running' || r.run.state === 'starting'));
  const loaded = $derived(app.connection === 'live' && app.overview !== null);

  // The exceptions: what needs a person. Everything else is quiet background activity, kept out of the way.
  const scheduled = $derived((app.overview?.orchestration ?? []).filter(x=>matchProject(x.task.projectId)&&matchesScheduledExecution(x.task,app.project(x.task.projectId)?.execution ?? {},app.globalExecution,runnerFilter,agentFilter)));
  const scheduleBlocked = $derived(scheduled.filter((x) => x.decision.state === 'blocked' || x.decision.state === 'potentially_conflicting'));
  const scheduleQuiet = $derived(scheduled.filter((x) => x.decision.state !== 'blocked' && x.decision.state !== 'potentially_conflicting'));
  const failed = $derived((app.overview?.failed ?? []).filter(x=>matchRun(x.run)));
  const reviewTasks = $derived((app.overview?.review ?? []).filter(x=>matchProject(x.task.projectId)&&(!runnerFilter&&!agentFilter||x.lastRun&&matchRun(x.lastRun))));
  const repository = $derived((app.overview?.repository ?? []).filter(x=>matchProject(x.projectId)));
  const readyCount = $derived(idle.length + reviewTasks.length);
  const allClear = $derived(
    loaded && questions.length === 0 && blocked.length === 0 && failed.length === 0 && readyCount === 0 && repository.length === 0 && scheduleBlocked.length === 0,
  );
  const reviewHref = (r: AttentionReview) => taskHref(r.task.projectId, r.task.id);

  const where = (q: Question) => app.taskInfo(q.taskId, q.projectId);
  const agentOf = (q: Question) => {
    const r = runs.find((x) => x.run.id === q.runId);
    return r ? agentName(app.agents, r.run.agentId) : '';
  };
  const href = (r: AttentionRun) => taskHref(r.run.projectId, r.run.taskId);
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

<div class="sections">
 <ControlInfrastructure filtersOnly bind:project={projectFilter} bind:runner={runnerFilter} bind:agent={agentFilter} />
  <section class="notify-setting" aria-label="Browser notifications">
    <div>
      <strong>Browser notifications</strong>
      <p class="muted small">Optional alerts for questions, blocked or failed runs, review, and important repository risks. They work only while this browser can stay connected; there is no push relay.</p>
    </div>
    {#if permission === 'unsupported'}
      <span class="muted small">Not supported here</span>
    {:else}
      <button class="btn small" onclick={toggleNotifications}>{optedIn ? 'Turn off' : permission === 'denied' ? 'Blocked by browser' : 'Enable alerts'}</button>
    {/if}
  </section>
  {#if app.overview && app.overview.projects.length > 1}
    <section aria-labelledby="projects-h">
      <h2 id="projects-h">Projects</h2>
      <ul class="strip">
        {#each app.overview.projects.filter(p=>matchProject(p.projectId)) as p (p.projectId)}
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

  <!-- This view is for exceptions: what needs an answer, what is stuck, what failed, what is ready, and where a
       repository is at risk. Successful background activity is kept out of the way, below. -->
  {#if allClear}
    <p class="card clear" role="status">
      <strong>All clear.</strong>
      <span class="muted">
        Nothing needs you{#if working.length}; {working.length === 1 ? '1 run retains' : `${working.length} runs retain`} execution ownership{/if}.
      </span>
    </p>
  {/if}

  <!-- What an agent is blocked on comes first, and is hard to miss. -->
  {#if questions.length}
    <section class="needs" aria-labelledby="needs-input" data-count={questions.length}>
      <h2 id="needs-input">
        Needs input
        <span class="n" aria-label={needsInputText(questions.length)}>{questions.length}</span>
      </h2>
      <ul class="list">
        {#each questions as q (q.id)}
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
    </section>
  {/if}

{#if scheduleBlocked.length}
 <section aria-labelledby="schedule-blocked"><h2 id="schedule-blocked">Blocked execution <span class="n b">{scheduleBlocked.length}</span></h2><ul class="list">
 {#each scheduleBlocked as item (item.task.id)}<li class="card item"><div class="where"><a class="who" href={taskHref(item.task.projectId,item.task.id)}>{item.task.title}</a><span class="muted">{item.projectName}</span></div><p><strong>{schedulingLabels[item.decision.state]}</strong> · {item.decision.reason}</p></li>{/each}
 </ul></section>
 {/if}
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

  {#if failed.length}
    <section class="failed" aria-labelledby="failed-h">
      <h2 id="failed-h">
        Failed
        <span class="n f" aria-label="{failed.length} failed">{failed.length}</span>
      </h2>
      <ul class="list">
        {#each failed as r (r.run.id)}
          <li class="card item fail">
            <div class="where">
              <a class="who" href={href(r)}>{r.taskTitle}</a>
              <span class="project-chip">
                <ProjectAvatar id={r.run.projectId} name={r.projectName} size={16} />
                {r.projectName} · {agentName(app.agents, r.run.agentId)}{#if r.run.endedAt} · {timeAgo(r.run.endedAt, app.now)}{/if}
              </span>
            </div>
            {#if cardActivity(r.run)}<p class="summary">{cardActivity(r.run)}</p>{/if}
            <a class="btn small go" href={href(r)}>Look at it</a>
          </li>
        {/each}
      </ul>
    </section>
  {/if}

  {#if readyCount}
    <section aria-labelledby="ready">
      <h2 id="ready">
        Ready for review
        <span class="n r" aria-label="{readyCount} ready for review">{readyCount}</span>
      </h2>
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
        {#each reviewTasks as r (r.task.id)}
          <li class="card item">
            <div class="where">
              <a class="who" href={reviewHref(r)}>{r.task.title}</a>
              <span class="project-chip">
                <ProjectAvatar id={r.task.projectId} name={r.projectName} size={16} />
                {r.projectName}{#if r.lastRun} · {agentName(app.agents, r.lastRun.agentId)}{/if} · in Review
              </span>
            </div>
          </li>
        {/each}
      </ul>
    </section>
  {/if}

  <!-- Only a risk or worse is listed here. Ordinary findings and housekeeping are in the project's Git screen. -->
  {#if repository.length}
    <section class="risk" aria-labelledby="risk-h">
      <h2 id="risk-h">
        Repository risk
        <span class="n k" aria-label="{repository.length} at risk">{repository.length}</span>
      </h2>
      <ul class="list">
        {#each repository as f (f.id)}
          <li class="card item repo" data-severity={f.severity}>
            <div class="where">
              <span class="g-chip" data-tone={severityTone(f.severity)}>{severityLabel(f.severity)}</span>
              <span class="project-chip">
                <ProjectAvatar id={f.projectId} name={f.projectName || '?'} size={16} />
                {f.projectName}
              </span>
              <span class="muted small">{basisLabel(f.basis)}</span>
            </div>
            <p class="summary">{f.title}</p>
            <p class="muted small">{f.explanation}</p>
            <p class="small"><strong>Next:</strong> {f.action.label}</p>
            <a class="btn small go" href={overviewHref(f.projectId)} onclick={() => app.enter(f.projectId)}>Open Git</a>
          </li>
        {/each}
      </ul>
    </section>
  {/if}

  {#if !loaded}
    <p class="card empty">Loading…</p>
  {/if}

  <!-- Quiet, successful background activity: here if you want it, never competing with the above. -->
  {#if working.length}
    <details class="quiet">
      <summary>Active runs ({working.length})</summary>
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
    </details>
  {/if}

  <ControlInfrastructure project={projectFilter} runner={runnerFilter} agent={agentFilter} />

  {#if app.agents.length}
    <details class="quiet">
      <summary>Agents ({app.agents.length})</summary>
      <ul class="list">
        {#each app.agents as a (a.id)}
          <li class="card item row">
            <span>{a.name}{#if a.version}<span class="muted small"> {a.version}</span>{/if}</span>
            <span class="pill" data-ok={a.available}>{a.available ? 'Ready' : (a.detail ?? 'Unavailable')}</span>
          </li>
        {/each}
      </ul>
    </details>
  {/if}
{#if scheduleQuiet.length}
 <section aria-labelledby="scheduled-queue"><h2 id="scheduled-queue">Scheduled and queued <span class="n">{scheduleQuiet.length}</span></h2><ul class="list">
 {#each scheduleQuiet as item (item.task.id)}<li class="card item"><div class="where"><a class="who" href={taskHref(item.task.projectId,item.task.id)}>{item.task.title}</a><span class="muted">{item.projectName}</span></div><p><strong>{schedulingLabels[item.decision.state]}</strong> · {item.decision.reason}</p></li>{/each}
 </ul></section>
 {/if}
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

  .notify-setting {
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
    gap: 12px;
    padding: 12px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--surface);
  }

  .notify-setting p { margin: 3px 0 0; }

  @media (max-width: 520px) {
    .notify-setting { grid-template-columns: 1fr; }
    .notify-setting .btn { justify-self: start; }
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

  .needs h2,
  .failed h2,
  .risk h2,
  #ready {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .failed h2 {
    color: var(--danger);
  }

  .risk h2 {
    color: var(--block);
  }

  .n.f {
    background: var(--danger);
    color: #fff;
  }

  .n.k {
    background: var(--block);
    color: #fff;
  }

  .n.r {
    background: var(--accent);
    color: var(--accent-text);
  }

  .item.fail {
    gap: 8px;
    padding: 14px;
    border: 1px solid color-mix(in srgb, var(--danger) 45%, var(--border));
    border-left: 4px solid var(--danger);
    background: color-mix(in srgb, var(--danger) 5%, var(--surface));
  }

  .item.repo {
    gap: 6px;
    padding: 14px;
    border: 1px solid color-mix(in srgb, var(--block) 45%, var(--border));
    border-left: 4px solid var(--block);
    background: color-mix(in srgb, var(--block) 5%, var(--surface));
  }

  .item.repo[data-severity='critical'] {
    border-color: color-mix(in srgb, var(--danger) 55%, var(--border));
    border-left-color: var(--danger);
    background: color-mix(in srgb, var(--danger) 7%, var(--surface));
  }

  .item.repo p {
    margin: 0;
    overflow-wrap: anywhere;
  }

  .clear {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 10px;
    padding: 14px;
    border-left: 4px solid var(--ok);
  }

  .quiet summary {
    cursor: pointer;
    padding: 8px 2px;
    font-weight: 600;
    color: var(--text-2);
  }

  .quiet[open] > summary {
    margin-bottom: 8px;
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

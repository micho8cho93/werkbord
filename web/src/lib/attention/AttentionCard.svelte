<script lang="ts">
  import { api } from '../api';
  import { KIND_LABEL, KIND_TONE, type AttentionItem } from '../attention';
  import { sinceShort } from '../board';
  import { agentLabel } from '../execution';
  import { oneLine } from '../format';
  import { overviewHref } from '../gitroute';
  import { severityLabel } from '../health';
  import Icon from '../Icon.svelte';
  import QuestionCard from '../QuestionCard.svelte';
  import { schedulingLabels } from '../scheduling';
  import { taskHref } from '../router.svelte';
  import { app } from '../state.svelte';

  // One thing that needs a person: what kind, where, how long it has waited, what exactly is
  // wanted, and the step that settles it. The Control Center and a project's Overview share it.

  let { item, showProject = true }: { item: AttentionItem; showProject?: boolean } = $props();

  const tone = $derived(KIND_TONE[item.kind]);
  const href = $derived(item.taskId ? taskHref(item.projectId, item.taskId) : overviewHref(item.projectId));
  const agent = $derived(item.run ? agentLabel(app.agents, item.run.agentId) : '');
  const where = $derived([showProject ? item.projectName : '', sinceShort(item.at, app.now)].filter(Boolean).join(' · '));

  let busy = $state('');
  async function reply(text: string) {
    if (!item.run || busy) return;
    busy = text;
    try {
      app.scope(item.projectId)?.upsertRun(await api.sendInput(item.projectId, item.run.id, text));
      app.refreshOverview();
    } catch (err) {
      app.notify(err instanceof Error ? err.message : String(err));
    } finally {
      busy = '';
    }
  }

  async function runAgain() {
    if (!item.taskId || busy) return;
    busy = 'again';
    try {
      app.scope(item.projectId)?.upsertRun(await api.startRun(item.projectId, item.taskId));
      app.notify(`Started again: “${item.title}”.`, 4000);
      app.refreshOverview();
    } catch (err) {
      app.notify(err instanceof Error ? err.message : String(err));
    } finally {
      busy = '';
    }
  }
</script>

{#if item.kind === 'review'}
  <li class="card row" data-kind={item.kind}>
    <span class="dot" data-tone="ok"></span>
    <div class="row-what">
      <a class="row-title" {href}>{item.title}</a>
      <p class="mm">{KIND_LABEL.review}{showProject ? ` · ${item.projectName}` : ''}{agent ? ` · ${agent}` : ''}</p>
    </div>
    <a class="btn small" {href}>Review</a>
  </li>
{:else}
  <li class="card item" data-kind={item.kind}>
    <p class="kind">
      <span class="dot" data-tone={tone} class:ring={tone === 'idle'}></span>
      <b>{item.kind === 'risk' && item.finding ? severityLabel(item.finding.severity) : KIND_LABEL[item.kind]}</b>
      <span>{where}</span>
      {#if agent}<span class="agent">{agent}</span>{/if}
    </p>
    <a class="title" {href}>{item.title}</a>

    {#if item.kind === 'question' && item.question}
      <QuestionCard question={item.question} showMeta={false} />
    {:else if item.kind === 'blocked' && item.run}
      <p class="well">{item.run.blocker?.summary ?? 'Stopped rather than guess.'}{#if item.run.blocker?.detail}<br />{oneLine(item.run.blocker.detail, 240)}{/if}</p>
      <div class="acts">
        {#each (item.run.blocker?.options ?? []).slice(0, 3) as o (o)}
          <button class="btn small" disabled={!!busy} onclick={() => reply(o)}>{busy === o ? 'Sending…' : o}</button>
        {/each}
        <a class="btn small" class:primary={!item.run.blocker?.options?.length} {href}><Icon name="reply" size={14} />Reply</a>
      </div>
    {:else if item.kind === 'idle' && item.run}
      {#if item.run.activity}<p class="well">{oneLine(item.run.activity, 220)}</p>{/if}
      <div class="acts"><a class="btn small primary" {href}><Icon name="reply" size={14} />Reply</a></div>
    {:else if item.kind === 'failed' && item.run}
      {#if item.run.reason}<p class="well">{oneLine(item.run.reason, 240)}</p>{/if}
      <div class="acts">
        <button class="btn small" disabled={!!busy} onclick={runAgain}><Icon name="play" size={12} />{busy === 'again' ? 'Starting…' : 'Run again'}</button>
        <a class="btn small" {href}>Look at it</a>
      </div>
    {:else if item.kind === 'schedule' && item.schedule}
      <p class="well"><strong>{schedulingLabels[item.schedule.decision.state]}</strong> · {item.schedule.decision.reason}</p>
      <div class="acts"><a class="btn small" {href}>Open task</a></div>
    {:else if item.kind === 'risk' && item.finding}
      <p class="expl">{item.finding.explanation}</p>
      <p class="next"><span class="mm">Next</span> {item.finding.action.label}</p>
      <div class="acts"><a class="btn small" href={overviewHref(item.projectId)} onclick={() => app.enter(item.projectId)}>Open Git</a></div>
    {/if}
  </li>
{/if}

<style>
  .item {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 14px;
    min-width: 0;
  }

  .item[data-kind='question'] {
    border-color: color-mix(in srgb, var(--warn) 55%, var(--border));
    box-shadow:
      var(--card-sh),
      0 0 0 3px var(--amber-ring);
  }

  .item[data-kind='failed'] {
    border-color: color-mix(in srgb, var(--danger) 40%, var(--border));
  }

  .item[data-kind='blocked'],
  .item[data-kind='schedule'],
  .item[data-kind='risk'] {
    border-color: color-mix(in srgb, var(--block) 40%, var(--border));
  }

  .kind {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 4px 8px;
    font-family: var(--mono);
    font-size: 11px;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--text-2);
  }

  .kind b {
    font-weight: 500;
    color: var(--text);
  }

  .kind .agent {
    margin-left: auto;
    letter-spacing: 0;
    text-transform: none;
  }

  @media (max-width: 599px) {
    .kind .agent {
      display: none;
    }
  }

  .dot.ring {
    background: transparent;
    box-shadow: inset 0 0 0 2px var(--accent);
  }

  .title,
  .row-title {
    font-weight: 500;
    font-size: 15px;
    line-height: 1.3;
    color: var(--text);
    text-decoration: none;
    overflow-wrap: anywhere;
  }

  .title:hover,
  .row-title:hover {
    text-decoration: underline;
  }

  .acts {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }

  .expl {
    font-size: 13px;
    color: var(--text-2);
    overflow-wrap: anywhere;
  }

  .next {
    font-size: 13px;
    display: flex;
    gap: 8px;
    align-items: baseline;
  }

  .row {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 12px 10px 14px;
    min-width: 0;
  }

  .row-what {
    flex: 1;
    min-width: 0;
  }

  .row-title {
    font-size: 14px;
    display: block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>

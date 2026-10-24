<script lang="ts">
  import { attentionAction, attentionGroups, standing, recordedTime, tallyByKind } from './aggregate';
  import { model } from './model.svelte';
  import WorkspaceTag from './WorkspaceTag.svelte';
  import type { Overview, OverviewEntry } from './types';
  import QuestionCard from '../lib/QuestionCard.svelte';
  import { ApiError } from '../lib/api';
  import type { Question } from '../lib/types';
  import { ask } from './native';

  let expanded = $state<Record<string, boolean>>({});
  let settled = $state<Record<string, boolean>>({});
  let now = $state(Date.now());
  $effect(() => { const timer = setInterval(() => now = Date.now(), 1000); return () => clearInterval(timer); });
  const source = (id: string): OverviewEntry | undefined => overview?.entries.find(e => e.workspace.id === id);
  function available(id: string): boolean {
    const e = source(id);
    return !!e?.summary && !e.error && e.workspace.state === 'ready' && now - Date.parse(e.summary.at) < 30000;
  }
  async function answerWith(q: Question, answer: string): Promise<Question> {
    if (!available('personal')) throw new Error('Individual is offline or stale. Reconnect before answering.');
    const result = await ask(a => a.Relay('personal', 'AnswerQuestion', [{ projectId: q.projectId, questionId: q.id, answer }])) as {
      status: number; body: Question & { error?: {code: string; message: string}; question?: Question };
    };
    if (result.status >= 400) throw new ApiError(result.status, result.body.error?.code ?? 'unknown', result.body.error?.message ?? 'Could not save the answer.', result.body.question);
    return result.body;
  }
  function onSettled(q: Question): void {
    settled[q.id] = true;
    void model.refreshOverview();
  }


  // What waits for the person or that they should know about, from every workspace: an agent's question, work that is blocked
  // or failed, a review someone asked for, and warnings about the machines a Team depends on. Worst first.
  // `embedded` is the Universal hub's panel: no title or tally of its own (the hub shows them), no page padding.
  let { overview, embedded = false }: { overview: Overview | null; embedded?: boolean } = $props();

  const groups = $derived(overview ? attentionGroups(overview) : []);
  const silent = $derived(overview ? overview.entries.filter((e) => e.error || e.workspace.state !== 'ready' || !e.summary) : []);
  const word: Record<string, string> = { critical: 'Urgent', warning: 'Needs you', info: 'For your information' };
  const tally = $derived(tallyByKind(groups));
</script>

<section class="page" class:embedded aria-labelledby="inbox-h">
  <header>
    <h1 id="inbox-h">Workspaces</h1>
    <p class="lede">Questions, blockers, reviews and warnings from all your workspaces</p>
  </header>

  {#if overview && !embedded}
    <ul class="tally" aria-label="Waiting by kind of workspace">
      {#each tally as t (t.kind)}
        <li data-kind={t.kind} data-testid="tally-{t.kind}">
          <span class="swatch" aria-hidden="true"></span>
          <span class="who">{t.label}</span>
          <span class="n">{t.actions}</span><span class="cap">{t.actions === 1 ? 'action needed' : 'actions needed'}</span>
          <span class="n">{t.warnings}</span><span class="cap">{t.warnings === 1 ? 'warning' : 'warnings'}</span>
        </li>
      {/each}
    </ul>
  {/if}

  {#each silent as e (e.workspace.id)}
    <p class="alert" role="status"><WorkspaceTag workspace={e.workspace} /> <strong>Current status unavailable.</strong> {standing(e)} Any displayed items are last reported; open the workspace to reconnect.</p>
  {/each}

  {#if !overview}
    <p class="empty">Looking…</p>
  {:else if groups.length === 0 && silent.length === 0}
    <p class="empty">Nothing needs you.</p>
  {/if}

  {#each groups as group (group.id)}
  <section class="group" aria-labelledby="attention-{group.id}">
    <h2 id="attention-{group.id}">{group.label}<span class="count">{group.rows.length}</span></h2>
    <ul>
    {#each group.rows as r, i (r.workspace.id + r.item.id)}
      {@const answering = expanded[r.item.id] && r.workspace.kind === 'personal' && r.item.kind === 'needs_input' && r.item.question}
      <li>
        <div class="row" data-severity={r.item.severity} data-kind={r.workspace.kind} data-testid="attention-item">
          <span class="sev" title={word[r.item.severity]} aria-label={word[r.item.severity]}></span>
          <span class="text">
            <span class="title">{answering ? 'Answering this question' : r.item.title}</span>
            <span class="context"><WorkspaceTag workspace={r.workspace} />{#if r.item.project}<span>{r.item.project}</span>{/if}</span>
            {#if r.item.detail}<span class="detail">{r.item.detail}</span>{/if}
          </span>
          {#if r.workspace.kind === 'personal' && r.item.kind === 'needs_input' && r.item.question && !settled[r.item.question.id]}
          <button class="btn action" type="button" aria-expanded={!!expanded[r.item.id]} aria-controls="inbox-answer-{r.item.id}" onclick={() => expanded[r.item.id] = !expanded[r.item.id]}>{expanded[r.item.id] ? 'Close answer' : 'Answer'}</button>
          {:else if !r.item.question || !settled[r.item.question.id]}
          <button class="btn action" class:primary={group.id !== 'warnings' && group.id === groups[0].id && i === 0} type="button" onclick={() => model.open(r.workspace.id, r.item.href)} aria-label="{r.workspace.kind === 'team' ? 'Open' : attentionAction(r.item)}: {r.item.title}">{r.workspace.kind === 'team' ? 'Open' : attentionAction(r.item)}</button>
          {/if}
        </div>
        {#if expanded[r.item.id] && r.workspace.kind === 'personal' && r.item.kind === 'needs_input' && r.item.question && !settled[r.item.question.id]}
          <section class="answer" id="inbox-answer-{r.item.id}" aria-label="Answer: {r.item.question.prompt}">
            {#if !available(r.workspace.id)}<p role="status">Answering is unavailable. Last reported: {recordedTime(source(r.workspace.id)?.summary?.at) || 'unknown'}. Open the workspace to reconnect.</p>{/if}
            <QuestionCard question={{ ...r.item.question, state: 'pending' }} revealContext disabled={!available(r.workspace.id)} {answerWith} {onSettled} notify={(message) => model.notice = message} />
          </section>
        {/if}
      </li>
    {/each}
    </ul>
  </section>
  {/each}
</section>

<style>
  .answer { padding: 0 14px 16px 36px; }
  .page {
    padding: 28px clamp(16px, 4vw, 48px);
    margin: 0 auto;
    overflow-y: auto;
    height: 100%;
  }
  .page.embedded { padding: 0; max-width: none; margin: 0; height: auto; overflow: visible; }
  .embedded header { display: none; }
  .embedded .group { margin-top: 14px; }
  .embedded .group:first-of-type { margin-top: 0; }
  .embedded ul { border-color: var(--border); box-shadow: none; }
  h1 {
    margin: 0;
  }
  .lede {
    margin: 4px 0 18px;
    color: var(--text-2);
  }
  .group { margin-top: 24px; }
  h2 { display: flex; align-items: center; gap: 8px; margin: 0 0 8px; font-size: 14px; }
  .count { color: var(--text-2); font-family: var(--mono); font-size: 11.5px; }
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--surface);
    box-shadow: var(--card-sh);
  }
  ul:empty {
    display: none;
  }
  li + li {
    border-top: 1px solid var(--border);
  }
  .row {
    --kind: var(--accent);
    box-shadow: inset 4px 0 0 var(--kind);
    display: grid;
    grid-template-columns: 10px minmax(0, 1fr) auto;
    gap: 12px;
    align-items: center;
    min-height: 64px;
    padding: 14px;
    font-size: 13px;
  }
  .row[data-kind='team'] { --kind: var(--block); }
  .tally {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
    gap: 12px;
    margin: 0 0 4px;
    padding: 0;
    border: 0;
    background: none;
    box-shadow: none;
  }
  .tally li {
    --kind: var(--accent);
    display: grid;
    grid-template-columns: auto 1fr auto auto auto auto;
    align-items: baseline;
    gap: 4px 6px;
    padding: 12px 14px;
    border: 1px solid color-mix(in srgb, var(--kind) 45%, var(--border));
    border-radius: var(--radius);
    background: color-mix(in srgb, var(--kind) 8%, var(--surface));
    box-shadow: var(--card-sh);
    font-size: 12px;
  }
  .tally li + li { border-top: 1px solid color-mix(in srgb, var(--kind) 45%, var(--border)); }
  .tally li[data-kind='team'] { --kind: var(--block); }
  .swatch { align-self: center; width: 10px; height: 10px; border-radius: 3px; background: var(--kind); }
  .who { font-weight: 600; font-size: 13px; }
  .n { font-family: var(--mono); font-size: 15px; font-weight: 600; text-align: right; }
  .cap { color: var(--text-2); margin-right: 6px; }
  .action { min-height: 44px; white-space: nowrap; }
  .sev {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--text-2);
  }
  [data-severity='warning'] .sev {
    background: var(--amber);
  }
  [data-severity='critical'] .sev {
    background: var(--danger);
  }
  .text {
    display: flex;
    flex-direction: column;
    min-width: 0;
    gap: 6px;
    overflow-wrap: anywhere;
  }
  .title {
    font-weight: 500;
  }
  .detail {
    color: var(--text-2);
    font-size: 12px;
    line-height: 1.5;
    max-width: 70ch;
  }
  .context { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; color: var(--text-2); font-size: 11.5px; }
  .alert {
    margin: 0 0 8px;
    padding: 10px 14px;
    border: 1px solid var(--border);
    border-left: 1px solid var(--danger);
    border-radius: var(--radius-sm);
    background: var(--surface);
  }
  .empty {
    color: var(--text-2);
  }
  @media (max-width: 720px) {
    .row { grid-template-columns: 10px minmax(0, 1fr); align-items: start; }
    .sev { margin-top: 6px; }
    .action { grid-column: 2; justify-self: start; white-space: normal; }
  }
</style>

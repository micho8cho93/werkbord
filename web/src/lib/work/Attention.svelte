<script lang="ts">
  import { attentionAction, attentionGroups, standing, recordedTime } from '../../shell/aggregate';
  import WorkspaceTag from '../WorkspaceTag.svelte';
  import type { Overview, OverviewEntry } from '../../shell/types';
  import QuestionCard from '../QuestionCard.svelte';
  import type { Question } from '../types';

  // What waits for the person or that they should know about, drawn once for the Mac window page and for the web app, under the cards of
  // Work: an agent's question (answered where it is listed, with the agent's own context in view), work that is blocked or failed, a review
  // someone asked for, and warnings about the machines a Team depends on. Worst first. It is handed the overview; each side says how an
  // answer is sent, what happens once one is accepted, and what opening an item does.
  let { overview, answerWith, onSettled, open, notify }: {
    overview: Overview | null;
    /** Sends the answer to the workspace that asked; `now` is the clock the page is using. */
    answerWith: (q: Question, answer: string, now: number) => Promise<Question>;
    /** The workspace accepted the answer, or says it was settled elsewhere. */
    onSettled: (q: Question) => void;
    /** An item may name no place to open. */
    open: (workspace: string, href: string | undefined) => void;
    notify: (message: string) => void;
  } = $props();

  let expanded = $state<Record<string, boolean>>({});
  let settled = $state<Record<string, boolean>>({});
  let now = $state(Date.now());
  $effect(() => { const timer = setInterval(() => now = Date.now(), 1000); return () => clearInterval(timer); });
  const source = (id: string): OverviewEntry | undefined => overview?.entries.find(e => e.workspace.id === id);
  function available(id: string): boolean {
    const e = source(id);
    return !!e?.summary && !e.error && e.workspace.state === 'ready' && now - Date.parse(e.summary.at) < 30000;
  }
  const send = (q: Question, answer: string) => answerWith(q, answer, now);
  function settle(q: Question): void {
    settled[q.id] = true;
    onSettled(q);
  }

  const groups = $derived(overview ? attentionGroups(overview) : []);
  const silent = $derived(overview ? overview.entries.filter((e) => e.error || e.workspace.state !== 'ready' || !e.summary) : []);
  const word: Record<string, string> = { critical: 'Urgent', warning: 'Needs attention', info: 'For your information' };
</script>

<details id="work-attention" class="attention">
<summary>Questions and workspace notices</summary>
<section class="page" aria-label="Questions and workspace notices">
  {#each silent as e (e.workspace.id)}
    <p class="alert" role="status"><WorkspaceTag workspace={e.workspace} /> <strong>Current status unavailable.</strong> {standing(e)} Any displayed items are last reported; open the workspace to reconnect.</p>
  {/each}

  {#if !overview}
    <p class="empty">Loading workspace activity…</p>
  {:else if groups.length === 0 && silent.length === 0}
    <p class="empty">Nothing needs your attention.</p>
  {/if}

  {#each groups as group (group.id)}
  <section class="group" aria-labelledby="attention-{group.id}">
    <h2 id="attention-{group.id}" class="section-title">{group.label}<span class="count">{group.rows.length}</span></h2>
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
          <button class="btn action" class:primary={group.id !== 'warnings' && group.id === groups[0].id && i === 0} type="button" onclick={() => open(r.workspace.id, r.item.href)} aria-label="{r.workspace.kind === 'team' ? 'Open' : attentionAction(r.item)}: {r.item.title}">{r.workspace.kind === 'team' ? 'Open' : attentionAction(r.item)}</button>
          {/if}
        </div>
        {#if expanded[r.item.id] && r.workspace.kind === 'personal' && r.item.kind === 'needs_input' && r.item.question && !settled[r.item.question.id]}
          <section class="answer" id="inbox-answer-{r.item.id}" aria-label="Answer: {r.item.question.prompt}">
            {#if !available(r.workspace.id)}<p role="status">Answering is unavailable. Last reported: {recordedTime(source(r.workspace.id)?.summary?.at) || 'unknown'}. Open the workspace to reconnect.</p>{/if}
            <QuestionCard question={{ ...r.item.question, state: 'pending' }} revealContext disabled={!available(r.workspace.id)} answerWith={send} onSettled={settle} {notify} />
          </section>
        {/if}
      </li>
    {/each}
    </ul>
  </section>
  {/each}
</section>
</details>

<style>
  .attention { border-top: 1px solid var(--border); padding-top: 14px; margin-top: 24px; }
  .attention summary { cursor: pointer; padding: 10px 0; color: var(--text-2); font-size: 13px; }
  .answer { padding: 0 14px 16px 36px; }
  .group { margin-top: 14px; }
  .group:first-of-type { margin-top: 0; }
  h2 { display: flex; align-items: center; gap: 8px; margin: 0 0 8px; font-size: 14px; }
  .count { color: var(--text-2); font-family: var(--mono); font-size: 12px; }
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
  .context { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; color: var(--text-2); font-size: 12px; }
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
  /* Paper, Graphite and Cloud: rows are plain list rows and the coloured edge, tinted fills and shadows stay in Classic. */
  :global(:root) .row { box-shadow: none; min-height: 56px; }
  :global(:root) ul { box-shadow: none; }
  @media (max-width: 720px) {
    .row { grid-template-columns: 10px minmax(0, 1fr); align-items: start; }
    .sev { margin-top: 6px; }
    .action { grid-column: 2; justify-self: start; white-space: normal; }
  }
</style>

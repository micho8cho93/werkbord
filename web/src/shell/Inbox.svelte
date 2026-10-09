<script lang="ts">
  import { attention, standing } from './aggregate';
  import { model } from './model.svelte';
  import WorkspaceTag from './WorkspaceTag.svelte';
  import type { Overview } from './types';

  // What waits for the person or that they should know about, from every workspace: an agent's question, work that is blocked
  // or failed, a review someone asked for, and warnings about the machines a Team depends on. Worst first.
  let { overview }: { overview: Overview | null } = $props();

  const rows = $derived(overview ? attention(overview) : []);
  const silent = $derived(overview ? overview.entries.filter((e) => e.error || (!e.summary && ['offline', 'unavailable'].includes(e.workspace.state))) : []);
  const word: Record<string, string> = { critical: 'Urgent', warning: 'Needs you', info: 'For your information' };
</script>

<section class="page" aria-labelledby="inbox-h">
  <header>
    <h1 id="inbox-h">Needs attention</h1>
    <p class="lede">Questions, blockers, reviews and warnings from all your workspaces</p>
  </header>

  {#each silent as e (e.workspace.id)}
    <p class="alert" role="status"><WorkspaceTag workspace={e.workspace} /> <strong>Not answering.</strong> {standing(e)}</p>
  {/each}

  {#if !overview}
    <p class="empty">Looking…</p>
  {:else if rows.length === 0 && silent.length === 0}
    <p class="empty">Nothing needs you.</p>
  {/if}

  <ul>
    {#each rows as r (r.workspace.id + r.item.id)}
      <li>
        <button class="row" type="button" data-severity={r.item.severity} onclick={() => (r.item.href ? model.open(r.workspace.id, r.item.href) : model.open(r.workspace.id))} data-testid="attention-item">
          <span class="sev" title={word[r.item.severity]} aria-label={word[r.item.severity]}></span>
          <span class="text">
            <span class="title">{r.item.title}</span>
            {#if r.item.detail || r.item.project}<span class="detail">{[r.item.project, r.item.detail].filter(Boolean).join(' · ')}</span>{/if}
          </span>
          <WorkspaceTag workspace={r.workspace} />
        </button>
      </li>
    {/each}
  </ul>
</section>

<style>
  .page {
    padding: 28px clamp(16px, 4vw, 48px);
    max-width: 960px;
    margin: 0 auto;
    overflow-y: auto;
    height: 100%;
  }
  h1 {
    margin: 0;
  }
  .lede {
    margin: 4px 0 18px;
    color: var(--text-2);
  }
  ul {
    list-style: none;
    margin: 12px 0 0;
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
    display: grid;
    grid-template-columns: 10px minmax(0, 1fr) auto;
    gap: 12px;
    align-items: center;
    width: 100%;
    min-height: 48px;
    padding: 8px 14px;
    border: 0;
    background: none;
    color: var(--text);
    font: inherit;
    font-size: 13px;
    text-align: left;
    cursor: pointer;
  }
  .row:hover {
    background: var(--surface-2);
  }
  .row:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }
  .sev {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--text-2);
  }
  [data-severity='warning'] .sev {
    background: var(--amber);
    box-shadow: 0 0 0 3px var(--amber-ring);
  }
  [data-severity='critical'] .sev {
    background: var(--danger);
  }
  .text {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .title {
    font-weight: 500;
  }
  .detail {
    color: var(--text-2);
    font-size: 12px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
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
</style>

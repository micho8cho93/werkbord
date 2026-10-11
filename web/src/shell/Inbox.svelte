<script lang="ts">
  import { attentionAction, attentionGroups, standing } from './aggregate';
  import { model } from './model.svelte';
  import WorkspaceTag from './WorkspaceTag.svelte';
  import type { Overview } from './types';

  // What waits for the person or that they should know about, from every workspace: an agent's question, work that is blocked
  // or failed, a review someone asked for, and warnings about the machines a Team depends on. Worst first.
  let { overview }: { overview: Overview | null } = $props();

  const groups = $derived(overview ? attentionGroups(overview) : []);
  const silent = $derived(overview ? overview.entries.filter((e) => e.error || e.workspace.state !== 'ready' || !e.summary) : []);
  const word: Record<string, string> = { critical: 'Urgent', warning: 'Needs you', info: 'For your information' };
</script>

<section class="page" aria-labelledby="inbox-h">
  <header>
    <h1 id="inbox-h">Needs attention</h1>
    <p class="lede">Questions, blockers, reviews and warnings from all your workspaces</p>
  </header>

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
      <li>
        <div class="row" data-severity={r.item.severity} data-testid="attention-item">
          <span class="sev" title={word[r.item.severity]} aria-label={word[r.item.severity]}></span>
          <span class="text">
            <span class="title">{r.item.title}</span>
            <span class="context"><WorkspaceTag workspace={r.workspace} />{#if r.item.project}<span>{r.item.project}</span>{/if}</span>
            {#if r.item.detail}<span class="detail">{r.item.detail}</span>{/if}
          </span>
          <button class="btn action" class:primary={group.id !== 'warnings' && group.id === groups[0].id && i === 0} type="button" onclick={() => model.open(r.workspace.id, r.item.href)} aria-label="{attentionAction(r.item)}: {r.item.title}">{attentionAction(r.item)}</button>
        </div>
      </li>
    {/each}
    </ul>
  </section>
  {/each}
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
  .group { margin-top: 24px; }
  h2 { display: flex; align-items: center; gap: 8px; margin: 0 0 8px; font-size: 14px; }
  .count { color: var(--text-2); font-family: var(--mono); font-size: 11px; }
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
    display: grid;
    grid-template-columns: 10px minmax(0, 1fr) auto;
    gap: 12px;
    align-items: center;
    min-height: 64px;
    padding: 14px;
    font-size: 13px;
  }
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
  .context { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; color: var(--text-2); font-size: 11px; }
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

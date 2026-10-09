<script lang="ts">
  import { myWork, running, standing } from './aggregate';
  import ExecBadge from './ExecBadge.svelte';
  import { model } from './model.svelte';
  import WorkspaceTag from './WorkspaceTag.svelte';
  import type { Overview } from './types';

  // The person's own work in every workspace they can open: the tasks their agents are doing in Personal, the tickets they
  // hold in each Team. Each row opens where it lives; nothing here changes any of it.
  let { overview }: { overview: Overview | null } = $props();

  const groups = $derived(overview ? myWork(overview) : []);
  const status: Record<string, string> = { todo: 'To do', doing: 'In progress', review: 'In review', done: 'Done' };
</script>

<section class="page" aria-labelledby="mywork-h">
  <header>
    <h1 id="mywork-h">My Work</h1>
    {#if overview}<p class="lede">{running(overview)} running now · across {overview.entries.length} workspace{overview.entries.length === 1 ? '' : 's'}</p>{/if}
  </header>

  {#if !overview}
    <p class="empty">Looking…</p>
  {:else if groups.length === 0}
    <p class="empty">Nothing of yours is in progress in any workspace.</p>
  {/if}

  {#each groups as g (g.workspace.id)}
    <section class="group" aria-label={g.workspace.name}>
      <h2><WorkspaceTag workspace={g.workspace} /> <span class="count">{g.items.length}</span></h2>
      {#if g.error}<p class="note" role="status">{g.error}</p>{/if}
      <ul>
        {#each g.items as w (w.id)}
          <li>
            <button class="row" type="button" onclick={() => model.open(g.workspace.id, w.href)} data-testid="work-item">
              <span class="details">
                <span class="title">{w.title}</span>
                <span class="meta">{w.project}</span>
                <span class="state">{status[w.status]}</span>
              </span>
              <ExecBadge execution={w.execution} />
            </button>
          </li>
        {/each}
      </ul>
    </section>
  {/each}

  {#if overview}
    {#each overview.entries.filter((e) => !e.summary && !e.error && e.workspace.state !== 'ready') as e (e.workspace.id)}
      <p class="note"><WorkspaceTag workspace={e.workspace} /> {standing(e)}</p>
    {/each}
  {/if}
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
  h2 {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 22px 0 8px;
    font-size: 13px;
  }
  .count {
    font-family: var(--mono);
    font-size: 11px;
    color: var(--text-2);
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--surface);
    box-shadow: var(--card-sh);
  }
  li + li {
    border-top: 1px solid var(--border);
  }
  .row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 160px) 92px 130px;
    gap: 12px;
    align-items: center;
    width: 100%;
    min-height: 44px;
    padding: 6px 14px;
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
  .details {
    display: contents;
  }
  .row:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }
  .title {
    font-weight: 500;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .meta,
  .state {
    color: var(--text-2);
    font-size: 12px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .note,
  .empty {
    color: var(--text-2);
    margin: 10px 0;
  }
  @media (max-width: 720px) {
    .row {
      grid-template-columns: minmax(0, 1fr) auto;
    }
    .details {
      display: grid;
      grid-template-columns: minmax(0, 1fr) auto;
      gap: 2px 8px;
      min-width: 0;
    }
    .title {
      grid-column: 1 / -1;
    }
  }
</style>

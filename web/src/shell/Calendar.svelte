<script lang="ts">
  import { agenda, standing } from './aggregate';
  import { model } from './model.svelte';
  import WorkspaceTag from './WorkspaceTag.svelte';
  import type { Overview } from './types';

  // Work that is set to start, in every workspace, in the person's own time zone. A Team's schedule is a request: it starts
  // nothing by itself, and the person's own approval decides. Opening a row goes to where it can be changed.
  let { overview }: { overview: Overview | null } = $props();

  const days = $derived(overview ? agenda(overview) : []);
  const dayLabel = (d: string): string => new Intl.DateTimeFormat(undefined, { weekday: 'long', month: 'short', day: 'numeric' }).format(new Date(`${d}T12:00:00`));
  const time = (iso: string): string => new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit' }).format(new Date(iso));
  const silent = $derived(overview ? overview.entries.filter((e) => !e.summary && (e.error || e.workspace.state !== 'ready')) : []);
</script>

<section class="page" aria-labelledby="cal-h">
  <header>
    <h1 id="cal-h">Calendar</h1>
    <p class="lede">Scheduled work in all your workspaces</p>
  </header>

  {#if !overview}
    <p class="empty">Looking…</p>
  {:else if days.length === 0}
    <p class="empty">Nothing is scheduled.</p>
  {/if}

  {#each days as d (d.day)}
    <section class="day" aria-label={dayLabel(d.day)}>
      <h2>{dayLabel(d.day)}</h2>
      <ul>
        {#each d.items as r (r.workspace.id + r.item.id)}
          <li>
            <button class="row" type="button" onclick={() => model.open(r.workspace.id, r.item.href)} data-testid="scheduled-item">
              <time datetime={r.item.at}>{time(r.item.at)}</time>
              <span class="title">{r.item.title}</span>
              <span class="details">
                <WorkspaceTag workspace={r.workspace} />
                <span class="state">{r.item.state.replaceAll('_', ' ')}</span>
              </span>
            </button>
          </li>
        {/each}
      </ul>
    </section>
  {/each}

  {#each silent as e (e.workspace.id)}
    <p class="note"><WorkspaceTag workspace={e.workspace} /> {standing(e)}: its schedule cannot be shown now.</p>
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
  h2 {
    margin: 22px 0 8px;
    font-size: 13px;
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
    grid-template-columns: 64px minmax(0, 1fr) auto 110px;
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
  time {
    font-family: var(--mono);
    font-size: 12px;
    color: var(--text-2);
  }
  .title {
    font-weight: 500;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .state {
    color: var(--text-2);
    font-size: 12px;
    text-transform: capitalize;
  }
  .note,
  .empty {
    color: var(--text-2);
    margin: 10px 0;
  }
  @media (max-width: 720px) {
    .row {
      grid-template-columns: 64px minmax(0, 1fr);
      gap: 4px 12px;
      padding-block: 10px;
    }
    time {
      grid-row: 1 / span 2;
      align-self: start;
    }
    .details {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: 4px 12px;
      grid-column: 2;
      min-width: 0;
    }
  }
</style>

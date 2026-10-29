<script lang="ts">
  import { model } from './model.svelte';
  import { KIND_LABEL, reviewRows, type ReviewKind } from './reviews';
  import FilterBar from './FilterBar.svelte';
  import Notice from './Notice.svelte';
  import PageFrame from './PageFrame.svelte';
  import WherePicker from './WherePicker.svelte';
  import WorkspaceTag from './WorkspaceTag.svelte';
  import Segmented from '../lib/Segmented.svelte';

  // Group reviews by the next action, across Personal and every team. Each row opens
  // in the workspace it belongs to, where the review itself is done.
  const kindIds: ('all' | ReviewKind)[] = ['all', 'to_review', 'changes', 'mine'];
  let where = $state('all');
  let kind = $state<'all' | ReviewKind>('all');

  const all = $derived(model.overview ? reviewRows(model.overview, where) : []);
  const rows = $derived(kind === 'all' ? all : all.filter((r) => r.kind === kind));
  const countOf = (k: 'all' | ReviewKind): number => (k === 'all' ? all.length : all.filter((r) => r.kind === k).length);
  const kinds = $derived(kindIds.map((id) => ({ id, label: id === 'all' ? 'All' : KIND_LABEL[id], count: countOf(id) })));
  const silent = $derived((model.overview?.entries ?? []).filter((e) => (where === 'all' || e.workspace.id === where) && (e.error || !e.summary)));
</script>

<PageFrame id="reviews-h" title="Reviews" lede="Work waiting on a review, across Personal and your teams.">
  <FilterBar>
    <WherePicker bind:where label="Reviews place" testid="reviews-where" />
    <div class="review-kinds"><Segmented compact options={kinds} value={kind} onchange={(id) => (kind = id)} label="Kind of review" /></div>
    <label class="review-kind-select">Kind of review<select bind:value={kind}>{#each kinds as option (option.id)}<option value={option.id}>{option.label} ({option.count})</option>{/each}</select></label>
  </FilterBar>

  {#each silent as e (e.workspace.id)}
    <Notice><WorkspaceTag workspace={e.workspace} /> Current status unavailable{e.error ? `: ${e.error}` : ''}. Its reviews are left out.</Notice>
  {/each}

  {#if !model.overview}
    <p class="empty" role="status">Looking…</p>
  {:else if rows.length === 0}
    <p class="empty" data-testid="reviews-empty">{kind === 'all' ? 'Nothing is waiting on a review.' : `Nothing in “${KIND_LABEL[kind]}”.`}</p>
  {:else}
    <div class="review-groups">
    {#each kindIds.filter((id): id is ReviewKind => id !== 'all' && (kind === 'all' || kind === id)) as group (group)}
      {@const items = rows.filter(r => r.kind === group)}
      {#if items.length}
      <section class="review-group" aria-label={KIND_LABEL[group]}>
      <h2>{KIND_LABEL[group]} <span class="count">{items.length}</span></h2>
      <ul class="list">
      {#each items as r (r.workspace.id + r.id)}
        <li class="row" data-kind={r.kind} data-testid="review-item">
          <span class="text">
            <span class="title">{r.title}</span>
            <span class="context"><WorkspaceTag workspace={r.workspace} />{#if r.project}<span>{r.project}</span>{/if}</span>
            {#if r.detail}<span class="detail">{r.detail}</span>{/if}
          </span>
          <button class="btn" class:primary={r.kind === 'to_review'} type="button" onclick={() => model.goTo(r.workspace.id, r.href)} aria-label="Open: {r.title}">Open</button>
        </li>
      {/each}
      </ul>
      </section>
      {/if}
    {/each}
    </div>
  {/if}
</PageFrame>

<style>
  .review-kinds { margin-left: auto; min-width: 0; max-width: 100%; overflow-x: auto; }
  .review-kind-select { display: none; }
  @media (max-width: 600px) {
    .review-kinds { display: none; }
    .review-kind-select { display: grid; gap: 4px; width: 100%; color: var(--text-2); font-size: 12px; }
    .review-kind-select select { width: 100%; min-height: 44px; padding: 8px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface); color: var(--text); font: inherit; font-size: 16px; }
  }
  .empty { margin: 0; color: var(--text-2); }
  .review-groups { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 300px), 1fr)); gap: 20px; align-items: start; }
  .review-group { min-width: 0; }
  h2 { display: flex; align-items: center; gap: 8px; margin: 0 0 12px; font-size: 14px; }
  .count { color: var(--text-2); font: 12px var(--mono); }
  .list { margin: 0; padding: 0; list-style: none; border: 1px solid var(--border); border-radius: var(--radius); background: var(--surface); }
  .row { display: flex; align-items: center; gap: 14px; padding: 12px 14px; border-bottom: 1px solid var(--border); }
  .row:last-child { border-bottom: 0; }
  .text { display: grid; gap: 4px; min-width: 0; flex: 1; }
  .title { font-weight: 600; overflow-wrap: anywhere; }
  .context { display: flex; flex-wrap: wrap; align-items: center; gap: 4px 10px; color: var(--text-2); font-size: 12px; }
  .detail { overflow-wrap: anywhere; color: var(--text-2); font-size: 12px; }
</style>

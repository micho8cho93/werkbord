<script lang="ts">
  import { api } from './api';
  import LabelChip from './LabelChip.svelte';
  import { LABEL_COLORS, sortedLabels } from './labels';
  import { app } from './state.svelte';

  // Choose any of the reusable labels, or make a new one on the spot (it is then there for every
  // project). Nothing is predefined: the list is whatever this person has made.

  let {
    selected = $bindable(),
    canCreate = true,
    label = 'Labels',
    floating = false,
  }: {
    selected: string[];
    canCreate?: boolean;
    label?: string;
    /** Open the choices over the page instead of pushing it down: for a filter in a toolbar. */
    floating?: boolean;
  } = $props();

  let open = $state(false);
  let search = $state('');
  let color = $state(LABEL_COLORS[5]);
  let busy = $state(false);
  let error = $state('');

  const all = $derived(sortedLabels(app.labels));
  const q = $derived(search.trim().toLowerCase());
  const shown = $derived(q ? all.filter((l) => l.name.toLowerCase().includes(q)) : all);
  const chosen = $derived(selected.map((id) => all.find((l) => l.id === id)).filter((l) => !!l));
  const exact = $derived(all.some((l) => l.name.toLowerCase() === q));

  function toggle(id: string) {
    selected = selected.includes(id) ? selected.filter((x) => x !== id) : [...selected, id];
  }

  async function create() {
    const name = search.trim();
    if (!name || busy) return;
    busy = true;
    error = '';
    try {
      const l = await api.createLabel(name, color);
      app.labels = [...app.labels, { ...l, tasks: 0 }];
      selected = [...selected, l.id];
      search = '';
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = false;
    }
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape' && open) {
      e.stopPropagation();
      open = false;
    }
  }
</script>

<div class="picker" class:floating onkeydown={onKey} role="presentation">
  <div class="row">
    {#each chosen as l (l.id)}
      <button type="button" class="chosen" onclick={() => toggle(l.id)} aria-label="Remove label {l.name}" title="Remove {l.name}"><LabelChip label={l} /><span class="x" aria-hidden="true">×</span></button>
    {/each}
    <button type="button" class="btn small" aria-expanded={open} onclick={() => (open = !open)}>{chosen.length ? 'Edit' : label}…</button>
  </div>

  {#if open}
    <div class="panel" role="group" aria-label={label}>
      <input class="input" placeholder={canCreate ? 'Find or name a label' : 'Find a label'} bind:value={search} aria-label="Find a label" data-autofocus />
      <ul class="opts">
        {#each shown as l (l.id)}
          <li>
            <label class="opt">
              <input type="checkbox" checked={selected.includes(l.id)} onchange={() => toggle(l.id)} />
              <LabelChip label={l} />
              <span class="n">{l.tasks}</span>
            </label>
          </li>
        {:else}
          <li class="none">{all.length ? 'No label by that name.' : canCreate ? 'No labels yet. Name one below.' : 'No labels yet. Make them in Settings.'}</li>
        {/each}
      </ul>
      {#if canCreate && q && !exact}
        <div class="make">
          <div class="swatches" role="radiogroup" aria-label="Colour">
            {#each LABEL_COLORS as c (c)}
              <button type="button" class="sw" role="radio" aria-checked={color === c} aria-label="Colour {c}" style:background={c} onclick={() => (color = c)}></button>
            {/each}
          </div>
          <button type="button" class="btn small primary" disabled={busy} onclick={create}>Create “{search.trim()}”</button>
        </div>
      {/if}
      {#if error}<p class="error" role="alert">{error}</p>{/if}
      <div class="done"><button type="button" class="btn small" onclick={() => (open = false)}>Done</button></div>
    </div>
  {/if}
</div>

<style>
  .picker {
    display: grid;
    gap: 8px;
  }

  .row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
  }

  .chosen {
    display: inline-flex;
    align-items: center;
    gap: 2px;
    padding: 0;
    border: 0;
    background: none;
    cursor: pointer;
    color: var(--text-2);
  }

  .x {
    font-size: 14px;
  }

  .floating {
    position: relative;
  }

  .floating .panel {
    position: absolute;
    top: calc(100% + 4px);
    left: 0;
    z-index: 20;
    width: 18rem;
    max-width: calc(100vw - 32px);
    background: var(--surface);
    box-shadow: var(--win-sh);
  }

  .panel {
    display: grid;
    gap: 8px;
    padding: 10px;
    border-radius: var(--radius-tray);
    background: var(--bg);
    border: 1px solid var(--border);
  }

  .opts {
    list-style: none;
    margin: 0;
    padding: 0;
    max-height: 200px;
    overflow: auto;
    display: grid;
    gap: 2px;
  }

  .opt {
    display: flex;
    align-items: center;
    gap: 8px;
    min-height: 32px;
    padding: 0 4px;
    border-radius: 6px;
    cursor: pointer;
    font-weight: 400;
  }

  .opt:hover {
    background: var(--surface-2);
  }

  .n {
    margin-left: auto;
    font-family: var(--mono);
    font-size: 11px;
    color: var(--text-2);
  }

  .none {
    padding: 6px 4px;
    font-size: 12px;
    color: var(--text-2);
  }

  .make {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: 8px;
  }

  .swatches {
    display: flex;
    gap: 6px;
  }

  .sw {
    width: 20px;
    height: 20px;
    padding: 0;
    border: 0;
    border-radius: 50%;
    cursor: pointer;
    box-shadow: inset 0 0 0 1px rgba(0, 0, 0, 0.18);
  }

  .sw[aria-checked='true'] {
    outline: 2px solid var(--text);
    outline-offset: 2px;
  }

  .done {
    display: flex;
    justify-content: flex-end;
  }
</style>

<script lang="ts">
  import { destinations, matching, type Destination } from './nav';
  import { model } from './model.svelte';

  // Go to…: one box that reaches any project, place or page of the window by typing a few letters, from wherever the person is
  // (⌘K). It only chooses what to show; it changes nothing in any workspace.

  let query = $state('');
  let at = $state(0);
  let input: HTMLInputElement | undefined = $state();
  const all = $derived(destinations(model.view?.items, model.overview));
  const found = $derived(matching(all, query).slice(0, 40));
  $effect(() => {
    // A new query starts at its first match.
    void query;
    at = 0;
  });
  $effect(() => {
    queueMicrotask(() => input?.focus());
  });

  function close(): void {
    model.closePalette();
  }

  function choose(d: Destination | undefined): void {
    if (!d) return;
    close();
    if ('page' in d.go) model.go(d.go.page);
    else if ('workspace' in d.go) void model.goTo(d.go.workspace, d.go.place);
    else model.addOpen = true;
  }

  function onkey(e: KeyboardEvent): void {
    if (e.key === 'Escape') {
      e.preventDefault();
      close();
    } else if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      if (found.length) at = (at + (e.key === 'ArrowDown' ? 1 : -1) + found.length) % found.length;
    } else if (e.key === 'Enter') {
      e.preventDefault();
      choose(found[at]);
    }
  }
</script>

<div class="scrim" onclick={close} role="presentation"></div>
<div class="box" role="dialog" aria-modal="true" aria-label="Go to" data-testid="palette">
  <input
    bind:this={input}
    bind:value={query}
    onkeydown={onkey}
    type="text"
    class="q"
    placeholder="Go to a project, a team or a page…"
    role="combobox"
    aria-expanded="true"
    aria-controls="palette-list"
    aria-activedescendant={found[at] ? `palette-${found[at].id}` : undefined}
    autocomplete="off"
    spellcheck="false"
    aria-label="Go to"
  />
  <ul id="palette-list" class="list" role="listbox" aria-label="Places">
    {#each found as d, i (d.id)}
      <li id="palette-{d.id}" role="option" aria-selected={i === at}>
        <button type="button" class="row" class:on={i === at} tabindex="-1" onmouseenter={() => (at = i)} onclick={() => choose(d)} data-destination={d.id}>
          <span class="title">{d.title}</span><span class="hint">{d.hint}</span>
        </button>
      </li>
    {:else}
      <li class="none" role="presentation">Nothing matches “{query}”.</li>
    {/each}
  </ul>
</div>

<style>
  .scrim {
    position: fixed;
    inset: 0;
    z-index: 40;
    background: color-mix(in srgb, var(--text) 18%, transparent);
  }
  .box {
    position: fixed;
    z-index: 41;
    top: 14vh;
    left: 50%;
    width: min(560px, calc(100vw - 24px));
    transform: translateX(-50%);
    display: grid;
    gap: 6px;
    padding: 8px;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--surface);
    box-shadow: var(--win-sh);
  }
  .q {
    width: 100%;
    padding: 10px 12px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--bg);
    color: var(--text);
    font: inherit;
    font-size: 14px;
  }
  .q:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -1px;
  }
  .list {
    display: grid;
    gap: 2px;
    max-height: min(52vh, 420px);
    margin: 0;
    padding: 0;
    overflow-y: auto;
    list-style: none;
  }
  .row {
    display: flex;
    align-items: baseline;
    gap: 10px;
    width: 100%;
    padding: 8px 10px;
    border: 0;
    border-radius: var(--radius-sm);
    background: none;
    color: var(--text);
    font: inherit;
    font-size: 13px;
    text-align: left;
    cursor: pointer;
  }
  .row.on {
    background: var(--surface-2);
  }
  .title {
    font-weight: 600;
  }
  .hint {
    min-width: 0;
    margin-left: auto;
    overflow: hidden;
    color: var(--text-2);
    font-size: 12px;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .none {
    padding: 10px;
    color: var(--text-2);
    font-size: 13px;
  }
</style>

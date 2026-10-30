<script lang="ts">
  import { model } from './model.svelte';
  import type { Group } from './settings';

  // The sections of Settings for where the person is: the same groups everywhere, and in each only what changes something there.
  // Home lists everything, a workspace at a time. On a narrow window the list becomes a menu.
  const groups = $derived(model.settingsGroups);
  const shown = $derived(model.settingsShown);
  const many = $derived(new Set(groups.map((g) => g.owner).filter(Boolean)).size > 1);
  let expanded = $state<Record<string, boolean>>({});
</script>

{#snippet sectionButtons(g: Group)}
  {#each g.sections as s (s.id)}
    <button class="item" type="button" aria-current={shown.id === s.id ? 'page' : undefined} data-section={s.id} onclick={() => model.openSettings(s.id)}>{s.label}</button>
  {/each}
{/snippet}

<label class="narrow">
  <span>Settings section</span>
  <select aria-label="Settings section" value={shown.id} onchange={(e) => model.openSettings(e.currentTarget.value)}>
    {#each groups as g (g.label + (g.owner ?? ''))}
      <optgroup label={g.owner ? `${g.owner} · ${g.label}` : g.label}>
        {#each g.sections as s (s.id)}<option value={s.id}>{s.label}</option>{/each}
      </optgroup>
    {/each}
  </select>
</label>

<nav class="sections" aria-label="Settings sections" data-testid="settings-sections">
  {#each groups as g, i (g.label + (g.owner ?? ''))}
    {#if many && g.owner && g.owner !== groups[i - 1]?.owner}<p class="owner" data-owner={g.owner}>{g.owner}</p>{/if}
    {#if g.advanced}
      <details class="group advanced" open={expanded[g.label + (g.owner ?? '')] || g.sections.some((s) => s.id === shown.id)} ontoggle={(event) => { expanded[g.label + (g.owner ?? '')] = event.currentTarget.open; }}>
        <summary>{g.label}</summary>
        {@render sectionButtons(g)}
      </details>
    {:else}
      <div class="group" role="group" aria-label={g.owner ? `${g.owner} ${g.label}` : g.label}>
      <h2>{g.label}</h2>
      {@render sectionButtons(g)}
      </div>
    {/if}
  {/each}
</nav>

<style>
  .sections {
    flex: none;
    width: 208px;
    padding: 16px 8px 24px;
    overflow-y: auto;
    border-right: 1px solid var(--border);
    background: var(--bg);
  }
  .group + .group,
  .owner + .group {
    margin-top: 24px;
  }
  .advanced { margin: 24px 8px 0; }
  .advanced summary { padding: 8px 0; color: var(--text-2); font-size: 12px; cursor: pointer; }
  .advanced .item { margin-left: 8px; width: calc(100% - 8px); }
  .owner {
    margin: 22px 8px 6px;
    padding-top: 14px;
    border-top: 1px solid var(--border);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text);
    font-size: 13px;
    font-weight: 600;
  }
  .owner:first-child {
    margin-top: 0;
    padding-top: 0;
    border-top: 0;
  }
  /* A group heading names what is under it; it is not something to press. The sections are indented beneath it on a rule. */
  h2 {
    margin: 0 8px 6px;
    padding-bottom: 6px;
    border-bottom: 2px solid color-mix(in srgb, var(--text) 55%, var(--border));
    color: var(--text);
    font-family: var(--mono);
    font-size: 12px;
    font-weight: 700;
    letter-spacing: 0.14em;
    text-transform: uppercase;
  }
  .item {
    display: block;
    width: calc(100% - 16px);
    min-height: 32px;
    margin: 0 8px 0 16px;
    padding: 0 10px;
    border: 0;
    border-left: 2px solid var(--border);
    border-radius: 0 var(--radius-sm) var(--radius-sm) 0;
    background: none;
    color: var(--text-2);
    font: inherit;
    font-size: 13px;
    text-align: left;
    cursor: pointer;
  }
  .item:hover {
    background: var(--surface-2);
    color: var(--text);
  }
  .item[aria-current='page'] {
    border-left-color: var(--accent);
    background: var(--surface-2);
    color: var(--text);
    font-weight: 600;
  }
  .item:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }
  .narrow {
    display: none;
  }
  @media (max-width: 899px) {
    .sections {
      display: none;
    }
    .narrow {
      display: grid;
      gap: 4px;
      flex: none;
      padding: 12px 16px 0;
      color: var(--text-2);
      font-size: 12px;
    }
    .narrow select {
      min-height: 36px;
      padding: 0 8px;
      border: 1px solid var(--border);
      border-radius: var(--radius-sm);
      background: var(--surface);
      color: var(--text);
      font: inherit;
    }
  }
</style>

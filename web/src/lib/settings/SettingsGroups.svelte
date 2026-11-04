<script lang="ts">
  export interface SettingsGroup { label: string; owner?: string; advanced?: boolean; sections: { id: string; label: string }[] }
  let { groups, selected, onselect, owner }: { groups: SettingsGroup[]; selected: string; onselect: (id: string) => void; owner?: string } = $props();
  let expanded = $state<Record<string, boolean>>({});
</script>
{#snippet sectionButtons(g: SettingsGroup)}
  {#each g.sections as s (s.id)}
    <button class="item" type="button" aria-current={selected === s.id ? 'page' : undefined} data-section={s.id} onclick={() => onselect(s.id)}>{s.label}</button>
  {/each}
{/snippet}

{#snippet groupList(list: SettingsGroup[], owner?: string)}
  {#each list as g (g.label + (g.owner ?? ''))}
    {#if g.advanced}
      <details class="group advanced" open={expanded[g.label + (g.owner ?? '')] || g.sections.some((s) => s.id === selected)} ontoggle={(event) => { expanded[g.label + (g.owner ?? '')] = event.currentTarget.open; }}>
        <summary>{g.label}</summary>
        {@render sectionButtons(g)}
      </details>
    {:else}
      <div class="group" role="group" aria-label={owner ? `${owner} ${g.label}` : g.label}>
        <h2>{g.label}</h2>
        {@render sectionButtons(g)}
      </div>
    {/if}
  {/each}
{/snippet}


{@render groupList(groups, owner)}
<style>
  .group { margin-top: 18px; }
  .group:first-of-type { margin-top: 4px; }
  /* A group heading names what is under it; it is not something to press. */
  h2 { margin: 0 8px 4px; color: var(--text-2); font-size: 11.5px; font-weight: 600; letter-spacing: 0.05em; text-transform: uppercase; }
  .item { position: relative; display: block; width: 100%; min-height: 34px; margin: 1px 0; padding: 0 10px; border: 0; border-radius: var(--radius-sm); background: none; color: var(--text-2); font: inherit; font-size: 13.5px; text-align: left; cursor: pointer; transition: background 120ms, color 120ms; }
  .item:hover { background: color-mix(in srgb, var(--text) 6%, var(--bg)); color: var(--text); }
  .item[aria-current='page'] { background: var(--surface); color: var(--text); font-weight: 600; box-shadow: 0 0 0 1px var(--tray-edge); }
  .item::before { content: ''; position: absolute; left: 0; top: 50%; width: 3px; height: 0; border-radius: 0 3px 3px 0; background: var(--accent); transform: translateY(-50%); transition: height 200ms var(--ease); }
  .item[aria-current='page']::before { height: 18px; }
  .item:focus-visible { outline: 2px solid var(--accent); outline-offset: -2px; }

  .advanced { margin-top: 14px; }
  .advanced summary { padding: 6px 8px; color: var(--text-2); font-size: 11.5px; font-weight: 600; letter-spacing: 0.05em; text-transform: uppercase; cursor: pointer; }
  @media (prefers-reduced-motion: reduce) { .item, .item::before { transition: none; } }
</style>

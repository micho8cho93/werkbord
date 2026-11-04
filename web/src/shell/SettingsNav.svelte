<script lang="ts">
  import SettingsGroups from '../lib/settings/SettingsGroups.svelte';
  import Icon from '../lib/Icon.svelte';
  import { model } from './model.svelte';
  import { sectionColour } from './nav';
  import type { Group } from './settings';

  // The sections of Settings for where the person is: the same groups everywhere, and in each only what changes something there.
  // Home lists everything, a workspace at a time, each one folded unless it holds the section on show. On a narrow window the list
  // becomes a menu. Back is the first thing in the column, and only the button answers the pointer.
  const groups = $derived(model.settingsGroups);
  const shown = $derived(model.settingsShown);
  const many = $derived(new Set(groups.map((g) => g.owner).filter(Boolean)).size > 1);
  let expanded = $state<Record<string, boolean>>({});
  const names: Record<string, string> = { workspace: 'your work', inbox: 'Work', calendar: 'Calendar', agents: 'Agents', reviews: 'Reviews', analytics: 'Analytics', git: 'Git' };
  const backLabel = $derived(model.settingsReturn?.pane ? 'the open item' : names[model.settingsReturn?.page ?? ''] ?? 'where you were');

  /** The groups in the order drawn: the window's own first, then each workspace's, so one workspace's groups can be folded together. */
  type Block = { owner?: string; id?: string; groups: Group[] };
  const blocks = $derived.by(() => {
    const out: Block[] = [];
    for (const g of groups) {
      const last = out[out.length - 1];
      if (many && g.owner) {
        if (last?.owner === g.owner) last.groups.push(g);
        else out.push({ owner: g.owner, id: g.sections[0]?.id.split('/')[0], groups: [g] });
      } else if (last && !last.owner) last.groups.push(g);
      else out.push({ groups: [g] });
    }
    return out;
  });
  const holds = (b: Block) => b.groups.some((g) => g.sections.some((s) => s.id === shown.id));
  const colourOf = (b: Block) => (b.owner === 'Personal' ? sectionColour('personal', 'personal') : sectionColour('team', b.id ?? b.owner ?? ''));
</script>

<aside class="column" data-testid="settings-column">
  <div class="top">
    {#if model.settingsReturn}
      <button class="back" type="button" data-testid="settings-back" title="Back to {backLabel}" onclick={() => model.leaveSettings()}><Icon name="left" size={15} /><span>Back to {backLabel}</span></button>
    {/if}
  </div>

  <label class="narrow">
    <span class="sr-only">Settings section</span>
    <select aria-label="Settings section" value={shown.id} onchange={(e) => model.openSettings(e.currentTarget.value)}>
      {#each groups as g (g.label + (g.owner ?? ''))}
        <optgroup label={g.owner ? `${g.owner} · ${g.label}` : g.label}>
          {#each g.sections as s (s.id)}<option value={s.id}>{s.label}</option>{/each}
        </optgroup>
      {/each}
    </select>
  </label>

  <nav class="sections" aria-label="Settings sections" data-testid="settings-sections">
    <h1 class="title">Settings</h1>
    {#each blocks as b, i (i)}
      {#if b.owner}
        <details class="owner" data-owner={b.owner} open={expanded['owner:' + b.owner] ?? holds(b)} ontoggle={(event) => { expanded['owner:' + b.owner] = event.currentTarget.open; }}>
          <summary><span class="dot" style:--c={colourOf(b)} aria-hidden="true"></span><span class="oname">{b.owner}</span></summary>
          <SettingsGroups groups={b.groups} selected={shown.id} onselect={id => model.openSettings(id)} owner={b.owner} />
        </details>
      {:else}
        <SettingsGroups groups={b.groups} selected={shown.id} onselect={id => model.openSettings(id)} />
      {/if}
    {/each}
  </nav>
</aside>

<style>
  .column { flex: none; width: 232px; min-height: 0; display: flex; flex-direction: column; border-right: 1px solid var(--border); background: var(--bg); }
  .top { flex: none; display: flex; align-items: center; min-height: 48px; padding: 8px 10px 0; }
  /* The button is as wide as its words and no wider: the pointer lights the button, not the column it sits in. */
  .back { display: inline-flex; align-items: center; gap: 4px; max-width: 100%; min-height: 32px; padding: 0 10px 0 6px; border: 0; border-radius: var(--radius-sm); background: transparent; color: var(--text-2); font: inherit; font-size: 13px; cursor: pointer; transition: background 120ms, color 120ms; }
  .back span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .back:hover { background: color-mix(in srgb, var(--text) 7%, var(--bg)); color: var(--text); }
  .back:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }
  .sections { flex: 1; min-height: 0; overflow-y: auto; padding: 4px 10px 24px; }
  .title { margin: 0; padding: 6px 8px 10px; font-size: 20px; font-weight: 600; letter-spacing: -0.01em; }
  /* Each workspace's settings fold together, so the list is as long as the place the person is in and no longer. */
  .owner { margin-top: 14px; padding-top: 8px; border-top: 1px solid var(--border); }
  .owner > summary { display: flex; align-items: center; gap: 8px; min-height: 34px; padding: 0 8px; border-radius: var(--radius-sm); color: var(--text); font-size: 13.5px; font-weight: 600; cursor: pointer; list-style: none; }
  .owner > summary::-webkit-details-marker { display: none; }
  .owner > summary:hover { background: color-mix(in srgb, var(--text) 6%, var(--bg)); }
  .owner > summary:focus-visible { outline: 2px solid var(--accent); outline-offset: -2px; }
  .dot { flex: none; width: 8px; height: 8px; border-radius: 50%; background: var(--c); }
  .oname { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .sr-only { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); }
  .narrow { display: none; }
  @media (max-width: 899px) {
    .column { width: auto; flex-direction: row; align-items: center; gap: 8px; padding: 0 16px; border-right: 0; border-bottom: 1px solid var(--border); }
    .top { padding: 8px 0; min-height: 0; }
    .sections { display: none; }
    .narrow { display: block; flex: 1; min-width: 0; padding: 8px 0; }
    .narrow select { width: 100%; min-height: 36px; padding: 0 8px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface); color: var(--text); font: inherit; }
  }
  @media (prefers-reduced-motion: reduce) {
    .back { transition: none; }
  }
</style>

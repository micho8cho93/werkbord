<script module lang="ts">
  export interface PickerProject { key: string; name: string }
  export interface PickerGroup {
    id: string;
    label: string;
    team: boolean;
    /** The colour that marks the place. */
    colour: string;
    ready: boolean;
    detail: string;
    projects: PickerProject[];
  }

</script>

<script lang="ts">
  import { tick } from 'svelte';
  import Icon from './Icon.svelte';

  // The project context, chosen from a list that looks like the rest of the window instead of the system's menu: grouped by where each
  // project lives, searchable once there are many, marked where a place is offline, and with the way to make a new one at the foot.
  // It knows nothing of where the projects come from: the window and the web app each hand it their own list and say what a choice does.
  let { groups: groupsFor, total, selected, title, where, tooltip = '', missing = false, onchoose, onnew }: {
    /** The places and their projects, narrowed to what the typed words match. */
    groups: (query: string) => PickerGroup[];
    total: number;
    /** The key of the chosen project, or '' for all of them. */
    selected: string;
    title: string;
    where: string;
    tooltip?: string;
    /** The chosen project is not among the ones that can be listed. */
    missing?: boolean;
    onchoose: (key: string) => void;
    onnew: () => void;
  } = $props();

  let open = $state(false);
  let query = $state('');
  let active = $state(0);
  let trigger: HTMLButtonElement | undefined = $state();
  let search: HTMLInputElement | undefined = $state();
  let list: HTMLDivElement | undefined = $state();
  let box = $state({ left: 0, top: 0, width: 280, maxHeight: 420 });

  const groups = $derived(groupsFor(query));
  const searchable = $derived(total > 6);
  /** The rows the keyboard moves over, in the order they are drawn: All projects, then each project. */
  const rows = $derived<(PickerProject | null)[]>([...(query.trim() ? [] : [null]), ...groups.flatMap((g) => g.projects)]);

  function place(): void {
    const r = trigger?.getBoundingClientRect();
    if (!r) return;
    const width = Math.max(r.width, 288);
    const left = Math.min(r.left, Math.max(8, innerWidth - width - 8));
    box = { left, top: r.bottom + 6, width, maxHeight: Math.max(220, innerHeight - r.bottom - 20) };
  }

  async function show(): Promise<void> {
    query = '';
    place();
    open = true;
    const at = rows.findIndex((r) => (r?.key ?? '') === selected);
    active = at < 0 ? 0 : at;
    await tick();
    (search ?? list)?.focus({ preventScroll: true });
    list?.querySelector('[data-active="true"]')?.scrollIntoView({ block: 'nearest' });
  }

  function hide(back = true): void {
    if (!open) return;
    open = false;
    if (back) trigger?.focus();
  }

  function choose(p: PickerProject | null): void {
    hide();
    if ((p?.key ?? '') !== selected) onchoose(p?.key ?? '');
  }

  function create(): void {
    hide(false);
    onnew();
  }

  function move(by: number): void {
    if (!rows.length) return;
    active = (active + by + rows.length) % rows.length;
    void tick().then(() => list?.querySelector('[data-active="true"]')?.scrollIntoView({ block: 'nearest' }));
  }

  function key(e: KeyboardEvent): void {
    if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); hide(); }
    else if (e.key === 'ArrowDown') { e.preventDefault(); move(1); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); move(-1); }
    else if (e.key === 'Home' && !searchable) { e.preventDefault(); active = 0; }
    else if (e.key === 'End' && !searchable) { e.preventDefault(); active = rows.length - 1; }
    else if (e.key === 'Enter' && rows.length) { e.preventDefault(); choose(rows[Math.min(active, rows.length - 1)]); }
    else if (e.key === 'Tab') hide(false);
  }

  function triggerKey(e: KeyboardEvent): void {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') { e.preventDefault(); void show(); }
  }

  function outside(e: PointerEvent): void {
    if (open && e.target instanceof Node && !list?.contains(e.target) && !trigger?.contains(e.target)) hide(false);
  }

  // Typing narrows the list, so the first match is the one Enter takes.
  $effect(() => { void query; active = 0; });
  // The list is placed under the trigger, so it must follow it when the window changes.
  $effect(() => {
    if (!open) return;
    const follow = () => place();
    addEventListener('resize', follow);
    return () => removeEventListener('resize', follow);
  });

  const rowId = (i: number) => `project-row-${i}`;
  /** The index in `rows` a project in a group is drawn at. */
  const indexOf = (p: PickerProject) => rows.findIndex((r) => r?.key === p.key);
</script>

<svelte:window onpointerdown={outside} />

<div class="project-picker" class:open>
  <button bind:this={trigger} class="trigger" type="button" aria-haspopup="listbox" aria-expanded={open} aria-label="Project context: {title}" title={tooltip}
    onclick={() => (open ? hide() : void show())} onkeydown={triggerKey} data-testid="project-context" data-value={selected}>
    <span class="mark" aria-hidden="true"><Icon name="folder" size={16} /></span>
    <span class="label">
      <span class="name">{title}</span>
      <span class="where">{where}</span>
    </span>
    <span class="chev" aria-hidden="true"><Icon name="down" size={14} /></span>
  </button>

  {#if open}
    <div class="pop" style:left="{box.left}px" style:top="{box.top}px" style:width="{box.width}px" style:max-height="{box.maxHeight}px" data-testid="project-list">
      {#if searchable}
        <label class="find">
          <Icon name="search" size={14} />
          <span class="sr-only">Find a project</span>
          <input bind:this={search} bind:value={query} type="text" placeholder="Find a project" autocomplete="off" spellcheck="false" role="combobox" aria-expanded="true" aria-controls="project-listbox" aria-activedescendant={rows.length ? rowId(active) : undefined} onkeydown={key} data-testid="project-find" />
        </label>
      {/if}
      <div bind:this={list} class="rows" id="project-listbox" role="listbox" aria-label="Projects" tabindex={searchable ? -1 : 0} aria-activedescendant={!searchable && rows.length ? rowId(active) : undefined} onkeydown={searchable ? undefined : key}>
        {#if !query.trim()}
          <button type="button" role="option" id={rowId(0)} class="row all" aria-selected={!selected} data-active={active === 0} onpointermove={() => (active = 0)} onclick={() => choose(null)} data-testid="project-option-all" data-value="">
            <span class="dot all-dot" aria-hidden="true"></span>
            <span class="text"><span class="rname">All projects</span><span class="sub">Personal and every team</span></span>
            {#if !selected}<span class="tick" aria-hidden="true"><Icon name="check" size={14} /></span>{/if}
          </button>
        {/if}
        {#each groups as g (g.id)}
          <div class="group" role="group" aria-label={g.label}>
            <div class="head">
              <span class="dot" style:--c={g.colour} aria-hidden="true"></span>
              <span class="gname">{g.label}</span>
              {#if g.team}<span class="kind">Team</span>{/if}
              {#if !g.ready}<span class="offline" title={g.detail}>Offline</span>{/if}
            </div>
            {#each g.projects as p (p.key)}
              {@const at = indexOf(p)}
              <button type="button" role="option" id={rowId(at)} class="row" aria-selected={p.key === selected} data-active={active === at} onpointermove={() => (active = at)} onclick={() => choose(p)} data-testid="project-option" data-value={p.key}>
                <span class="text"><span class="rname">{p.name}</span></span>
                {#if p.key === selected}<span class="tick" aria-hidden="true"><Icon name="check" size={14} /></span>{/if}
              </button>
            {:else}
              <p class="none">{g.ready ? 'No projects yet' : g.detail || 'Not connected right now'}</p>
            {/each}
          </div>
        {/each}
        {#if missing && !query.trim()}
          <p class="none lone">The project you chose is not available. Choose All projects or another project.</p>
        {/if}
        {#if query.trim() && !groups.length}
          <p class="none lone">No project matches “{query.trim()}”.</p>
        {/if}
      </div>
      <div class="foot">
        <button type="button" class="new" onclick={create} data-testid="project-new"><span class="plus" aria-hidden="true"><Icon name="plus" size={14} /></span>New project</button>
      </div>
    </div>
  {/if}
</div>

<style>
  .project-picker { position: relative; display: block; min-width: 0; }
  .trigger { display: flex; align-items: center; gap: 10px; width: 100%; min-height: 48px; padding: 6px 10px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface); color: var(--text); font: inherit; text-align: left; cursor: pointer; transition: border-color 120ms, background 120ms, box-shadow 120ms; }
  .trigger:hover { border-color: color-mix(in srgb, var(--accent) 40%, var(--border)); }
  .project-picker.open .trigger { border-color: color-mix(in srgb, var(--accent) 55%, var(--border)); box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 14%, transparent); }
  .trigger:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
  .mark { flex: none; display: grid; place-items: center; width: 28px; height: 28px; border-radius: 8px; background: var(--surface-2); color: var(--text-2); }
  .label { flex: 1; min-width: 0; display: grid; line-height: 1.25; }
  .name { font-size: 13.5px; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .where { font-size: 11.5px; color: var(--text-2); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .chev { flex: none; display: grid; color: var(--text-2); transition: transform 180ms var(--ease); }
  .project-picker.open .chev { transform: rotate(180deg); }

  .pop { position: fixed; z-index: 60; display: flex; flex-direction: column; min-height: 0; border: 1px solid var(--tray-edge, var(--border)); border-radius: 12px; background: var(--surface); box-shadow: var(--win-sh); overflow: hidden; animation: pop-in 140ms var(--ease) both; transform-origin: top left; }
  .find { display: flex; align-items: center; gap: 8px; margin: 8px 8px 0; padding: 0 10px; min-height: 36px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--bg); color: var(--text-2); }
  .find:focus-within { border-color: color-mix(in srgb, var(--accent) 55%, var(--border)); }
  .find input { flex: 1; min-width: 0; border: 0; outline: 0; background: transparent; color: var(--text); font: inherit; font-size: 13px; }
  .rows { flex: 1; min-height: 0; overflow-y: auto; padding: 8px; display: grid; gap: 2px; align-content: start; outline: none; }
  .group { display: grid; gap: 2px; margin-top: 8px; }
  .group:first-child { margin-top: 0; }
  .head { display: flex; align-items: center; gap: 8px; padding: 6px 8px 4px; font-size: 11px; font-weight: 600; letter-spacing: .04em; text-transform: uppercase; color: var(--text-2); }
  .gname { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; }
  .kind, .offline { flex: none; padding: 1px 6px; border-radius: 999px; font-size: 10.5px; letter-spacing: 0; text-transform: none; background: var(--surface-2); color: var(--text-2); }
  .offline { margin-left: auto; background: color-mix(in srgb, var(--amber) 22%, var(--surface)); color: var(--text); }
  .dot { flex: none; width: 8px; height: 8px; border-radius: 50%; background: var(--c, var(--text-2)); }
  .all-dot { background: conic-gradient(var(--accent), #7B4FC0, #0A7C86, var(--accent)); }
  .row { display: flex; align-items: center; gap: 10px; width: 100%; min-height: 36px; padding: 6px 8px 6px 16px; border: 0; border-radius: var(--radius-sm); background: transparent; color: var(--text); font: inherit; font-size: 13.5px; text-align: left; cursor: pointer; }
  .row.all { padding-left: 8px; }
  .row[data-active='true'] { background: color-mix(in srgb, var(--text) 6%, var(--surface)); }
  .row[aria-selected='true'] { font-weight: 600; }
  .row:focus-visible { outline: 2px solid var(--accent); outline-offset: -2px; }
  .text { flex: 1; min-width: 0; display: grid; }
  .rname { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .sub { font-size: 11.5px; font-weight: 400; color: var(--text-2); }
  .tick { flex: none; display: grid; color: var(--accent); }
  .none { margin: 0; padding: 6px 8px 6px 16px; font-size: 12.5px; color: var(--text-2); }
  .none.lone { padding: 14px 8px; text-align: center; }
  .foot { flex: none; padding: 8px; border-top: 1px solid var(--border); background: var(--surface); }
  .new { display: flex; align-items: center; gap: 10px; width: 100%; min-height: 38px; padding: 6px 8px; border: 0; border-radius: var(--radius-sm); background: transparent; color: var(--text); font: inherit; font-size: 13.5px; font-weight: 600; text-align: left; cursor: pointer; }
  .new:hover { background: color-mix(in srgb, var(--accent) 9%, var(--surface)); }
  .new:focus-visible { outline: 2px solid var(--accent); outline-offset: -2px; }
  .plus { display: grid; place-items: center; width: 22px; height: 22px; border-radius: 6px; background: color-mix(in srgb, var(--accent) 14%, var(--surface)); color: var(--accent); }
  .sr-only { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); }

  @keyframes pop-in { from { opacity: 0; transform: translateY(-4px) scale(.98); } to { opacity: 1; transform: none; } }

  /* The folded sidebar shows the folder alone; the list opens beside it at full width. */
  :global(.side.collapsed) .trigger { justify-content: center; padding: 6px; }
  :global(.side.collapsed) .label, :global(.side.collapsed) .chev { display: none; }
  @media (max-width: 640px) {
    .trigger { justify-content: center; padding: 6px; }
    .label, .chev { display: none; }
  }
  @media (prefers-reduced-motion: reduce) {
    .pop { animation: none; }
    .chev { transition: none; }
  }
</style>

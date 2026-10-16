<script lang="ts">
  import { onMount } from 'svelte';
  import { ApiError, api } from '../api';
  import { isPhone } from '../phones/capability';
  import { agentName, runStatus } from '../format';
  import Icon from '../Icon.svelte';
  import Mark from '../Mark.svelte';
  import { globalHref, taskHref } from '../location';
  import { app } from '../state.svelte';
  import { activeRunners, connectionWord, deckCounts, deckKeys, type DeckKey } from './deck';
  import Keycap from './Keycap.svelte';
  import Tour from '../Tour.svelte';
  import DeckLibrary from './DeckLibrary.svelte';
  import Sheet from '../Sheet.svelte';
  import WorkflowSettings from './WorkflowSettings.svelte';
  import DeckRuns from './DeckRuns.svelte';
  import { deckApi, targetLabel, initialLayout, place, rearrange, workflow, type ButtonId, type DeckExecution, type DeckLayout, type DeckSlot, type DeckStart } from './layout';

  const counts = $derived(deckCounts(app.overview, app.needsYou));
  const keys = $derived(deckKeys(counts));
  const link = $derived(connectionWord(app.connection));
  const runners = $derived(activeRunners(app.runners));
  const loaded = $derived(app.overview !== null);
  const active = $derived((app.overview?.runs ?? []).filter((x) => runStatus(x.run).active));
  const waiting = $derived(active.filter((x) => runStatus(x.run).needsInput).length);
  const working = $derived(active.filter((x) => x.run.state === 'running' || x.run.state === 'starting'));
  const headline = $derived([working.length ? `${working.length} ${working.length === 1 ? 'agent' : 'agents'} running` : '', waiting ? `${waiting} waiting for you` : ''].filter(Boolean).join(' · ') || 'No agents are running');
  const names = $derived([...new Set(active.map((x) => agentName(app.agents, x.run.agentId)))].join(', '));
  let layout = $state<DeckLayout>(initialLayout());
  let ready = $state(false);
  let saving = $state(false);
  let editing = $state(false);
  let error = $state('');
  let announcement = $state('');
  let library = $state<number | null>(null);
  let configuring = $state<number | null>(null);
  let history = $state<DeckExecution[]>([]);
  let pending = $state<({ slotId: string; requestId: string } & DeckStart) | null>(null);
  let picking = $state<DeckSlot | null>(null);
  let picked = $state<string[]>([]);
  let once = false;
  let approving = $state<{slot: DeckSlot; override: boolean; targets?: string[]} | null>(null);
  let starting = $state(false);
  let storageKey = '';
  let dragFrom: number | null = null;
  let dragTo = $state<number | null>(null);

  async function refreshHistory() { history = await deckApi.history(); }
  async function load() {
    error = '';
    try { layout = await deckApi.load(); await refreshHistory(); ready = true; }
    catch (err) { error = err instanceof Error ? err.message : String(err); }
  }
  onMount(() => {
    let disposed = false;
    void (async () => {
      try {
        const owner = isPhone() ? (await api.phoneSelf()).id : 'owner';
        storageKey = `werkbord.deck.pending.${owner}`;
        try { pending = JSON.parse(sessionStorage.getItem(storageKey) ?? 'null'); } catch { /* no previous request */ }
        if (!disposed) await load();
      } catch (err) { if (!disposed) error = err instanceof Error ? err.message : String(err); }
    })();
    const visibility = () => { if (document.visibilityState === 'visible') void refreshHistory().catch(() => {}); };
    document.addEventListener('visibilitychange', visibility);
    return () => { disposed = true; document.removeEventListener('visibilitychange', visibility); };
  });
  async function persist(next = layout) {
    saving = true; error = '';
    try { layout = await deckApi.save(next); announcement = 'Deck saved'; }
    catch (err) { error = err instanceof Error ? err.message : String(err); throw err; }
    finally { saving = false; }
  }
  async function done() { try { await persist(); editing = false; } catch { /* keep the edited layout */ } }
  function choose(button: ButtonId) {
    if (library === null) return;
    const index = library;
    const slot: DeckSlot = { id: crypto.randomUUID(), button };
    layout.slots = place(layout.slots, index, slot);
    library = null;
    if (workflow(button)) configuring = index;
  }
  async function configure(slot: DeckSlot) {
    if (configuring === null) return;
    const next = { ...layout, slots: place(layout.slots, configuring, slot) };
    if (editing) layout = next; else await persist(next);
    configuring = null;
  }
  function move(from: number, to: number) {
    layout.slots = rearrange(layout.slots, from, to); library = null;
    announcement = `Moved button from position ${from + 1} to ${to + 1}`;
  }
  function dragging(from: number, x: number, y: number) {
    dragFrom = from;
    const target = document.elementFromPoint(x,y)?.closest<HTMLElement>('[data-deck-position]');
    dragTo = target ? Number(target.dataset.deckPosition) : null;
  }
  function drop(cancelled = false) { if (!cancelled && dragFrom !== null && dragTo !== null) move(dragFrom, dragTo); dragFrom = null; dragTo = null; }
  function latest(slot: DeckSlot) {
    const h = history.find(h => h.slotId === slot.id);
    return h ? app.scope(h.projectId)?.latestRun[h.taskId] : undefined;
  }
  function keyFor(slot: DeckSlot | null): DeckKey {
    if (!slot) return { id: 'empty', label: 'Add button', icon: 'plus', href: '', count: 0, tone: 'plain', note: '' };
    const w = workflow(slot.button);
    if (!w) return keys.find(k => k.id === slot.button)!;
    const run = latest(slot);
    return { id: slot.button, label: w.label, icon: w.icon, href: '', count: 0, tone: run && runStatus(run).active ? (runStatus(run).needsInput ? 'wait' : 'work') : 'plain', note: run ? runStatus(run).label : '' };
  }
  function clearPending() { pending = null; try { sessionStorage.removeItem(storageKey); } catch { /* server still deduplicates */ } }
  async function launch(slot: DeckSlot, override = false, targets?: string[], approved = false) {
    if (starting || app.connection !== 'live') return;
    if (slot.target === 'ask' && !targets) { picking = slot; picked = []; once = override; return; }
    if ((!slot.target || slot.target === 'current') && !slot.projectId && !app.lastProjectId) { configuring = layout.slots.findIndex(s => s?.id === slot.id); return; }
    if (!approved && (slot.output === 'changes' || slot.approval === 'always')) { approving = {slot, override, targets}; return; }
    const run = latest(slot);
    if (!override && run && runStatus(run).active) { location.hash = taskHref(run.projectId, run.taskId, run.id); return; }
    if (pending) { error = 'Check the previous workflow start before starting another.'; return; }
    pending = { slotId: slot.id, requestId: crypto.randomUUID(), approved, currentProjectId: app.lastProjectId, ...(targets ? {projectIds: targets} : {}), ...(override ? {override: slot} : {}) };
    try { sessionStorage.setItem(storageKey, JSON.stringify(pending)); }
    catch { error = 'This browser cannot save the start request. Enable browser storage and try again.'; pending = null; return; }
    await executePending();
  }
  async function executePending() {
    if (!pending || starting || app.connection !== 'live') return;
    const invocation = pending;
    starting = true; error = '';
    try {
      const result = await deckApi.start(invocation.slotId, invocation.requestId, {approved: invocation.approved, currentProjectId: invocation.currentProjectId, projectIds: invocation.projectIds, override: invocation.override});
      for (const task of result.tasks) { app.warm(task.projectId); app.scope(task.projectId)?.upsertTask(task); }
      for (const run of result.runs) app.scope(run.projectId)?.upsertRun(run);
      history = [{ ...invocation, projectId: result.task.projectId, taskId: result.task.id }, ...history.filter(h => h.requestId !== invocation.requestId)].slice(0,32);
      clearPending();
      await refreshHistory();
      announcement = `${result.task.title} started`;
      app.refreshOverview();
    } catch (err) {
      const refused = err instanceof ApiError && err.status < 500;
      if (refused) clearPending();
      error = `${err instanceof Error ? err.message : String(err)}${refused ? ' Check Workflow executions for a saved task.' : ' The start may have been accepted. Check its task, or retry the same start safely.'}`;
      await refreshHistory().catch(() => {});
    } finally { starting = false; }
  }

</script>

<div class="deck-page">
  <Tour surface="phone" enabled={ready && !editing && configuring === null && library === null && !starting} />
  <header class="top">
    <div class="id">
      <a class="brand" href={globalHref('deck')} aria-label="Werkbord, Control Deck">
        <Mark height={20} />
        <span class="wm">werkbord</span>
      </a>
      <p class="status mm" data-testid="deck-fleet">
        {#if app.connection === 'offline'}
          <button class="reconnect" data-tone={link.tone} type="button" onclick={() => app.reconnect()} aria-label="Offline. Reconnect" data-testid="deck-connection"><span class="dot" aria-hidden="true"></span>{link.word}. Reconnect</button>
        {:else}
          <span class="conn" data-tone={link.tone} role="status" data-testid="deck-connection"><span class="dot" aria-hidden="true"></span>{link.word}</span>
        {/if}
        {#if loaded} · {runners.length} {runners.length === 1 ? 'runner' : 'runners'}{counts.running ? ` · ${counts.running} ${counts.running === 1 ? 'agent' : 'agents'} running` : ''}{/if}
      </p>
    </div>
    <nav class="header-actions" aria-label="Deck tools">
      <button class="btn icon" type="button" onclick={() => (app.switcherOpen = true)} aria-label="Jump to" title="Jump to"><Icon name="search" /></button>
      <a class="btn icon" href={globalHref('help')} aria-label="Help and shortcuts" title="Help and shortcuts"><Icon name="help" /></a>
      <a class="btn icon" href={globalHref('settings')} aria-label="Settings" title="Settings"><Icon name="settings" /></a>
    </nav>
  </header>
  <div class="deck-toolbar">
    <span class="deck-title">Control Deck</span>
    <button class="btn small" type="button" disabled={!ready || saving || starting} onclick={() => { if (editing) void done(); else { editing = true; announcement = 'Editing deck'; } }} data-testid="edit-deck">{saving ? 'Saving…' : editing ? 'Done' : 'Edit Deck'}</button>
  </div>
  {#if editing}<p class="edit-hint">Tap to replace or remove. Drag to swap positions, or use Move to position in the button menu.</p>{/if}
  {#if error}<p class="error" role="alert">{error}</p><button class="btn small" onclick={load} disabled={saving || starting}>Reload saved deck</button>{/if}
  {#if pending}
    <div class="pending" role="status">
      <p>{starting ? 'Starting workflow…' : 'A workflow start needs checking. Its task may already exist.'}</p>
      {#if !starting}
        <button class="btn small" disabled={app.connection !== 'live'} onclick={() => void executePending()}>Retry same start</button>
        {#if history.find(h => h.requestId === pending?.requestId)}
          {@const h = history.find(h => h.requestId === pending?.requestId)!}
          <a class="btn small" href={taskHref(h.projectId,h.taskId)} data-workspace="individual" onclick={clearPending}>Check saved task</a>
        {/if}
      {/if}
    </div>
  {/if}
  <span class="visually-hidden" role="status" aria-live="polite">{announcement}</span>

  <section class="housing" aria-label="Control Deck">
    <div class="grid" data-testid="deck-grid" aria-busy={!ready}>
      {#each layout.slots as slot, i (i)}
        <div class="position" class:drop-target={dragTo === i} data-deck-position={i}>
          <Keycap key={keyFor(slot)} empty={!slot} {editing}
            target={slot && workflow(slot.button) ? targetLabel(slot, app.projects, app.lastProjectId) : ''}
            disabled={!ready || saving || starting || (!!slot && !!workflow(slot.button) && app.connection !== 'live')}
            onclick={!slot || editing ? () => { editing = true; library = i; } : workflow(slot.button) ? () => void launch(slot) : undefined}
            onconfigure={slot && workflow(slot.button) ? () => { configuring = i; } : undefined}
            ondrag={(x,y) => dragging(i,x,y)} ondrop={drop} />
        </div>
      {/each}
    </div>
  </section>
  <DeckRuns {history} />
  {#if library !== null}
    <DeckLibrary position={library} slot={layout.slots[library]} onclose={() => library = null} onchoose={choose}
      onremove={() => { if (library !== null) layout.slots = place(layout.slots,library,null); library = null; }}
      onmove={to => { if (library !== null) move(library,to); }}
      onconfigure={() => { configuring = library; library = null; }} />
  {/if}
  {#if configuring !== null && layout.slots[configuring]}
    <WorkflowSettings slot={layout.slots[configuring]!} onclose={() => configuring = null} onsave={configure} onrun={editing ? undefined : async slot => { configuring = null; await launch(slot, true); }} />
  {/if}

  {#if approving}
    <Sheet title="Approve workflow execution" onclose={() => approving = null}>
      <p>{workflow(approving.slot.button)?.label} · {targetLabel(approving.slot, app.projects, app.lastProjectId)}</p>
      <p>{approving.slot.output === 'changes' ? 'Authorise proposing changes in task worktrees. The agent must request approval for its concrete changes before editing. Commits, pushes and destructive actions require separate approval.' : 'Authorise investigation and relevant tests for this invocation.'}</p>
      {#snippet footer()}<button class="btn" onclick={() => approving = null}>Cancel</button><button class="btn primary" onclick={() => { const a = approving!; approving = null; void launch(a.slot, a.override, a.targets, true); }}>Approve and run</button>{/snippet}
    </Sheet>
  {/if}
  {#if picking}
    <Sheet title="Choose workflow repositories" onclose={() => picking = null}>
      <div class="target-picker">{#each app.projects.filter(p => p.kind !== 'work' && p.repoPath) as p (p.id)}<label><input type="checkbox" value={p.id} bind:group={picked} />{p.name}</label>{/each}</div>
      {#snippet footer()}<button class="btn" onclick={() => picking = null}>Cancel</button><button class="btn primary" disabled={!picked.length} onclick={() => { const slot = picking!; picking = null; void launch(slot, once, picked); }}>Run workflow</button>{/snippet}
    </Sheet>
  {/if}

  <a class="activity card" href={globalHref('agents')} data-testid="deck-activity" aria-label="Agents. {headline}">
    <span class="tile" aria-hidden="true"><Icon name="activity" size={20} /></span>
    <span class="who">
      <span class="task">{loaded ? headline : 'Looking…'}</span>
      {#if names}<span class="meta mm">{names}</span>{/if}
    </span>
    <Icon name="right" size={16} />
  </a>

</div>

<style>
  .target-picker { display: grid; gap: 8px; }
  .target-picker label { display: flex; gap: 8px; align-items: center; min-height: 44px; }
  .deck-page {
    --deck-edge-light: light-dark(var(--surface), color-mix(in srgb, var(--text) 9%, transparent));
    --deck-wall: light-dark(color-mix(in srgb, var(--text) 20%, var(--border)), color-mix(in srgb, var(--ground) 72%, var(--border)));
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: calc(10px + env(safe-area-inset-top)) 16px 20px;
    max-width: 440px;
    margin: 0 auto;
  }

  .header-actions { display: flex; flex: none; gap: 4px; }
  .deck-toolbar { display: flex; justify-content: space-between; align-items: center; gap: 12px; }
  .deck-title { font-size: 14px; font-weight: 500; }
  .deck-toolbar button { min-height: 44px; }
  .edit-hint { margin: 0; color: var(--text-2); font-size: 12px; line-height: 1.5; }
  .pending { display: flex; flex-wrap: wrap; gap: 8px; }
  .pending p { flex-basis: 100%; margin: 0; }
  .position { min-width: 0; }
  .drop-target { outline: 2px solid var(--accent); outline-offset: 3px; border-radius: 17px; }
  @media (max-width: 359px) { .brand { gap: 5px; } .wm { font-size: 15px; } .top { gap: 6px; } .header-actions { gap: 0; } }
  .top {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    min-height: 44px;
  }

  .brand {
    display: flex;
    align-items: center;
    gap: 10px;
    color: var(--text);
    text-decoration: none;
  }

  .wm {
    font-family: var(--mono);
    font-weight: 600;
    font-size: 17px;
    letter-spacing: -0.06em;
    line-height: 1;
  }

  .id {
    display: grid;
    gap: 6px;
    min-width: 0;
  }

  /* The connection and the machines, as one quiet line under the name. */
  .status {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0 4px;
    margin: 0;
  }

  .conn,
  .reconnect {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    color: var(--text);
  }

  .reconnect {
    min-height: 44px;
    margin: -12px 0;
    padding: 0;
    border: 0;
    background: none;
    font: inherit;
    cursor: pointer;
  }

  .status .dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--text-2);
  }

  [data-tone='ok'] > .dot {
    background: var(--ok);
  }

  [data-tone='warn'] > .dot {
    background: var(--warn);
  }

  [data-tone='bad'] > .dot {
    background: var(--danger);
  }

  .btn.icon {
    width: 44px;
    height: 44px;
    padding: 0;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    text-decoration: none;
  }

  /* The keyboard. A thick matte housing in the tray's paper, with a little grain, a lit top edge and a dark lower one; the keys
     do not stretch to fill the screen, the material around them is part of it. */
  .housing {
    padding: 18px;
    border-radius: 26px;
    border: 1px solid var(--border);
    background:
      radial-gradient(color-mix(in srgb, var(--text) 6%, transparent) 0.6px, transparent 0.7px) 0 0 / 3px 3px,
      linear-gradient(180deg, color-mix(in srgb, var(--surface-2) 70%, var(--surface)), var(--surface-2) 55%, color-mix(in srgb, var(--surface-2) 86%, var(--ground)));
    box-shadow:
      inset 0 2px 0 var(--deck-edge-light),
      inset 0 -3px 0 color-mix(in srgb, var(--text) 10%, transparent),
      inset 0 0 0 5px color-mix(in srgb, var(--text) 3%, transparent),
      0 1px 1px color-mix(in srgb, var(--text) 14%, transparent),
      0 14px 24px -14px color-mix(in srgb, var(--text) 40%, transparent);
  }

  .grid {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 12px;
  }

  /* The narrowest phones keep nine keys in three columns by giving the keys the room: less housing, closer keys. */
  @media (max-width: 359px) {
    .housing {
      padding: 12px;
      border-radius: 22px;
    }

    .grid {
      gap: 8px;
    }
  }

  .activity {
    display: flex;
    align-items: center;
    gap: 12px;
    min-height: 64px;
    padding: 10px 14px;
    color: var(--text);
    text-decoration: none;
  }

  .activity > :global(svg:last-child) {
    margin-left: auto;
    flex: none;
    color: var(--text-2);
  }

  .tile {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex: none;
    width: 40px;
    height: 40px;
    border-radius: 10px;
    background: var(--surface-2);
    box-shadow: var(--tray-sh);
    color: var(--accent);
  }

  .who {
    display: grid;
    gap: 2px;
    min-width: 0;
  }

  .task {
    font-size: 14px;
    font-weight: 500;
  }

  .meta {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>

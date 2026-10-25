<script lang="ts">
  import { acknowledge, layout as animateLayout, milestone } from '../motion';
  import { onMount } from 'svelte';
  import { ApiError, api } from '../api';
  import { isPhone } from '../phones/capability';
  import { runStatus } from '../format';
  import Icon from '../Icon.svelte';
    import Mark from '../Mark.svelte';
  import { globalHref, taskHref } from '../location';
  import { app } from '../state.svelte';
  import { activeRunners, connectionWord, deckCounts, deckKeys, workspaceHref, type DeckKey } from './deck';
  import { assistant } from '../assistant/sessions.svelte';
  import { personalAPI } from '../personal/api';
  import { personalWorkspaceDraft } from '../personal/conversation';
  import type { Workspace } from '../personal/schema';
  import Keycap from './Keycap.svelte';
  import { markHere, seenHere, tourDue } from '../setup/tours';
  import { eligibleRunners, executionAgents, resolveFor } from '../execution';
  import DeckOnboarding from './DeckOnboarding.svelte';
  import DeckLibrary from './DeckLibrary.svelte';
  import Sheet from '../Sheet.svelte';
  import WorkflowSettings from './WorkflowSettings.svelte';
  import DeckRuns from './DeckRuns.svelte';
  import WorkflowBanner from './WorkflowBanner.svelte';
  import RecommendationList from '../recommend/RecommendationList.svelte';
  import { suggestedWorkflow } from '../recommend/recommend';
  import { workflows } from './workflows.svelte';
  import { terminal, workflowRecovery } from './workflow-progress';
  import { deckApi, isWorkspaceButton, targetLabel, initialLayout, newSlot, place, rearrange, workflow, type ButtonId, type DeckExecution, type DeckLayout, type DeckSlot, type DeckStart } from './layout';

  const counts = $derived(deckCounts(app.overview, app.needsYou));
  const keys = $derived(deckKeys(counts));
  const link = $derived(connectionWord(app.connection));
  const runners = $derived(activeRunners(app.runners));
  const loaded = $derived(app.overview !== null);
  let layout = $state<DeckLayout>(initialLayout());
  // The generated workspaces, whichever device made them: they are the controller's, so a change anywhere reloads this list.
  let workspaces = $state<Workspace[]>([]);
  let workspacesLoaded = $state(false);
  $effect(() => {
    void app.personalRevision;
    let live = true;
    personalAPI.workspaces().then((list) => { if (live) { workspaces = list; workspacesLoaded = true; } }, () => { /* not served here: the deck shows no workspaces */ });
    return () => { live = false; };
  });
  // Tapping an empty position starts Edit Deck itself; going to Salie from there changes nothing yet, so the deck goes back to normal.
  let openedEditing = false;
  function createWithSalie() {
    if (openedEditing) editing = false;
    openedEditing = false;
    assistant.personalEditing = undefined;
    assistant.draft = personalWorkspaceDraft('');
    assistant.show();
  }
  let ready = $state(false);
  let saving = $state(false);
  let savedRevision = $state(0);
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
  let guide = $state(0);
  let guideReady = $state(false);
  let guideSlot = $state('');
  let inspected = $state(false);
  let preview = $state(false);
  let guideStorage = '';
  const selectedWorkflow = $derived(layout.slots.find(s => s?.id === guideSlot) ?? layout.slots.slice(6).find(s => s && workflow(s.button)));
  const hasWorkflow = $derived(layout.slots.slice(6).some(s => s && workflow(s.button)));
  const exampleProject = $derived(app.projects.find(p => p.id === app.onboarding?.example?.projectId && p.repoPath));
  const guideProject = $derived(app.project(selectedWorkflow?.projectIds?.[0] ?? '') ?? exampleProject ?? app.projects.find(p => p.kind !== 'work' && p.repoPath));
  const defaults = $derived(resolveFor({}, guideProject?.execution, app.globalExecution));
  const liveDemo = $derived(!!guideProject && app.connection === 'live' && eligibleRunners(app.runners, guideProject.id, selectedWorkflow?.runner || defaults.runner).length > 0 && executionAgents(app.agents, app.runners, guideProject.id, selectedWorkflow?.runner || defaults.runner).some(a => !(selectedWorkflow?.agent || defaults.agent) || a.id === (selectedWorkflow?.agent || defaults.agent)));
  function advance(step: number) { guide = step; try { localStorage.setItem(guideStorage, JSON.stringify({step, slot: guideSlot, preview})); } catch { /* the saved deck remains authoritative */ } }
  async function finishGuide() {
    try {
      await persist(); markHere('phone');
      if (!seenHere('phone')) throw new Error('Enable browser storage to remember completed onboarding. Your deck is saved.');
      localStorage.removeItem(guideStorage); guide = 0; milestone();
    } catch (err) { error = err instanceof Error ? err.message : String(err); }
  }
  function addGuideButton() { const empty = layout.slots.findIndex((s,i) => i >= 6 && !s); library = empty >= 0 ? empty : 6; }
  function configureGuide() { if (selectedWorkflow) configuring = layout.slots.findIndex(s => s?.id === selectedWorkflow.id); }
  function recommendBugs() {
    const existing = layout.slots.find(s => s?.button === 'find-bugs');
    if (existing) { guideSlot = existing.id; configuring = layout.slots.findIndex(s => s?.id === existing.id); return; }
    const empty = layout.slots.findIndex((s,i) => i >= 6 && !s);
    const index = empty >= 0 ? empty : layout.slots.findIndex(s => s?.id === selectedWorkflow?.id);
    if (index < 6) return;
    const slot = newSlot(crypto.randomUUID(), 'find-bugs');
    layout.slots = place(layout.slots,index,slot); guideSlot = slot.id; configuring = index;
  }
  $effect(() => {
    if (preview) return;
    if (guide === 5 && workflows.selected) inspected = true;
    if (guide === 5 && inspected && !workflows.selected) { inspected = false; advance(6); }
  });
  // Suggestions point at a workflow's button; they never press it. The button is marked, and the person presses it themselves.
  let suggested = $state<string[]>([]);
  let pointed = $state('');
  function pointAt(button: string) {
    const name = workflow(button)?.label ?? 'That workflow';
    const index = layout.slots.findIndex(s => s?.button === button);
    if (index >= 0) {
      pointed = button;
      announcement = `${name} is marked on the Deck. Press it to start it.`;
      document.querySelector<HTMLElement>(`[data-deck-position="${index}"]`)?.scrollIntoView({ block: 'center', behavior: 'smooth' });
      return;
    }
    const empty = layout.slots.findIndex((s, i) => i >= 6 && !s);
    if (empty < 0) { announcement = `${name} is not on your Deck, and the Deck is full. Edit Deck to swap a button.`; return; }
    // Adding a button is the ordinary Edit Deck flow: the person chooses it in the library and saves.
    editing = true; library = empty;
    announcement = `${name} is not on your Deck. Choose it to add it.`;
  }
  let dragFrom: number | null = null;
  let dragTo = $state<number | null>(null);

  async function refreshHistory() { history = await deckApi.history(); }
  async function load() {
    error = '';
    try {
      layout = await deckApi.load(); await refreshHistory(); ready = true;
      if (!guideReady) {
        if (tourDue(app.onboarding, 'phone')) {
          try { const saved = JSON.parse(localStorage.getItem(guideStorage) ?? 'null'); guide = saved?.step >= 1 && saved.step <= 6 ? saved.step : 1; guideSlot = saved?.slot ?? ''; preview = saved?.preview === true; }
          catch { guide = 1; }
        }
        guideReady = true;
      }
    }
    catch (err) { error = err instanceof Error ? err.message : String(err); }
  }
  onMount(() => {
    let disposed = false;
    const arrived = suggestedWorkflow(location.hash);
    void (async () => {
      try {
        const owner = isPhone() ? (await api.phoneSelf()).id : 'owner';
        storageKey = `werkbord.deck.pending.${owner}`;
        try { pending = JSON.parse(sessionStorage.getItem(storageKey) ?? 'null'); } catch { /* no previous request */ }
        if (!disposed) {
          guideStorage = `werkbord.deck.onboarding.${owner}`;
          await load();
          // Arrived from a suggestion elsewhere (the assistant, the Git screen): mark the workflow it named.
          if (arrived) pointAt(arrived);
        }
      } catch (err) { if (!disposed) error = err instanceof Error ? err.message : String(err); }
    })();
    const visibility = () => { if (document.visibilityState === 'visible') void refreshHistory().catch(() => {}); };
    document.addEventListener('visibilitychange', visibility);
    return () => { disposed = true; document.removeEventListener('visibilitychange', visibility); };
  });
  async function persist(next = layout) {
    saving = true; error = '';
    try { layout = await deckApi.save(next); announcement = 'Deck saved'; savedRevision++; }
    catch (err) { error = err instanceof Error ? err.message : String(err); throw err; }
    finally { saving = false; }
  }
  async function done() { try { await persist(); editing = false; } catch { /* keep the edited layout */ } }
  function choose(button: ButtonId) {
    if (library === null) return;
    const index = library;
    const slot = newSlot(crypto.randomUUID(), button);
    layout.slots = place(layout.slots, index, slot);
    library = null;
    if (guide) {
      if (workflow(button)) guideSlot = slot.id;
      void persist().catch(() => {});
    }
  }
  async function configure(slot: DeckSlot) {
    if (configuring === null) return;
    const next = { ...layout, slots: place(layout.slots, configuring, slot) };
    if (editing && !guide) layout = next; else await persist(next);
    configuring = null;
    if (guide === 3 || guide === 4) advance(4);
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
    if (isWorkspaceButton(slot.button)) {
      const made = workspaces.find((w) => w.id === slot.button);
      if (made) return { id: slot.id, label: made.name, icon: 'folder', workspaceIcon: made.icon, href: guide ? '' : workspaceHref(made.id), count: 0, tone: 'plain', note: '' };
      return { id: slot.id, label: workspacesLoaded ? 'Removed workspace' : 'Workspace', icon: 'folder', href: '', count: 0, tone: 'plain', note: workspacesLoaded ? 'No longer saved. Edit the deck to remove this button.' : '' };
    }
    const w = workflow(slot.button);
    if (!w) { const key = keys.find(k => k.id === slot.button)!; return guide ? {...key, href: ''} : key; }
    const run = latest(slot);
    return { id: slot.button, label: w.label, icon: w.icon, href: '', count: 0, tone: run && runStatus(run).active ? (runStatus(run).needsInput ? 'wait' : 'work') : 'plain', note: run ? runStatus(run).label : '' };
  }
  function clearPending() { pending = null; try { sessionStorage.removeItem(storageKey); } catch { /* server still deduplicates */ } }
  async function launch(slot: DeckSlot, override = false, targets?: string[], approved = false) {
    if (starting || app.connection !== 'live') return;
    const tracked = workflows.executions.find(x => x.slotId === slot.id && !terminal(x.run));
    if (!override && tracked) { if (guide === 4) advance(5); workflows.open(tracked.taskId); return; }
    const run = latest(slot);
    if (!override && run && runStatus(run).active) { workflows.open(run.taskId); return; }
    if (slot.target === 'ask' && !targets) { picking = slot; picked = []; once = override; return; }
    if (!targets && (!slot.target || slot.target === 'current') && !slot.projectId && !app.lastProjectId) { picking = {...slot, target: 'ask'}; picked = []; once = true; return; }
    if (!approved && (slot.output === 'changes' || slot.approval === 'always')) { approving = {slot, override, targets}; return; }
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
    workflows.starting = { name: workflow(layout.slots.find(s => s?.id === invocation.slotId)?.button ?? '')?.label ?? 'Workflow', requestId: invocation.requestId };
    try {
      const result = await deckApi.start(invocation.slotId, invocation.requestId, {approved: invocation.approved, currentProjectId: invocation.currentProjectId, projectIds: invocation.projectIds, override: invocation.override});
      for (const task of result.tasks) { app.warm(task.projectId); app.scope(task.projectId)?.upsertTask(task); }
      for (const run of result.runs) app.scope(run.projectId)?.upsertRun(run);
      history = [{ ...invocation, projectId: result.task.projectId, taskId: result.task.id }, ...history.filter(h => h.requestId !== invocation.requestId)].slice(0,32);
      clearPending();
      await refreshHistory();
      await workflows.refresh();
      announcement = `${result.task.title} started`;
      if (guide === 4) advance(5);
      app.refreshOverview();
    } catch (err) {
      const refused = err instanceof ApiError && err.status < 500;
      if (refused) clearPending();
      error = `${err instanceof Error ? err.message : String(err)}${refused ? ' Check Workflow executions for a saved task.' : ' The start may have been accepted. Check its task, or retry the same start safely.'}`;
      await refreshHistory().catch(() => {});
      await workflows.refresh();
    } finally { starting = false; workflows.starting = null; }
  }

</script>

<div class="deck-page">

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
        {#if loaded}<span class="runner-count">· {runners.length} {runners.length === 1 ? 'runner' : 'runners'}</span>{/if}
      </p>
    </div>
    <nav class="header-actions" aria-label="Deck tools">
      <button class="btn icon" use:acknowledge={{ value: savedRevision, success: savedRevision > 0 }} class:editing type="button" disabled={!ready || saving || starting || !!guide}
        aria-label={saving ? 'Saving…' : editing ? 'Done' : 'Edit Deck'} title={saving ? 'Saving…' : editing ? 'Save deck' : 'Edit Deck'} aria-pressed={editing}
        onclick={() => { if (editing) void done(); else { editing = true; announcement = 'Editing deck'; } }} data-testid="edit-deck"><Icon name={editing ? 'check' : 'edit'} /></button>
      <button class="btn icon" type="button" onclick={() => (app.switcherOpen = true)} aria-label="Jump to" title="Jump to"><Icon name="search" /></button>
      <a class="btn icon" href={globalHref('help')} aria-label="Help and shortcuts" title="Help and shortcuts"><Icon name="help" /></a>
      <a class="btn icon" href={globalHref('settings')} aria-label="Settings" title="Settings"><Icon name="settings" /></a>
    </nav>
  </header>
  <WorkflowBanner />
  {#if guide === 5 && preview}
    <section class="preview" aria-label="Interactive preview progress">
      <button class="btn" onclick={() => inspected = true}>Interactive preview · Open execution details</button>
    </section>
  {/if}
  {#if guideReady && guide}
    <DeckOnboarding step={guide} {hasWorkflow} full={layout.slots.slice(6).every(Boolean)} workflowName={workflow(selectedWorkflow?.button ?? '')?.label ?? ''}
      liveDemo={(liveDemo && !error) || !!pending} {preview} busy={saving || starting} onnext={advance} onadd={addGuideButton} onconfigure={configureGuide}
      onrecommend={recommendBugs} onpreview={() => { preview = true; advance(4); }} onfinish={finishGuide} />
  {/if}
  {#if editing}<p class="edit-hint">Tap to replace or remove. Drag to swap positions, or use Move to position in the button menu.</p>{/if}
  {#if error}<p class="error" role="alert">{error} {workflowRecovery(error)}</p><button class="btn small quiet" onclick={load} disabled={saving || starting}>Reload saved deck</button>{/if}
  {#if pending}
    <div class="pending" role="status">
      <p>{starting ? 'Starting workflow…' : 'A workflow start needs checking. Its task may already exist.'}</p>
      {#if !starting}
        <button class="btn small quiet" disabled={app.connection !== 'live'} onclick={() => void executePending()}>Retry same start</button>
        {#if history.find(h => h.requestId === pending?.requestId)}
          {@const h = history.find(h => h.requestId === pending?.requestId)!}
          <a class="btn small" href={taskHref(h.projectId,h.taskId)} data-workspace="individual" onclick={clearPending}>Check saved task</a>
        {/if}
      {/if}
    </div>
  {/if}
  <span class="visually-hidden" role="status" aria-live="polite">{announcement}</span>

  <section class="housing" aria-label="Control Deck">
    <div class="grid" use:animateLayout={{ revision: layout.slots.map(s => s?.id ?? 'empty').join('|'), enabled: ready, entries: true }} data-testid="deck-grid" aria-busy={!ready}>
      {#each layout.slots as slot, i (i)}
        <div class="position" class:drop-target={dragTo === i} class:guided={!!guide && guide >= 3 && slot?.id === selectedWorkflow?.id} class:suggested={!guide && !!slot && (suggested.includes(slot.button) || pointed === slot.button)} data-deck-position={i} data-motion-key={slot?.id}>
          <Keycap key={keyFor(slot)} empty={!slot} {editing} workflow={!!slot && !!workflow(slot.button)}
            target={slot && workflow(slot.button) ? targetLabel(slot, app.projects, app.lastProjectId) : ''}
            disabled={!ready || saving || starting || (!!slot && !!workflow(slot.button) && app.connection !== 'live' && !(guide && (preview || guide === 3))) || (!!guide && (i < 6 || guide === 1 || guide === 5 || guide === 6))}
            onclick={guide && i >= 6 ? () => {
              if (guide === 2) { library = i; return; }
              if (!slot || !workflow(slot.button)) return;
              guideSlot = slot.id;
              if (guide === 3) configureGuide();
              else if (guide === 4 && preview) advance(5);
              else if (guide === 4) void launch(slot);
            } : !slot || editing ? () => { if (guide) { if (guide === 2 && i >= 6) library = i; } else { openedEditing = !editing; editing = true; library = i; } } : slot && workflow(slot.button) ? () => void launch(slot) : undefined}
            onconfigure={slot && workflow(slot.button) ? () => { if (guide) guideSlot = slot.id; configuring = i; } : undefined}
            ondrag={(x,y) => dragging(i,x,y)} ondrop={drop} />
        </div>
      {/each}
    </div>
  </section>
  {#if app.lastProjectId && !guide && !editing}
    <RecommendationList projectId={app.lastProjectId} onworkflow={pointAt} onsuggest={ids => (suggested = ids)} />
  {/if}
  {#if guide === 5 && preview}
    {#if inspected}
      <Sheet title="Interactive preview · execution details" onclose={() => { inspected = false; advance(6); }}>
        <p>Preview only. No agent is running and no repository has been scanned.</p>
        <dl><dt>Repository</dt><dd>{guideProject?.name ?? 'Select a repository in workflow settings'}</dd><dt>Current stage</dt><dd>Waiting to run (preview)</dd><dt>Completed stages</dt><dd>None — preview only</dd><dt>Agent activity</dt><dd>No live activity. A real run shows reported output here.</dd><dt>Findings or results</dt><dd>No scan results. A real scan reports credible findings, or “No bugs found in this scan.”</dd></dl>
        <button class="btn warn" onclick={() => { inspected = false; advance(6); }}>Stop preview</button>
        {#snippet footer()}<button class="btn primary" onclick={() => { inspected = false; advance(6); }}>Return to Control Deck</button>{/snippet}
      </Sheet>
    {/if}
  {/if}
  <DeckRuns {history} />
  {#if library !== null}
    <DeckLibrary onboarding={!!guide} {workspaces} position={library} slot={layout.slots[library]} onclose={() => library = null} onchoose={choose} oncreate={() => { library = null; createWithSalie(); }}
      onremove={() => { if (library !== null) layout.slots = place(layout.slots,library,null); library = null; if (guide) void persist().catch(() => {}); }}
      onmove={to => { if (library !== null) move(library,to); }}
      onconfigure={() => { configuring = library; library = null; }} />
  {/if}
  {#if configuring !== null && layout.slots[configuring]}
    <WorkflowSettings onboarding={!!guide} onpreview={() => { configuring = null; preview = true; advance(4); }} slot={layout.slots[configuring]!} onclose={() => configuring = null} onsave={configure} onrun={editing || guide ? undefined : async slot => { configuring = null; await launch(slot, true); }} />
  {/if}

  {#if approving}
    <Sheet title="Approve workflow execution" onclose={() => approving = null}>
      <p>{workflow(approving.slot.button)?.label} · {approving.targets?.map(id => app.project(id)?.name ?? 'Unavailable').join(', ') || targetLabel(approving.slot, app.projects, app.lastProjectId)}</p>
      <p>{approving.slot.output === 'changes' ? 'Authorise proposing changes in task worktrees. The agent must request approval for its concrete changes before editing. Commits, pushes and destructive actions require separate approval.' : 'Authorise investigation and relevant tests for this invocation.'}</p>
      {#snippet footer()}<button class="btn" onclick={() => approving = null}>Cancel</button><button class="btn primary" onclick={() => { const a = approving!; approving = null; void launch(a.slot, a.override, a.targets, true); }}>Approve and run</button>{/snippet}
    </Sheet>
  {/if}
  {#if picking}
    <Sheet title="Run {workflow(picking.button)?.label ?? 'workflow'}" onclose={() => picking = null}>
      <p>{workflow(picking.button)?.description}</p>
      <p class="muted">Choose where to run this time. Hold the workflow button to change its defaults.</p>
      <fieldset class="target-picker"><legend>Repositories</legend>{#each app.projects.filter(p => p.kind !== 'work' && p.repoPath) as p (p.id)}<label><input type="checkbox" value={p.id} bind:group={picked} /><span><strong>{p.name}</strong><small>{p.repoPath}</small></span></label>{:else}<p>Add a Git repository on your computer, then return here.</p>{/each}</fieldset>
      {#snippet footer()}<button class="btn" onclick={() => picking = null}>Cancel</button><button class="btn primary" disabled={!picked.length || starting || app.connection !== 'live'} onclick={() => { const slot = picking!; picking = null; void launch(slot, once, picked); }}>Run {workflow(picking?.button ?? '')?.label ?? 'workflow'}</button>{/snippet}
    </Sheet>
  {/if}

</div>

<style>
  .guided { outline: 2px solid var(--accent); outline-offset: 4px; border-radius: 17px; }
  /* A suggested workflow's key is marked, not pressed. The ring is dashed so that it is not mistaken for the onboarding guide's. */
  .suggested { outline: 2px dashed var(--accent); outline-offset: 4px; border-radius: 17px; }
  .preview { display: grid; gap: 8px; }
  dt { margin-top: 14px; font-weight: 600; }
  dd { margin: 4px 0 0; color: var(--text-2); }
  .target-picker { display: grid; gap: 0; border: 0; padding: 0; margin: 4px 0; min-width: 0; }
  .target-picker legend { font-weight: 600; margin-bottom: 8px; }
  .target-picker label { display: flex; gap: 12px; align-items: center; min-height: 60px; padding: 8px 0; border-bottom: 1px solid var(--border); cursor: pointer; }
  .target-picker input { width: 20px; height: 20px; flex: none; accent-color: var(--accent); }
  .target-picker span { min-width: 0; }
  .target-picker strong { font-weight: 500; }
  .target-picker small { display: block; color: var(--text-2); font: 11.5px/1.5 var(--mono); overflow-wrap: anywhere; margin-top: 3px; }
  .deck-page {
    --deck-edge-light: light-dark(var(--surface), color-mix(in srgb, var(--text) 9%, transparent));
    --deck-wall: light-dark(color-mix(in srgb, var(--text) 20%, var(--border)), color-mix(in srgb, var(--ground) 72%, var(--border)));
    display: flex;
    flex-direction: column;
    gap: 20px;
    min-height: 100dvh;
    padding: calc(14px + env(safe-area-inset-top)) 20px calc(28px + env(safe-area-inset-bottom));
    max-width: 440px;
    margin: 0 auto;
  }

  .header-actions { display: flex; flex: none; gap: 10px; }
  .header-actions .editing { color: var(--accent); background: var(--surface-2); }
  .edit-hint { margin: 0; color: var(--text-2); font-size: 12px; line-height: 1.5; }
  .pending { display: flex; flex-wrap: wrap; gap: 8px; }
  .pending p { flex-basis: 100%; margin: 0; }
  .position { min-width: 0; }
  .drop-target { outline: 2px solid var(--accent); outline-offset: 3px; border-radius: 17px; }
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
  .runner-count { white-space: nowrap; }
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
    margin-block: auto;
    padding: 24px;
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
    gap: 20px 18px;
  }

  /* The narrowest phones keep nine keys in three columns by giving the keys the room: less housing, closer keys. */
  @media (max-width: 359px) {
    .deck-page { padding-inline: 12px; }
    .brand { gap: 4px; }
    .brand :global(svg) { height: 18px; width: auto; }
    .wm { font-size: 14px; }
    .top { gap: 4px; }
    .header-actions { gap: 4px; }
    .housing {
      padding: 16px;
      border-radius: 22px;
    }

    .grid {
      gap: 14px 12px;
    }
  }

</style>

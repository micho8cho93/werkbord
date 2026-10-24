<script lang="ts">
  import { untrack } from 'svelte';
  import WorkspaceIcon from '../lib/personal/WorkspaceIcon.svelte';
  import WorkspaceRenderer from '../lib/personal/WorkspaceRenderer.svelte';
  import { WorkspaceSession } from '../lib/personal/session.svelte';
  import WorkspaceManagement from '../lib/personal/WorkspaceManagement.svelte';
  import { shellPersonalAPI } from './personal';
  import { model } from './model.svelte';
  import PersonalTeamProjects from './PersonalTeamProjects.svelte';
  import { shellPlace } from './nav';
  import type { ShortcutTarget } from '../lib/personal/schema';
  let { id }: { id: string } = $props();
  let session = $state<WorkspaceSession | null>(null);
  let problem = $state('');
  let loading = $state(true);
  let attempt = $state(0);
  let openTeams = $state<Record<string, boolean>>({});
  const saved = $derived(session?.workspace);
  const knownTeams = $derived(model.view?.items.filter(i => i.kind === 'team') ?? []);
  const assignedTeams = $derived(saved?.visibility.teamWorkspaceIds ?? []);
  // Closing Salie refreshes the catalogue; reload a changed definition by revision,
  // without ever navigating the retained Individual/Team frames.
  const catalogueRevision = $derived(model.generated.find(w => w.id === id)?.revision);
  $effect(() => {
    const target = id; void attempt; void catalogueRevision;
    let active = true;
    loading = true; problem = ''; session = null;
    const next = new WorkspaceSession(shellPersonalAPI);
    // The window opens the places a shortcut names; each is one of the window's own, never an address from the workspace.
    next.navigable = ['home', 'projects', 'analytics', 'calendar', 'data-library', 'settings', 'assistant'];
    next.navigate = (place: ShortcutTarget) => {
      if (place === 'calendar') model.go('calendar');
      else if (place === 'data-library') model.go('personal-library');
      else if (place === 'settings') void model.openSettings();
      else if (place === 'assistant') void model.customizeGenerated();
      else void model.visit(shellPlace(model.view?.items.find(i => i.id === model.current)?.kind ?? 'personal', place as 'home' | 'projects' | 'analytics'));
    };
    untrack(() => { void next.load(target).then(() => { if (active) session = next; }).catch(() => {
      if (active) problem = 'This workspace could not be loaded. Check the local controller and reload. Your saved records are safe.';
    }).finally(() => { if (active) loading = false; }); });
    return () => { active = false; };
  });
  async function updated(workspace: NonNullable<WorkspaceSession['workspace']>) {
    session!.workspace = workspace;
    await model.refreshGenerated();
    if (!model.visibleGenerated.some(w => w.id === id)) {
      if (model.universal) model.go('inbox'); else await model.open(model.current);
    }
  }
</script>
<div class="personal-page" data-personal-workspace={id}>
  <header>
    <div class="title"><WorkspaceIcon icon={saved?.icon} name={saved?.name} size={20} /><h1>{saved?.name ?? 'Personal workspace'}</h1></div>
    <div class="head-actions">
    <button class="btn primary" disabled={!saved || loading || session?.busy} onclick={() => model.customizeGenerated(id, saved?.revision)}>Customize with Salie</button>
    <button class="btn quiet" disabled={loading || session?.busy} onclick={() => attempt++}>Reload workspace</button>
    {#if saved && !session?.busy}<WorkspaceManagement workspace={saved} api={shellPersonalAPI} teams={knownTeams} context={model.universal ? 'universal' : model.current} onupdated={updated} onlibrary={() => model.go('personal-library')} ondeleted={async preview => {
      await model.refreshGenerated();
      model.notice = preview.collections.some(c => !c.delete) ? 'Workspace deleted. Preserved personal collections are available in the Data library.' : 'Workspace and its exclusively owned personal data deleted.';
      model.go('personal-library');
    }} />{/if}
    </div>
  </header>
  {#if problem}<p role="alert">{problem} <button class="btn quiet" onclick={() => attempt++}>Reload workspace</button></p>{/if}
  {#if loading}<p role="status">Loading workspace…</p>
  {:else if session}
    {#key session}<svelte:boundary><WorkspaceRenderer {session} />{#snippet failed(error, reset)}<p role="alert">This interface could not be rendered. Your records are safe. <button class="btn quiet" onclick={() => { void error; reset(); attempt++; }}>Reload workspace</button></p>{/snippet}</svelte:boundary>{/key}
    {#each assignedTeams as teamId (teamId)}
      <details class="team" ontoggle={event => { openTeams[teamId] = event.currentTarget.open; }}><summary>Team projects · {knownTeams.find(i => i.id === teamId)?.name ?? teamId} · read-only</summary>{#if openTeams[teamId]}<PersonalTeamProjects workspaceId={id} {teamId} />{/if}</details>
    {/each}
  {/if}
</div>
<style>
  .personal-page { width: 100%; max-width: 1800px; margin: 0 auto; padding: 24px clamp(16px, 2.5vw, 40px); min-width: 0; }
  header, .title { display: flex; align-items: center; gap: 10px; }
  header { flex-wrap: wrap; margin-bottom: 20px; }
  .title { min-width: 0; margin-right: auto; }
  .head-actions { display: flex; flex-wrap: wrap; align-items: center; justify-content: flex-end; gap: 8px; }
  .title :global(svg) { flex: none; }
  h1 { font-size: 20px; font-weight: 600; margin: 0; overflow-wrap: anywhere; }
  .team { margin-top: 20px; padding-top: 12px; border-top: 1px solid var(--border); }
  @media (max-width: 600px) { .personal-page { padding: 16px; } }
</style>

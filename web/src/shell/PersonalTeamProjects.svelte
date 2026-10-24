<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { z } from 'zod/v4';
  import { ask } from './native';
  import { model } from './model.svelte';
  let { workspaceId, teamId }: { workspaceId: string; teamId: string } = $props();
  let data = $state<{projects: {id: string; name: string}[]; at: string} | null>(null);
  let problem = $state('');
  let loading = $state(false);
  let now = $state(Date.now());
  let generation = 0;
  const available = $derived(model.view?.items.some(i => i.id === teamId && i.kind === 'team' && i.state === 'ready'));
  const fresh = $derived(data && now - Date.parse(data.at) < 30000 && now >= Date.parse(data.at) - 5000);
  const result = z.object({ projects: z.array(z.object({id: z.string().max(125), name: z.string().max(240)})).max(1000), at: z.iso.datetime() });
  async function refresh() {
    if (loading) return;
    const request = ++generation;
    loading = true; problem = ''; data = null;
    try {
      const next = result.parse(await ask(a => a.PersonalTeamProjects(workspaceId, teamId)));
      if (request === generation) { data = next; now = Date.now(); }
    } catch { if (request === generation) problem = 'Team projects are unavailable. Reconnect or check your permissions.'; }
    finally { if (request === generation) loading = false; }
  }
  $effect(() => {
    if (available) untrack(() => { void refresh(); });
    else { generation++; loading = false; data = null; }
    return () => { generation++; };
  });
  onMount(() => {
    const timer = setInterval(() => { now = Date.now(); if (available) untrack(() => { void refresh(); }); }, 10000);
    return () => { generation++; clearInterval(timer); };
  });
</script>

<section aria-label="Team projects, read-only" class="team-projects">
  {#if !available}<p role="status">Team access is unavailable. Reconnect or check your membership.</p>
  {:else if loading}<p role="status">Checking Team project access…</p>
  {:else if problem}<p role="status">{problem} <button class="btn quiet" onclick={refresh}>Retry</button></p>
  {:else if data && fresh}
    <p class="hint">Read-only · Checked {new Date(data.at).toLocaleTimeString()}</p>
    {#if data.projects.length}<ul>{#each data.projects as project (project.id)}<li>{project.name}</li>{/each}</ul>
    {:else}<p>No projects are available to your Team membership.</p>{/if}
  {:else}<p role="status">This Team snapshot is stale. Refresh before relying on it. <button class="btn quiet" onclick={refresh}>Refresh</button></p>{/if}
</section>
<style>
  .team-projects { padding: 8px 0; font-size: 13px; }
  p { color: var(--text-2); }
  .hint { font-size: 12px; }
  ul { padding-left: 20px; }
</style>

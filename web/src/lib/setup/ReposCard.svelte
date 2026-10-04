<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '../api';
  import { app } from '../state.svelte';
  import type { GitHubStatusInfo, RepoChoice, RepoList } from '../types';
  import SetupCard from './SetupCard.svelte';

  /** `github` is the connection, so that the list is fetched when it becomes available. */
  let { github = null, onadded }: { github?: GitHubStatusInfo | null; onadded?: () => void } = $props();

  let list = $state<RepoList | null>(null);
  let loading = $state(false);
  let error = $state('');
  let filter = $state('');
  let selected = $state<Record<string, boolean>>({});
  let adding = $state(false);
  /** What happened to each repository being added: the row says it, and a failure stays on its row. */
  let outcome = $state<Record<string, { state: 'working' | 'done' | 'failed'; text: string }>>({});

  // A repository by folder, for when GitHub is not connected (or the repository is not on it).
  let path = $state('');
  let pathBusy = $state(false);
  let pathError = $state('');

  const connected = $derived(github?.state === 'signed_in');

  async function load() {
    loading = true;
    error = '';
    try {
      list = await api.githubRepos();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
      list = null;
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    if (connected) void load();
  });
  $effect(() => {
    if (connected && !list && !loading && !error) void load();
  });

  const shown = $derived(
    (list?.repos ?? []).filter((r) => !filter.trim() || r.fullName.toLowerCase().includes(filter.trim().toLowerCase())),
  );
  const chosen = $derived((list?.repos ?? []).filter((r) => selected[r.fullName] && !r.project));
  const toClone = $derived(chosen.filter((r) => r.localPaths.length === 0).length);

  /** `~` for the home directory, so a path reads the way the user writes it. */
  function tidy(p: string): string {
    return p.replace(/^\/(?:Users|home)\/[^/]+/, '~');
  }

  async function addChosen() {
    adding = true;
    for (const r of chosen) {
      outcome[r.fullName] = { state: 'working', text: r.localPaths.length ? 'Adding…' : 'Cloning…' };
      try {
        const res = await api.githubAddRepo(r.fullName, r.localPaths[0] ?? '');
        app.upsertProject(res.project);
        outcome[r.fullName] = { state: 'done', text: res.cloned ? `Cloned to ${tidy(res.project.repoPath)}` : 'Added' };
        selected[r.fullName] = false;
      } catch (err) {
        outcome[r.fullName] = { state: 'failed', text: err instanceof Error ? err.message : String(err) };
      }
    }
    adding = false;
    await load();
    onadded?.();
  }

  async function addPath(e: SubmitEvent) {
    e.preventDefault();
    pathBusy = true;
    pathError = '';
    try {
      app.upsertProject(await api.registerProject(path.trim(), ''));
      path = '';
      onadded?.();
    } catch (err) {
      pathError = err instanceof Error ? err.message : String(err);
    } finally {
      pathBusy = false;
    }
  }

  const status = $derived(app.projects.length > 0 ? 'done' : 'todo');
  const summary = $derived(app.projects.length > 0 ? `${app.projects.length} ${app.projects.length === 1 ? 'project' : 'projects'}` : 'None yet');

  function rowNote(r: RepoChoice): string {
    if (r.project) return `Already a project: ${r.project.name}`;
    if (r.localPaths.length > 0) return `On this computer · ${r.localPaths.map(tidy).join(', ')}`;
    return 'GitHub only · not on this computer: it will be cloned';
  }
</script>

<SetupCard title="Repositories" {status} {summary}>
  <p class="muted">Each repository becomes a project with its own board, Git view and activity. Agents work in a copy of it (a Git worktree) and never in your checkout.</p>

  {#if connected}
    {#if loading && !list}
      <p class="muted">Looking for your repositories…</p>
    {:else if error}
      <p class="error" role="alert">{error}</p>
      <div class="row"><button class="btn small" onclick={load}>Try again</button></div>
    {:else if list}
      <div class="tools">
        <label class="visually-hidden" for="repo-filter">Filter repositories</label>
        <input id="repo-filter" class="input" type="search" placeholder="Filter {list.repos.length} repositories" bind:value={filter} />
      </div>
      <ul class="repos">
        {#each shown as r (r.fullName)}
          {@const o = outcome[r.fullName]}
          <li class="repo" data-local={r.localPaths.length > 0} data-project={!!r.project}>
            <label>
              <input type="checkbox" disabled={!!r.project || adding} bind:checked={selected[r.fullName]} />
              <span class="what">
                <span class="name">
                  {r.fullName}
                  {#if r.private}<span class="chip">private</span>{/if}
                  {#if r.fork}<span class="chip">fork</span>{/if}
                  {#if r.archived}<span class="chip">archived</span>{/if}
                </span>
                <span class="note" data-where={r.project ? 'project' : r.localPaths.length ? 'local' : 'remote'}>{rowNote(r)}</span>
                {#if o}<span class="outcome" data-state={o.state} role={o.state === 'failed' ? 'alert' : undefined}>{o.text}</span>{/if}
              </span>
            </label>
          </li>
        {:else}
          <li class="muted">No repository matches.</li>
        {/each}
      </ul>
      {#if list.truncated}<p class="muted small">Showing your most recently pushed repositories.</p>{/if}
      <div class="row">
        <button class="btn primary" disabled={adding || chosen.length === 0} onclick={addChosen}>
          {adding ? 'Adding…' : chosen.length === 0 ? 'Select repositories' : `Add ${chosen.length} ${chosen.length === 1 ? 'repository' : 'repositories'}${toClone ? ` (${toClone} to clone)` : ''}`}
        </button>
      </div>
      {#if toClone > 0}
        <p class="muted small">Cloning copies the repository from GitHub into {tidy(list.cloneDir)}, using your own sign-in.</p>
      {/if}
    {/if}
  {:else}
    <p class="muted">Connect GitHub above to choose from your repositories, or add one from a folder on this computer:</p>
  {/if}

  <form class="path" onsubmit={addPath}>
    <label for="repo-path">{connected ? 'Or add a folder' : 'Folder'}</label>
    <div class="row">
      <input
        id="repo-path"
        class="input mono"
        placeholder="/Users/you/code/my-app"
        autocapitalize="off"
        autocomplete="off"
        spellcheck="false"
        bind:value={path}
      />
      <button class="btn" type="submit" disabled={pathBusy || !path.trim()}>{pathBusy ? 'Checking…' : 'Add'}</button>
    </div>
    {#if pathError}<p class="error" role="alert">{pathError}</p>{/if}
  </form>
</SetupCard>

<style>
  .row {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }

  .tools {
    display: grid;
  }

  .repos {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 6px;
    max-height: 360px;
    overflow: auto;
  }

  .repo label {
    display: flex;
    gap: 10px;
    align-items: flex-start;
    min-height: 44px;
    padding: 8px 10px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--surface);
    cursor: pointer;
  }

  .repo[data-project='true'] label {
    opacity: 0.7;
    cursor: default;
  }

  input[type='checkbox'] {
    flex: none;
    width: 18px;
    height: 18px;
    margin: 3px 0 0;
  }

  .what {
    display: grid;
    gap: 1px;
    min-width: 0;
  }

  .name {
    font-weight: 600;
    overflow-wrap: anywhere;
  }

  .chip {
    margin-left: 4px;
    padding: 1px 6px;
    border-radius: 99px;
    border: 1px solid var(--border);
    font-size: 0.72rem;
    font-weight: 500;
    color: var(--text-2);
  }

  /* On this computer (access to a clone) and GitHub-only (metadata) are told apart at a glance. */
  .note {
    font-size: 0.82rem;
    color: var(--text-2);
    overflow-wrap: anywhere;
  }

  .note[data-where='local'] {
    color: var(--ok);
  }

  .outcome {
    font-size: 0.82rem;
  }

  .outcome[data-state='done'] {
    color: var(--ok);
  }

  .outcome[data-state='failed'] {
    color: var(--danger);
  }

  .path {
    display: grid;
    gap: 4px;
  }

  .path label {
    font-size: 0.9rem;
    font-weight: 550;
  }

  .small {
    font-size: 0.8rem;
  }
</style>

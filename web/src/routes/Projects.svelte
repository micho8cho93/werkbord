<script lang="ts">
  import FolderPicker from '../lib/FolderPicker.svelte';
  import GitHubCard from '../lib/setup/GitHubCard.svelte';
  import ReposCard from '../lib/setup/ReposCard.svelte';
  import type { GitHubStatusInfo } from '../lib/types';
  import { api } from '../lib/api';
  import { activitySummary, attentionCount, shortPath } from '../lib/projects';
  import { projectHref, router, switchedTo } from '../lib/router.svelte';
  import { app } from '../lib/state.svelte';

  let source = $state<'local' | 'github' | 'work'>('local');
  let github = $state<GitHubStatusInfo | null>(null);
  let path = $state('');
  let name = $state('');
  let busy = $state(false);
  let formError = $state('');

  async function register(e: SubmitEvent) {
    e.preventDefault();
    busy = true;
    formError = '';
    try {
      const p = await api.registerProject(path.trim(), name.trim());
      path = '';
      name = '';
      app.upsertProject(p);
      app.enter(p.id);
      router.go(switchedTo(router.location, p.id));
    } catch (err) {
      formError = err instanceof Error ? err.message : String(err);
    } finally {
      busy = false;
    }
  }

  async function createWork(e: SubmitEvent) {
    e.preventDefault();
    busy = true;
    formError = '';
    try {
      const p = await api.createWorkProject(name.trim());
      name = '';
      app.upsertProject(p);
      app.enter(p.id);
      router.go(switchedTo(router.location, p.id));
    } catch (err) {
      formError = err instanceof Error ? err.message : String(err);
    } finally {
      busy = false;
    }
  }

  const activity = (id: string) => app.overview?.projects.find((p) => p.projectId === id);
</script>

<div class="layout">
  <section class="pn">
    <div class="ph">Projects<span class="chip">{app.projects.length}</span></div>
    {#if app.projects.length}
      <ul class="list">
        {#each app.projects as p (p.id)}
          {@const a = activity(p.id)}
          {@const n = attentionCount(a)}
          <li>
            <a class="project" href={projectHref(p.id, 'overview')} onclick={() => app.enter(p.id)}>
              <span class="sq"></span>
              <span class="what">
                <span class="name">{p.name}</span>
                <span class="mm path">{p.kind === 'work' ? 'Work project · no repository' : shortPath(p.repoPath)}{p.repository ? ` · ${p.repository.currentBranch || 'detached'}` : ''}</span>
              </span>
              <span class="mm sum">{activitySummary(a) || 'quiet'}</span>
              {#if n}<span class="num pend">{n}</span>{/if}
            </a>
          </li>
        {/each}
      </ul>
    {:else}
      <p class="muted">No projects yet. Register a local Git repository to get a board, a calendar and its Git, or start a work project for anything that is not code.</p>
    {/if}
  </section>

  <section class="pn register">
    <h3>Add a project</h3>
    <div class="seg" role="group" aria-label="Project source">
      <button aria-pressed={source === 'local'} onclick={() => source = 'local'}>Local folder</button>
      <button aria-pressed={source === 'github'} onclick={() => source = 'github'}>GitHub repository</button>
      <button aria-pressed={source === 'work'} onclick={() => source = 'work'}>Work project</button>
    </div>
    {#if source === 'work'}
      <p class="muted">A board and a timeline with no Git repository, for work that is not code: a launch, a hiring round, a renovation. Nothing is read from or written to your disk.</p>
      <form onsubmit={createWork}>
        <label>
          <span>Name</span>
          <input class="input" placeholder="Q4 launch" maxlength="120" required bind:value={name} />
        </label>
        {#if formError}<p class="error" role="alert">{formError}</p>{/if}
        <button class="btn primary" type="submit" disabled={busy || !name.trim()}>{busy ? 'Creating…' : 'Create work project'}</button>
      </form>
    {/if}
    {#if source === 'local'}
    <p class="muted">Choose a Git repository on the computer running Werkbord. Each project gets its own board, calendar, Git and runs.</p>
    <FolderPicker bind:value={path} disabled={busy} />
    <form onsubmit={register}>
      <label>
        <span>Selected folder</span>
        <input class="input mono" placeholder="/Users/you/code/my-app" autocapitalize="off" autocomplete="off" spellcheck="false" required bind:value={path} />
      </label>
      <label>
        <span>Name <span class="muted opt">optional</span></span>
        <input class="input" placeholder="Defaults to the folder name" maxlength="120" bind:value={name} />
      </label>
      {#if formError}<p class="error" role="alert">{formError}</p>{/if}
      <button class="btn primary" type="submit" disabled={busy || !path.trim()}>{busy ? 'Checking…' : 'Add project'}</button>
    </form>
    {:else if source === 'github'}
      <GitHubCard onchange={s => github = s} />
      <ReposCard {github} showFolder={false} />
    {/if}
  </section>
</div>

<style>
  .layout {
    display: grid;
    gap: 20px;
    width: 100%;
    align-items: start;
  }

  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
  }

  .project {
    display: flex;
    align-items: center;
    gap: 12px;
    min-height: 52px;
    padding: 8px 8px;
    border-top: 1px solid var(--border);
    color: inherit;
    text-decoration: none;
  }

  li:first-child .project {
    border-top: 0;
  }

  .project:hover {
    background: var(--bg);
  }

  .sq {
    width: 7px;
    height: 7px;
    flex: none;
    background: var(--text);
  }

  .what {
    display: grid;
    gap: 1px;
    min-width: 0;
    flex: 1;
  }

  .name {
    font-weight: 500;
    font-size: 14px;
    overflow-wrap: anywhere;
  }

  .path,
  .sum {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .sum {
    max-width: 40%;
  }

  h3 {
    font-size: 15px;
  }

  form {
    display: grid;
    gap: 12px;
  }

  label {
    display: grid;
    gap: 6px;
    font-size: 13px;
    font-weight: 500;
  }

  .opt {
    font-family: var(--mono);
    font-size: 11px;
    font-weight: 400;
  }

  form .btn {
    justify-self: start;
  }

  @media (min-width: 1000px) {
    .layout {
      grid-template-columns: minmax(0, 1fr) minmax(380px, 0.8fr);
    }
  }

  @media (max-width: 640px) {
    .sum {
      display: none;
    }
  }
</style>

<script lang="ts">
  import { api } from '../lib/api';
  import ProjectAvatar from '../lib/ProjectAvatar.svelte';
  import { activitySummary } from '../lib/projects';
  import { projectHref, router, switchedTo } from '../lib/router.svelte';
  import { app } from '../lib/state.svelte';

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

  const activity = (id: string) => app.overview?.projects.find((p) => p.projectId === id);
</script>

<div class="layout">
  <section class="repos">
    <h2>Projects</h2>
    {#each app.projects as p (p.id)}
      <a class="card project" href={projectHref(p.id)} onclick={() => app.enter(p.id)}>
        <ProjectAvatar id={p.id} name={p.name} size={40} />
        <span class="what">
          <span class="name">{p.name}</span>
          <span class="path mono">{p.repoPath}</span>
          <span class="meta muted">
            {#if p.repository}{p.repository.currentBranch || 'detached HEAD'}{/if}
            {#if activitySummary(activity(p.id))}· {activitySummary(activity(p.id))}{/if}
          </span>
        </span>
      </a>
    {:else}
      <p class="card empty">No projects yet. Register a local Git repository to get a board.</p>
    {/each}
  </section>

  <section class="card register">
    <h2>Register a repository</h2>
    <p class="muted">
      Point Werkbord at an existing Git checkout on this computer. Nothing is copied; the controller only reads its
      metadata. Each repository becomes a project with its own board, Git view and activity.
    </p>
    <form onsubmit={register}>
      <label>
        <span>Path</span>
        <input
          class="input mono"
          placeholder="/Users/you/code/my-app"
          autocapitalize="off"
          autocomplete="off"
          spellcheck="false"
          required
          bind:value={path}
        />
      </label>
      <label>
        <span>Name <span class="muted">(optional)</span></span>
        <input class="input" placeholder="Defaults to the folder name" maxlength="120" bind:value={name} />
      </label>
      {#if formError}<p class="error" role="alert">{formError}</p>{/if}
      <button class="btn primary" type="submit" disabled={busy || !path.trim()}>
        {busy ? 'Checking…' : 'Register'}
      </button>
    </form>
  </section>
</div>

<style>
  .layout {
    display: grid;
    gap: 20px;
    max-width: 1100px;
  }

  .register {
    display: grid;
    gap: 10px;
    padding: 16px;
  }

  form {
    display: grid;
    gap: 12px;
  }

  label {
    display: grid;
    gap: 4px;
    font-size: 0.9rem;
    font-weight: 550;
  }

  .repos {
    display: grid;
    gap: 10px;
    align-content: start;
  }

  .project {
    display: flex;
    align-items: center;
    gap: 14px;
    padding: 12px 14px;
    min-width: 0;
    color: inherit;
    text-decoration: none;
  }

  .project:hover {
    border-color: color-mix(in srgb, var(--accent) 40%, var(--border));
  }

  .what {
    display: grid;
    gap: 1px;
    min-width: 0;
  }

  .name {
    font-weight: 650;
    overflow-wrap: anywhere;
  }

  .path {
    color: var(--text-2);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .meta {
    font-size: 0.82rem;
  }

  @media (min-width: 900px) {
    .layout {
      grid-template-columns: minmax(0, 1fr) 360px;
      align-items: start;
    }
  }
</style>

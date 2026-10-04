<script lang="ts">
  import { api } from '../lib/api';
  import { app } from '../lib/state.svelte';
  import type { Project } from '../lib/types';

  let path = $state('');
  let name = $state('');
  let busy = $state(false);
  let formError = $state('');
  let refreshing = $state<string>('');

  async function register(e: SubmitEvent) {
    e.preventDefault();
    busy = true;
    formError = '';
    try {
      const p = await api.registerProject(path.trim(), name.trim());
      path = '';
      name = '';
      await app.refresh();
      app.selectProject(p.id);
    } catch (err) {
      formError = err instanceof Error ? err.message : String(err);
    } finally {
      busy = false;
    }
  }

  async function refresh(p: Project) {
    refreshing = p.id;
    try {
      await api.refreshProject(p.id);
      await app.refresh();
    } catch (err) {
      app.handleError(err);
    } finally {
      refreshing = '';
    }
  }

  const fmt = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' });
</script>

<div class="layout">
  <section class="card register">
    <h2>Register a repository</h2>
    <p class="muted">
      Point Devboard at an existing Git checkout on this computer. Nothing is copied; the controller only reads its
      metadata.
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

  <section class="repos">
    <h2>Repositories</h2>
    {#each app.projects as p (p.id)}
      {@const repo = p.repository}
      <article class="card repo">
        <header>
          <h3>{p.name}</h3>
          <button class="btn" onclick={() => refresh(p)} disabled={refreshing === p.id}>
            {refreshing === p.id ? 'Refreshing…' : 'Refresh'}
          </button>
        </header>
        <dl>
          <dt>Path</dt>
          <dd class="mono">{p.repoPath}</dd>
          {#if repo}
            <dt>Branch</dt>
            <dd>{repo.currentBranch || 'detached HEAD'}</dd>
            <dt>HEAD</dt>
            <dd class="mono">{repo.headCommit ? repo.headCommit.slice(0, 12) : 'no commits'}</dd>
            {#if repo.defaultBranch}
              <dt>Default</dt>
              <dd>{repo.defaultBranch}</dd>
            {/if}
            <dt>Remotes</dt>
            <dd>
              {#each repo.remotes as r (r.name)}
                <div class="mono">{r.name} · {r.url}</div>
              {:else}
                <span class="muted">none</span>
              {/each}
            </dd>
            <dt>Inspected</dt>
            <dd>{fmt.format(new Date(repo.inspectedAt))}</dd>
          {/if}
        </dl>
      </article>
    {:else}
      <p class="card empty">No repositories registered.</p>
    {/each}
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

  .repo {
    padding: 14px 16px;
    min-width: 0;
  }

  .repo header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    margin-bottom: 10px;
  }

  h3 {
    font-size: 1rem;
    overflow-wrap: anywhere;
  }

  dl {
    display: grid;
    grid-template-columns: max-content minmax(0, 1fr);
    gap: 6px 14px;
    margin: 0;
    font-size: 0.88rem;
  }

  dt {
    color: var(--text-2);
  }

  dd {
    margin: 0;
    overflow-wrap: anywhere;
  }

  @media (min-width: 900px) {
    .layout {
      grid-template-columns: 360px minmax(0, 1fr);
      align-items: start;
    }
  }
</style>

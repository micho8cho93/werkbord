<script lang="ts">
  import Icon from '../Icon.svelte';
  import { projectHref } from '../router.svelte';
  import { app } from '../state.svelte';
  import RepoCard from './RepoCard.svelte';

  // The Git Control Center from the phone: what needs attention in any repository first, then each repository's standing. A
  // repository opens the Control Center itself, where the diffs and every action are, each with the confirmation it already has.
  const repos = $derived(app.projects.filter((p) => p.kind !== 'work'));
  const risks = $derived(app.overview?.repository ?? []);
</script>

<div class="repos">
  {#if app.projects.length > 0 && repos.length === 0}
    <p class="note">None of your projects has a Git repository. Add a repository project to see its branches and pull requests here.</p>
  {:else if risks.length}
    <section aria-labelledby="git-attn">
      <h2 id="git-attn" class="lab">Needs attention <span class="num pend">{risks.length}</span></h2>
      <ul class="risks">
        {#each risks.slice(0, 5) as f (f.id)}
          <li class="card risk" data-testid="repo-risk">
            <p class="kind"><span class="dot" data-tone="bad" aria-hidden="true"></span><b>{f.title}</b></p>
            <p class="mm">{f.projectName ?? app.project(f.projectId)?.name ?? ''}</p>
            <p class="expl">{f.explanation}</p>
            <p class="next"><span class="mm">Next</span> {f.action.label}</p>
            <a class="btn small" href={projectHref(f.projectId, 'git')} onclick={() => app.enter(f.projectId)}>Open Git<Icon name="right" size={14} /></a>
          </li>
        {/each}
      </ul>
      {#if risks.length > 5}<p class="note">and {risks.length - 5} more, in each repository below.</p>{/if}
    </section>
  {/if}

  {#if repos.length}
    <section aria-labelledby="git-repos">
      <h2 id="git-repos" class="lab">Repositories</h2>
      <ul>{#each repos as p (p.id)}<RepoCard project={p} />{/each}</ul>
    </section>
  {:else if app.projects.length === 0}
    <p class="note">Add a project to see its Git state here.</p>
  {/if}
</div>

<style>
  .repos {
    display: grid;
    gap: 18px;
    padding: 4px 16px 24px;
  }

  section {
    display: grid;
    gap: 8px;
  }

  h2 {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 0;
  }

  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 10px;
  }

  .note {
    margin: 0;
    color: var(--text-2);
    font-size: 13px;
  }

  .risk {
    display: grid;
    gap: 6px;
    padding: 14px;
    border-color: color-mix(in srgb, var(--danger) 40%, var(--border));
  }

  .risk p {
    margin: 0;
  }

  .kind {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 14px;
  }

  .expl {
    font-size: 13px;
  }

  .risk .btn {
    justify-self: start;
    min-height: 44px;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    text-decoration: none;
  }
</style>

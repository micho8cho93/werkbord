<script lang="ts">
  import { model } from './model.svelte';
  import { contextKey } from './project-context';
  import WorkspaceTag from '../lib/WorkspaceTag.svelte';

  // Git across projects, or for a team: each project's repository is its own page, so this lists them and opens the one chosen.
  // Each opens its Git in the page of the workspace it belongs to.
  const selected = $derived(model.selectedProject);
  const shown = $derived(model.projects.filter(p => !selected || contextKey(p) === contextKey(selected)));
  const itemOf = (id: string) => model.view?.items.find(i => i.id === id);
  function open(workspace: string, project: string) {
    model.selectProject({ workspace, project });
    void model.openGit();
  }
</script>

<section class="page" aria-labelledby="git-h">
  <header><h1 id="git-h">Git</h1><p class="lede">{selected ? model.contextLabel : 'The repository of each project'}</p></header>
  {#if !model.overview}<p class="empty">Loading projects…</p>
  {:else if !shown.length}<p class="empty">No projects to show.</p>
  {:else}
    <ul>
      {#each shown as p (contextKey(p))}
        {@const item = itemOf(p.workspace)}
        <li>
          <button type="button" class="project" onclick={() => open(p.workspace, p.project)}><strong>{p.name}</strong>{#if item && p.kind === 'team'}<WorkspaceTag workspace={item} />{/if}<span class="muted">Open Git</span></button>
        </li>
      {/each}
    </ul>
  {/if}
</section>

<style>
  .page { padding: 28px clamp(16px, 4vw, 48px); overflow-y: auto; height: 100%; }
  h1 { margin: 0; }
  .lede { margin: 4px 0 18px; color: var(--text-2); }
  ul { list-style: none; margin: 0; padding: 0; border: 1px solid var(--border); border-radius: var(--radius); background: var(--surface); }
  li + li { border-top: 1px solid var(--border); }
  .project { display: flex; align-items: center; gap: 12px; width: 100%; min-height: 56px; padding: 12px 16px; border: 0; background: transparent; color: var(--text); font: inherit; text-align: left; }
  button.project { cursor: pointer; }
  button.project:hover { background: var(--hover); }
  .muted { margin-left: auto; color: var(--text-2); font-size: 12px; }
  .empty { color: var(--text-2); }
</style>

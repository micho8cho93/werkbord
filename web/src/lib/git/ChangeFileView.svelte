<script lang="ts">
  import { api } from '../api';
  import { changesHref } from '../gitroute';
  import type { ChangeKind } from '../gitroute';
  import { splitPath } from '../gitui';
  import type { Project } from '../types';
  import FileDiffView from './FileDiffView.svelte';

  let { project, worktree, kind, path, oldPath }: { project: Project; worktree: string; kind: ChangeKind; path: string; oldPath: string } = $props();
  const p = $derived(splitPath(path));
</script>

<div class="g-page">
  <a class="g-back" href={changesHref(project.id, worktree)}>← Working changes</a>
  <header>
    <h2 class="path g-wrap"><span class="dir">{p.dir}</span>{p.name}</h2>
    <p class="g-small muted">{kind === 'staged' ? 'Staged' : kind === 'unstaged' ? 'Modified, not staged' : 'Untracked'}{#if oldPath} · renamed from {oldPath}{/if}</p>
  </header>
  <FileDiffView label={`${worktree}|${kind}|${path}`} load={(offset) => api.git.changeDiff(project.id, worktree, kind, path, oldPath, offset, 400)} />
</div>

<style>
  .path {
    font-size: 0.95rem;
    text-transform: none;
    letter-spacing: 0;
    color: var(--text);
    font-family: var(--mono);
    font-weight: 600;
  }

  .dir {
    color: var(--text-2);
    font-weight: 500;
  }
</style>

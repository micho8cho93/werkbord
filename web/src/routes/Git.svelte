<script lang="ts">
  import { untrack } from 'svelte';
  import '../lib/git/git.css';
  import BranchView from '../lib/git/BranchView.svelte';
  import ChangeFileView from '../lib/git/ChangeFileView.svelte';
  import ChangesView from '../lib/git/ChangesView.svelte';
  import CommitsView from '../lib/git/CommitsView.svelte';
  import FileView from '../lib/git/FileView.svelte';
  import GitSheets from '../lib/git/GitSheets.svelte';
  import Overview from '../lib/git/Overview.svelte';
  import { sheets } from '../lib/git/sheets.svelte';
  import { gitStore } from '../lib/git/store.svelte';
  import { parseGit } from '../lib/gitroute';
  import { router } from '../lib/router.svelte';
  import type { Project } from '../lib/types';

  // The Git Control Center of one project. This file only chooses the screen: the address
  // says which (see gitroute.ts), so Back, a shared link and a reload all land in the right place.

  let { project }: { project: Project } = $props();

  const store = $derived(gitStore(project.id));
  const screen = $derived(parseGit(router.sub));

  // Looking at Git starts it loading, and keeps it current while it is looked at.
  $effect(() => store.watch());

  // GitHub is asked for once, separately, so it can never hold up the local picture.
  $effect(() => {
    const s = store;
    untrack(() => {
      if (!s.github && !s.githubLoading) void s.loadGitHub();
    });
  });

  // Moving between screens never leaves a sheet open on the wrong one.
  $effect(() => {
    void router.sub;
    sheets.close();
  });
</script>

{#if screen.screen === 'branch'}
  <BranchView {project} {store} scope={screen.scope} name={screen.name} />
{:else if screen.screen === 'file'}
  <FileView {project} scope={screen.scope} name={screen.name} path={screen.path} oldPath={screen.oldPath} />
{:else if screen.screen === 'changes'}
  <ChangesView {project} worktree={screen.worktree} />
{:else if screen.screen === 'change'}
  <ChangeFileView {project} worktree={screen.worktree} kind={screen.kind} path={screen.path} oldPath={screen.oldPath} />
{:else if screen.screen === 'commits'}
  <CommitsView {project} scope={screen.scope} name={screen.name} />
{:else}
  <Overview {project} {store} />
{/if}

<GitSheets projectId={project.id} {store} />

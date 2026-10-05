<script lang="ts">
  import CleanSheet from './CleanSheet.svelte';
  import DeleteSheet from './DeleteSheet.svelte';
  import MergeSheet from './MergeSheet.svelte';
  import PRSheet from './PRSheet.svelte';
  import PushSheet from './PushSheet.svelte';
  import { sheets } from './sheets.svelte';
  import type { GitStore } from './store.svelte';

  let { projectId, store }: { projectId: string; store: GitStore } = $props();
  const s = $derived(sheets.current);
</script>

<!-- A sheet is made fresh each time it opens, so nothing from the last one carries over. -->
{#key s}
  {#if s?.kind === 'merge'}
    <MergeSheet {projectId} {store} branchName={s.branch} onmerged={s.onmerged} />
  {:else if s?.kind === 'push'}
    <PushSheet {projectId} {store} branchName={s.branch} />
  {:else if s?.kind === 'pr'}
    <PRSheet {projectId} {store} branchName={s.branch} />
  {:else if s?.kind === 'delete'}
    <DeleteSheet {projectId} {store} branchName={s.branch} />
  {:else if s?.kind === 'clean'}
    <CleanSheet {projectId} {store} worktreeId={s.worktreeId} label={s.label} head={s.head} />
  {/if}
{/key}

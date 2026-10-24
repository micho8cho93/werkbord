<script lang="ts">
  import { onMount, setContext } from 'svelte';
  import { Renderer } from '@openuidev/svelte-lang';
  import { WORKSPACE_CONTEXT, type WorkspaceSession } from './session.svelte';
  import { workspaceLibrary, workspaceSource } from './library';
  let { session }: { session: WorkspaceSession } = $props();
  // A mounted renderer belongs to one loaded workspace session.
  // svelte-ignore state_referenced_locally
  setContext(WORKSPACE_CONTEXT, session);
  const source = $derived(session.workspace ? workspaceSource(session.workspace) : '');
  let invalid = $state(false);
  onMount(() => {const timer = setInterval(() => {void session.refreshRecords();}, 60000); return () => clearInterval(timer);});
</script>

{#if session.workspace}
  <div class="saved-workspace" aria-label={session.workspace.name} aria-busy={session.busy}>
    <div class="save-status" aria-live="polite">{session.busy ? 'Saving…' : session.notice}</div>
    {#if session.problem}<p class="problem" role="alert">{session.problem}</p>{/if}
    {#if invalid}<p class="problem" role="alert">This saved interface could not be rendered. Your collection records are safe. Reload after correcting its definition.</p>
    {:else}<Renderer response={source} library={workspaceLibrary} isStreaming={false} onParseResult={(result) => invalid = !!result && (!result.root || result.meta.incomplete || result.meta.errors.length > 0 || result.meta.unresolved.length > 0)} />{/if}
  </div>
{/if}

<style>
  .saved-workspace { min-width: 0; }
  .save-status { min-height: 24px; color: var(--text-2); font-size: 12px; }
  .problem { padding: 12px; border: 1px solid var(--border); border-radius: var(--radius); color: var(--danger-text); font-size: 13px; overflow-wrap: anywhere; }
</style>

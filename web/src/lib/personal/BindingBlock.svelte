<script lang="ts">
  import { untrack } from 'svelte';
  import type { ComponentRenderProps } from '@openuidev/svelte-lang';
  import { workspaceSession } from './session.svelte';
  import type { PersonalAPI } from './api';
  let {props}: ComponentRenderProps<{bindingId:string}> = $props();
  const session = workspaceSession();
  let result = $state<Awaited<ReturnType<PersonalAPI['binding']>> | null>(null);
  let problem = $state('');
  let attempt = $state(0);
  $effect(() => {const wid = session.workspace!.id, bid = props.bindingId; void attempt; let active = true; result = null; problem = '';
    if (session.readOnly) return;
    untrack(() => {void session.api.binding(wid,bid).then(r => {if (active) result = r;}).catch(() => {if (active) problem = 'This project is unavailable. Check its access or ask Salie to update this binding.';});});
    return () => {active = false;};
  });
</script>
<section aria-label="Connected project data">
  {#if session.readOnly}<p>Connected project data · read-only · resolves after saving</p>
  {:else if problem}<p role="status">{problem}</p><button class="btn quiet" onclick={() => attempt++}>Reload project data</button>
  {:else if result}<div class="heading"><h3>{result.name}</h3><button class="btn quiet" onclick={() => attempt++}>Refresh</button></div><p>{result.unresolved} unresolved · {result.total} total tasks</p><ul>{#each result.tasks as task (task.id)}<li><span>{task.title}</span><span>{task.state}</span></li>{/each}</ul>{#if result.total > result.tasks.length && result.tasks.length}<p>Showing the first {result.tasks.length} tasks. Counts include the full project.</p>{/if}
  {:else}<p role="status">Loading project data…</p>{/if}
</section>
<style>
  section { padding: 16px; border: 1px solid var(--border); border-radius: var(--radius); background: var(--surface); min-width: 0; }
  :global(.card) section { padding: 0; border: 0; background: none; }
  .heading, li { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 8px; }
  h3 { margin: 0; font-size: 16px; }
  p, li { color: var(--text-2); font-size: 13px; overflow-wrap: anywhere; }
  ul { list-style: none; margin: 0; padding: 0; }
  li { padding-block: 8px; border-top: 1px solid var(--border); }
</style>

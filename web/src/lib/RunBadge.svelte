<script lang="ts">
  import { app } from './state.svelte';
  import { runReportStale, runStatus } from './format';
  import type { Run } from './types';

  let { run }: { run: Run } = $props();
  const status = $derived(runStatus(run));
  const stale = $derived(runReportStale(run, app.connection === 'live', app.runners));
  const requested = $derived(!status.active ? '' : run.pendingRequests?.includes('stop') ? ' · stop requested' : run.pendingRequests?.includes('finish') ? ' · finish requested' : '');
</script>

<span class="badge" data-tone={status.tone}><span class="dot" aria-hidden="true"></span>{stale ? 'Last reported: ' : ''}{status.label}{requested}</span>

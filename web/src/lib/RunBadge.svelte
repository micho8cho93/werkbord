<script lang="ts">
  import { runStatus } from './format';
  import type { Run } from './types';

  let { run }: { run: Run } = $props();
  const status = $derived(runStatus(run));
</script>

<span class="badge" data-tone={status.tone}><span class="dot" aria-hidden="true"></span>{status.label}</span>

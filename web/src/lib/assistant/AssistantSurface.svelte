<script lang="ts">
  import { assistant } from './sessions.svelte';
  import PendingInteraction from './PendingInteraction.svelte';
  import Assistant from './Assistant.svelte';

  // The assistant's own page in the desktop window (embed.ts, `surface`): the shell frames it over the whole window. It draws
  // nothing until the sidebar's button opens it, so the window shows what it did before.
</script>

<Assistant />

{#if assistant.compact && !assistant.mounted}<PendingInteraction floating={false} />{/if}

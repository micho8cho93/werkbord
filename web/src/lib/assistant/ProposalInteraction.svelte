<script lang="ts">
  import { provideSession } from './session-context';
  import ConfirmCard from './ConfirmCard.svelte';
  import type { AssistantStore } from './store.svelte';
  import type { ConfirmItem } from './transcript';
  let { store, item }: { store: AssistantStore; item: ConfirmItem } = $props();
  // svelte-ignore state_referenced_locally
  provideSession(store);
</script>
<ConfirmCard {item} />

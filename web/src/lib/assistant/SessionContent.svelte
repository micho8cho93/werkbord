<script lang="ts">
  import Conversation from './Conversation.svelte';
  import Composer from './Composer.svelte';
  import { provideSession } from './session-context';
  import type { AssistantStore } from './store.svelte';
  let { store }: { store: AssistantStore } = $props();
  // This component is keyed by tab identity. Async UI callbacks stay bound to this session.
  // svelte-ignore state_referenced_locally
  provideSession(store);
</script>
<Conversation />
{#if store.status === 'ready' || store.hasConversation}<Composer />{/if}

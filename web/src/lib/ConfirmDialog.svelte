<script lang="ts">
  import Sheet from './Sheet.svelte';
  import { confirmState } from './confirm.svelte';

  const pending = $derived(confirmState.current);
</script>

{#if pending}
  <Sheet title={pending.title} onclose={() => pending.settle(false)} width="26rem" layer={120}>
    <p data-testid="confirm-message">{pending.message}</p>
    {#snippet footer()}
      <button class="btn" data-autofocus={pending.danger ? '' : undefined} data-testid="confirm-cancel" onclick={() => pending.settle(false)}>Cancel</button>
      <button class="btn {pending.danger ? 'danger' : 'primary'}" data-autofocus={pending.danger ? undefined : ''} data-testid="confirm-ok" onclick={() => pending.settle(true)}>{pending.confirmLabel}</button>
    {/snippet}
  </Sheet>
{/if}

<script lang="ts">
  import { onMount } from 'svelte';
  import { ask } from './native';
  import { partialUpdate, type UpdateState } from './partial-update';

  // Shown whenever the part of Werkbord that runs your work is older than this app, for as long as that is so: a refusal said
  // once and forgotten is how a window ends up on old screens without anyone knowing. "Finish update" is the app's own dialog.
  let current = $state<UpdateState | null>(null);
  let busy = $state(false);
  const say = $derived(partialUpdate(current));

  async function refresh() {
    try { current = await ask((a) => a.UpdateState()); } catch { /* An app from before this existed has nothing to say. */ }
  }
  async function finish() {
    busy = true;
    try { await ask((a) => a.RequestUpdate()); } catch { /* The app says what happened in its own dialog. */ }
    finally { busy = false; void refresh(); }
  }
  onMount(() => {
    void refresh();
    const timer = setInterval(() => void refresh(), 30_000);
    window.addEventListener('focus', refresh);
    return () => { clearInterval(timer); window.removeEventListener('focus', refresh); };
  });
</script>

{#if say}
  <div class="message" role="status" data-testid="partial-update">
    <span><strong>{say.title}</strong> {say.body}</span>
    <button class="btn primary" type="button" disabled={busy} onclick={finish}>{busy ? 'Updating…' : 'Finish update'}</button>
  </div>
{/if}

<style>
  .message {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 12px;
    padding: 8px 16px;
    border-bottom: 1px solid var(--border);
    background: var(--surface-2);
  }
</style>

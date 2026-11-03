<script lang="ts">
  import { onMount } from 'svelte';
  import { callDesktop } from './desktop';
  import { desktop } from './desktop.svelte';
  import { partialUpdate, type UpdateState } from './partial-update';

  // In the desktop app, shown whenever the part of Werkbord that runs your work (this page, served by the controller) is older than
  // the app, for as long as that is so: a refusal said once and forgotten is how a window ends up on old screens without anyone
  // knowing. "Finish update" is the app's own dialog. In a browser it is never there.
  let current = $state<UpdateState | null>(null);
  let busy = $state(false);
  const say = $derived(desktop.available ? partialUpdate(current) : null);

  async function refresh() {
    if (!desktop.available) return;
    try { current = await callDesktop<UpdateState>('UpdateState', [], 5000); } catch { /* An app from before this existed has nothing to say. */ }
  }
  async function finish() {
    busy = true;
    try { await callDesktop('RequestUpdate', [], 0); } catch { /* The app says what happened in its own dialog. */ }
    finally { busy = false; void refresh(); }
  }
  $effect(() => { if (desktop.available) void refresh(); });
  onMount(() => {
    const timer = setInterval(() => void refresh(), 30_000);
    window.addEventListener('focus', refresh);
    return () => { clearInterval(timer); window.removeEventListener('focus', refresh); };
  });
</script>

{#if say}
  <div role="status" class="banner" data-testid="partial-update">
    <span class="text"><strong>{say.title}</strong> <span class="what">{say.body}</span></span>
    <button class="btn primary" type="button" disabled={busy} onclick={finish}>{busy ? 'Updating…' : 'Finish update'}</button>
  </div>
{/if}

<style>
  /* The accent's quiet strip, as the update banner: information, not something that needs you. */
  .banner { display: flex; align-items: center; gap: 12px; min-height: 40px; padding: 6px 24px; background: color-mix(in srgb, var(--accent) 9%, var(--bg)); border-bottom: 1px solid color-mix(in srgb, var(--accent) 30%, var(--border)); color: var(--text); font-size: 13px; }
  .text { flex: 1; min-width: 0; }
  .what { color: var(--text-2); }
</style>

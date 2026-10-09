<script lang="ts">
  import { onMount } from 'svelte';
  import { ask } from './native';
  import type { MigrationStatus } from './types';

  let status = $state<MigrationStatus | null>(null);
  let busy = $state(false);
  let error = $state('');
  let components = $state('');

  onMount(() => {
    void ask(async (a) => {
      if (a.MigrationStatus) status = await a.MigrationStatus();
      components = (await a.Info()).components ?? '';
    }).catch((e: Error) => (error = e.message));
  });

  async function migrate(action: 'adopt' | 'rollback') {
    busy = true;
    error = '';
    try {
      status = await ask((a) => {
        if (!a.Migrate) throw new Error('Reopen the updated Werkbord app to migrate.');
        return a.Migrate(action);
      });
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      busy = false;
    }
  }
</script>

{#if status && status.phase !== 'not_needed'}
  <section class="migration" aria-labelledby="migration-heading" aria-busy={busy}>
    <h2 id="migration-heading">Your existing installation</h2>
    <p>
      {#if status.phase === 'verified'}
        Adoption verified. Your original data and applications are kept.
      {:else if status.phase === 'prepared'}
        Your backup is ready. Retry adoption to verify the existing services.
      {:else if status.phase === 'rolled_back'}
        Desktop adoption was rolled back. Your services and current work are preserved.
      {:else}
        Keep using your tasks, credentials and teams in this window. Back up and verify the existing services before updating them.
      {/if}
    </p>
    <details>
      <summary>Detected installations</summary>
      <ul>
        {#each status.installations as installation (installation.path)}
          <li><code>{installation.path}</code></li>
        {/each}
      </ul>
    </details>
    <div class="actions">
      {#if status.phase !== 'verified'}
        <button class="btn primary" type="button" disabled={busy} onclick={() => migrate('adopt')}>
          {busy ? 'Checking installation…' : status.phase === 'prepared' ? 'Retry adoption' : 'Back up and adopt…'}
        </button>
      {/if}
      {#if status.phase === 'prepared' || status.phase === 'verified'}
        <button class="btn" type="button" disabled={busy} onclick={() => migrate('rollback')}>Roll back adoption…</button>
      {/if}
    </div>
  </section>
{/if}
{#if error}<p class="error" role="alert">{error}</p>{/if}
{#if components}
  <details class="components">
    <summary>Bundled component versions</summary>
    <pre>{components}</pre>
  </details>
{/if}

<style>
  .migration { margin-top: 24px; padding-top: 18px; border-top: 1px solid var(--border); }
  h2 { margin: 0; font-size: 14px; }
  p { max-width: 70ch; color: var(--text-2); font-size: 13px; }
  summary { cursor: pointer; font-size: 13px; }
  ul { padding-left: 20px; }
  code, pre { font-family: var(--mono); font-size: 12px; overflow-wrap: anywhere; white-space: pre-wrap; }
  .actions { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 12px; }
  .error { color: var(--danger-text); }
  .components { margin-top: 24px; }
</style>

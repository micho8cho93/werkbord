<script lang="ts">
  import { api } from '../api';
  import { dismissEnrollment, enrollment } from './enroll.svelte';

  /** Shown once, on a phone that has just scanned its code: it is signed in already, and the name it was given can change. */
  let name = $state('');
  let editing = $state(false);
  let saving = $state(false);
  let error = $state('');

  async function save(e: SubmitEvent) {
    e.preventDefault();
    saving = true;
    error = '';
    try {
      const p = await api.renameSelf(name.trim());
      enrollment.name = p.name;
      editing = false;
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      saving = false;
    }
  }
</script>

{#if enrollment.state === 'done'}
  <div class="welcome" role="status" data-testid="phone-welcome">
    {#if editing}
      <form onsubmit={save}>
        <label for="phone-name">Name of this phone</label>
        <input id="phone-name" class="input" maxlength="60" bind:value={name} />
        <button class="btn small primary" type="submit" disabled={saving || !name.trim()}>Save</button>
        <button class="btn small quiet" type="button" onclick={() => (editing = false)}>Cancel</button>
      </form>
      {#if error}<p class="error" role="alert">{error}</p>{/if}
    {:else}
      <p>Signed in as <strong>{enrollment.name}</strong>. You stay signed in until you revoke this phone on your computer.</p>
      <div class="row">
        <button class="btn small quiet" onclick={() => ((name = enrollment.name), (editing = true))}>Rename</button>
        <button class="btn small" onclick={dismissEnrollment}>OK</button>
      </div>
    {/if}
  </div>
{/if}

<style>
  .welcome {
    margin: 8px 12px 0;
    padding: 10px 12px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--surface-2);
    font-size: 13px;
    display: grid;
    gap: 8px;
  }

  p {
    margin: 0;
  }

  .row,
  form {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
    align-items: center;
  }

  form label {
    flex-basis: 100%;
    font-size: 12px;
    color: var(--text-2);
  }

  form .input {
    flex: 1;
    min-width: 0;
  }
</style>

<script lang="ts">
  import Sheet from '../lib/Sheet.svelte';
  import { ask } from './native';
  import { model } from './model.svelte';

  // Adding a Team is two ordinary things: a place to create one or join one (Team's own screens do the rest), and, the first
  // time on this Mac, Team's service, which needs the person's say-so and an administrator password. Nothing here installs
  // anything: it asks the app, and the app asks the person.
  const team = $derived(model.view?.team);
  const needsSetup = $derived(team && team.state !== 'ready');
  const noInstaller = $derived(needsSetup && !model.view?.teamInstaller && team?.state === 'not_installed');
  const releases = 'https://github.com/micho8cho93/werkbord/releases';
</script>

<Sheet title="Add a Team" onclose={() => (model.addOpen = false)}>
<div data-testid="add-team-dialog">
  {#if model.view?.invitation}<p class="note">You opened a Team invitation. It will be used to join.</p>{/if}

  {#if noInstaller}
    <p>Werkbord Team is not installed on this Mac. It is a separate download with its own license.</p>
    <div class="actions">
      <button class="btn primary" type="button" onclick={() => ask((a) => a.OpenExternal(releases))}>Get Werkbord Team</button>
      <button class="btn" type="button" onclick={() => (model.addOpen = false)}>Close</button>
    </div>
  {:else if needsSetup}
    <p>{team?.detail || 'Team is not set up on this computer.'}</p>
    <p class="fine">Setting it up installs a background service with your administrator password. Your agents never run inside it.</p>
    <div class="actions">
      <button class="btn primary" type="button" onclick={() => team?.state === 'stopped' ? model.teamService('start') : model.activateTeam()} disabled={!!model.busy}>{team?.state === 'stopped' ? 'Start Team…' : 'Set up Team…'}</button>
      <button class="btn" type="button" onclick={() => (model.addOpen = false)}>Not now</button>
    </div>
  {:else}
    <p>Create a Team for your organization, or join one with an invitation from its administrator. You can belong to several.</p>
    <div class="actions">
      <button class="btn primary" type="button" onclick={() => model.addTeam()} disabled={!!model.busy} data-testid="add-team-continue">Create or join a Team</button>
      <button class="btn" type="button" onclick={() => (model.addOpen = false)}>Cancel</button>
    </div>
  {/if}
  {#if model.error}<p class="error" role="alert">{model.error}</p>{/if}
  {#if model.busy}<p class="note" role="status">{model.busy}</p>{/if}
</div>
</Sheet>

<style>
  p {
    margin: 0 0 10px;
    font-size: 13.5px;
  }
  .fine,
  .note {
    color: var(--text-2);
    font-size: 12.5px;
  }
  .error {
    color: var(--danger-text);
  }
  .actions {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
    margin-top: 14px;
  }
</style>

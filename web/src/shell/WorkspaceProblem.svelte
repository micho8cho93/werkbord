<script lang="ts">
  import { model } from './model.svelte';

  // What shows where the person's own Werkbord would be when it cannot be shown: too old for this window (it refuses to be
  // framed and lacks what the window reads), or not running. Never a blank page.
  const item = $derived(model.view?.items.find((i) => i.id === 'personal'));
  const outdated = $derived(model.personalOutdated);
</script>

<section class="problem" aria-labelledby="problem-h" data-testid="personal-problem">
  {#if outdated}
    <h1 id="problem-h">Update Werkbord to use it here</h1>
    <p>{item?.detail}</p>
    <p class="muted">
      Your projects, tasks, runs and repositories stay where they are. Werkbord backs up your installation first, asks you
      before changing anything, and does not update while agents are working.
    </p>
    <div class="actions">
      <button class="btn primary" type="button" onclick={() => model.updatePersonal()} disabled={!!model.busy} data-testid="update-personal">{model.busy || 'Update Werkbord…'}</button>
      <button class="btn" type="button" onclick={() => model.go('workspaces')}>Workspaces and devices</button>
    </div>
  {:else}
    <h1 id="problem-h">Werkbord is not running on this computer</h1>
    <p>{item?.detail || 'The window could not reach it.'}</p>
    <div class="actions">
      <button class="btn primary" type="button" onclick={async () => { await model.refresh(); await model.open('personal'); }}>Try again</button>
      <button class="btn" type="button" onclick={() => model.go('workspaces')}>Workspaces and devices</button>
    </div>
  {/if}
</section>

<style>
  .problem {
    max-width: 560px;
    margin: 12vh auto 0;
    padding: 0 24px;
  }
  h1 {
    margin: 0 0 12px;
    font-size: 22px;
    letter-spacing: -0.01em;
  }
  p {
    margin: 0 0 10px;
    line-height: 1.5;
  }
  .muted {
    color: var(--text-2);
  }
  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    margin-top: 18px;
  }
</style>

<script lang="ts">
  import { model } from './model.svelte';

  // What shows where the person's own Werkbord would be when the shell cannot reach it. Availability may be unknown.
  const item = $derived(model.view?.items.find((i) => i.id === 'personal'));
</script>

<section class="problem" aria-labelledby="problem-h" data-testid="personal-problem">
  <h1 id="problem-h">Cannot reach Werkbord on this computer</h1>
  <p>{item?.detail || 'Check the connection, then refresh this workspace.'}</p>
  <div class="actions">
    <button class="btn primary" type="button" onclick={async () => { await model.refresh(); await model.open('personal'); }}>Check connection</button>
    <button class="btn" type="button" onclick={() => model.go('workspaces')}>Devices &amp; Hosting</button>
  </div>
</section>

<style>
  .problem {
    max-width: 560px;
    margin: 12vh auto 0;
    padding: 0 24px;
  }
  h1 {
    margin: 0 0 12px;
    font-size: 20px;
    letter-spacing: -0.01em;
  }
  p {
    margin: 0 0 10px;
    line-height: 1.5;
  }
  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    margin-top: 18px;
  }
</style>

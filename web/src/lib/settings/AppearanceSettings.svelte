<script lang="ts">
  import { look, STYLES, theme } from '../theme.svelte';

  // How Werkbord looks: the same two choices in the Mac window and in the web app, kept on this device.
</script>

<section class="pn" aria-labelledby="theme-h">
  <div class="head">
    <h3 id="theme-h">Theme</h3>
    <p class="muted">Light or dark, everywhere in Werkbord. “Device” follows your system unless you choose.</p>
  </div>
  <div class="seg" role="group" aria-label="Theme">
    <button type="button" aria-pressed={theme.choice === ''} onclick={() => theme.set('')}>Device</button>
    <button type="button" aria-pressed={theme.choice === 'light'} onclick={() => theme.set('light')}>Light</button>
    <button type="button" aria-pressed={theme.choice === 'dark'} onclick={() => theme.set('dark')}>Dark</button>
  </div>
</section>
<section class="pn" aria-labelledby="style-h">
  <div class="head">
    <h3 id="style-h">Style</h3>
    <p class="muted">How Werkbord is drawn. {STYLES.find((s) => s.id === look.choice)?.about}</p>
  </div>
  <div class="seg" role="group" aria-label="Style">
    {#each STYLES as s (s.id)}
      <button type="button" aria-pressed={look.choice === s.id} onclick={() => look.set(s.id)}>{s.label}</button>
    {/each}
  </div>
</section>

<style>
  .head { display: grid; gap: 4px; margin-bottom: 10px; }
</style>

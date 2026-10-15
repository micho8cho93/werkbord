<script lang="ts">
  import RunnersPanel from '../RunnersPanel.svelte';

  // The person's own runners: the machines that carry out their agents. It is the panel Settings already has, with the same
  // controls and the same rules; this page only gives it a place of its own on the phone. A runner belongs to the person who
  // paired it, and nothing here reaches anyone else's machine.
</script>

<div class="runners">
  <RunnersPanel />
</div>

<style>
  .runners {
    padding: 4px 16px 24px;
  }
</style>

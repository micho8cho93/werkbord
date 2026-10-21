<script lang="ts">
  import Icon from '../Icon.svelte';
  import { router } from '../router.svelte';
  import { parentHref, parentTitle } from './nav';

  // The label and destination both follow the page hierarchy.
  const where = $derived(parentTitle(router.location));
</script>

<button class="btn icon back" type="button" onclick={() => location.hash = parentHref(router.location)} aria-label={where ? `Back to ${where}` : 'Back'} title="Back" data-testid="phone-back">
  <Icon name="left" size={18} />
</button>

<style>
  .back {
    flex: none;
    width: 44px;
    height: 44px;
    padding: 0;
    margin-left: -8px;
  }
</style>

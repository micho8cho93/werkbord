<script lang="ts">
  import Icon from '../Icon.svelte';
  import { router } from '../router.svelte';
  import { parentTitle } from './nav';
  import { trail } from './trail.svelte';

  // Back, at the left of a phone page's heading. It goes where the person came from, and to the page above when they did not
  // come from anywhere in the app (a link, a reload).
  const where = $derived(parentTitle(router.location));
</script>

<button class="btn icon back" type="button" onclick={() => trail.back()} aria-label={where ? `Back to ${where}` : 'Back'} title="Back" data-testid="phone-back">
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

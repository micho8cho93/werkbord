<script lang="ts">
  import { streamedText } from './stream-motion';
  let { text, live }: { text: string; live: boolean } = $props();
</script>

<span use:streamedText={{ text, live }}></span>

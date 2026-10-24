<script lang="ts" module>
  import type { AppIconName } from './app-icons';
  /** Every icon the interface draws: one library (Lucide), one stroke weight at every size, and one icon for each meaning. */
  export type IconName = AppIconName;
</script>

<script lang="ts">
  import { appIconComponents } from './app-icons';
  let { name, size = 16 }: { name: IconName; size?: number } = $props();
  const Glyph = $derived(appIconComponents[name]);
</script>

<!-- A constant 1.6px line however large the icon is drawn, so a small icon is not heavy and a large one is not thin. -->
<Glyph {size} strokeWidth={1.6} absoluteStrokeWidth aria-hidden="true" />

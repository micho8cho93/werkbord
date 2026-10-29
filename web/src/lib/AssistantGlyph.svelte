<script lang="ts">
  // Salie's own mark: the two sparkles the interface has always used for her. It is drawn by hand rather than taken from
  // Lucide, so it takes the same props as a Lucide icon and uses the same dimensions as other interface icons.
  let { size = 16, strokeWidth = 1.6, absoluteStrokeWidth = false, ...rest }: { size?: number; strokeWidth?: number; absoluteStrokeWidth?: boolean; [key: string]: unknown } = $props();
  const width = $derived(absoluteStrokeWidth ? (strokeWidth * 16) / size : strokeWidth);
</script>

<svg xmlns="http://www.w3.org/2000/svg" width={size} height={size} viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width={width} stroke-linecap="round" stroke-linejoin="round" {...rest}>
  <path d="M6.8 2.2l1.4 3.8 3.8 1.4-3.8 1.4-1.4 3.8-1.4-3.8L1.6 7.4 5.4 6z" /><path d="M12.6 1.6v2.8M11.2 3h2.8" />
</svg>

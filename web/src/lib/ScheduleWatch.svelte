<script lang="ts">
  import { api } from "./api";
  import type { ProjectScope } from "./scope.svelte";
  let { scope }: { scope: ProjectScope } = $props();
  $effect(() => {
    // Refresh after durable changes and periodically as scheduled times approach.
    void scope.tasks.map((t) => t.version).join();
    void Object.values(scope.latestRun)
      .map((r) => `${r.id}:${r.version}`)
      .join();
    let active = true;
    const refresh = () =>
      api.schedule(scope.projectId).then(
        (ds) => {
          if (active) scope.decisions = ds;
        },
        () => {},
      );
    void refresh();
    const timer = setInterval(refresh, 10000);
    return () => {
      active = false;
      clearInterval(timer);
    };
  });
</script>

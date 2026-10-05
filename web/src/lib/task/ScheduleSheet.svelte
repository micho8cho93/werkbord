<script lang="ts">
  import { untrack } from 'svelte';
  import { api } from '../api';
  import { editableOrchestration, emptyOrchestration } from '../scheduling';
  import ScheduleFields from '../ScheduleFields.svelte';
  import Sheet from '../Sheet.svelte';
  import type { ProjectScope } from '../scope.svelte';
  import type { Orchestration, Task } from '../types';

  // When the controller should start this task by itself, and what it waits for.

  let { task, scope, onclose }: { task: Task; scope: ProjectScope; onclose: () => void } = $props();

  let value = $state<Orchestration>(untrack(() => editableOrchestration(task.orchestration ?? emptyOrchestration())));
  let busy = $state(false);

  async function save(o: Orchestration) {
    busy = true;
    try {
      scope.upsertTask(await api.editTask(task, { orchestration: o }));
      onclose();
    } finally {
      busy = false;
    }
  }
</script>

<Sheet title="Schedule and dependencies" {onclose} width="40rem">
  <ScheduleFields bind:value tasks={scope.tasks} taskId={task.id} onsave={save} {busy} />
</Sheet>

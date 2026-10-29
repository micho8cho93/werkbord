<script lang="ts">
  import Calendar from './Calendar.svelte';
  import EditTaskSheet from '../lib/task/EditTaskSheet.svelte';
  import { movePlan } from '../lib/planningGestures';
  import { resolveFor } from '../lib/execution';
  import { app } from '../lib/state.svelte';
  import type { Task } from '../lib/types';
  import PlanningCalendar from '../lib/PlanningCalendar.svelte';
  import type { ProjectScope } from '../lib/scope.svelte';
  import type { Project } from '../lib/types';
  let { scope, project }: { scope: ProjectScope; project: Project } = $props();
  let editing = $state<Task | null>(null);
  const below = $derived(resolveFor({}, project.execution, app.globalExecution));
  const planned = $derived(scope.activeTasks.filter(t => t.plan?.start || t.plan?.end).map(t => ({ id: t.id, title: t.title, project: project.name, start: t.plan?.start || t.plan?.end || '', end: t.plan?.end || t.plan?.start || '', status: t.state, kind: 'planned' as const, milestone: t.plan?.milestone })));
</script>
<div class="scheduling">
  <div class="content">
    {#if project.kind === 'work'}<PlanningCalendar rows={planned} onopen={id => editing = scope.activeTasks.find(t => t.id === id) ?? null} onmove={drop => movePlan(scope, drop)} />
    {:else}<Calendar {scope} {project} />{/if}
  </div>
</div>
{#if editing}<EditTaskSheet side task={editing} {scope} inherited={below} onclose={() => editing = null} />{/if}
<style>
  .scheduling { height: 100%; min-height: 0; display: flex; flex-direction: column; }
  .content { flex: 1; min-height: 0; }
  @media (max-width: 899px) { .scheduling { height: auto; } }
</style>

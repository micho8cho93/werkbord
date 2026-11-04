<script lang="ts">
  import CalendarPage from '../lib/calendar/CalendarPage.svelte';
  import { PERSONAL, workSummary } from '../lib/work/summary.svelte';
  import { planningOperation } from '../lib/planningBridge';
  import { router } from '../lib/router.svelte';
  import { app } from '../lib/state.svelte';
  import { scopedOverview } from '../shell/project-context';

  // The Calendar page in the web app: the shared one, fed from the workspace summary, reading and changing a task directly and opening it on
  // its own page. It shows every project, or the one the project picker names (#/calendar?project=<id>).
  const project = $derived(new URLSearchParams(router.query).get('project') ?? '');
  const overview = $derived(scopedOverview(workSummary.overview, [PERSONAL], project ? { workspace: PERSONAL.id, project } : null));
  $effect(() => { void app.overview; void workSummary.refresh(); });
  $effect(() => { const timer = setInterval(() => void workSummary.refresh(), 15_000); return () => clearInterval(timer); });
</script>

<CalendarPage {overview} task={new URLSearchParams(router.query).get('task') ?? ''} call={(_workspace, href, operation, payload) => planningOperation(href, operation, payload)} refresh={() => workSummary.refresh(true)}
  open={(_workspace, href) => { location.hash = href; }}
  lede={project ? `Scheduled and planned work in ${app.project(project)?.name ?? 'this project'}` : undefined} />

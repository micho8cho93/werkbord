<script lang="ts">
  import Tour from '../lib/Tour.svelte';
  import AnalyticsPage from '../lib/analytics/AnalyticsPage.svelte';
  import { api } from '../lib/api';
  import { router } from '../lib/router.svelte';
  import { app } from '../lib/state.svelte';
  import { PERSONAL } from '../lib/work/summary.svelte';

  // The Analytics page in the web app: the shared one, reading the controller's report. It shows every project, or the one the project
  // picker names (#/analytics?project=<id>, or the project of an old #/p/<id>/analytics link).
  const project = $derived(new URLSearchParams(router.query).get('project') || router.projectId);
  const selected = $derived(project ? { workspace: PERSONAL.id, project } : null);
</script>

<Tour surface="analytics" />
<AnalyticsPage items={[PERSONAL]} {selected} label={project ? `${app.project(project)?.name ?? project} · Personal` : 'All Projects'}
  read={(_workspace, projectId, days) => api.analytics(projectId, days, '')} names={{ projects: Object.fromEntries(app.projects.map(p => [p.id, p.name])), runners: Object.fromEntries(app.runners.map(r => [r.id, r.name])) }} revision={app.analyticsRevision} />

<script lang="ts">
  import ProjectPicker, { type PickerGroup } from '../lib/ProjectPicker.svelte';
  import { model } from './model.svelte';
  import { contextKey, parseContext, projectGroups } from './project-context';
  import { sectionColour } from './nav';

  // The window's project context: its projects are the ones its workspaces report, grouped by where each lives.
  const selected = $derived(contextKey(model.selectedProject));
  const current = $derived(model.projects.find((p) => contextKey(p) === selected));
  const missing = $derived(!!selected && !current);
  const title = $derived(missing ? 'Project unavailable' : current ? current.name : 'All projects');
  const where = $derived(current ? current.owner : missing ? 'Reconnect to see it' : 'Personal and teams');

  const groups = (query: string): PickerGroup[] => projectGroups(model.view?.items ?? [], model.projects, query).map((g) => ({
    id: g.id, label: g.label, team: g.kind === 'team', colour: sectionColour(g.kind, g.id), ready: g.ready, detail: g.detail,
    projects: g.projects.map((p) => ({ key: contextKey(p), name: p.name })),
  }));
</script>

<ProjectPicker {groups} total={model.projects.length} {selected} {title} {where} {missing} tooltip={model.contextLabel}
  onchoose={(key) => { const p = parseContext(key); model.selectProject(p ? { workspace: p.workspace, project: p.project } : null); }}
  onnew={() => model.newProject()} />

<script lang="ts">
  import { untrack } from 'svelte';
  import Sidebar, { type SidebarItem } from '../Sidebar.svelte';
  import ProjectPicker, { type PickerGroup } from '../ProjectPicker.svelte';
  import { assistant } from '../assistant/sessions.svelte';
  import { originOf } from '../assistant/transition';
  import { globalHref, projectHref, router, sessionsHref, switchedTo } from '../router.svelte';
  import { sectionApplies } from '../location';
  import { app } from '../state.svelte';
  import { workSummary } from '../work/summary.svelte';
  import { railBadges } from '../../shell/rail';
  import { catchUpSession } from '../../shell/catchup-session.svelte';

  // The web app's sidebar is the shared one (lib/Sidebar.svelte), the same as the Mac window's, told what the web app lists and what
  // choosing each thing does. (Inside the Mac window's own page the window's sidebar does this job and this one is not shown.)

  /** Calendar, Agents and Analytics are pages of every project: the project they show is in the address (#/calendar?project=<id>), the picker's choice. */
  const calendar = $derived(router.view === 'calendar');
  const everyProject = $derived(calendar || router.view === 'sessions' || router.view === 'analytics');
  const current = $derived(everyProject ? (new URLSearchParams(router.query).get('project') ?? '') : router.projectId);
  const project = $derived(app.project(router.projectId || app.lastProjectId) ?? app.projects[0]);
  const view = $derived(router.view);

  const groups = (query: string): PickerGroup[] => {
    const words = query.toLowerCase().split(/\s+/).filter(Boolean);
    const projects = app.projects.filter((p) => words.every((w) => `${p.name} Personal`.toLowerCase().includes(w))).map((p) => ({ key: p.id, name: p.name }));
    return words.length && !projects.length ? [] : [{ id: 'personal', label: 'Personal', team: false, colour: 'var(--accent)', ready: true, detail: '', projects }];
  };
  const chosen = $derived(app.projects.find((p) => p.id === current));
  /** Switching keeps the section you are in, where the other project has one. */
  const choose = (id: string) => {
    if (everyProject) location.hash = (calendar ? globalHref('calendar') : router.view === 'analytics' ? globalHref('analytics') : sessionsHref()) + (id ? `?project=${encodeURIComponent(id)}` : '');
    else router.go(id ? switchedTo(router.location, id) : { view: 'control', projectId: '', taskId: '' });
  };

  // What waits in each place is worked out from the workspace summary by the same function the window uses (shell/rail.ts), so the two
  // sidebars say the same thing. The summary is read now and then, because the sidebar is on every page.
  $effect(() => { void app.overview; void workSummary.refresh(); });
  $effect(() => { const timer = setInterval(() => void workSummary.refresh(), 15_000); return () => clearInterval(timer); });
  // The last visit is kept from the first page on, so Work can say what changed since (the window does the same).
  $effect(() => untrack(() => catchUpSession.start()));
  $effect(() => { catchUpSession.ingest(workSummary.overview); });
  const badges = $derived(railBadges(workSummary.overview));
  const destinations = $derived<SidebarItem[]>([
    {
      key: 'work', label: 'Work', icon: 'control',
      href: project && current ? projectHref(project.id, 'work') : globalHref('control'),
      current: view === 'control' || view === 'task' || view === 'work',
      badge: badges.inbox,
    },
    { key: 'calendar', label: 'Calendar', icon: 'calendar', href: globalHref('calendar'), current: calendar },
    ...(project && sectionApplies('git', project.kind) ? [{ key: 'git', label: 'Git', icon: 'git' as const, href: projectHref(project.id, 'git'), current: view === 'git' }] : []),
    { key: 'agents', label: 'Agents', icon: 'activity', href: globalHref('sessions'), current: view === 'sessions', badge: badges.agents },
    { key: 'analytics', label: 'Analytics', icon: 'analytics', href: globalHref('analytics'), current: view === 'analytics' },
  ]);
  const bottom = $derived<SidebarItem[]>([
    { key: 'settings', label: 'Settings', icon: 'settings', href: globalHref('settings'), current: view === 'settings' },
    { key: 'help', label: 'Help', icon: 'help', href: globalHref('help'), current: view === 'help', ariaLabel: 'Help and keyboard shortcuts', title: 'Help and keyboard shortcuts' },
  ]);
</script>

{#snippet picker()}
  <ProjectPicker {groups} total={app.projects.length} selected={current} title={chosen ? chosen.name : 'All projects'} where={chosen ? 'Personal' : 'Everything in Werkbord'}
    tooltip={chosen ? chosen.repoPath : ''} onchoose={choose} onnew={() => router.go({ view: 'projects', projectId: '', taskId: '' })} />
{/snippet}
<Sidebar
  brand={{ label: 'werkbord. Go to Work', href: globalHref('control') }}
  {picker}
  salie={app.remote ? undefined : {
    label: assistant.waiting > 0 ? `Salie, ${assistant.waiting} ${assistant.waiting === 1 ? 'request is' : 'requests are'} waiting for you` : assistant.running ? 'Salie, working' : 'Salie',
    title: 'Salie', waiting: assistant.waiting, running: assistant.running, expanded: assistant.isOpen, disabled: false,
    press: (rect, button) => assistant.toggle(originOf(rect), button), testid: 'assistant-button',
  }}
  {destinations}
  {bottom} />

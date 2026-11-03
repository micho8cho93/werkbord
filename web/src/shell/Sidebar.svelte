<script lang="ts">
  import Sidebar, { type SidebarItem } from '../lib/Sidebar.svelte';
  import type { IconName } from '../lib/Icon.svelte';
  import { model, type Page } from './model.svelte';
  import ProjectPicker from './ProjectPicker.svelte';
  import { railBadges } from './rail';

  // The window's sidebar is the shared one (lib/Sidebar.svelte), told what this window lists and what choosing each thing does.
  const places: { label: string; page: Page; icon: IconName }[] = [
    { label: 'Work', page: 'inbox', icon: 'control' },
    { label: 'Calendar', page: 'calendar', icon: 'calendar' },
    { label: 'Git', page: 'git', icon: 'git' },
    { label: 'Agents', page: 'agents', icon: 'activity' },
    { label: 'Analytics', page: 'analytics', icon: 'analytics' },
  ];
  // What is waiting in each place, so a glance at the rail says whether something needs the person and what sort.
  const badges = $derived(railBadges(model.overview));
  // A workspace's page is Work, except where a Team page is on one of its other tabs (People, Reviews, Settings…), which the rail has no
  // name for: Work is not marked there, because it is not where the person is.
  const onWork = $derived.by(() => {
    if (model.page !== 'workspace') return false;
    const place = model.places[model.current] ?? '';
    if (model.onGit) return false;
    if (model.current === 'personal' || !place.startsWith('?')) return true;
    const tab = new URLSearchParams(place.slice(1)).get('tab');
    return !tab || tab === 'work';
  });
  const helpUnavailable = $derived(model.view?.items.find((i) => i.id === 'personal')?.state === 'unavailable');
  let salieButton: HTMLButtonElement | undefined = $state();
  const salieLabel = $derived(model.assistant.waiting ? `Salie · ${model.assistant.waiting} ${model.assistant.waiting === 1 ? 'request is' : 'requests are'} waiting for you` : 'Salie');
  $effect(() => {
    model.assistantSidebarButton = salieButton;
    return () => { model.assistantSidebarButton = undefined; };
  });

  const destinations = $derived<SidebarItem[]>(places.map((d) => ({
    key: d.page, label: d.label, icon: d.icon,
    current: model.page === d.page || (d.page === 'inbox' && onWork) || (d.page === 'git' && model.onGit),
    go: () => model.go(d.page),
    badge: d.page === 'inbox' ? badges.inbox : d.page === 'agents' ? badges.agents : undefined,
  })));
  const bottom = $derived<SidebarItem[]>([
    { key: 'settings', label: 'Settings', icon: 'settings', current: model.page === 'settings', go: () => model.openSettings() },
    { key: 'help', label: 'Help', icon: 'help', current: model.page === 'help', disabled: helpUnavailable, go: () => model.openHelp(), ariaLabel: 'Help and keyboard shortcuts', title: 'Help and keyboard shortcuts', testid: 'help-button' },
  ]);
</script>

{#snippet picker()}<ProjectPicker />{/snippet}
<Sidebar
  bind:salieButton
  brand={{ label: 'werkbord. Go to Work', go: () => model.go('inbox') }}
  {picker}
  salie={{
    label: salieLabel,
    title: model.remotePersonal ? 'Salie requires the local controller' : salieLabel,
    waiting: model.assistant.waiting, running: !!model.assistant.running, expanded: model.assistantShown,
    disabled: model.remotePersonal || model.view?.items.find((i) => i.id === 'personal')?.state === 'unavailable',
    press: (rect) => void model.toggleAssistant(rect),
  }}
  {destinations}
  {bottom} />

<script lang="ts">
  import ClientsPanel from '../lib/ClientsPanel.svelte';
  import ProjectHistory from '../lib/ProjectHistory.svelte';
  import MemorySharing from '../lib/MemorySharing.svelte';
  import ProjectMemory from '../lib/ProjectMemory.svelte';
  import Tour from '../lib/Tour.svelte';
  import { onMount, untrack } from 'svelte';
  import { api } from '../lib/api';
  import { clock, formatTime } from '../lib/clock.svelte';
  import { compact, resolveFor, summaryLine } from '../lib/execution';
  import ExecutionFields from '../lib/ExecutionFields.svelte';
  import ConnectionsPanel from '../lib/ConnectionsPanel.svelte';
  import MCPClientsPanel from '../lib/MCPClientsPanel.svelte';
  import { disableNotifications, enableNotifications, notificationPermission, notificationsEnabled } from '../lib/notifications';
  import { globalHref, router } from '../lib/router.svelte';
  import RoutingRules from '../lib/RoutingRules.svelte';
  import RunnersPanel from '../lib/RunnersPanel.svelte';
  import DeviceSetup from '../lib/DeviceSetup.svelte';
  import AgentSettings from '../lib/AgentSettings.svelte';
  import GitHubCard from '../lib/setup/GitHubCard.svelte';
  import { embedded } from '../lib/embed';
  import { isPhone } from '../lib/phones/capability';
  import PhonesPanel from '../lib/phones/PhonesPanel.svelte';
  import { desktop } from '../lib/desktop.svelte';
  import { app } from '../lib/state.svelte';
  import { look, STYLES, theme } from '../lib/theme.svelte';
  import type { ExecutionConfig, Health, Runner } from '../lib/types';

  // Everything that is set once and changed rarely, one section at a time: each has its own
  // address (#/settings?runners), so the rail and other pages link straight to it.

  // In the desktop window this page is shown by the window's own Settings, which lists these sections itself, owns Theme and Alerts,
  // and asks this page for one section at a time: there is no list or heading of its own here. Opened in a browser or on a phone
  // it is the whole of Settings, as it always was.
  const framed = embedded();
  let memoryProject = $state('');
  const localMemoryProject = $derived(app.projects.find(p => p.id === memoryProject)?.id ?? app.projects[0]?.id ?? '');
  let mcpDirection = $state<'service' | 'client'>('service');

  const GROUPS = [
    { label: 'Projects & workspaces', sections: [{ id: 'history', label: 'Project history' }] },
    { label: 'Work', sections: [{ id: 'defaults', label: 'Task defaults' }, { id: 'routing', label: 'Routing rules' }, { id: 'time', label: 'Time format' }] },
    { label: 'Machines', sections: [{ id: 'runners', label: 'Runners' }, { id: 'agents', label: 'Agents' }, { id: 'computer', label: 'This computer' }] },
    { label: 'Connections', sections: [{ id: 'phones', label: 'Phones' }, { id: 'github', label: 'GitHub' }, { id: 'mcp', label: 'MCP services' }, { id: 'memory', label: 'Personal memory' }] },
    { label: 'Preferences', sections: [{ id: 'alerts', label: 'Alerts and display' }] },
  ] as const;
  // A phone's own settings are its sign-in and its display; the rest is changed at the computer.
  const PHONE_SECTIONS = ['phones', 'alerts'];
  // The window has the theme and alerts of its own, and the time format is the one thing of "display" left here; in a browser it is all one section.
  const own = (id: string) => (framed ? id !== 'alerts' : id !== 'time');
  const shownGroups = GROUPS.map((g) => ({ ...g, sections: g.sections.filter((s) => own(s.id) && (!isPhone() || PHONE_SECTIONS.includes(s.id))) })).filter((g) => g.sections.length);
  const SECTIONS = GROUPS.flatMap((g) => g.sections.filter((s) => own(s.id)));
  type SectionId = (typeof SECTIONS)[number]['id'];
  const section = $derived<SectionId>(SECTIONS.find((s) => s.id === router.query && (!isPhone() || PHONE_SECTIONS.includes(s.id)))?.id ?? (isPhone() ? 'phones' : 'defaults'));
  const href = (id: SectionId) => (id === 'defaults' ? globalHref('settings') : `${globalHref('settings')}?${id}`);

  let checking = $state(false);
  async function checkUpdateNow() {
    checking = true;
    try {
      await app.checkUpdate(true);
      if (app.update?.available) app.dismissUpdate(''); // asked for, so it is offered again
    } finally {
      checking = false;
    }
  }

  // ---- the global defaults: the bottom of the hierarchy, which a project and a task override ----
  let draft = $state<ExecutionConfig>({});
  let draftFor = '';
  let saving = $state(false);
  let saved = $state(false);
  let error = $state('');

  // Follows the stored defaults until the user starts editing; another device's change shows up here.
  $effect(() => {
    const key = JSON.stringify(app.globalExecution);
    if (key !== draftFor && untrack(() => !draftFor || JSON.stringify(draft) === draftFor)) {
      draftFor = key;
      draft = { ...app.globalExecution };
    }
  });

  const dirty = $derived(JSON.stringify(compact(draft)) !== JSON.stringify(compact(app.globalExecution)));
  const effective = $derived(resolveFor({}, {}, compact(draft)));

  async function save() {
    saving = true;
    error = '';
    saved = false;
    try {
      await app.saveGlobalExecution(compact(draft));
      draftFor = JSON.stringify(compact(draft));
      saved = true;
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      saving = false;
    }
  }

  // ---- this computer ----
  let runner = $state<Runner | null>(null);
  let health = $state<Health | null>(null);
  onMount(() => {
    api.listRunners().then((rs) => (runner = rs.find((r) => r.kind === 'local') ?? rs[0] ?? null), () => {});
    api.health().then((h) => (health = h), () => {});
  });

  async function runSetupAgain() {
    app.onboarding = await api.resetOnboarding().catch(() => app.onboarding);
    router.go({ view: 'onboarding', projectId: '', taskId: '' });
  }

  // ---- alerts ----
  let permission = $state(notificationPermission());
  function syncAlerts() { permission = notificationPermission(); optedIn = notificationsEnabled() && permission === 'granted'; }
  let optedIn = $state(notificationsEnabled());
  async function toggleNotifications() {
    if (optedIn) {
      disableNotifications();
      optedIn = false;
      return;
    }
    permission = notificationPermission();
    if (permission === 'denied') { app.notify('Allow alerts in your browser or macOS notification settings, then return here.'); return; }
    permission = await enableNotifications();
    optedIn = permission === 'granted';
  }
</script>
<svelte:window onfocus={syncAlerts} />


<div class="settings" class:framed>
  <Tour surface="settings" enabled={!isPhone() && !framed} />
  <label class="mobile-section">
    <span>Settings section</span>
    <select aria-label="Settings section" value={section} onchange={(event) => { location.hash = href(event.currentTarget.value as SectionId); }}>
      {#each SECTIONS as s (s.id)}<option value={s.id}>{s.label}</option>{/each}
    </select>
  </label>
  <nav class="sections" aria-label="Settings sections">
    {#each shownGroups as group (group.label)}
      <div class="section-group" role="group" aria-label={group.label}>
        <h2>{group.label}</h2>
        <div class="section-links">
          {#each group.sections as s (s.id)}
            <a href={href(s.id)} aria-current={section === s.id ? 'page' : undefined}>{s.label}</a>
          {/each}
        </div>
      </div>
    {/each}
  </nav>

  <div class="content">
    {#if section === 'defaults'}
      <section class="pn">
        <div class="head">
          <h3>Defaults for every task</h3>
          <p class="muted">
            What a task uses unless its project or the task says otherwise. Unconfigured tasks use Lowest cost. Existing explicit agent and model choices remain manual.
          </p>
        </div>
        <ExecutionFields bind:value={draft} idPrefix="global" columns />
        {#if app.agents.length > 0}
          <p class="note">New tasks get: <strong>{summaryLine(effective, app.agents, app.agentOptions) || 'Lowest cost, selected when the run starts'}</strong></p>
        {/if}
        {#if error}<p class="error" role="alert">{error}</p>{/if}
        <div class="row">
          <button class="btn primary" disabled={saving || !dirty} onclick={save}>{saving ? 'Saving…' : 'Save defaults'}</button>
          {#if dirty}<button class="btn quiet" onclick={() => (draft = { ...app.globalExecution })}>Discard</button>{/if}
          {#if saved && !dirty}<span class="ok" role="status">Saved</span>{/if}
        </div>
      </section>
    {:else if section === 'history'}
      <ProjectHistory />
    {:else if section === 'runners'}
      <RunnersPanel />
 {#if !isPhone() && !app.remote}<ClientsPanel />{/if}
      {#if !isPhone()}<DeviceSetup />{/if}
    {:else if section === 'routing'}
      <RoutingRules />
    {:else if section === 'phones'}
      <PhonesPanel />
    {:else if section === 'github'}
      <GitHubCard />
    {:else if section === 'memory'}
      <section class="pn">
        <h2 class="band">Local project memory</h2>
        <p>Inspect, correct or delete remembered information on this computer. Team memory is managed separately in that Team’s Settings, subject to its project permissions.</p>
        <label class="memory-project">Project <select class="input" aria-label="Memory project" value={localMemoryProject} onchange={event => memoryProject = event.currentTarget.value}>
          {#each app.projects as project (project.id)}<option value={project.id}>{project.name}</option>{/each}
        </select></label>
        {#if !localMemoryProject}<p class="muted">Add a project to start remembering decisions and preferences.</p>{/if}
      </section>
      {#if localMemoryProject}{#key localMemoryProject}<ProjectMemory projectId={localMemoryProject} />{/key}{/if}
      <MemorySharing />
    {:else if section === 'mcp'}
      <div class="mcp-direction" role="group" aria-label="MCP connection direction">
        <button class="btn" aria-pressed={mcpDirection === 'service'} onclick={() => mcpDirection = 'service'}>Use a service in Werkbord</button>
        <button class="btn" aria-pressed={mcpDirection === 'client'} onclick={() => mcpDirection = 'client'}>Let a client use Werkbord</button>
      </div>
      <div hidden={mcpDirection !== 'service'}><ConnectionsPanel /></div>
      <div hidden={mcpDirection !== 'client'}><MCPClientsPanel /></div>
    {:else if section === 'agents'}
      <AgentSettings />
    {:else if section === 'time'}
      <section class="pn">
        <div class="head">
          <h3>Time format</h3>
          <p class="muted">How times are written everywhere: {formatTime(app.now)} now. “Device” uses your system format. Kept on this device.</p>
        </div>
        <div class="seg" role="group" aria-label="Clock">
          <button aria-pressed={clock.choice === ''} onclick={() => clock.set('')}>Device</button>
          <button aria-pressed={clock.choice === '12'} onclick={() => clock.set('12')}>12-hour</button>
          <button aria-pressed={clock.choice === '24'} onclick={() => clock.set('24')}>24-hour</button>
        </div>
      </section>
    {:else if section === 'alerts'}
      <section class="pn">
        <div class="head">
          <h3>Browser alerts</h3>
          <p class="muted">
            Alerts for questions, blocked or failed runs, finished work and repository risk. They arrive only while this page stays open in a
            browser: Werkbord has no push service. Kept on this device.
          </p>
        </div>
        <div class="row">
          {#if permission === 'unsupported'}
            <span class="muted">This browser does not support alerts.</span>
          {:else if permission === 'denied' && !optedIn}
            <span class="muted">Your browser is blocking alerts from this site. Allow them in the browser's site settings, then reload this page.</span>
          {:else}
            <button class="btn" class:primary={!optedIn} onclick={toggleNotifications}>{optedIn ? 'Turn alerts off' : 'Turn alerts on'}</button>
            <span class="note"><span class="dot" data-tone={optedIn ? 'ok' : 'neutral'}></span> {optedIn ? 'On' : 'Off'}</span>
          {/if}
        </div>
      </section>
      <section class="pn">
        <div class="head">
          <h3>Theme</h3>
          <p class="muted">“Device” follows your system setting.</p>
        </div>
        <div class="seg" role="group" aria-label="Theme">
          <button aria-pressed={theme.choice === ''} onclick={() => theme.set('')}>Device</button>
          <button aria-pressed={theme.choice === 'light'} onclick={() => theme.set('light')}>Light</button>
          <button aria-pressed={theme.choice === 'dark'} onclick={() => theme.set('dark')}>Dark</button>
        </div>
      </section>
      <section class="pn">
        <div class="head">
          <h3>Style</h3>
          <p class="muted">{STYLES.find(s => s.id === look.choice)?.about}</p>
        </div>
        <div class="seg" role="group" aria-label="Style">
          {#each STYLES as s (s.id)}
            <button aria-pressed={look.choice === s.id} onclick={() => look.set(s.id)}>{s.label}</button>
          {/each}
        </div>
      </section>
      <section class="pn">
        <div class="head">
          <h3>Clock</h3>
          <p class="muted">How times are written everywhere: {formatTime(app.now)} now. “Device” uses your system format. Kept on this device.</p>
        </div>
        <div class="seg" role="group" aria-label="Clock">
          <button aria-pressed={clock.choice === ''} onclick={() => clock.set('')}>Device</button>
          <button aria-pressed={clock.choice === '12'} onclick={() => clock.set('12')}>12-hour</button>
          <button aria-pressed={clock.choice === '24'} onclick={() => clock.set('24')}>24-hour</button>
        </div>
      </section>
    {:else if section === 'computer'}
      <section class="pn">
        <div class="head">
          <h3>This computer</h3>
          {#if runner}
            <p class="muted">{runner.name} · {runner.os}/{runner.arch}. It is the runner on this computer, and it is {runner.online ? 'online' : 'offline'}: agents work here, in Git worktrees, as you.</p>
          {/if}
        </div>
        {#if health}
          <dl class="kv">
            <dt>Werkbord</dt><dd class="mono">{health.version}{desktop.available ? ' · desktop app' : ''}</dd>
            <dt>Database</dt><dd class="mono">{health.database}</dd>
            <dt>Updates</dt>
            <dd>
              {#if !app.update}
                <span class="muted">Not checked</span>
              {:else if app.update.disabled}
                <span class="muted">Update checks are turned off in config.json</span>
              {:else if !app.update.release}
                <span class="muted">Built from source: update it from its source tree</span>
              {:else if app.update.available}
                {app.update.latest?.replace(/^v/, '')} is available
              {:else if app.update.error}
                <span class="muted">Could not look: {app.update.error}</span>
              {:else}
                Up to date
              {/if}
              {#if app.update && app.update.release && !app.update.disabled}
                <button class="btn" type="button" disabled={checking} onclick={checkUpdateNow}>{checking ? 'Checking…' : 'Check now'}</button>
              {/if}
            </dd>
          </dl>
        {/if}
        <p class="note">Run <code>werkbord doctor</code> in a terminal to check Werkbord, its database, Git, agents, network and GitHub.</p>
        <div class="row"><button class="btn" onclick={runSetupAgain}>Run setup again</button></div>
      </section>
    {/if}
  </div>
</div>

<style>
  .memory-project { display: grid; gap: 6px; margin-top: 12px; max-width: 360px; }
  .memory-project select { min-height: 44px; width: 100%; }
  .mcp-direction { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 20px; }
  .mcp-direction button { white-space: normal; min-height: 44px; }
  .mcp-direction [aria-pressed="true"] { color: var(--accent); background: var(--surface-2); }
  .section-group + .section-group { margin-top: 24px; }
  /* A group heading names what is under it and is not pressable; its sections are indented beneath it on a rule. */
  .section-group h2 { margin: 0 12px 6px; padding-bottom: 6px; border-bottom: 1px solid var(--border); font: 600 13px var(--font); letter-spacing: 0; text-transform: none; color: var(--text); }
  :global(:root[data-style='classic']) .section-group h2 { border-bottom: 2px solid color-mix(in srgb, var(--text) 55%, var(--border)); font: 700 12px var(--mono); letter-spacing: 0.14em; text-transform: uppercase; }
  .section-links { display: flex; flex-direction: column; gap: 2px; }
  .settings {
    height: 100%;
    min-height: 0;
    display: grid;
    grid-template-columns: 220px minmax(0, 1fr);
  }

  .sections {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 20px 12px;
    border-right: 1px solid var(--border);
    overflow-y: auto;
  }

  .mobile-section { display: none; }

  /* In the window: one section, no list of its own (the window's Settings has it). */
  .settings.framed { display: block; height: auto; }
  .settings.framed .sections, .settings.framed .mobile-section { display: none; }

  .sections a {
    display: flex;
    align-items: center;
    min-height: 34px;
    margin: 0 12px 0 20px;
    padding: 0 10px;
    border-left: 2px solid var(--border);
    border-radius: 0 var(--radius-sm) var(--radius-sm) 0;
    color: var(--text-2);
    text-decoration: none;
    font-weight: 500;
    font-size: 13px;
  }

  .sections a:hover {
    color: var(--text);
  }

  .sections a[aria-current='page'] {
    border-left-color: var(--accent);
    background: var(--surface-2);
    color: var(--text);
    font-weight: 600;
  }

  .content {
    min-height: 0;
    overflow-y: auto;
    padding: 20px 24px 32px;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .content > :global(*) {
    max-width: 1200px;
  }

  .head {
    display: grid;
    gap: 4px;
  }

  h3 {
    font-size: 16px;
  }

  .row {
    display: flex;
    gap: 10px;
    align-items: center;
    flex-wrap: wrap;
  }

  .ok {
    color: var(--ok-text);
    font-size: 13px;
  }

  .note {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 13px;
    color: var(--text-2);
  }

  .note strong {
    color: var(--text);
    font-weight: 500;
  }

  .kv {
    display: grid;
    grid-template-columns: max-content minmax(0, 1fr);
    gap: 6px 16px;
    margin: 0;
    font-size: 13px;
  }

  .kv dt {
    color: var(--text-2);
  }

  .kv dd {
    margin: 0;
  }

  @media (max-width: 899px) {
    .settings {
      display: block;
      height: auto;
    }

    .sections {
      display: none;
    }

    .mobile-section {
      display: grid;
      grid-template-columns: auto minmax(0, 1fr);
      align-items: center;
      gap: 12px;
      padding: 8px 16px;
      color: var(--text-2);
      font-size: 13px;
    }

    .mobile-section select {
      width: 100%;
      min-width: 0;
      min-height: 44px;
      padding: 0 12px;
      border: 1px solid var(--border);
      border-radius: var(--radius-sm);
      background: var(--surface);
      color: var(--text);
      font: inherit;
      font-weight: 500;
    }

    .mobile-section select:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }

    .section-group { flex: none; }

    .content {
      overflow: visible;
      padding: 8px 16px 24px;
    }
  }
</style>

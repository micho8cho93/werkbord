<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { api } from '../lib/api';
  import { compact, resolveFor, summaryLine } from '../lib/execution';
  import ExecutionFields from '../lib/ExecutionFields.svelte';
  import LabelManager from '../lib/LabelManager.svelte';
  import { disableNotifications, enableNotifications, notificationPermission, notificationsEnabled } from '../lib/notifications';
  import { globalHref, router } from '../lib/router.svelte';
  import RoutingRules from '../lib/RoutingRules.svelte';
  import RunnersPanel from '../lib/RunnersPanel.svelte';
  import AgentsCard from '../lib/setup/AgentsCard.svelte';
  import GitHubCard from '../lib/setup/GitHubCard.svelte';
  import NetworkCard from '../lib/setup/NetworkCard.svelte';
  import { desktop } from '../lib/desktop.svelte';
  import { app } from '../lib/state.svelte';
  import { theme } from '../lib/theme.svelte';
  import type { ExecutionConfig, Health, Runner } from '../lib/types';

  // Everything that is set once and changed rarely, one section at a time: each has its own
  // address (#/settings?runners), so the rail and other pages link straight to it.

  const SECTIONS = [
    { id: 'defaults', label: 'Task defaults' },
    { id: 'labels', label: 'Labels' },
    { id: 'runners', label: 'Runners' },
    { id: 'routing', label: 'Routing rules' },
    { id: 'phone', label: 'Phone access' },
    { id: 'github', label: 'GitHub' },
    { id: 'agents', label: 'Agents' },
    { id: 'alerts', label: 'Alerts and theme' },
    { id: 'computer', label: 'This computer' },
  ] as const;
  type SectionId = (typeof SECTIONS)[number]['id'];
  const section = $derived<SectionId>(SECTIONS.find((s) => s.id === router.query)?.id ?? 'defaults');
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
  let optedIn = $state(notificationsEnabled());
  async function toggleNotifications() {
    if (optedIn) {
      disableNotifications();
      optedIn = false;
      return;
    }
    permission = await enableNotifications();
    optedIn = permission === 'granted';
  }
</script>

<div class="settings">
  <nav class="sections" aria-label="Settings sections">
    {#each SECTIONS as s (s.id)}
      <a href={href(s.id)} aria-current={section === s.id ? 'page' : undefined}>{s.label}</a>
    {/each}
  </nav>

  <div class="content">
    {#if section === 'defaults'}
      <section class="pn">
        <div class="head">
          <h3>Defaults for every task</h3>
          <p class="muted">
            What a task uses unless its project or the task itself says otherwise. You do not have to set any of it: left alone, the agent picks its
            own model and reasoning.
          </p>
        </div>
        <ExecutionFields bind:value={draft} idPrefix="global" columns />
        {#if app.agents.length > 0}
          <p class="note">New tasks get: <strong>{summaryLine(effective, app.agents, app.agentOptions) || 'the first agent that works, with its own defaults'}</strong></p>
        {/if}
        {#if error}<p class="error" role="alert">{error}</p>{/if}
        <div class="row">
          <button class="btn primary" disabled={saving || !dirty} onclick={save}>{saving ? 'Saving…' : 'Save defaults'}</button>
          {#if dirty}<button class="btn quiet" onclick={() => (draft = { ...app.globalExecution })}>Discard</button>{/if}
          {#if saved && !dirty}<span class="ok" role="status">Saved</span>{/if}
        </div>
      </section>
    {:else if section === 'labels'}
      <section class="pn">
        <div class="head">
          <h3>Labels</h3>
          <p class="muted">
            Names and colours of your own choosing, put on any task in any project and used to filter the board and the timeline. A label never changes how a
            task runs; who does the work (a person, an agent or both) is a separate setting on the task.
          </p>
        </div>
        <LabelManager />
      </section>
    {:else if section === 'runners'}
      <RunnersPanel />
    {:else if section === 'routing'}
      <RoutingRules />
    {:else if section === 'phone'}
      <NetworkCard />
    {:else if section === 'github'}
      <GitHubCard />
    {:else if section === 'agents'}
      <AgentsCard />
    {:else if section === 'alerts'}
      <section class="pn">
        <div class="head">
          <h3>Browser alerts</h3>
          <p class="muted">
            Alerts for questions, blocked or failed runs, finished work and repository risk. They come only while this browser can stay connected: there is
            no push relay. The choice is kept on this device.
          </p>
        </div>
        <div class="row">
          {#if permission === 'unsupported'}
            <span class="muted">This browser does not support alerts.</span>
          {:else}
            <button class="btn" class:primary={!optedIn} onclick={toggleNotifications}>{optedIn ? 'Turn alerts off' : permission === 'denied' ? 'Blocked by the browser' : 'Turn alerts on'}</button>
            <span class="note"><span class="dot" data-tone={optedIn ? 'ok' : 'neutral'}></span> {optedIn ? 'On' : 'Off'}</span>
          {/if}
        </div>
      </section>
      <section class="pn">
        <div class="head">
          <h3>Theme</h3>
          <p class="muted">Follows your device unless you choose. Kept on this device.</p>
        </div>
        <div class="seg" role="group" aria-label="Theme">
          <button aria-pressed={theme.choice === ''} onclick={() => theme.set('')}>Device</button>
          <button aria-pressed={theme.choice === 'light'} onclick={() => theme.set('light')}>Light</button>
          <button aria-pressed={theme.choice === 'dark'} onclick={() => theme.set('dark')}>Dark</button>
        </div>
      </section>
    {:else if section === 'computer'}
      <section class="pn">
        <div class="head">
          <h3>This computer</h3>
          {#if runner}
            <p class="muted">{runner.name} · {runner.os}/{runner.arch}. It is your first runner and {runner.online ? 'online' : 'offline'}: agents run here, in Git worktrees, as you.</p>
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
                <span class="muted">Checking is turned off (<code>noUpdateCheck</code>)</span>
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
        <p class="note">Run <code>werkbord doctor</code> in a terminal to check the controller, database, Git, agents, network and GitHub.</p>
        <div class="row"><button class="btn" onclick={runSetupAgain}>Run setup again</button></div>
      </section>
    {/if}
  </div>
</div>

<style>
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

  .sections a {
    display: flex;
    align-items: center;
    min-height: 34px;
    padding: 0 12px;
    border-radius: var(--radius-sm);
    color: var(--text-2);
    text-decoration: none;
    font-weight: 500;
    font-size: 13px;
  }

  .sections a:hover {
    color: var(--text);
  }

  .sections a[aria-current='page'] {
    background: var(--surface-2);
    color: var(--text);
    box-shadow: var(--press-sh);
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
    font-size: 15px;
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
      flex-direction: row;
      overflow-x: auto;
      scrollbar-width: none;
      padding: 0 16px 8px;
      border-right: 0;
    }

    .sections a {
      flex: none;
      min-height: 40px;
      border-radius: 999px;
      border: 1px solid var(--border);
      background: var(--surface);
      font-size: 14px;
    }

    .sections a[aria-current='page'] {
      background: var(--text);
      color: var(--bg);
      border-color: var(--text);
      box-shadow: none;
    }

    .content {
      overflow: visible;
      padding: 8px 16px 24px;
    }
  }
</style>

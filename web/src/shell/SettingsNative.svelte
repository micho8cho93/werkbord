<script lang="ts">
  import { onMount } from 'svelte';
  import { look, STYLES, theme } from '../lib/theme.svelte';
  import { alerts } from './alerts.svelte';
  import { ask } from './native';
  import { model } from './model.svelte';

  // The settings that are about this window and this person, not about any one workspace: how it looks and what it tells you
  // while you are in another app. They are the same wherever Settings is opened from.
  let { section }: { section: 'appearance' | 'alerts' | 'shortcuts' | 'about' } = $props();

  onMount(() => void alerts.refresh());

  let tested = $state<'' | 'shown' | 'failed'>('');
  async function test() {
    tested = (await alerts.test()) ? 'shown' : 'failed';
  }
  // Updates & about: what this app is, whether there is a newer one, and where to look when something is wrong. Each button is the
  // app's own action (its dialogs, its files); this page only asks.
  let info = $state<{ version: string; platform: string; updater?: boolean } | null>(null);
  let checking = $state(false);
  let outcome = $state('');
  $effect(() => {
    if (section !== 'about' || info) return;
    ask((a) => a.Info()).then((i) => { info = i; }).catch(() => { /* The version is simply not shown. */ });
  });
  async function check() {
    checking = true;
    outcome = '';
    try {
      const r = await ask((a) => a.RequestUpdate());
      outcome = r.declined ? 'No update was installed.' : r.message || (r.ok ? 'Done.' : 'Nothing happened.');
    } catch (e) {
      outcome = (e as Error).message;
    } finally {
      checking = false;
    }
  }
  const platformName = (p: string) => ({ darwin: 'macOS', windows: 'Windows', linux: 'Linux' })[p] ?? p;
  const releases = 'https://github.com/micho8cho93/werkbord/releases';
  const project = 'https://github.com/micho8cho93/werkbord';
  const mac = $derived(info ? info.platform === 'darwin' : true);
  const mod = $derived(mac ? '⌘' : 'Ctrl+');
  const shortcuts = $derived<{ keys: string; what: string }[]>([
    { keys: `${mod}K`, what: 'Go to a project, a page or a place' },
    { keys: `${mod}B`, what: 'Fold or show the sidebar' },
    { keys: `${mod}1`, what: 'Work' },
    { keys: `${mod}2`, what: 'Your Personal workspace' },
    { keys: `${mod}3`, what: 'Calendar' },
    { keys: `${mod}4`, what: 'Reviews' },
    { keys: `${mod}5`, what: 'Analytics' },
    { keys: `${mod}0`, what: 'Devices & Hosting' },
    { keys: `${mod}R`, what: 'Reload the window' },
    { keys: `${mac ? '⇧⌘' : 'Ctrl+Shift+'}O`, what: 'Open Werkbord in your browser' },
    { keys: 'Esc', what: 'Leave Settings or Help for where you were' },
  ]);
  const unsupported = $derived(alerts.status === 'unsupported');
  const denied = $derived(alerts.status === 'denied');
</script>

<div class="wrap">
  {#if section === 'appearance'}
    <section class="pn" aria-labelledby="theme-h">
      <div class="head">
        <h3 id="theme-h">Theme</h3>
        <p class="muted">Light or dark for the whole window and every workspace in it. It follows your Mac unless you choose.</p>
      </div>
      <div class="seg" role="group" aria-label="Theme">
        <button type="button" aria-pressed={theme.choice === ''} onclick={() => theme.set('')}>Device</button>
        <button type="button" aria-pressed={theme.choice === 'light'} onclick={() => theme.set('light')}>Light</button>
        <button type="button" aria-pressed={theme.choice === 'dark'} onclick={() => theme.set('dark')}>Dark</button>
      </div>
    </section>
    <section class="pn" aria-labelledby="style-h">
      <div class="head">
        <h3 id="style-h">Style</h3>
        <p class="muted">How the window and every workspace in it are drawn. {STYLES.find(s => s.id === look.choice)?.about}</p>
      </div>
      <div class="seg" role="group" aria-label="Style">
        {#each STYLES as s (s.id)}
          <button type="button" aria-pressed={look.choice === s.id} onclick={() => look.set(s.id)}>{s.label}</button>
        {/each}
      </div>
    </section>

  {:else if section === 'shortcuts'}
    <section class="pn" aria-labelledby="keys-h">
      <div class="head">
        <h3 id="keys-h">Keyboard shortcuts</h3>
        <p class="muted">For the window as a whole. Inside a task or a plan, a page lists its own where they apply.</p>
      </div>
      <dl class="keys" data-testid="shortcut-list">
        {#each shortcuts as k (k.keys)}<div><dt><kbd>{k.keys}</kbd></dt><dd>{k.what}</dd></div>{/each}
      </dl>
    </section>

  {:else if section === 'about'}
    <section class="pn" aria-labelledby="about-h">
      <div class="head">
        <h3 id="about-h">Werkbord</h3>
        <p class="muted">{#if info}Version <span class="mono">{info.version.replace(/^v/, '')}</span> · {platformName(info.platform)}{:else}One app for your own work and your teams’.{/if}</p>
      </div>
      <div class="row">
        <button class="btn primary" type="button" onclick={check} disabled={checking} data-testid="check-updates">{checking ? 'Checking…' : 'Check for updates'}</button>
        <button class="btn" type="button" onclick={() => ask((a) => a.OpenExternal(releases))}>Release notes</button>
      </div>
      <p class="note" role="status" aria-live="polite" data-testid="update-outcome">{outcome}</p>
      <p class="muted small">Nothing is installed without asking you first. After an update, Team’s background service may need to catch up once; Werkbord asks for that as it opens, and your teams come back as they were.</p>
    </section>
    <section class="pn" aria-labelledby="help-h">
      <div class="head">
        <h3 id="help-h">Help and support</h3>
        <p class="muted">If something is not working, these show what Werkbord knows, in files only you can read.</p>
      </div>
      <div class="row">
        <button class="btn" type="button" onclick={() => model.openHelp()}>Guides and shortcuts</button>
        <button class="btn" type="button" onclick={() => ask((a) => a.ShowDiagnostics())}>Show diagnostics</button>
        <button class="btn" type="button" onclick={() => ask((a) => a.OpenLogs())}>Show the log</button>
        <button class="btn" type="button" onclick={() => ask((a) => a.OpenExternal(project))}>Werkbord on GitHub</button>
      </div>
    </section>
    <section class="pn" aria-labelledby="data-h">
      <div class="head">
        <h3 id="data-h">Your data</h3>
        <p class="muted">Your Personal projects, tasks and agent history live on this Mac, and a team’s live on the hosts your team runs. Information leaves this Mac only for the places you connect: your team’s hosts, GitHub, your phone, and the services you add. Looking for updates reads GitHub’s public release list; it can be turned off with <code>noUpdateCheck</code> in <code>config.json</code>.</p>
      </div>
    </section>

  {:else}
    <section class="pn" aria-labelledby="alerts-h">
      <div class="head">
        <h3 id="alerts-h">Alerts</h3>
        <p class="muted">
          When an agent asks you a question, gets blocked or fails, or finishes work for you to review while you are in another app, Werkbord
          shows a notification and bounces its icon in the Dock. Nothing is sent anywhere: they come from this computer.
        </p>
      </div>
      {#if unsupported}
        <p class="muted" role="status">This window cannot show system alerts here. Werkbord still marks what needs your attention in the sidebar and in Workspaces.</p>
      {:else}
        <div class="row">
          <button class="btn" class:primary={!alerts.enabled} type="button" aria-pressed={alerts.enabled} onclick={() => alerts.set(!alerts.enabled)}>
            {alerts.enabled ? 'Turn alerts off' : 'Turn alerts on'}
          </button>
          <span class="note"><span class="dot" data-tone={alerts.enabled && !denied ? 'ok' : 'neutral'}></span> {alerts.enabled ? 'On' : 'Off'}</span>
          <button class="btn open" type="button" onclick={test}>Show a test alert</button>
        </div>
        {#if denied}
          <p class="note" role="status" data-testid="alerts-denied">
            macOS is not letting Werkbord show alerts. Turn them on for Werkbord in System Settings, under Notifications.
          </p>
          <div class="row"><button class="btn open" type="button" onclick={() => alerts.openSystemSettings()}>Open Notification settings</button></div>
        {:else if tested === 'shown'}
          <p class="note" role="status">The alert was sent. If you did not see it, check Focus is not on.</p>
        {:else if tested === 'failed'}
          <p class="note" role="status">The alert could not be shown. Check Notification settings for Werkbord in System Settings.</p>
        {/if}
      {/if}
    </section>
    {#if model.view?.items.some((i) => i.kind === 'team')}
      <p class="muted small">Team tickets and mentions show in Workspaces and in each Team’s Workspace page.</p>
    {/if}
  {/if}
</div>

<style>
  .wrap {
    display: grid;
    gap: 12px;
    max-width: 720px;
    padding: 20px 24px 32px;
  }
  .head {
    display: grid;
    gap: 4px;
    margin-bottom: 10px;
  }
  .row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 10px;
    margin-top: 6px;
  }
  .small {
    font-size: 0.82rem;
  }
  .note:empty {
    display: none;
  }
  .keys {
    display: grid;
    margin: 0;
  }
  .keys div {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    padding: 9px 0;
    border-top: 1px solid var(--border);
  }
  .keys div:first-child {
    border-top: 0;
  }
  .keys dt {
    order: 2;
  }
  .keys dd {
    margin: 0;
    order: 1;
    font-size: 13.5px;
  }
  kbd {
    display: inline-block;
    min-width: 28px;
    padding: 2px 8px;
    border: 1px solid var(--border);
    border-bottom-width: 2px;
    border-radius: 6px;
    background: var(--surface-2);
    color: var(--text);
    font: 600 12.5px var(--mono);
    text-align: center;
  }
</style>

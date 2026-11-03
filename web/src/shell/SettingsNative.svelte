<script lang="ts">
  import { onMount } from 'svelte';
  import AppearanceSettings from '../lib/settings/AppearanceSettings.svelte';
  import ShortcutsSettings from '../lib/settings/ShortcutsSettings.svelte';
  import AboutSettings from '../lib/settings/AboutSettings.svelte';
  import { sharedShortcuts } from '../lib/settings/shortcuts';
  import { alerts } from './alerts.svelte';
  import { ask } from './native';
  import { model } from './model.svelte';

  // The settings that are about this window and this person, not about any one workspace: how it looks and what it tells you
  // while you are in another app. They are the same wherever Settings is opened from. Appearance, Keyboard shortcuts and Updates & about
  // are the shared ones (lib/settings), told what this window can do; Alerts are the window's own.
  let { section }: { section: 'appearance' | 'alerts' | 'shortcuts' | 'about' } = $props();

  onMount(() => void alerts.refresh());

  let tested = $state<'' | 'shown' | 'failed'>('');
  async function test() {
    tested = (await alerts.test()) ? 'shown' : 'failed';
  }
  const host = {
    info: () => ask((a) => a.Info()),
    checkUpdate: () => ask((a) => a.RequestUpdate()),
    openExternal: (url: string) => void ask((a) => a.OpenExternal(url)),
    showDiagnostics: () => void ask((a) => a.ShowDiagnostics()),
    openLogs: () => void ask((a) => a.OpenLogs()),
    openHelp: () => model.openHelp(),
  };
  let platform = $state('darwin');
  $effect(() => { if (section === 'shortcuts') ask((a) => a.Info()).then((i) => { platform = i.platform; }).catch(() => {}); });
  const mac = $derived(platform === 'darwin');
  const mod = $derived(mac ? '⌘' : 'Ctrl+');
  // The keys that are the window's own: the pages of the window, and the app's menu.
  const shortcuts = $derived([
    ...sharedShortcuts(mac),
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
    <AppearanceSettings />
  {:else if section === 'shortcuts'}
    <ShortcutsSettings keys={shortcuts} note="For the window as a whole. Inside a task or a plan, a page lists its own where they apply." />
  {:else if section === 'about'}
    <AboutSettings {host} />
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
</style>

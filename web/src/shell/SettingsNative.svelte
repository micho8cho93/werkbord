<script lang="ts">
  import { onMount } from 'svelte';
  import { look, STYLES, theme } from '../lib/theme.svelte';
  import { alerts } from './alerts.svelte';
  import { model } from './model.svelte';

  // The settings that are about this window and this person, not about any one workspace: how it looks and what it tells you
  // while you are in another app. They are the same wherever Settings is opened from.
  let { section }: { section: 'appearance' | 'alerts' } = $props();

  onMount(() => void alerts.refresh());

  let tested = $state<'' | 'shown' | 'failed'>('');
  async function test() {
    tested = (await alerts.test()) ? 'shown' : 'failed';
  }
  const unsupported = $derived(alerts.status === 'unsupported');
  const denied = $derived(alerts.status === 'denied');
</script>

<div class="wrap">
  {#if section === 'appearance'}
    <section class="pn" aria-label="Workspace settings">
      <div class="head"><h3>Projects and workspaces</h3><p class="muted">Manage this computer, connected workspaces and project setup.</p></div>
      <div><button class="btn" type="button" onclick={() => model.go('workspaces')}>Devices &amp; Hosting</button> <button class="btn" type="button" onclick={() => model.addOpen = true}>New team or join…</button> <button class="btn" type="button" onclick={() => model.goTo('personal', '#/projects')}>Manage personal projects</button></div>
    </section>
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

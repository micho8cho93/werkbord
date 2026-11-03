<script lang="ts">
  // Updates & about: what this app is, whether there is a newer one, and where to look when something is wrong. Each button is the Mac
  // app's own action (its dialogs, its files); this page only asks, through what the window or the web app hands it.
  let { host }: {
    host: {
      info: () => Promise<{ version: string; platform: string }>;
      checkUpdate: () => Promise<{ ok?: boolean; declined?: boolean; message?: string }>;
      openExternal: (url: string) => void;
      showDiagnostics: () => void;
      openLogs: () => void;
      openHelp: () => void;
    };
  } = $props();

  let info = $state<{ version: string; platform: string } | null>(null);
  let checking = $state(false);
  let outcome = $state('');
  $effect(() => {
    if (info) return;
    host.info().then((i) => { info = i; }).catch(() => { /* The version is simply not shown. */ });
  });
  async function check() {
    checking = true;
    outcome = '';
    try {
      const r = await host.checkUpdate();
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
</script>

<section class="pn" aria-labelledby="about-h">
  <div class="head">
    <h3 id="about-h">Werkbord</h3>
    <p class="muted">{#if info}Version <span class="mono">{info.version.replace(/^v/, '')}</span> · {platformName(info.platform)}{:else}One app for your own work and your teams’.{/if}</p>
  </div>
  <div class="row">
    <button class="btn primary" type="button" onclick={check} disabled={checking} data-testid="check-updates">{checking ? 'Checking…' : 'Check for updates'}</button>
    <button class="btn" type="button" onclick={() => host.openExternal(releases)}>Release notes</button>
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
    <button class="btn" type="button" onclick={() => host.openHelp()}>Guides and shortcuts</button>
    <button class="btn" type="button" onclick={() => host.showDiagnostics()}>Show diagnostics</button>
    <button class="btn" type="button" onclick={() => host.openLogs()}>Show the log</button>
    <button class="btn" type="button" onclick={() => host.openExternal(project)}>Werkbord on GitHub</button>
  </div>
</section>
<section class="pn" aria-labelledby="data-h">
  <div class="head">
    <h3 id="data-h">Your data</h3>
    <p class="muted">Your Personal projects, tasks and agent history live on this Mac, and a team’s live on the hosts your team runs. Information leaves this Mac only for the places you connect: your team’s hosts, GitHub, your phone, and the services you add. Looking for updates reads GitHub’s public release list; it can be turned off with <code>noUpdateCheck</code> in <code>config.json</code>.</p>
  </div>
</section>

<style>
  .head { display: grid; gap: 4px; margin-bottom: 10px; }
  .row { display: flex; flex-wrap: wrap; align-items: center; gap: 10px; margin-top: 6px; }
  .small { font-size: 0.82rem; }
  .note:empty { display: none; }
</style>

<script lang="ts">
  import { hosting, standing } from './aggregate';
  import { model } from './model.svelte';
  import { runnersPlace } from './nav';
  import type { Item, Overview } from './types';

  // Every workspace on this computer, what this computer does for each, and where to manage it. Hosting is shown here as
  // the workspace reports it; the devices, roles, Hosts and backups are managed in the workspace's own pages, one click away.
  let { overview }: { overview: Overview | null } = $props();

  const items = $derived((model.view?.items ?? []).filter((i) => i.state !== 'setup' || i.id === model.current));
  const entry = (i: Item) => overview?.entries.find((e) => e.workspace.id === i.id);
  const teamState = $derived(model.view?.team);
  const roleName: Record<string, string> = { runner: 'Runner', workspace_host: 'Workspace Host', connectivity_host: 'Connectivity Host' };
  const roleHelp: Record<string, string> = {
    runner: 'Runs your own agents with your own credentials.',
    workspace_host: 'Keeps this workspace’s shared data and coordinates it.',
    connectivity_host: 'Helps devices find and reach one another.',
  };
  const serviceWords: Record<string, string> = {
    ready: 'Team is running.',
    not_installed: 'Team is not set up on this computer.',
    stopped: 'Team’s service is installed but not running.',
    outdated: 'Team’s service is older than this app.',
    refused: 'Team’s service did not accept this app.',
  };
</script>

<section class="page" aria-labelledby="ws-h">
  <header>
    <h1 id="ws-h">Workspaces and devices</h1>
    <p class="lede">Closing this window stops nothing: your agents, schedules and Team services keep running.</p>
  </header>

  <section class="service" aria-label="Team service" data-state={teamState?.state}>
    <div>
      <h2>Team on this computer</h2>
      <p>{teamState?.detail || serviceWords[teamState?.state ?? 'not_installed']}</p>
    </div>
    <div class="actions">
      {#if teamState?.state === 'not_installed' || teamState?.state === 'outdated'}
        <button class="btn primary" type="button" onclick={() => model.activateTeam()} disabled={!!model.busy}>{teamState.state === 'outdated' ? 'Update Team' : 'Set up Team'}</button>
      {:else if teamState?.state === 'stopped'}
        <button class="btn" type="button" onclick={() => model.teamService('start')} disabled={!!model.busy}>Start Team</button>
      {:else if teamState?.state === 'ready'}
        <button class="btn" type="button" onclick={() => model.teamService('stop')} disabled={!!model.busy}>Stop Team</button>
      {/if}
    </div>
  </section>

  <ul class="cards">
    {#each items as i (i.id)}
      {@const e = entry(i)}
      {@const host = e ? hosting(e) : null}
      {@const roles = e?.summary?.workspace.deviceRoles ?? i.deviceRoles ?? []}
      <li class="card" data-workspace={i.id} data-state={i.state}>
        <header>
          <h2>{i.name}</h2>
          <span class="kind">{i.kind === 'personal' ? 'Individual · this computer' : i.role ? `Team · ${i.role}` : 'Team'}</span>
        </header>
        <p class="standing" data-state={i.state}>{e ? standing(e) : i.detail || i.state}</p>

        {#if roles.length}
          <ul class="roles" aria-label="What this computer does here">
            {#each roles as r (r)}
              <li title={roleHelp[r]}>{roleName[r] ?? r}</li>
            {/each}
          </ul>
        {/if}

        {#if host}
          <dl class="infra">
            <dt>Hosts</dt>
            <dd>{host.hosts}</dd>
            <dt>Saving changes</dt>
            <dd class:bad={!host.writable}>{host.writable ? 'Available' : 'Read-only until enough Hosts return'}</dd>
            {#each e?.summary?.infra?.checks ?? [] as c (c.label)}
              <dt>{c.label}</dt>
              <dd data-check={c.state}>{c.value}</dd>
            {/each}
          </dl>
          {#each host.warnings.slice(0, 3) as w (w)}
            <p class="warn">{w}</p>
          {/each}
        {/if}

        <div class="actions">
          {#if i.state === 'setup' || i.state === 'connecting' || i.state === 'ready' || i.state === 'offline'}
            <button class="btn" type="button" onclick={() => model.open(i.id)}>{i.state === 'setup' ? 'Finish setting up' : 'Open'}</button>
          {/if}
          {#if i.kind === 'personal' && i.state === 'ready'}
            <button class="btn" type="button" onclick={() => model.open(i.id, runnersPlace('personal'))} data-testid="manage-runners">Runners</button>
          {/if}
          {#if i.kind === 'team' && i.state !== 'setup'}
            <button class="btn" type="button" onclick={() => model.open(i.id, '?tab=hosts')} data-testid="manage-hosts">Hosts and devices</button>
            <button class="btn" type="button" onclick={() => model.open(i.id, '?tab=connectivity')}>Connectivity</button>
            <button class="btn" type="button" onclick={() => model.open(i.id, '?tab=backups')}>Backups</button>
            <button class="btn" type="button" onclick={() => model.open(i.id, '?tab=settings')}>Settings and leaving</button>
            {#if i.state !== 'leaving' && !roles.includes('runner')}
              <button class="btn" type="button" onclick={() => model.connectRunner(i.id)} data-testid="connect-runner">Connect my Individual runner</button>
            {/if}
          {/if}
          {#if i.kind === 'team' && (i.state === 'setup' || i.state === 'leaving' || (i.state === 'connecting' && !i.role))}
            <button class="btn quiet" type="button" onclick={() => model.forget(i.id)}>Remove from this list</button>
          {/if}
        </div>
      </li>
    {/each}
  </ul>

  <p class="foot"><button class="btn" type="button" onclick={() => (model.addOpen = true)} data-testid="add-team-page">Add a Team…</button></p>
</section>

<style>
  .page {
    padding: 28px clamp(16px, 4vw, 48px);
    max-width: 960px;
    margin: 0 auto;
    overflow-y: auto;
    height: 100%;
  }
  h1 {
    margin: 0;
  }
  h2 {
    margin: 0;
    font-size: 14px;
  }
  .lede {
    margin: 4px 0 18px;
    color: var(--text-2);
  }
  .service {
    display: flex;
    gap: 16px;
    align-items: center;
    justify-content: space-between;
    padding: 14px 16px;
    margin-bottom: 18px;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--surface);
    box-shadow: var(--card-sh);
  }
  .service p {
    margin: 2px 0 0;
    color: var(--text-2);
    font-size: 13px;
  }
  .cards {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 12px;
  }
  .card {
    padding: 14px 16px;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--surface);
    box-shadow: var(--card-sh);
  }
  .card > header {
    display: flex;
    gap: 10px;
    align-items: baseline;
    flex-wrap: wrap;
  }
  .kind {
    font-family: var(--mono);
    font-size: 11px;
    color: var(--text-2);
  }
  .standing {
    margin: 4px 0 8px;
    font-size: 13px;
    color: var(--text-2);
  }
  .standing[data-state='offline'],
  .standing[data-state='unavailable'] {
    color: var(--danger-text);
  }
  .roles {
    list-style: none;
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
    margin: 0 0 10px;
    padding: 0;
  }
  .roles li {
    padding: 2px 8px;
    border: 1px solid var(--border);
    border-radius: 999px;
    background: var(--surface-2);
    font-size: 12px;
  }
  .infra {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: 2px 14px;
    margin: 0 0 8px;
    font-size: 13px;
  }
  .infra dt {
    color: var(--text-2);
  }
  .infra dd {
    margin: 0;
  }
  .infra dd.bad,
  .infra dd[data-check='bad'] {
    color: var(--danger-text);
  }
  .infra dd[data-check='warn'] {
    color: var(--warn-text);
  }
  .warn {
    margin: 4px 0;
    padding: 6px 10px;
    border-left: 1px solid var(--amber);
    background: var(--surface-2);
    font-size: 12.5px;
  }
  .actions {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
    margin-top: 10px;
  }
  .foot {
    margin: 18px 0 0;
  }
</style>

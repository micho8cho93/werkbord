<script lang="ts">
  import { onMount } from 'svelte';
  import Sheet from '../lib/Sheet.svelte';
  import Segmented from '../lib/Segmented.svelte';
  import { ask } from './native';
  import { model } from './model.svelte';
  import { plan } from './plan.svelte';
  import type { Invitation } from './types';

  // New team or join one: one dialog, one flow. Creating asks for a name; joining asks for the invitation link. Either ends with the
  // one question this window asks about running work, "Connect this computer to the team", and then the app asks once more in a dialog
  // only the person can press (a page, even this one, never decides that a team may reach their agents). Nothing is installed here
  // without the person's say-so: when Team's service is missing the button says so, and the app asks first.
  type Way = 'create' | 'join';
  let way = $state<Way>('create');
  let teamName = $state('');
  let myName = $state('');
  let deviceName = $state('');
  let link = $state('');
  let invitation = $state<Invitation | null>(null);
  let checking = $state(false);
  let problem = $state('');
  let connect = $state(true);

  const team = $derived(model.view?.team);
  const needsSetup = $derived(!!team && team.state !== 'ready');
  const noInstaller = $derived(needsSetup && !model.view?.teamInstaller && team?.state === 'not_installed');
  const working = $derived(!!model.busy);
  const releases = 'https://github.com/micho8cho93/werkbord/releases';

  onMount(async () => {
    try {
      const d = await ask((a) => a.TeamDefaults());
      myName ||= d.name;
      deviceName ||= d.device;
    } catch {
      // The fields start empty; the person types them.
    }
    // An invitation the app was opened with is the way in: say which team it is for, and let the person confirm.
    if (model.view?.invitation) {
      try {
        const l = await ask((a) => a.PendingInvitation());
        if (l) {
          way = 'join';
          link = l;
          await check();
        }
      } catch {
        // Nothing to hand over.
      }
    }
  });

  async function check() {
    problem = '';
    model.error = '';
    invitation = null;
    checking = true;
    try {
      invitation = await ask((a) => a.CheckInvitation(link));
      link = link.trim();
    } catch (e) {
      problem = (e as Error).message;
    } finally {
      checking = false;
    }
  }

  function close() {
    model.addOpen = false;
  }

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    problem = '';
    if (way === 'create') await model.createTeam(teamName, myName, connect);
    else await model.joinTeam(link, myName, deviceName, connect, invitation?.name ?? '');
  }

  const expires = (iso: string) => {
    const d = new Date(iso);
    return Number.isNaN(d.getTime()) ? '' : d.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' });
  };
  const s = $derived(model.setup);
</script>

<Sheet title="New team or join one" onclose={close} width="38rem">
<div class="dlg" data-testid="add-team-dialog">
  {#if s}
    <div data-testid="add-team-progress" role="status">
      {#if s.error}
        <p class="error" role="alert">{s.error}</p>
        {#if s.exists}
          <p>{s.name || 'The team'} is on this computer but could not connect. It stays in the sidebar; you can leave it from its own page.</p>
        {/if}
        <div class="actions">
          <button class="btn primary" type="button" onclick={() => model.cancelSetup()} data-testid="add-team-clear">{s.exists ? 'Close' : 'Start over'}</button>
        </div>
      {:else if s.waiting}
        <h3>Waiting for {s.name || 'the team'}’s administrator</h3>
        <p>Your invitation was checked. An administrator has to approve this computer in the team’s Devices page. You can close this window: the team appears in the sidebar once you are in{s.connect ? ', and you are then asked about connecting your runner' : ''}.</p>
        <div class="actions">
          <button class="btn" type="button" onclick={() => model.cancelSetup()} data-testid="add-team-withdraw">Withdraw</button>
          <button class="btn primary" type="button" onclick={close}>Close</button>
        </div>
      {:else}
        <h3>{s.kind === 'create' ? 'Creating your team…' : 'Joining the team…'}</h3>
        <p>{s.working || 'Almost there.'} This continues if you close this window.</p>
        <div class="actions"><button class="btn" type="button" onclick={close}>Close</button></div>
      {/if}
    </div>
  {:else if noInstaller}
    <p>This build of Werkbord does not include Team’s service. Team comes with the Werkbord app from a release.</p>
    <div class="actions">
      <button class="btn primary" type="button" onclick={() => ask((a) => a.OpenExternal(releases))}>Get the Werkbord app</button>
      <button class="btn" type="button" onclick={close}>Close</button>
    </div>
  {:else}
    <Segmented compact options={[{ id: 'create', label: 'Create a team' }, { id: 'join', label: 'Join with a link' }]} value={way} onchange={(w) => { way = w; problem = ''; model.error = ''; }} label="Create or join" />
    <form onsubmit={submit}>
      {#if way === 'create'}
        <label class="f"><span>Team name</span>
          <input name="team-name" bind:value={teamName} maxlength="80" required autocomplete="organization" placeholder="Smith &amp; Co" data-autofocus data-testid="add-team-name" />
        </label>
        <label class="f"><span>Your name</span>
          <input name="your-name" bind:value={myName} maxlength="80" required autocomplete="name" data-testid="add-team-owner" />
        </label>
        <div class="f"><span>Where it is hosted</span>
          <p class="host">Start with this computer as your team’s only host. Your data stays on machines you own.</p>
          <p class="fine">Keep this computer online while your team works. Invite people and check backups in Settings after setup.</p>
          <p class="fine">
            {#if needsSetup}Team’s service is not running on this Mac yet. The first time, macOS asks for an administrator password to start it.{:else}Werkbord runs no cloud for your team.{/if}
          </p>
        </div>
      {:else}
        {#if !invitation}
          <label class="f"><span>Invitation link</span>
            <textarea class="input" name="invitation" rows="3" bind:value={link} required autocomplete="off" spellcheck="false" placeholder="werkbord://join/…" data-autofocus data-testid="add-team-link"></textarea>
          </label>
          <div class="actions"><button class="btn primary" type="button" onclick={check} disabled={!link.trim() || checking} data-testid="add-team-check">{checking ? 'Checking…' : 'Check invitation'}</button></div>
        {:else}
          <p class="ok" data-testid="add-team-invitation">Invitation for <strong>{invitation.name}</strong>{expires(invitation.expiresAt) ? `, valid until ${expires(invitation.expiresAt)}` : ''}. <button class="link" type="button" onclick={() => { invitation = null; }}>Use another</button></p>
          <label class="f"><span>Your name</span>
            <input name="your-name" bind:value={myName} maxlength="80" required autocomplete="name" data-autofocus data-testid="add-team-member" />
          </label>
          <label class="f"><span>Name for this computer</span>
            <input name="device-name" bind:value={deviceName} maxlength="80" required placeholder="e.g. Office Mac" data-testid="add-team-device" />
          </label>
        {/if}
      {/if}

      {#if way === 'create' || invitation}
        <p class="plan" data-testid="add-team-plan">
          {#if plan.premium}<strong>Premium:</strong> teams of any size.
          {:else}<strong>Free plan: up to 2 people</strong>, you and a partner. Teams of 3 or more need Premium for everyone in the team.{/if}
        </p>
        <label class="check"><input type="checkbox" bind:checked={connect} data-testid="add-team-connect" />
          <span><strong>Connect this computer to the team</strong>
            <span class="fine">So you can run your own tickets with your own agent. Every run still asks you first, and the team never runs anything on your machine.</span></span>
        </label>
        {#if model.error}<p class="error" role="alert">{model.error}</p>{/if}
        {#if problem}<p class="error" role="alert">{problem}</p>{/if}
        {#if model.busy}<p class="fine" role="status">{model.busy}</p>{/if}
        <div class="actions">
          <button class="btn" type="button" onclick={close}>Cancel</button>
          <button class="btn primary" type="submit" disabled={working} data-testid="add-team-continue">
            {#if way === 'create'}{needsSetup ? 'Set up Team and create' : 'Create team'}{:else}{needsSetup ? 'Set up Team and join' : 'Join team'}{/if}
          </button>
        </div>
      {:else}
        {#if problem}<p class="error" role="alert">{problem}</p>{/if}
        <div class="actions"><button class="btn" type="button" onclick={close}>Cancel</button></div>
      {/if}
    </form>
  {/if}
</div>
</Sheet>

<style>
  .dlg {
    display: grid;
    gap: 12px;
  }
  form {
    display: grid;
    gap: 12px;
  }
  h3 {
    margin: 0 0 4px;
    font-size: 15px;
    font-weight: 600;
  }
  p {
    margin: 0;
    font-size: 13.5px;
  }
  .f {
    display: grid;
    gap: 4px;
    font-size: 13px;
    font-weight: 600;
  }
  .f > span {
    color: var(--text);
  }
  .f input,
  .f textarea {
    width: 100%;
    font-weight: 400;
  }
  .host {
    padding: 8px 10px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    font-weight: 400;
    background: var(--surface);
  }
  .fine {
    margin: 0;
    color: var(--text-2);
    font-size: 12.5px;
    font-weight: 400;
  }
  .plan {
    padding: 10px 12px;
    border-radius: var(--radius-sm);
    background: var(--surface-2);
    font-size: 13px;
  }
  .check {
    display: flex;
    gap: 10px;
    align-items: flex-start;
    font-size: 13px;
  }
  .check input {
    margin-top: 3px;
  }
  .check span span {
    display: block;
    margin-top: 2px;
  }
  .ok {
    color: var(--ok-text, var(--text));
  }
  .error {
    color: var(--danger-text, var(--danger));
  }
  .link {
    padding: 0;
    border: 0;
    background: none;
    color: var(--accent-text, var(--text));
    text-decoration: underline;
    cursor: pointer;
    font: inherit;
  }
  .actions {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
    margin-top: 4px;
  }
</style>

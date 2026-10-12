<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from './api';
  import { app } from './state.svelte';
  import type { MCPBoardOperation, MCPClientGrant, MCPProposal } from './types';

  let grants = $state<MCPClientGrant[]>([]);
  let operations = $state<MCPBoardOperation[]>([]);
  let proposals = $state<MCPProposal[]>([]);
  let name = $state('');
  let selectedProjects = $state<string[]>([]);
  let selectedOperations = $state<string[]>([]);
  let oneTimeToken = $state('');
  let busy = $state(false);
  let loading = $state(true);
  let error = $state('');
  let notice = $state('');

  onMount(() => { void reload(); });

  async function reload() {
    loading = true;
    error = '';
    try {
      const [nextGrants, nextOperations, nextProposals] = await Promise.all([
        api.mcpClientGrants(), api.mcpOperations(), api.mcpProposals(),
      ]);
      grants = nextGrants;
      operations = nextOperations;
      proposals = nextProposals;
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally { loading = false; }
  }

  function toggle(values: string[], value: string, enabled: boolean): string[] {
    return enabled ? [...new Set([...values, value])] : values.filter((item) => item !== value);
  }

  async function createGrant(event: SubmitEvent) {
    event.preventDefault();
    if (busy) return;
    busy = true; error = ''; notice = ''; oneTimeToken = '';
    try {
      const created = await api.createMCPClientGrant({ name: name.trim(), projects: selectedProjects, operations: selectedOperations });
      oneTimeToken = created.token;
      grants = [created.grant, ...grants];
      name = ''; selectedProjects = []; selectedOperations = [];
      notice = 'Client created. Copy its credential now; Werkbord will not show it again.';
    } catch (err) { error = err instanceof Error ? err.message : String(err); }
    finally { busy = false; }
  }

  async function revoke(grant: MCPClientGrant) {
    if (busy || grant.revoked) return;
    busy = true; error = ''; notice = '';
    try {
      await api.revokeMCPClientGrant(grant.id);
      grants = grants.map((item) => item.id === grant.id ? { ...item, revoked: true } : item);
      notice = `${grant.name} can no longer connect. Its pending proposals remain available for your decision.`;
    } catch (err) { error = err instanceof Error ? err.message : String(err); }
    finally { busy = false; }
  }

  async function decide(item: MCPProposal, approve: boolean) {
    if (busy) return;
    busy = true; error = ''; notice = '';
    try {
      await api.resolveMCPProposal(item.grantId, item.proposal.actionId, approve);
      notice = approve ? 'Change approved and applied.' : 'Change declined.';
      await reload();
    } catch (err) { error = err instanceof Error ? err.message : String(err); }
    finally { busy = false; }
  }

  async function copyToken() {
    try { await navigator.clipboard.writeText(oneTimeToken); notice = 'Credential copied.'; }
    catch { notice = 'Select and copy the credential above.'; }
  }

  const endpoint = $derived(typeof window === 'undefined' ? 'http://127.0.0.1:7420/mcp' : `${window.location.origin}/mcp`);
</script>

<section class="pn">
  <div class="head">
    <h3>External MCP clients</h3>
    <p class="muted">Give an external agent its own credential, limited to selected projects and board operations. It cannot start runs, change execution settings, or approve its own changes.</p>
  </div>

  {#if error}<p class="error" role="alert">{error}</p>{/if}
  {#if notice}<p class="ok" role="status">{notice}</p>{/if}

  {#if oneTimeToken}
    <div class="issued" role="group" aria-label="New MCP client credential">
      <strong>Copy this credential now</strong>
      <p class="note">It is shown once. Store it in the MCP client's environment as <code>WERKBORD_MCP_TOKEN</code>.</p>
      <code class="token">{oneTimeToken}</code>
      <button class="btn small" onclick={copyToken}>Copy credential</button>
      <p class="note">Streamable HTTP endpoint: <code>{endpoint}</code></p>
    </div>
  {/if}

  <form class="grant-form" onsubmit={createGrant}>
    <h4>Create a client credential</h4>
    <label><span>Client name</span><input class="input" bind:value={name} maxlength="80" placeholder="For example, meeting-notes agent" required /></label>
    <fieldset>
      <legend>Projects</legend>
      {#if app.projects.length === 0}<p class="note">Register a project before creating a scoped client.</p>{/if}
      <div class="choices">
        {#each app.projects as project (project.id)}
          <label class="check"><input type="checkbox" checked={selectedProjects.includes(project.id)} onchange={(event) => selectedProjects = toggle(selectedProjects, project.id, event.currentTarget.checked)} /><span>{project.name}</span></label>
        {/each}
      </div>
    </fieldset>
    <fieldset>
      <legend>Operations</legend>
      <div class="choices">
        {#each operations as operation (operation.name)}
          <label class="operation check">
            <input type="checkbox" checked={selectedOperations.includes(operation.name)} onchange={(event) => selectedOperations = toggle(selectedOperations, operation.name, event.currentTarget.checked)} />
            <span><strong>{operation.title || operation.name}</strong><small>{operation.description}</small></span>
          </label>
        {/each}
      </div>
    </fieldset>
    <div class="row"><button class="btn primary" disabled={busy || !name.trim() || !selectedProjects.length || !selectedOperations.length}>{busy ? 'Creating…' : 'Create scoped client'}</button></div>
  </form>

  <div class="list-head">
    <h4>Pending changes</h4>
    <button class="btn small" disabled={loading || busy} onclick={reload}>{loading ? 'Refreshing…' : 'Refresh'}</button>
  </div>
  {#if loading}<p class="muted" role="status">Loading MCP clients…</p>
  {:else if proposals.length === 0}<p class="empty">No external changes are waiting for approval.</p>
  {:else}
    <ul class="items proposals">
      {#each proposals as item (`${item.grantId}:${item.proposal.actionId}`)}
        <li>
          <div><strong>{item.client}</strong><span class="muted"> · {item.proposal.operation}</span><p>{item.proposal.summary}</p><small>Expires {new Date(item.proposal.expiresAt).toLocaleString()}</small></div>
          <div class="row"><button class="btn primary small" disabled={busy} onclick={() => decide(item, true)}>Approve</button><button class="btn small" disabled={busy} onclick={() => decide(item, false)}>Decline</button></div>
        </li>
      {/each}
    </ul>
  {/if}

  <div class="list-head"><h4>Client credentials</h4></div>
  {#if !loading && grants.length === 0}<p class="empty">No MCP clients have been created.</p>{/if}
  <ul class="items">
    {#each grants as grant (grant.id)}
      <li>
        <div><strong>{grant.name}</strong><p>{grant.projects.length} {grant.projects.length === 1 ? 'project' : 'projects'} · {grant.operations.length} operations · Created {new Date(grant.createdAt).toLocaleDateString()}</p><small class="mono">{grant.operations.join(', ')}</small>{#if grant.revoked}<span class="revoked">Revoked</span>{/if}</div>
        {#if !grant.revoked}<button class="btn small danger" disabled={busy} onclick={() => revoke(grant)}>Revoke</button>{/if}
      </li>
    {/each}
  </ul>
</section>

<style>
  h4 { margin:0; font-size:14px; font-weight:600; }
  .grant-form { display:grid; gap:14px; padding:16px 0; margin:12px 0; border-top:1px solid var(--border); border-bottom:1px solid var(--border); }
  .grant-form > label { display:grid; gap:5px; font-size:12px; color:var(--text-2); }
  .grant-form .input { width:100%; }
  fieldset { margin:0; padding:10px 12px; min-width:0; border:1px solid var(--border); border-radius:8px; }
  legend { padding:0 5px; color:var(--text-2); font-size:12px; }
  .choices { display:grid; gap:6px; grid-template-columns:repeat(auto-fit,minmax(min(100%,230px),1fr)); }
  .check { display:flex; align-items:flex-start; gap:8px; min-height:30px; font-size:13px; }
  .check input { margin-top:3px; accent-color:var(--accent); }
  .operation span { display:grid; gap:3px; }
  .operation small, .items small { color:var(--text-2); line-height:1.4; }
  .row { display:flex; align-items:center; gap:8px; flex-wrap:wrap; }
  .issued { display:grid; justify-items:start; gap:8px; padding:12px; margin:12px 0; border:1px solid var(--accent); border-radius:8px; }
  .token { max-width:100%; overflow-wrap:anywhere; padding:8px; background:var(--surface-2); border-radius:5px; }
  .list-head { display:flex; align-items:center; justify-content:space-between; gap:12px; margin:20px 0 8px; }
  .items { list-style:none; padding:0; margin:0; display:grid; }
  .items li { display:flex; justify-content:space-between; align-items:flex-start; gap:12px; padding:12px 0; border-top:1px solid var(--border); }
  .items p { margin:5px 0; font-size:13px; }
  .proposals p { color:var(--text-1); }
  .revoked { display:inline-block; margin-left:8px; font-size:12px; color:var(--text-2); }
  code { overflow-wrap:anywhere; }
  @media(max-width:560px) { .items li { flex-direction:column; } }
</style>

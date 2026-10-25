<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from './api';
  import { clientConfig } from './mcp-setup';
  import { app } from './state.svelte';
  import type { MCPBoardOperation, MCPClientGrant, MCPProposal, MCPTaskPlan, MCPTaskPlanChange } from './types';

  let grants = $state<MCPClientGrant[]>([]);
  let operations = $state<MCPBoardOperation[]>([]);
  let proposals = $state<MCPProposal[]>([]);
  let planDrafts = $state<Record<string, MCPTaskPlan>>({});
  let name = $state('');
  let selectedProjects = $state<string[]>([]);
  let selectedOperations = $state<string[]>([]);
  let oneTimeToken = $state('');
  let stdio = $state(false);
  let connectionURL = $state(`${window.location.origin}/mcp`);
  const config = $derived.by(() => { try { return clientConfig(connectionURL, oneTimeToken, stdio); } catch { return ''; } });
  async function copyConfig() {
    try { await navigator.clipboard.writeText(config); notice = 'Connection settings copied. Paste them into your MCP client and reconnect it.'; }
    catch { notice = 'Select and copy the connection settings below.'; }
  }
  let busy = $state(false);
  let loading = $state(true);
  let error = $state('');
  let notice = $state('');
  let newTaskSerial = 0;

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
      planDrafts = Object.fromEntries(nextProposals.filter((item) => item.plan).map((item) => [proposalKey(item), structuredClone(item.plan!)]));
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

  function proposalKey(item: MCPProposal) { return `${item.grantId}:${item.proposal.actionId}`; }

  function planFor(item: MCPProposal) { return planDrafts[proposalKey(item)] ?? item.plan; }

  function editTask(item: MCPProposal, index: number, patch: Partial<MCPTaskPlanChange>) {
    const plan = planFor(item);
    if (!plan) return;
    const tasks = plan.tasks.map((task, taskIndex) => taskIndex === index ? { ...task, ...patch } : task);
    planDrafts[proposalKey(item)] = { ...plan, tasks };
  }

  function addPlanTask(item: MCPProposal) {
    const plan = planFor(item);
    if (!plan || plan.tasks.length >= 50) return;
    planDrafts[proposalKey(item)] = { ...plan, tasks: [...plan.tasks, { tempId: `tmp_review_${Date.now()}_${newTaskSerial++}`, title: 'New task', description: '', dependencies: [] }] };
  }

  function removePlanTask(item: MCPProposal, index: number) {
    const plan = planFor(item);
    if (!plan) return;
    planDrafts[proposalKey(item)] = { ...plan, tasks: plan.tasks.filter((_, taskIndex) => taskIndex !== index) };
  }

  function editDate(item: MCPProposal, index: number, field: 'start' | 'end' | 'milestone', value: string | boolean) {
    const task = planFor(item)?.tasks[index];
    if (!task) return;
    const plan = { ...(task.plan ?? {}), [field]: value };
    editTask(item, index, { plan });
  }

  async function savePlan(item: MCPProposal) {
    const plan = planFor(item);
    if (!plan || busy) return;
    busy = true; error = ''; notice = '';
    try {
      const updated = await api.replaceMCPTaskPlan(item.grantId, item.proposal.actionId, plan);
      proposals = proposals.map((proposal) => proposalKey(proposal) === proposalKey(item) ? updated : proposal);
      if (updated.plan) planDrafts[proposalKey(updated)] = structuredClone(updated.plan);
      notice = 'Plan saved. Werkbord checked it again; approve it when the preview looks right.';
    } catch (err) { error = err instanceof Error ? err.message : String(err); }
    finally { busy = false; }
  }

  function dependenciesText(task: MCPTaskPlanChange) { return (task.dependencies ?? []).join(', '); }

  function parseDependencies(value: string) {
    return [...new Set(value.split(/[\s,]+/).map((id) => id.trim()).filter(Boolean))];
  }

  async function copyToken() {
    try { await navigator.clipboard.writeText(oneTimeToken); notice = 'Credential copied.'; }
    catch { notice = 'Select and copy the credential above.'; }
  }

  const endpoint = $derived(typeof window === 'undefined' ? 'http://127.0.0.1:7420/mcp' : `${window.location.origin}/mcp`);
</script>

<section class="pn">
  <div class="head">
    <h3>Connect a client to Werkbord</h3>
    <p class="muted">Give an external agent its own credential, limited to selected projects and board operations. It cannot start runs, change execution settings, or approve its own changes.</p>
  </div>

  {#if error}<p class="error" role="alert">{error}</p>{/if}
  {#if notice}<p class="ok" role="status">{notice}</p>{/if}

  <p class="note">Werkbord’s MCP server is already built in. Name your client, choose its projects and operations, then create and copy its connection settings. Any compatible MCP client can connect. <a href="#/help?article=mcp">How MCP setup works</a></p>
  {#if oneTimeToken}
    <div class="issued" role="group" aria-label="New MCP client credential">
      <strong>Copy this credential now</strong>
      <p class="note">It is shown once. Store it in the MCP client's environment as <code>WERKBORD_MCP_TOKEN</code>.</p>
      <code class="token">{oneTimeToken}</code>
      <button class="btn small quiet" onclick={copyToken}>Copy credential</button>
      <label>Address reachable from your client<input class="input" type="url" bind:value={connectionURL} /></label>
      <p class="note">Default: {endpoint}. For a client on another computer, enter this controller’s reachable private address ending in /mcp. A phone’s relay address may only work in its paired browser.</p>
      <label class="check"><input type="checkbox" bind:checked={stdio} /><span>My client needs a local process (stdio)</span></label>
      {#if stdio}<p class="note">Install the werkbord command on the client’s computer. It bridges to the address above.</p>{/if}
      {#if config}<pre class="config">{config}</pre><button class="btn primary" type="button" onclick={copyConfig}>Copy connection settings</button>{:else}<p class="error">Enter a valid HTTP or HTTPS address without embedded credentials.</p>{/if}
      <p class="note">Paste the settings into your client’s MCP configuration. For a URL-based setup screen, use the address above and an Authorization header with Bearer followed by this credential. Reconnect the client and ask it to list your projects.</p>
      <button class="btn small" onclick={() => { oneTimeToken = ''; notice = 'Credential hidden. Revoke and replace it if you did not save it.'; }}>I’ve saved it — hide credential</button>
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
      {#each proposals as item (proposalKey(item))}
        <li>
          <div class="proposal-body">
            <strong>{item.client}</strong><span class="muted"> · {item.proposal.operation}</span><p>{item.proposal.summary}</p>
            {#if item.plan}
              {@const plan = planFor(item)}
              {@const hasUnsavedEdits = JSON.stringify(plan) !== JSON.stringify(item.plan)}
              <div class="plan-preview">
                <h5>Review task plan <small>Project {item.plan.projectId} · {plan?.tasks.length ?? 0} {(plan?.tasks.length ?? 0) === 1 ? 'task' : 'tasks'}</small></h5>
                {#if item.validationError}<p class="error" role="alert">{item.validationError}</p>{/if}
                {#if item.warnings?.length}
                  <ul class="warnings" aria-label="Timeline warnings">
                    {#each item.warnings as warning, warningIndex (`${warning.code}:${warning.itemId}:${warning.otherId}:${warningIndex}`)}
                      <li class:warning-error={warning.severity === 'error'}><strong>{warning.severity}</strong> · {warning.message}</li>
                    {/each}
                  </ul>
                {/if}
                <div class="plan-tasks">
                  {#each plan?.tasks ?? item.plan.tasks as task, index (`${task.tempId ?? task.taskId}:${index}`)}
                    <fieldset class="plan-task">
                      <legend>{task.tempId ? `New task · ${task.tempId}` : `Update task · ${task.taskId} · v${task.version}`}</legend>
                      <label><span>Title</span><input class="input" value={task.title ?? ''} maxlength="200" oninput={(event) => editTask(item, index, { title: event.currentTarget.value })} /></label>
                      <label><span>Description</span><textarea class="input" rows="3" maxlength="20000" oninput={(event) => editTask(item, index, { description: event.currentTarget.value })}>{task.description ?? ''}</textarea></label>
                      <div class="date-fields">
                        <label><span>Start</span><input class="input" type="date" value={task.plan?.start ?? ''} oninput={(event) => editDate(item, index, 'start', event.currentTarget.value)} /></label>
                        <label><span>End</span><input class="input" type="date" value={task.plan?.end ?? ''} oninput={(event) => editDate(item, index, 'end', event.currentTarget.value)} /></label>
                        <label class="check"><input type="checkbox" checked={task.plan?.milestone ?? false} onchange={(event) => editDate(item, index, 'milestone', event.currentTarget.checked)} /><span>Milestone</span></label>
                      </div>
                      <label><span>Dependencies <small>Task IDs or temporary IDs, separated by commas</small></span><input class="input mono" value={dependenciesText(task)} oninput={(event) => editTask(item, index, { dependencies: parseDependencies(event.currentTarget.value) })} /></label>
                      <label><span>Source reference</span><input class="input" value={task.sourceRef ?? ''} maxlength="2000" oninput={(event) => editTask(item, index, { sourceRef: event.currentTarget.value })} /></label>
                      <button class="btn small danger" disabled={busy} onclick={() => removePlanTask(item, index)}>Remove task</button>
                    </fieldset>
                  {/each}
                </div>
                <div class="row plan-actions"><button class="btn small" disabled={busy || !plan?.tasks.length} onclick={() => savePlan(item)}>Save edited plan</button><button class="btn small add" disabled={busy || (plan?.tasks.length ?? 0) >= 50} onclick={() => addPlanTask(item)}>Add task</button><small>Saving replaces the pending proposal and reruns validation.</small></div>
                {#if hasUnsavedEdits}<small class="note">Save your edits before approving.</small>{/if}
              </div>
            {/if}
            <small>Expires {new Date(item.proposal.expiresAt).toLocaleString()}</small>
            <div class="row decision-actions"><button class="btn primary small" disabled={busy || !!item.validationError || (item.plan && JSON.stringify(planFor(item)) !== JSON.stringify(item.plan))} onclick={() => decide(item, true)}>Approve plan</button><button class="btn small" disabled={busy} onclick={() => decide(item, false)}>Decline</button></div>
          </div>
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
  .config { white-space: pre-wrap; overflow-wrap: anywhere; max-width: 100%; min-width: 0; padding: 12px; background: var(--surface-2); }
  .issued label { max-width: 100%; display: grid; gap: 6px; }
  .issued label.check { display: flex; }
  h4 { margin:0; font-size:14px; font-weight:600; }
  .grant-form { display:grid; gap:14px; padding:16px 0; margin:12px 0; border-top:1px solid var(--border); border-bottom:1px solid var(--border); }
  .grant-form > label { display:grid; gap:5px; font-size:12px; color:var(--text-2); }
  .grant-form .input { width:100%; }
  fieldset { margin:0; padding:10px 12px; min-width:0; border:1px solid var(--border); border-radius:10px; }
  legend { padding:0 5px; color:var(--text-2); font-size:12px; }
  .choices { display:grid; gap:6px; grid-template-columns:repeat(auto-fit,minmax(min(100%,230px),1fr)); }
  .check { display:flex; align-items:flex-start; gap:8px; min-height:30px; font-size:13px; }
  .check input { margin-top:3px; accent-color:var(--accent); }
  .operation span { display:grid; gap:3px; }
  .operation small, .items small { color:var(--text-2); line-height:1.4; }
  .row { display:flex; align-items:center; gap:8px; flex-wrap:wrap; }
  .issued { display:grid; justify-items:start; gap:8px; padding:12px; margin:12px 0; border:1px solid var(--accent); border-radius:10px; }
  .token { max-width:100%; overflow-wrap:anywhere; padding:8px; background:var(--surface-2); border-radius:6px; }
  .list-head { display:flex; align-items:center; justify-content:space-between; gap:12px; margin:20px 0 8px; }
  .items { list-style:none; padding:0; margin:0; display:grid; }
  .items li { display:flex; justify-content:space-between; align-items:flex-start; gap:12px; padding:12px 0; border-top:1px solid var(--border); }
  .items p { margin:5px 0; font-size:13px; }
  .proposals p { color:var(--text-1); }
  .proposal-body { display:grid; gap:8px; width:100%; min-width:0; }
  .plan-preview { display:grid; gap:10px; padding:12px; margin:8px 0; border:1px solid var(--border); border-radius:10px; background:var(--surface-1); }
  .plan-preview h5 { margin:0; font-size:13px; }
  .plan-preview h5 small { display:block; margin-top:4px; font-weight:400; color:var(--text-2); }
  .plan-tasks { display:grid; gap:10px; }
  .plan-task { display:grid; gap:9px; }
  .plan-task label { display:grid; gap:4px; font-size:12px; color:var(--text-2); }
  .plan-task label small { display:block; }
  .plan-task .input { width:100%; }
  .date-fields { display:grid; grid-template-columns:repeat(auto-fit,minmax(130px,1fr)); gap:8px; align-items:end; }
  .date-fields .check { display:flex; align-items:center; gap:7px; min-height:32px; }
  .date-fields input[type=checkbox] { accent-color:var(--accent); }
  .warnings { display:grid; gap:5px; margin:0; padding:9px 10px 9px 28px; border-radius:6px; background:var(--surface-2); font-size:12px; }
  .warnings li { color:var(--text-2); }
  .warnings .warning-error { color:var(--danger); }
  .plan-actions small { color:var(--text-2); }
  .decision-actions { margin-top:2px; }
  .revoked { display:inline-block; margin-left:8px; font-size:12px; color:var(--text-2); }
  code { overflow-wrap:anywhere; }
  @media(max-width:560px) { .items li { flex-direction:column; } }
</style>

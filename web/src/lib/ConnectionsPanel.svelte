<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from './api';
  import { importConnection } from './mcp-setup';
  import type { MCPConnection, MCPContextPolicy } from './types';

  type Draft = {
    id?: string;
    name: string;
    transport: 'stdio' | 'streamable_http';
    command: string;
    args: string;
    url: string;
    environment: string;
    authMode: 'none' | 'bearer' | 'environment' | 'oauth';
    authEnv: string;
    bearerToken: string;
  };

  const emptyDraft = (): Draft => ({ name: '', transport: 'stdio', command: '', args: '', url: '', environment: '', authMode: 'none', authEnv: '', bearerToken: '' });
  let importText = $state('');
  function importSettings() {
    try { draft = { ...emptyDraft(), ...importConnection(importText) }; editing = true; importText = ''; error = ''; }
    catch (err) { error = err instanceof Error ? err.message : String(err); }
  }

  let connections = $state<MCPConnection[]>([]);
  let loading = $state(true);
  let editing = $state(false);
  let draft = $state<Draft>(emptyDraft());
  let busy = $state(false);
  let testing = $state('');
  let error = $state('');
  let notice = $state('');
  let armedDelete = $state('');
  let resourceContent = $state<{id:string; uri:string; text:string} | null>(null);

  onMount(() => { void reload(); });

  async function reload() {
    loading = true;
    try { connections = await api.mcpConnections(); }
    catch (err) { error = err instanceof Error ? err.message : String(err); }
    finally { loading = false; }
  }

  function startNew() { draft = emptyDraft(); editing = true; error = ''; notice = ''; }
  function edit(c: MCPConnection) {
    draft = {
      id: c.id, name: c.name, transport: c.transport, command: c.command ?? '', args: (c.args ?? []).join('\n'), url: c.url ?? '',
      environment: Object.entries(c.environment ?? {}).map(([child, source]) => `${child}=${source}`).join('\n'),
      authMode: c.authMode ?? 'none', authEnv: c.authEnv ?? '', bearerToken: '',
    };
    editing = true; error = ''; notice = '';
  }

  async function save(e: SubmitEvent) {
    e.preventDefault();
    if (busy) return;
    busy = true; error = ''; notice = '';
    const environment: Record<string,string> = {};
    for (const row of draft.environment.split(/\r?\n/).map(x => x.trim()).filter(Boolean)) {
      const [child, source] = row.split('=', 2).map(x => x.trim());
      if (!child || !source) { error = 'Write each environment reference as CHILD=SOURCE_VARIABLE.'; busy = false; return; }
      environment[child] = source;
    }
    try {
      const c = await api.saveMCPConnection({
        id: draft.id, name: draft.name.trim(), transport: draft.transport, command: draft.command.trim(),
        args: draft.args.split(/\r?\n/).filter((x) => x.length > 0), url: draft.url.trim(), environment,
        authMode: draft.authMode, authEnv: draft.authEnv.trim(), bearerToken: draft.bearerToken,
      });
      put(c); editing = false; notice = 'Connection saved. It is disabled until you enable it.';
    } catch (err) { error = err instanceof Error ? err.message : String(err); }
    finally { busy = false; }
  }

  function put(c: MCPConnection) { connections = [...connections.filter(x => x.id !== c.id), c].sort((a,b) => a.name.localeCompare(b.name)); }

  async function test(c: MCPConnection) {
    testing = c.id; error = ''; notice = '';
    try { const updated = await api.testMCPConnection(c.id); put(updated); notice = `${updated.tools?.length ?? 0} tools and ${updated.resources?.length ?? 0} resources found.`; }
    catch (err) { error = err instanceof Error ? err.message : String(err); await reload(); }
    finally { testing = ''; }
  }

  async function reauthorize(c: MCPConnection) {
    testing = c.id; error = ''; notice = '';
    try {
      const updated = await api.reauthorizeMCPConnection(c.id);
      put(updated);
      notice = 'Sign-in completed and the service was rediscovered.';
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
      await reload();
    } finally { testing = ''; }
  }

  async function enable(c: MCPConnection, enabled: boolean) {
    busy = true; error = ''; notice = '';
    try { put(await api.setMCPConnectionEnabled(c.id, enabled)); }
    catch (err) { error = err instanceof Error ? err.message : String(err); }
    finally { busy = false; }
  }

  async function context(c: MCPConnection, kind: 'assistant'|'coding', enabled: boolean) {
    busy = true; error = '';
    try { put(await api.setMCPContext(c.id, kind, enabled)); }
    catch (err) { error = err instanceof Error ? err.message : String(err); }
    finally { busy = false; }
  }

  async function policy(c: MCPConnection, tool: string, kind: 'assistant'|'coding', value: MCPContextPolicy) {
    busy = true; error = ''; notice = '';
    try { put(await api.setMCPToolPolicy(c.id, tool, kind, value)); }
    catch (err) { error = err instanceof Error ? err.message : String(err); }
    finally { busy = false; }
  }

  async function remove(c: MCPConnection) {
    if (armedDelete !== c.id) { armedDelete = c.id; return; }
    busy = true; error = '';
    try { await api.deleteMCPConnection(c.id); connections = connections.filter(x => x.id !== c.id); armedDelete = ''; notice = `${c.name} removed.`; }
    catch (err) { error = err instanceof Error ? err.message : String(err); }
    finally { busy = false; }
  }

  async function readResource(c: MCPConnection, uri: string, kind: 'assistant'|'coding') {
    error = ''; resourceContent = null;
    try {
      const result = await api.readMCPResource(c.id, kind, uri);
      resourceContent = { id:c.id, uri, text:JSON.stringify(result, null, 2) };
    } catch (err) { error = err instanceof Error ? err.message : String(err); }
  }

  function policyValue(value: string | undefined): MCPContextPolicy { return value === 'allow' || value === 'deny' ? value : 'confirm'; }
  function transportChanged(value: 'stdio' | 'streamable_http') {
    draft.transport = value;
    if (value === 'stdio') { draft.authMode = 'none'; draft.authEnv = ''; draft.bearerToken = ''; }
  }
</script>

<section class="pn">
  <div class="head">
    <h3>Connect Werkbord to a service</h3>
    <p class="muted">Connect services the assistant or coding agents can use through Werkbord. Credentials stay with the controller and are never passed to an agent. New connections start disabled.</p>
  </div>

  {#if error}<p class="error" role="alert">{error}</p>{/if}
  {#if notice}<p class="ok" role="status">{notice}</p>{/if}

  <details class="setup-help">
    <summary>Have an MCP configuration? Paste it here</summary>
    <p class="note">Copy one server’s JSON configuration from its documentation. Werkbord fills in the fields for you; review them before saving. Nothing runs until you test or enable the connection.</p>
    <label class="import-field"><span>MCP configuration</span><textarea class="input mono" rows="5" bind:value={importText} spellcheck="false" placeholder={'{"mcpServers":{"my-service":{"url":"https://service.example/mcp"}}}'}></textarea></label>
    <button class="btn" type="button" disabled={!importText.trim()} onclick={importSettings}>Fill in connection</button>
  </details>
  <p class="note">For a hosted server, choose Remote service and enter its MCP URL. For a server installed on this computer, choose Local process and enter its executable and arguments. <a href="#/help?article=mcp">MCP setup help</a></p>

  {#if editing}
    <form class="editor" onsubmit={save}>
      <h4>{draft.id ? 'Edit connection' : 'Add an MCP connection'}</h4>
      <label><span>Name</span><input class="input" bind:value={draft.name} maxlength="100" required /></label>
      <label><span>Transport</span>
        <select class="select" value={draft.transport} onchange={(e) => transportChanged(e.currentTarget.value as Draft['transport'])}>
          <option value="stdio">Local process (stdio)</option>
          <option value="streamable_http">Remote service (Streamable HTTP)</option>
        </select>
      </label>
      {#if draft.transport === 'stdio'}
        <label><span>Executable</span><input class="input mono" bind:value={draft.command} placeholder="npx" required /></label>
        <label><span>Arguments <span class="muted">one per line</span></span><textarea class="input mono" rows="3" bind:value={draft.args} placeholder="-y\n@company/mcp-server"></textarea></label>
        <label><span>Environment references <span class="muted">optional · CHILD=SOURCE_VARIABLE, one per line</span></span><textarea class="input mono" rows="2" bind:value={draft.environment} placeholder="SERVICE_TOKEN=MY_SERVICE_TOKEN"></textarea></label>
      {:else}
        <label><span>Service URL</span><input class="input mono" type="url" bind:value={draft.url} placeholder="https://service.example/mcp" required /></label>
      {/if}
      {#if draft.transport === 'streamable_http'}
        <label><span>Authentication</span>
          <select class="select" bind:value={draft.authMode}>
            <option value="none">No authentication</option>
            <option value="bearer">Bearer token saved in OS credential storage</option>
            <option value="environment">Bearer token from an environment variable</option>
            <option value="oauth">OAuth (sign in when testing)</option>
          </select>
        </label>
        {#if draft.authMode === 'bearer'}
          <label><span>Bearer token <span class="muted">{draft.id && connections.find(c => c.id === draft.id)?.secretStored ? 'leave blank to keep the saved token' : 'saved in the OS credential store'}</span></span><input class="input mono" type="password" autocomplete="new-password" bind:value={draft.bearerToken} /></label>
        {:else if draft.authMode === 'environment'}
          <label><span>Environment variable name</span><input class="input mono" bind:value={draft.authEnv} placeholder="SERVICE_TOKEN" /></label>
        {:else if draft.authMode === 'oauth'}
          <p class="note">Werkbord opens the service sign-in page during testing. OAuth tokens stay in the OS credential store and are refreshed by the controller.</p>
        {/if}
      {/if}
      {#if draft.transport === 'stdio'}
        <p class="note">The command runs on this computer without a shell. Only the environment references above are added to its standard process environment.</p>
      {:else}
        <p class="note">Remote connections require HTTPS. Plain HTTP is accepted only for loopback URLs.</p>
      {/if}
      <div class="row">
        <button class="btn primary" type="submit" disabled={busy || !draft.name.trim()}>{busy ? 'Saving…' : 'Save connection'}</button>
        <button class="btn" type="button" disabled={busy} onclick={() => (editing = false)}>Cancel</button>
      </div>
    </form>
  {:else}
    <div class="row"><button class="btn primary" onclick={startNew}>Add connection</button></div>
  {/if}

  {#if loading}
    <p class="muted" role="status">Loading connections…</p>
  {:else if connections.length === 0 && !editing}
    <p class="empty">No MCP connections yet. Add one to discover its tools and resources; nothing will be available to agents until you enable it.</p>
  {:else}
    <ul class="connections">
      {#each connections as c (c.id)}
        <li>
          <div class="connection-head">
            <div class="name-block">
              <h4>{c.name}</h4>
              <span class="mono muted">{c.transport === 'stdio' ? `${c.command ?? ''} ${(c.args ?? []).join(' ')}` : c.url}</span>
              <span class="status" data-tone={c.enabled ? 'ok' : 'neutral'}>{c.enabled ? 'Enabled' : 'Disabled'}</span>
            </div>
            <div class="row actions">
              <button class="btn small" disabled={busy || testing === c.id} onclick={() => test(c)}>{testing === c.id ? 'Testing…' : 'Test and discover'}</button>
              {#if c.authMode === 'oauth' && c.secretStored && c.lastError}<button class="btn small" disabled={busy || testing === c.id} onclick={() => reauthorize(c)}>{testing === c.id ? 'Signing in…' : 'Sign in again'}</button>{/if}
              <button class="btn small" disabled={busy} onclick={() => edit(c)}>Edit</button>
              <button class="btn small danger" disabled={busy} onclick={() => remove(c)}>{armedDelete === c.id ? 'Remove?' : 'Remove'}</button>
            </div>
          </div>
          {#if c.lastError}<p class="error">Last test failed: {c.lastError}</p>
          {:else if c.testedAt}<p class="note">Last tested {new Date(c.testedAt).toLocaleString()}</p>
          {:else}<p class="note">Not tested. Test the connection to review its tools and resources.</p>{/if}
          <div class="grants" aria-label="Connection access">
            <label class="check"><input type="checkbox" checked={c.enabled} disabled={busy || !c.testedAt} onchange={(e) => enable(c,e.currentTarget.checked)} /><span>Connection enabled</span></label>
            <label class="check"><input type="checkbox" checked={c.assistantEnabled} disabled={busy || !c.enabled} onchange={(e) => context(c,'assistant',e.currentTarget.checked)} /><span>Assistant</span></label>
            <label class="check"><input type="checkbox" checked={c.codingEnabled} disabled={busy || !c.enabled} onchange={(e) => context(c,'coding',e.currentTarget.checked)} /><span>Coding agents</span></label>
          </div>
          {#if (c.tools?.length ?? 0) > 0}
            <details class="discovery">
              <summary>{c.tools?.length} discovered {c.tools?.length === 1 ? 'tool' : 'tools'}</summary>
              <ul class="tool-list">
                {#each c.tools ?? [] as tool (tool.name)}
                  <li>
                    <div><strong class="mono">{tool.name}</strong>{#if tool.description}<p>{tool.description}</p>{/if}{#if tool.readOnlyHint}<span class="hint">The service describes this tool as read-only; this is an unverified hint.</span>{/if}</div>
                    <label><span>Assistant</span><select class="select small-select" aria-label="Assistant policy for {tool.name}" value={policyValue(tool.assistantPolicy)} disabled={busy || !c.enabled || !c.assistantEnabled} onchange={(e) => policy(c,tool.name,'assistant',e.currentTarget.value as MCPContextPolicy)}><option value="deny">Deny</option><option value="confirm">Confirm each call</option><option value="allow">Allow</option></select></label>
                    <label><span>Coding</span><select class="select small-select" aria-label="Coding policy for {tool.name}" value={policyValue(tool.codingPolicy)} disabled={busy || !c.enabled || !c.codingEnabled} onchange={(e) => policy(c,tool.name,'coding',e.currentTarget.value as MCPContextPolicy)}><option value="deny">Deny</option><option value="confirm">Confirm each call</option><option value="allow">Allow</option></select></label>
                  </li>
                {/each}
              </ul>
            </details>
          {/if}
          {#if (c.resources?.length ?? 0) > 0}
            <details class="discovery">
              <summary>{c.resources?.length} discovered {c.resources?.length === 1 ? 'resource' : 'resources'}</summary>
              <ul class="resource-list">
                {#each c.resources ?? [] as resource (resource.uri)}
                  <li>
                    <div><strong>{resource.title || resource.name}</strong><span class="mono uri">{resource.uri}</span>{#if resource.description}<p>{resource.description}</p>{/if}</div>
                    <button class="btn small open" disabled={!c.enabled || !c.assistantEnabled} onclick={() => readResource(c,resource.uri,'assistant')}>Read for assistant</button>
                    <button class="btn small open" disabled={!c.enabled || !c.codingEnabled} onclick={() => readResource(c,resource.uri,'coding')}>Read for coding</button>
                  </li>
                {/each}
              </ul>
            </details>
          {/if}
          {#if resourceContent?.id === c.id}<div class="resource-result"><strong class="mono">{resourceContent.uri}</strong><pre>{resourceContent.text}</pre><button class="btn small" onclick={() => (resourceContent = null)}>Close result</button></div>{/if}
        </li>
      {/each}
    </ul>
  {/if}
</section>

<style>
  h4 { margin: 0; font-size: 14px; font-weight: 600; }
  .setup-help { margin-block: 12px; }
  .setup-help summary { cursor: pointer; min-height: 44px; }
  .import-field { display: grid; gap: 6px; margin-bottom: 10px; }
  .import-field textarea { width: 100%; }
  .editor { display:grid; gap:12px; padding:14px 0; border-top:1px solid var(--border); border-bottom:1px solid var(--border); }
  .editor label { display:grid; gap:5px; font-size:12px; color:var(--text-2); }
  .editor .input, .editor .select { width:100%; min-width:0; }
  textarea.input { padding:8px 10px; resize:vertical; }
  .row { display:flex; align-items:center; gap:8px; flex-wrap:wrap; }
  .connections { list-style:none; margin:0; padding:0; display:grid; }
  .connections > li { padding:16px 0; border-top:1px solid var(--border); display:grid; gap:10px; }
  .connection-head { display:flex; align-items:flex-start; justify-content:space-between; gap:12px; flex-wrap:wrap; }
  .name-block { display:grid; gap:4px; min-width:0; }
  .name-block .mono { overflow-wrap:anywhere; }
  .status { font-size:12px; color:var(--text-2); }
  .status[data-tone='ok'] { color:var(--ok-text); }
  .grants { display:flex; gap:16px; flex-wrap:wrap; }
  .check { display:flex; align-items:center; gap:7px; min-height:32px; font-size:13px; }
  .check input { accent-color:var(--accent); }
  .discovery { padding-top:8px; border-top:1px solid var(--border); }
  summary { cursor:pointer; font-size:13px; font-weight:500; }
  .tool-list, .resource-list { list-style:none; padding:0; margin:8px 0 0; display:grid; }
  .tool-list li, .resource-list li { display:grid; grid-template-columns:minmax(0,1fr) auto auto; align-items:start; gap:12px; padding:10px 0; border-top:1px solid var(--border); }
  .tool-list p, .resource-list p { color:var(--text-2); margin:4px 0; font-size:12px; max-width:70ch; overflow-wrap:anywhere; }
  .tool-list label { display:grid; gap:4px; font-size:11.5px; color:var(--text-2); }
  .small-select { min-width:150px; }
  .hint { font-size:11.5px; color:var(--text-2); }
  .uri { display:block; overflow-wrap:anywhere; margin-top:3px; }
  .resource-result { display:grid; gap:8px; padding:10px; background:var(--surface-2); border-radius:var(--radius-sm); }
  pre { max-height:300px; overflow:auto; white-space:pre-wrap; overflow-wrap:anywhere; font:12px/1.5 var(--font-mono); }
  .empty { color:var(--text-2); margin:0; }
  @media (max-width:650px) {
    .tool-list li, .resource-list li { grid-template-columns:minmax(0,1fr); }
    .small-select { min-width:0; width:100%; }
    .actions { width:100%; }
  }
</style>

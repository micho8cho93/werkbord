<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from './api';
  import { app } from './state.svelte';
  import { deviceOperations, type DeviceRecord, type GitSnapshot } from './device-operations';
  import type { Project, Run } from './types';
  let { project }: { project: Project } = $props();
  let runnerId = $state('');
  let runId = $state('');
  let runs = $state<Run[]>([]);
  let snapshot = $state<GitSnapshot | null>(null);
  let review = $state<GitSnapshot | null>(null);
  let files = $state<string[]>([]);
  let message = $state('');
  let busy = $state(false);
  let error = $state('');
  let notice = $state('');
  let operationId = $state('');
  let pending = $state(false);
  let unacknowledged = $state(false);
  let mounted = true;
  const key = $derived(`werkbord:device-git:${project.id}`);
  const runners = $derived(app.runners.filter(r => r.kind === 'local' || r.projects.includes(project.id)));
  const target = $derived(app.runners.find(r => r.id === runnerId));
  const choices = $derived(runs.filter(r => (r.runnerId === runnerId || !r.remote && target?.kind === 'local') && ['completed','failed','stopped'].includes(r.state)));
  const changed = $derived(snapshot ? [...new Set([...snapshot.status.staged, ...snapshot.status.unstaged, ...snapshot.status.untracked].flatMap(f => f.oldPath ? [f.oldPath,f.path] : [f.path]))] : []);
  function remember(id: string) { operationId = id; try { sessionStorage.setItem(key,id); } catch { /* Storage may be disabled. The visible receipt remains available. */ } }
  function receive(record: DeviceRecord) {
    pending = record.result.state === 'pending';
    if (pending) return;
    const result = record.result;
    if (result.state !== 'done') { error = result.error || 'Inspect the execution machine before issuing another write.'; review = null; return; }
    if (result.git) {
      runnerId = result.git.runnerId; runId = result.git.runId ?? '';
      snapshot = result.git;
      review = result.git.previewId ? result.git : null;
    }
    if (['git.commit','git.push'].includes(record.operation.request.kind)) { snapshot = null; review = null; files = []; }
    if (result.message) notice = `${result.message}${result.outcome && result.outcome !== 'done' ? ` (${result.outcome})` : ''}`;
  }
  async function inspect(id = operationId) {
    if (!id || busy) return;
    busy = true; error = '';
    try { receive(await deviceOperations.get(id)); } catch(e) { error = (e as Error).message; } finally { busy = false; }
  }
  async function send(kind: string, action?: 'commit' | 'push') {
    if (busy || pending || unacknowledged || !runnerId) return;
    busy = true; error = ''; notice = '';
    const body = { requestId: crypto.randomUUID(), runnerId, projectId: project.id, runId: runId || undefined, kind, action,
      ...(kind === 'git.prepare' ? { files: action === 'commit' ? files : [], message: action === 'commit' ? message : '' } : {}),
      ...(kind === 'git.commit' || kind === 'git.push' ? { previewId: review?.previewId, confirmed: true } : {}) };
    // Persist the request before sending: a lost response must retain the same ID.
    unacknowledged = true;
    try { sessionStorage.setItem(`${key}:request`, JSON.stringify(body)); } catch { /* Receipt ID is shown once acknowledged. */ }
    try {
      const record = await deviceOperations.submit(body); remember(record.operation.id); receive(record); unacknowledged = false;
      try { sessionStorage.removeItem(`${key}:request`); } catch { /* Optional recovery storage. */ }
    } catch(e) { error = `${(e as Error).message} Check the saved request before issuing a new write.`; }
    finally { busy = false; }
  }
  async function recoverRequest() {
    if (busy) return;
    busy = true; error = '';
    try {
      const raw = sessionStorage.getItem(`${key}:request`);
      if (!raw) { notice = 'No unacknowledged request saved on this browser.'; return; }
      const record = await deviceOperations.submit(JSON.parse(raw)); remember(record.operation.id); receive(record); unacknowledged = false;
      sessionStorage.removeItem(`${key}:request`);
    } catch(e) { error = (e as Error).message; } finally { busy = false; }
  }
  function reset() { snapshot = null; review = null; files = []; notice = ''; }
  onMount(() => {
    mounted = true;
    void Promise.all([api.listRunners(),api.listProjectRuns(project.id)]).then(([r,history]) => { if (mounted) { app.runners = r; runs = history; if (!runnerId) runnerId = r.find(x => x.kind === 'local')?.id ?? r[0]?.id ?? ''; } }).catch(e => { error = (e as Error).message; });
    try { unacknowledged = !!sessionStorage.getItem(`${key}:request`); const id = sessionStorage.getItem(key); if (id) { operationId = id; void inspect(id); } } catch { /* Storage is optional. */ }
    const timer = setInterval(() => { if (pending && !busy) void inspect(); },2000);
    return () => { mounted = false; clearInterval(timer); };
  });
</script>
<section class="pn device-git" aria-label="Git on an execution machine">
  <header><h3>Git on an execution machine</h3><p class="muted">Review and confirm changes where the repository lives. Git and agent sign-ins stay on that machine.</p></header>
  <div class="body">
    <div class="targets">
      <label>Machine<select class="input" bind:value={runnerId} onchange={reset} disabled={busy || pending || unacknowledged}>{#each runners as r (r.id)}<option value={r.id}>{r.name}{r.online ? '' : ' · offline'}</option>{/each}</select></label>
      <label>Checkout<select class="input" bind:value={runId} onchange={reset} disabled={busy || pending || unacknowledged}><option value="">Registered repository</option>{#each choices as r (r.id)}<option value={r.id}>Run {r.id}</option>{/each}</select></label>
      <button class="btn" disabled={busy || pending || unacknowledged || !runnerId} onclick={() => send('git.status')}>Inspect checkout</button>
    </div>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    {#if notice}<p role="status">{notice}</p>{/if}
    {#if pending}<p class="muted" role="status">Waiting for the execution machine. This request will keep its identity across reconnects.</p>{/if}
    {#if snapshot && !review}
      <p><strong>{snapshot.status.branch || 'Detached HEAD'}</strong> · <code>{snapshot.status.head}</code></p>
      <fieldset disabled={busy || pending || unacknowledged}><legend>Files to commit</legend>
        {#each changed as path (path)}<label class="check"><input type="checkbox" bind:group={files} value={path} /> <code>{path}</code></label>{:else}<p class="muted">No uncommitted changes.</p>{/each}
      </fieldset>
      <label>Commit message<textarea class="input" rows="2" maxlength="4096" bind:value={message} disabled={busy || pending || unacknowledged}></textarea></label>
      <div class="actions"><button class="btn" disabled={busy || pending || unacknowledged || !files.length || !message.trim()} onclick={() => send('git.prepare','commit')}>Review commit</button><button class="btn" disabled={busy || pending || unacknowledged} onclick={() => send('git.prepare','push')}>Review push to origin</button></div>
    {/if}
    {#if review}
      <div class="review">
        <h4>Confirm {review.action} on {target?.name ?? review.runnerId}</h4>
        <p>{project.name} · {review.runId ? `Run ${review.runId}` : 'Registered repository'} · branch <strong>{review.status.branch}</strong></p>
        <p>HEAD <code>{review.status.head}</code></p>
        {#if review.action === 'commit'}<p>{review.message}</p><ul>{#each review.files ?? [] as path (path)}<li><code>{path}</code></li>{/each}</ul>{:else}<p>Publish this exact commit to origin, branch <strong>{review.status.branch}</strong>. No force push.</p>{/if}
        <p class="muted">Confirmation expires after five minutes. Any relevant checkout change requires another review.</p>
        <div class="actions"><button class="btn primary" disabled={busy || pending || unacknowledged} onclick={() => send(`git.${review?.action}`)}>Confirm {review.action}</button><button class="btn" disabled={busy || pending || unacknowledged} onclick={() => review = null}>Cancel review</button></div>
      </div>
    {/if}
    <div class="receipt"><button class="btn small" disabled={busy} onclick={recoverRequest}>Recover unacknowledged request</button>{#if operationId}<span class="muted">Operation <code>{operationId}</code></span><button class="btn small" disabled={busy} onclick={() => inspect()}>Check outcome</button>{/if}</div>
  </div>
</section>
<style>
  .device-git { margin-block: 16px; } header,.body { padding: 16px; } header { border-bottom: 1px solid var(--border); } header p { margin-top: 6px; max-width: 70ch; } .body { display: grid; gap: 16px; } .targets,.actions,.receipt { display: flex; flex-wrap: wrap; gap: 12px; align-items: end; } label { display: grid; gap: 6px; } .targets label { flex: 1; min-width: 180px; } fieldset { border: 0; padding: 0; display: grid; gap: 8px; } .check { display: flex; align-items: center; } .review { padding-block: 16px; border-block: 1px solid var(--border); display: grid; gap: 12px; } code { overflow-wrap: anywhere; } .receipt { align-items: center; font-size: 12px; } h3,h4,p { margin: 0; } .error { color: var(--red); } @media(max-width: 600px) { .targets { align-items: stretch; flex-direction: column; } }
</style>

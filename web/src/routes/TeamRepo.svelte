<script lang="ts">
  import { onDestroy } from 'svelte';
  import { api } from '../lib/api';
  import { embedded } from '../lib/embed';
  import { timeAgo } from '../lib/format';
  import type { GitHubStatusInfo, GitHubPR, RepoAction, RepoActionResult, RepoAdmin, RepoState } from '../lib/types';

  // A team's repository as this person's own GitHub sign-in sees it. It only reads. Whoever cannot see it here (GitHub is not
  // connected, or does not show them the repository) is sent back to what the team has reported, and told why.
  let { repo }: { repo: string } = $props();

  let view = $state<RepoState | null>(null);
  let failed = $state('');
  let loading = $state(false);
  let now = $state(Date.now());
  let tab = $state<'commits' | 'branches' | 'pulls' | 'access'>('commits');
  let admin = $state<RepoAdmin | null>(null);
  let adminFailed = $state('');
  async function loadAdmin() {
    try { admin = await api.githubRepositoryAdmin(repo); adminFailed = ''; } catch (e) { adminFailed = e instanceof Error ? e.message : String(e); }
  }

  async function load() {
    loading = true;
    try { view = await api.githubRepository(repo); failed = ''; }
    catch (e) { failed = e instanceof Error ? e.message : String(e); }
    finally { loading = false; now = Date.now(); }
  }
  $effect(() => { void repo; view = null; void load(); });

  // ---- doing things ----
  // Every change is asked for in a panel that names the repository and what will happen; the repository's name goes with the request,
  // and the first change to a repository also asks the person to say they recognise it (its address came from a teammate).
  type Pending = { kind: RepoAction['action']; title: string; say: string; confirm: string; number?: number; sha?: string; branch?: string; user?: string; sensitive?: boolean };
  let pending = $state<Pending | null>(null);
  let typed = $state('');
  let f = $state({ role: 'write' as 'read' | 'triage' | 'write' | 'maintain' | 'admin', reviews: 1, branch: '', from: '', base: '', title: '', body: '', draft: false, event: 'approve' as 'approve' | 'request_changes' | 'comment', method: 'squash' as 'merge' | 'squash' | 'rebase' });
  let recognised = $state(false);
  let acting = $state(false);
  let outcome = $state<RepoActionResult | null>(null);
  const KNOWN = 'werkbord.repo-recognised';
  const known = (): string[] => { try { return JSON.parse(localStorage.getItem(KNOWN) ?? '[]') as string[]; } catch { return []; } };
  const isKnown = $derived(known().includes(repo));
  const canPush = $derived(!!view?.repo?.permissions.push);

  function ask(p: Pending) {
    outcome = null;
    f = { role: 'write', reviews: admin?.protection?.requiredReviews || 1, branch: p.branch ?? '', from: view?.repo?.defaultBranch ?? '', base: view?.repo?.defaultBranch ?? '', title: '', body: '', draft: false, event: 'approve', method: 'squash' };
    recognised = false;
    typed = '';
    pending = p;
  }
  async function perform() {
    const p = pending;
    if (!p || acting) return;
    acting = true;
    const req: RepoAction = { repo, confirmRepo: repo, action: p.kind };
    switch (p.kind) {
      case 'create_branch': req.branch = f.branch.trim(); req.from = f.from; break;
      case 'delete_branch': req.branch = p.branch; break;
      case 'create_pr': req.branch = f.branch; req.base = f.base; req.title = f.title; req.body = f.body; req.draft = f.draft; break;
      case 'comment': req.number = p.number; req.body = f.body; break;
      case 'review': req.number = p.number; req.event = f.event; req.body = f.body; break;
      case 'merge': req.number = p.number; req.sha = p.sha; req.method = f.method; break;
      case 'update_branch': req.number = p.number; req.sha = p.sha; break;
      case 'set_role': req.user = p.user ?? typed.trim(); req.confirmTarget = typed.trim(); req.role = f.role; break;
      case 'remove_collaborator': req.user = p.user; req.confirmTarget = typed.trim(); break;
      case 'protect_branch': req.branch = p.branch; req.reviews = Number(f.reviews); break;
      case 'unprotect_branch': req.branch = p.branch; break;
    }
    try {
      outcome = await api.githubRepositoryAction(req);
      if (outcome.ok) {
        try { localStorage.setItem(KNOWN, JSON.stringify([...new Set([...known(), repo])].slice(-50))); } catch { /* Asked again next time. */ }
        pending = null;
        await load();
        if (admin) await loadAdmin();
      }
    } catch (e) {
      outcome = { action: p.kind, outcome: 'failed', ok: false, message: e instanceof Error ? e.message : String(e) };
    } finally { acting = false; }
  }
  const mayMerge = (p: GitHubPR) => canPush && p.state === 'open' && !p.draft;

  // Connecting GitHub: the same one-time-code sign-in as setup. Nothing is kept by Werkbord; the person's own gh holds the sign-in.
  let login = $state<GitHubStatusInfo | null>(null);
  let connecting = $state(false);
  let connectError = $state('');
  let poll: ReturnType<typeof setInterval> | undefined;
  async function connect() {
    connecting = true;
    connectError = '';
    try {
      await api.githubLogin();
      login = await api.github();
      poll ??= setInterval(async () => {
        try {
          login = await api.github();
          if (login.state === 'signed_in') { clearInterval(poll); poll = undefined; await load(); }
          else if (login.state !== 'signing_in') { clearInterval(poll); poll = undefined; }
        } catch { /* The next look tries again. */ }
      }, 2000);
    } catch (e) {
      connectError = e instanceof Error ? e.message : String(e);
    } finally { connecting = false; }
  }
  onDestroy(() => { if (poll) clearInterval(poll); });

  const reported = () => window.parent.postMessage({ type: 'werkbord.frame', event: 'page', page: 'reported' }, '*');
  const role = (p: NonNullable<RepoState['repo']>['permissions']) => (p.admin ? 'Admin' : p.maintain ? 'Maintain' : p.push ? 'Write' : p.triage ? 'Triage' : p.pull ? 'Read' : 'No access');
  const open = (url: string) => { window.open(url, '_blank', 'noopener'); };
</script>

<section class="repo" aria-labelledby="repo-h">
  <header>
    <div>
      <h1 id="repo-h">{repo}</h1>
      {#if view?.repo?.description}<p class="muted">{view.repo.description}</p>{/if}
    </div>
    <div class="acts">
      <button class="btn" type="button" onclick={load} disabled={loading}>Refresh</button>
      {#if view?.repo}<button class="btn" type="button" onclick={() => open(view!.repo!.url)}>Open on GitHub</button>{/if}
    </div>
  </header>

  {#if failed}
    <p class="note" role="alert">{failed}</p>
  {:else if !view}
    <p class="muted" role="status">Asking GitHub…</p>
  {:else if view.state !== 'live' || !view.repo}
    <div class="note" role="status">
      <strong>{view.state === 'not_connected' ? 'GitHub is not connected on this computer.' : view.state === 'no_access' ? 'GitHub does not show this repository to your account.' : 'GitHub could not be reached.'}</strong>
      <p>{view.message ?? ''} You can still see what the team has reported about this repository.</p>
      {#if login?.state === 'signing_in' && login.login?.code}
        <p>On GitHub, enter this code and approve:</p>
        <p class="code"><code>{login.login.code}</code></p>
        <a class="btn primary" href={login.login.url ?? 'https://github.com/login/device'} target="_blank" rel="noopener noreferrer">Open GitHub</a>
      {:else if view.state === 'not_connected'}
        <button class="btn primary" type="button" onclick={connect} disabled={connecting}>Connect GitHub</button>
      {/if}
      {#if connectError}<p role="alert">{connectError}</p>{/if}
      {#if embedded()}<button class="btn" type="button" onclick={reported}>Show what the team reported</button>{/if}
    </div>
  {:else}
    {@const r = view.repo}
    <p class="facts mm">
      <span class="chip">{role(r.permissions)}</span>
      <span>{r.private ? 'Private' : 'Public'}</span>
      <span>Default branch <code>{r.defaultBranch}</code></span>
      {#if r.pushedAt}<span>Last push {timeAgo(r.pushedAt, now)}</span>{/if}
    </p>
    <div class="bar" role="group" aria-label="Do something">
      <button class="btn" type="button" disabled={!canPush} title={canPush ? '' : 'Your role on this repository does not allow creating branches.'} onclick={() => ask({ kind: 'create_branch', title: 'New branch', say: 'Creates a branch on GitHub, starting at the latest commit of the branch you choose.', confirm: 'Create branch' })}>New branch</button>
      <button class="btn" type="button" onclick={() => ask({ kind: 'create_pr', title: 'New pull request', say: 'Opens a pull request on GitHub from a branch that is already there.', confirm: 'Open pull request' })}>New pull request</button>
    </div>
    {#if outcome && !outcome.ok && !pending}<p class="note" role="alert"><strong>{outcome.outcome === 'refused' ? 'Not done.' : outcome.outcome === 'unverified' ? 'Not confirmed.' : 'GitHub said no.'}</strong> {outcome.message}</p>{/if}
    {#if outcome?.ok}<p class="done" role="status">{outcome.message}</p>{/if}
    <nav class="tabs" aria-label="Repository sections">
      <button type="button" aria-current={tab === 'commits' ? 'page' : undefined} onclick={() => (tab = 'commits')}>Commits<span>{r.commits.length}</span></button>
      <button type="button" aria-current={tab === 'branches' ? 'page' : undefined} onclick={() => (tab = 'branches')}>Branches<span>{r.branches.length}</span></button>
      <button type="button" aria-current={tab === 'pulls' ? 'page' : undefined} onclick={() => (tab = 'pulls')}>Pull requests<span>{r.pullRequests.length}</span></button>
      <button type="button" aria-current={tab === 'access' ? 'page' : undefined} onclick={() => { tab = 'access'; if (!admin) void loadAdmin(); }}>Access &amp; settings</button>
    </nav>
    {#if tab === 'commits'}
      <ul class="list">
        {#each r.commits as c (c.sha)}
          <li><div><strong>{c.message}</strong><span class="muted">{c.author}{c.at ? ' · ' + timeAgo(c.at, now) : ''}</span></div><code>{c.sha.slice(0, 7)}</code></li>
        {:else}<li class="muted">No commits yet.</li>{/each}
      </ul>
    {:else if tab === 'branches'}
      <ul class="list">
        {#each r.branches as b (b.name)}
          <li><div><strong>{b.name}</strong>{#if b.name === r.defaultBranch}<span class="chip">default</span>{/if}{#if b.protected}<span class="chip">protected</span>{/if}</div><span class="row-acts"><code>{b.sha.slice(0, 7)}</code>{#if b.name !== r.defaultBranch && !b.protected}<button class="btn quiet small" type="button" disabled={!canPush} title={canPush ? '' : 'Your role on this repository does not allow deleting branches.'} onclick={() => ask({ kind: 'delete_branch', branch: b.name, title: 'Delete branch ' + b.name, say: 'Deletes the branch ' + b.name + ' on GitHub. Its commits stay in any pull request that used it, but the branch is gone for everyone.', confirm: 'Delete branch' })}>Delete</button>{/if}</span></li>
        {:else}<li class="muted">No branches yet.</li>{/each}
      </ul>
    {:else if tab === 'access'}
      {#if adminFailed}<p class="note" role="alert">{adminFailed}</p>
      {:else if !admin}<p class="muted" role="status">Asking GitHub…</p>
      {:else}
        <h2 class="sec">People with access</h2>
        {#if admin.collabNote}<p class="muted">{admin.collabNote}</p>{/if}
        <ul class="list">
          {#each admin.collaborators as c (c.login)}
            <li><div><strong>{c.login}</strong></div><span class="row-acts"><span class="chip">{c.role}</span>
              {#if admin.canAdmin}
                <button class="btn quiet small" type="button" onclick={() => ask({ kind: 'set_role', user: c.login, title: 'Change role of ' + c.login, say: 'Changes what ' + c.login + ' can do in this repository on GitHub.', confirm: 'Change role', sensitive: true })}>Change role</button>
                <button class="btn quiet small" type="button" onclick={() => ask({ kind: 'remove_collaborator', user: c.login, title: 'Remove ' + c.login, say: 'Takes away ' + c.login + '’s access to this repository on GitHub.', confirm: 'Remove', sensitive: true })}>Remove</button>
              {/if}</span></li>
          {:else}<li class="muted">Nobody is listed.</li>{/each}
        </ul>
        {#if admin.canAdmin}<div class="bar"><button class="btn" type="button" onclick={() => ask({ kind: 'set_role', title: 'Invite someone', say: 'Gives someone access to this repository on GitHub. If they are not a collaborator yet, GitHub invites them.', confirm: 'Invite', sensitive: true })}>Invite someone</button></div>{/if}

        <h2 class="sec">Rule on {admin.branch}</h2>
        {#if admin.protectNote}<p class="muted">{admin.protectNote}</p>{/if}
        {#if admin.protection}
          <p>{admin.protection.protected ? 'Needs ' + admin.protection.requiredReviews + ' approving ' + (admin.protection.requiredReviews === 1 ? 'review' : 'reviews') + ' before a pull request can be merged.' : 'No rule: anyone with write access can push to it.'}</p>
          <div class="bar">
            <button class="btn" type="button" onclick={() => ask({ kind: 'protect_branch', branch: admin!.branch, title: 'Rule on ' + admin!.branch, say: 'Requires approving reviews before a pull request can be merged into ' + admin!.branch + '. Administrators are not bound by it.', confirm: 'Set rule' })}>{admin.protection.protected ? 'Change rule' : 'Require reviews'}</button>
            {#if admin.protection.protected}<button class="btn" type="button" onclick={() => ask({ kind: 'unprotect_branch', branch: admin!.branch, title: 'Remove the rule on ' + admin!.branch, say: 'Anyone with write access will be able to push to ' + admin!.branch + ' directly.', confirm: 'Remove rule', sensitive: true })}>Remove rule</button>{/if}
          </div>
        {/if}

        {#if admin.pages}
          <h2 class="sec">GitHub Pages</h2>
          {#if admin.pages.enabled}<p>On{#if admin.pages.url}: <a href={admin.pages.url} target="_blank" rel="noopener noreferrer">{admin.pages.url}</a>{/if}</p>
          {:else if admin.canAdmin}<p>Off.</p><div class="bar"><button class="btn" type="button" onclick={() => ask({ kind: 'enable_pages', title: 'Turn on GitHub Pages', say: 'Publishes a site for this repository on GitHub Pages, deployed by a workflow. Anyone can read a published site.', confirm: 'Turn on Pages', sensitive: true })}>Turn on Pages</button></div>
          {:else}<p>Off.</p>{/if}
        {/if}
      {/if}
    {:else}
      <ul class="list">
        {#each r.pullRequests as p (p.number)}
          <li><div><strong>#{p.number} {p.title}</strong><span class="muted">{p.headBranch} → {p.baseBranch}{p.author ? ' · ' + p.author : ''}</span></div><span class="row-acts"><span class="chip" data-state={p.state}>{p.draft ? 'draft' : p.state}</span>
            {#if p.state === 'open'}
              <button class="btn quiet small" type="button" onclick={() => ask({ kind: 'comment', number: p.number, title: 'Comment on #' + p.number, say: 'Adds a comment to #' + p.number + ' on GitHub, under your name.', confirm: 'Comment' })}>Comment</button>
              <button class="btn quiet small" type="button" onclick={() => ask({ kind: 'review', number: p.number, title: 'Review #' + p.number, say: 'Records your review of #' + p.number + ' on GitHub, under your name.', confirm: 'Submit review' })}>Review</button>
              <button class="btn quiet small" type="button" disabled={!mayMerge(p) || !p.headSha} title={!canPush ? 'Your role on this repository does not allow merging.' : p.draft ? 'A draft cannot be merged.' : ''} onclick={() => ask({ kind: 'merge', number: p.number, sha: p.headSha, title: 'Merge #' + p.number, say: 'Merges #' + p.number + ' (' + p.headBranch + ' into ' + p.baseBranch + ') on GitHub. If it has changed since you looked, GitHub refuses.', confirm: 'Merge' })}>Merge</button>
              <button class="btn quiet small" type="button" disabled={!canPush || !p.headSha} onclick={() => ask({ kind: 'update_branch', number: p.number, sha: p.headSha, title: 'Update #' + p.number, say: 'Asks GitHub to bring ' + p.headBranch + ' up to date with ' + p.baseBranch + '.', confirm: 'Update branch' })}>Update branch</button>
            {/if}</span></li>
        {:else}<li class="muted">No pull requests.</li>{/each}
      </ul>
    {/if}
  {/if}
</section>

{#if pending && view?.repo}
  <div class="scrim">
    <div class="panel" role="dialog" aria-modal="true" aria-labelledby="act-h" tabindex="-1">
    <form onsubmit={(e) => { e.preventDefault(); void perform(); }}>
      <h2 id="act-h">{pending.title}</h2>
      <p class="where">On <strong>{repo}</strong>{view.repo.private ? ' (private)' : ''}. {pending.say}</p>
      {#if pending.kind === 'create_branch'}
        <label>New branch name<input bind:value={f.branch} required maxlength="200" autocomplete="off" /></label>
        <label>Start from<select bind:value={f.from}>{#each view.repo.branches as b (b.name)}<option>{b.name}</option>{/each}</select></label>
      {:else if pending.kind === 'create_pr'}
        <label>Branch with the changes<select bind:value={f.branch} required><option value="" disabled>Choose a branch</option>{#each view.repo.branches.filter(b => b.name !== f.base) as b (b.name)}<option>{b.name}</option>{/each}</select></label>
        <label>Into<select bind:value={f.base}>{#each view.repo.branches as b (b.name)}<option>{b.name}</option>{/each}</select></label>
        <label>Title<input bind:value={f.title} required maxlength="256" /></label>
        <label>Description<textarea bind:value={f.body} rows="4" maxlength="20000"></textarea></label>
        <label class="check"><input type="checkbox" bind:checked={f.draft} /> Open as a draft</label>
      {:else if pending.kind === 'comment'}
        <label>Comment<textarea bind:value={f.body} rows="4" required maxlength="20000"></textarea></label>
      {:else if pending.kind === 'review'}
        <label>Your review<select bind:value={f.event}><option value="approve">Approve</option><option value="request_changes">Request changes</option><option value="comment">Comment only</option></select></label>
        <label>Message{f.event === 'approve' ? ' (optional)' : ''}<textarea bind:value={f.body} rows="4" required={f.event !== 'approve'} maxlength="20000"></textarea></label>
      {:else if pending.kind === 'set_role'}
        {#if !pending.user}<label>GitHub user name<input bind:value={typed} required maxlength="39" autocomplete="off" /></label>{/if}
        <label>Role<select bind:value={f.role}><option value="read">Read</option><option value="triage">Triage</option><option value="write">Write</option><option value="maintain">Maintain</option><option value="admin">Admin</option></select></label>
        {#if pending.user}<label>Type <strong>{pending.user}</strong> to confirm<input bind:value={typed} required autocomplete="off" /></label>{/if}
      {:else if pending.kind === 'remove_collaborator'}
        <label>Type <strong>{pending.user}</strong> to confirm<input bind:value={typed} required autocomplete="off" /></label>
      {:else if pending.kind === 'protect_branch'}
        <label>Approving reviews needed<select bind:value={f.reviews}>{#each [1, 2, 3, 4, 5, 6] as n (n)}<option value={n}>{n}</option>{/each}</select></label>
      {:else if pending.kind === 'merge'}
        <label>How<select bind:value={f.method}><option value="squash">Squash and merge</option><option value="merge">Create a merge commit</option><option value="rebase">Rebase and merge</option></select></label>
      {/if}
      {#if !isKnown}<label class="check"><input type="checkbox" bind:checked={recognised} required /> I recognise <strong>{repo}</strong> as this team's repository. Its address was set by someone on the team.</label>{/if}
      {#if outcome && !outcome.ok}<p class="note" role="alert"><strong>{outcome.outcome === 'refused' ? 'Not done.' : outcome.outcome === 'unverified' ? 'Not confirmed.' : 'GitHub said no.'}</strong> {outcome.message}</p>{/if}
      <div class="foot">
        <button class="btn" type="button" onclick={() => { pending = null; outcome = null; }}>Cancel</button>
        <button class="btn primary" type="submit" disabled={acting}>{acting ? 'Working…' : pending.confirm}</button>
      </div>
    </form>
    </div>
  </div>
{/if}

<style>
  .bar { display: flex; gap: 8px; margin: 0 0 12px; }
.sec { font-size: 14px; margin: 20px 0 8px; }
  .done { color: var(--ok-text, var(--text)); margin: 0 0 12px; }
  .row-acts { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; justify-content: flex-end; }
  .scrim { position: fixed; inset: 0; z-index: 50; display: grid; place-items: center; background: color-mix(in srgb, var(--text) 35%, transparent); padding: 16px; }
  .panel { width: min(520px, 100%); max-height: 90vh; overflow: auto; display: grid; gap: 12px; padding: 20px; border: 1px solid var(--border); border-radius: var(--radius); background: var(--surface); box-shadow: var(--win-sh); }
  .panel form { display: grid; gap: 12px; }
  .panel h2 { margin: 0; font-size: 16px; }
  .panel .where { margin: 0; color: var(--text-2); }
  .panel label { display: grid; gap: 4px; font-size: 13px; }
  .panel label.check { display: flex; align-items: flex-start; gap: 8px; }
  .panel input:not([type='checkbox']), .panel select, .panel textarea { font: inherit; padding: 8px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface); color: var(--text); }
  .foot { display: flex; justify-content: flex-end; gap: 8px; }
  .repo { padding: 28px clamp(16px, 4vw, 48px); max-width: 960px; }
  header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 16px; }
  h1 { margin: 0; font-size: 20px; }
  .muted { color: var(--text-2); font-size: 12px; margin: 4px 0 0; }
  .acts { display: flex; gap: 8px; }
  .facts { display: flex; flex-wrap: wrap; gap: 4px 14px; align-items: center; margin: 0 0 16px; color: var(--text-2); font-size: 12px; }
  .chip { display: inline-block; padding: 1px 8px; border-radius: 999px; background: var(--surface-2); color: var(--text-2); font-size: 12px; margin-left: 6px; }
  .facts .chip { margin-left: 0; }
  .note { padding: 16px; border: 1px solid var(--border); border-radius: var(--radius); background: var(--surface); }
  .note p { margin: 6px 0 12px; color: var(--text-2); }
  .tabs { display: flex; gap: 16px; border-bottom: 1px solid var(--border); margin-bottom: 8px; }
  .tabs button { border: 0; background: transparent; color: var(--text-2); padding: 8px 4px; font: inherit; cursor: pointer; border-bottom: 2px solid transparent; }
  .tabs button[aria-current='page'] { color: var(--text); border-bottom-color: var(--accent); }
  .tabs span { margin-left: 6px; font-size: 12px; color: var(--text-2); }
  .list { list-style: none; margin: 0; padding: 0; border: 1px solid var(--border); border-radius: var(--radius); background: var(--surface); }
  .list li { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 12px 16px; }
  .list li + li { border-top: 1px solid var(--border); }
  .list div { display: grid; gap: 2px; min-width: 0; }
  .list strong { font-weight: 500; overflow-wrap: anywhere; }
  .code code { font-size: 20px; letter-spacing: 0.1em; color: var(--text); }
  code { font-family: var(--mono); font-size: 12px; color: var(--text-2); }
</style>

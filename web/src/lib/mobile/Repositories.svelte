<script lang="ts">
  import { untrack } from 'svelte';
  import { timeAgo } from '../format';
  import { gitStore } from '../git/store.svelte';
  import { branchHref } from '../gitroute';
  import { checksLabel, prStateLabel, reviewLabel } from '../gitui';
  import Icon from '../Icon.svelte';
  import { projectHref } from '../router.svelte';
  import { app } from '../state.svelte';
  import RepoCard from './RepoCard.svelte';
  import { branchesNeedingYou, latestCommits, openPullRequests, type RepoSource, type RepoView } from './repositories';
  import Segmented from '../Segmented.svelte';

  // The Git Control Center from the phone, four ways: how every repository stands, the open pull requests, the branches that want you
  // and the latest commits. Each row opens the Control Center itself (the branch, the repository), where the diffs and every action are,
  // each with the confirmation it already has. Nothing here changes a repository.
  const repos = $derived(app.projects.filter((p) => p.kind !== 'work'));
  const risks = $derived(app.overview?.repository ?? []);
  let view = $state<RepoView>('overview');

  // Looking at Git starts every repository's store loading, and keeps it current while it is looked at; GitHub is asked once, apart.
  $effect(() => {
    const stops = repos.map((p) => gitStore(p.id).watch());
    untrack(() => {
      for (const p of repos) {
        const s = gitStore(p.id);
        if (!s.github && !s.githubLoading) void s.loadGitHub();
      }
    });
    return () => stops.forEach((stop) => stop());
  });

  const sources = $derived<RepoSource[]>(repos.map((p) => ({ project: { id: p.id, name: p.name }, overview: gitStore(p.id).overview, github: gitStore(p.id).github })));
  const prs = $derived(openPullRequests(sources));
  const branches = $derived(branchesNeedingYou(sources));
  const commits = $derived(latestCommits(sources));
  const githubAsked = $derived(sources.some((s) => s.github?.available));
  const views = $derived([
    { id: 'overview', label: 'Overview' },
    { id: 'prs', label: 'Pull requests', count: prs.length || undefined },
    { id: 'branches', label: 'Branches', count: branches.length || undefined },
    { id: 'commits', label: 'Commits' },
  ] as const);
</script>

<div class="repos">
  {#if app.projects.length > 0 && repos.length === 0}
    <p class="note">None of your projects has a Git repository. Add a repository project to see its branches and pull requests here.</p>
  {:else if app.projects.length === 0}
    <p class="note">Add a project to see its Git state here.</p>
  {:else}
    <Segmented options={views} value={view} onchange={(id: RepoView) => (view = id)} label="Git" />

    {#if view === 'overview'}
      {#if risks.length}
        <section aria-labelledby="git-attn">
          <h2 id="git-attn" class="lab">Needs attention <span class="num pend">{risks.length}</span></h2>
          <ul class="risks">
            {#each risks.slice(0, 5) as f (f.id)}
              <li class="card risk" data-testid="repo-risk">
                <p class="kind"><span class="dot" data-tone="bad" aria-hidden="true"></span><b>{f.title}</b></p>
                <p class="mm">{f.projectName ?? app.project(f.projectId)?.name ?? ''}</p>
                <p class="expl">{f.explanation}</p>
                <p class="next"><span class="mm">Next</span> {f.action.label}</p>
                <a class="btn small open" href={projectHref(f.projectId, 'git')} onclick={() => app.enter(f.projectId)}>Open Git<Icon name="right" size={14} /></a>
              </li>
            {/each}
          </ul>
          {#if risks.length > 5}<p class="note">and {risks.length - 5} more, in each repository below.</p>{/if}
        </section>
      {/if}
      <section aria-labelledby="git-repos">
        <h2 id="git-repos" class="lab">Repositories</h2>
        <ul>{#each repos as p (p.id)}<RepoCard project={p} />{/each}</ul>
      </section>
    {:else if view === 'prs'}
      {#if !githubAsked}
        <p class="note">Pull requests come from GitHub, and it is not connected for any of these repositories yet. Connect it in Settings.</p>
      {:else if prs.length === 0}
        <p class="note">No pull request is open.</p>
      {:else}
        <ul>
          {#each prs as { project, pr } (project.id + pr.number)}
            {@const st = prStateLabel(pr)}
            {@const rv = reviewLabel(pr.review)}
            {@const ck = checksLabel(pr.checks)}
            <li class="card row" data-testid="pr">
              <a href={projectHref(project.id, 'git')} onclick={() => app.enter(project.id)}>
                <span class="top"><span class="dot" data-tone={st.tone} aria-hidden="true"></span><b class="title">#{pr.number} {pr.title}</b></span>
                <span class="meta mm">{project.name} · {pr.headBranch} → {pr.baseBranch}</span>
                <span class="meta mm">{st.text}{rv ? ` · ${rv.text}` : ''}{ck ? ` · ${ck.text}` : ''}{pr.updatedAt ? ` · ${timeAgo(pr.updatedAt, app.now)}` : ''}</span>
              </a>
            </li>
          {/each}
        </ul>
      {/if}
    {:else if view === 'branches'}
      {#if branches.length === 0}
        <p class="note">No branch needs you.</p>
      {:else}
        <ul>
          {#each branches as { project, branch, reason } (project.id + branch.name)}
            <li class="card row" data-testid="branch">
              <a href={branchHref(project.id, branch.scope, branch.name)} onclick={() => app.enter(project.id)}>
                <span class="top"><Icon name="branch" size={14} /><b class="title mono">{branch.name}</b></span>
                <span class="meta mm">{project.name}{branch.vsTarget.ahead ? ` · ${branch.vsTarget.ahead} ahead` : ''}{branch.notPushed ? ` · ${branch.notPushed} not pushed` : ''}</span>
                {#if reason}<span class="why">{reason}</span>{/if}
              </a>
            </li>
          {/each}
        </ul>
      {/if}
    {:else if commits.length === 0}
      <p class="note">No commits read yet.</p>
    {:else}
      <ul>
        {#each commits as { project, commit } (project.id + commit.sha)}
          <li class="card row" data-testid="commit">
            <a href={projectHref(project.id, 'git')} onclick={() => app.enter(project.id)}>
              <span class="top"><span class="sha mono">{commit.sha.slice(0, 7)}</span><b class="title">{commit.subject}</b></span>
              <span class="meta mm">{project.name} · {commit.author} · {timeAgo(commit.date, app.now)}</span>
            </a>
          </li>
        {/each}
      </ul>
    {/if}
  {/if}
</div>

<style>
  .repos {
    display: grid;
    gap: 14px;
    padding: 4px 16px 24px;
  }

  section {
    display: grid;
    gap: 8px;
  }

  h2 {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 0;
  }

  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 10px;
  }

  .note {
    margin: 0;
    color: var(--text-2);
    font-size: 13px;
  }

  .risk {
    display: grid;
    gap: 6px;
    padding: 14px;
    border-color: color-mix(in srgb, var(--danger) 40%, var(--border));
  }

  .risk p {
    margin: 0;
  }

  .kind {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 14px;
  }

  .expl {
    font-size: 13px;
  }

  .risk .btn {
    justify-self: start;
    min-height: 44px;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    text-decoration: none;
  }

  .row a {
    display: grid;
    gap: 4px;
    min-height: 56px;
    padding: 12px 14px;
    color: var(--text);
    text-decoration: none;
    -webkit-tap-highlight-color: transparent;
  }

  .row a:active {
    background: var(--surface-2);
    box-shadow: var(--press-sh);
    border-radius: var(--radius);
  }

  .top {
    display: flex;
    align-items: baseline;
    gap: 8px;
  }

  .title {
    font-size: 14px;
    font-weight: 500;
    overflow-wrap: anywhere;
  }

  .meta,
  .why {
    overflow-wrap: anywhere;
  }

  .why {
    font-size: 13px;
    color: var(--warn-text);
  }

  .sha {
    font-size: 11.5px;
    color: var(--text-2);
  }
</style>

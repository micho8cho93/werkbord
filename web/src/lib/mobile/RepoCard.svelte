<script lang="ts">
  import { untrack } from 'svelte';
  import { timeAgo } from '../format';
  import { gitStore } from '../git/store.svelte';
  import { prStateLabel, reviewLabel, checksLabel } from '../gitui';
  import Icon from '../Icon.svelte';
  import { projectHref } from '../router.svelte';
  import { app } from '../state.svelte';
  import type { Project } from '../types';
  import { repoDigest, workingLine } from './repositories';

  // One repository: where it stands (branch, uncommitted work, what is ahead or behind), what its health says, its open pull requests
  // and last commits, all as the Git Control Center reads them. The card opens that Control Center, where the diffs and every action
  // (each with its own confirmation) are; nothing is changed from here.
  let { project }: { project: Project } = $props();
  const store = $derived(gitStore(project.id));
  $effect(() => store.watch());
  $effect(() => {
    const s = store;
    untrack(() => {
      if (!s.github && !s.githubLoading) void s.loadGitHub();
    });
  });
  const d = $derived(repoDigest(store.overview, store.github, store.health));
  const commits = $derived((store.overview?.local.recentCommits ?? []).slice(0, 3));
  const prs = $derived(d?.openPRs ?? []);
  // Health is its own call and may be off or fail; say so rather than "not checked".
  const healthLine = $derived(store.healthDisabled ? 'Repository health is not turned on' : store.healthError ? `Health could not be read: ${store.healthError}` : (d?.line ?? ''));
</script>

<li class="card repo" data-testid="repo" data-tone={d?.tone ?? 'neutral'}>
  <a class="head" href={projectHref(project.id, 'git')} onclick={() => app.enter(project.id)}>
    <span class="dot" data-tone={d?.tone ?? 'neutral'} aria-hidden="true"></span>
    <span class="name">{project.name}</span>
    <Icon name="right" size={16} />
  </a>
  {#if !d}
    <p class="note mm">{store.error || 'Reading the repository…'}</p>
  {:else}
    <p class="line"><Icon name="branch" size={14} /><span class="mono">{d.branch}</span></p>
    <p class="meta mm">{workingLine(d)}{d.conflicted ? ` · ${d.conflicted} in conflict` : ''}</p>
    <p class="health" data-tone={d.tone}>{healthLine}</p>
    {#if d.branchesNeedingYou}<p class="meta mm">{d.branchesNeedingYou} {d.branchesNeedingYou === 1 ? 'branch needs' : 'branches need'} you</p>{/if}

    {#if d.openPRs === null}
      <p class="meta mm">Pull requests: {store.github ? (store.github.message ?? 'GitHub is not connected for this repository.') : 'asking GitHub…'}</p>
    {:else if prs.length}
      <div class="prs">
        <span class="lab">Pull requests</span>
        <ul>
          {#each prs.slice(0, 3) as pr (pr.number)}
            {@const st = prStateLabel(pr)}
            {@const ck = checksLabel(pr.checks)}
            {@const rv = reviewLabel(pr.review)}
            <li>
              <span class="dot" data-tone={st.tone} aria-hidden="true"></span>
              <span class="pt"><b>#{pr.number}</b> {pr.title}</span>
              <span class="mm">{st.text}{rv ? ` · ${rv.text}` : ''}{ck ? ` · ${ck.text}` : ''}</span>
            </li>
          {/each}
        </ul>
        {#if prs.length > 3}<p class="meta mm">and {prs.length - 3} more</p>{/if}
      </div>
    {/if}

    {#if commits.length}
      <div class="commits">
        <span class="lab">Recent commits</span>
        <ul>
          {#each commits as c (c.sha)}
            <li><span class="sha mono">{c.sha.slice(0, 7)}</span><span class="cs">{c.subject}</span><span class="mm">{timeAgo(c.date, app.now)}</span></li>
          {/each}
        </ul>
      </div>
    {/if}
  {/if}
</li>

<style>
  .repo {
    display: grid;
    gap: 6px;
    padding: 4px 14px 14px;
    list-style: none;
  }

  .head {
    display: flex;
    align-items: center;
    gap: 10px;
    min-height: 48px;
    color: var(--text);
    text-decoration: none;
  }

  .head :global(svg) {
    margin-left: auto;
    color: var(--text-2);
  }

  .name {
    font-size: 15px;
    font-weight: 600;
  }

  p {
    margin: 0;
  }

  .line {
    display: flex;
    align-items: center;
    gap: 6px;
    color: var(--text-2);
  }

  .line .mono {
    color: var(--text);
    overflow-wrap: anywhere;
  }

  .health {
    font-size: 13px;
  }

  .health[data-tone='bad'] {
    color: var(--danger-text);
  }

  .health[data-tone='ask'] {
    color: var(--warn-text);
  }

  .health[data-tone='ok'] {
    color: var(--ok-text);
  }

  .prs,
  .commits {
    display: grid;
    gap: 6px;
    margin-top: 6px;
  }

  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 6px;
  }

  .prs li {
    display: grid;
    grid-template-columns: auto 1fr;
    column-gap: 8px;
    align-items: baseline;
  }

  .prs li .mm {
    grid-column: 2;
  }

  .pt,
  .cs {
    overflow-wrap: anywhere;
    font-size: 13px;
  }

  .commits li {
    display: grid;
    grid-template-columns: auto 1fr auto;
    gap: 8px;
    align-items: baseline;
  }

  .sha {
    font-size: 11.5px;
    color: var(--text-2);
  }
</style>

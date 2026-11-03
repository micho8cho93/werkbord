<script lang="ts">
  import Icon from '../lib/Icon.svelte';
  import AgentMark from '../lib/AgentMark.svelte';
  import Avatar from '../lib/Avatar.svelte';
  import { agentName } from '../lib/agent-marks';
  import { resolveAgentState } from '../lib/identity';
  import { model } from './model.svelte';
  import { actionable } from './aggregate';
  import { pending } from './decisions';
  import { decisions } from './decisions.svelte';
  import { agentUses, crumbs, describeParticipant, participants, personalGit, runningCount, teamGit, type AgentUse } from './context-header';
  import { gitHead } from './git-head.svelte';
  import HeaderPopover from '../lib/HeaderPopover.svelte';
  import ContextBar from '../lib/ContextBar.svelte';
  import ConnectedTools from './ConnectedTools.svelte';
  import WorkspaceTag from './WorkspaceTag.svelte';

  // The header: where the person is, and what is going on in the project context in view. Everything here comes from the scoped overview
  // the window already has: no new request for agents or people, and Git is read once per project and on focus (git-head.svelte.ts).
  // It states what a workspace reported. A person is shown by role and by what is assigned to them, never by whether they are around;
  // an agent animates only when its source says the report is fresh.
  const MARKS = 4;
  const unavailable = $derived((model.contextOverview?.entries ?? []).filter(e => e.error || !e.summary));
  const overview = $derived(model.contextOverview);
  const selected = $derived(model.selectedProject);

  let now = $state(Date.now());
  $effect(() => { const timer = setInterval(() => (now = Date.now()), 30_000); return () => clearInterval(timer); });

  const uses = $derived(agentUses(overview));
  const running = $derived(runningCount(uses));
  const needsYou = $derived(
    pending(decisions.list, selected, new Set(Object.keys(decisions.answered))).length
    + (overview?.entries ?? []).reduce((n, e) => n + (e.summary?.attention ?? []).filter(a => !a.question && a.kind !== 'needs_input' && actionable(a)).length, 0),
  );
  const people = $derived(participants(overview));

  // Git: one repository, only when a project is chosen. Several repositories are never merged into one branch.
  $effect(() => { void gitHead.load(selected?.workspace === 'personal' ? selected.project : null); });
  $effect(() => {
    const refresh = () => { if (!document.hidden && selected?.workspace === 'personal') void gitHead.load(selected.project); };
    window.addEventListener('focus', refresh);
    document.addEventListener('visibilitychange', refresh);
    return () => { window.removeEventListener('focus', refresh); document.removeEventListener('visibilitychange', refresh); };
  });
  const git = $derived(selected ? (selected.workspace === 'personal' ? personalGit(gitHead.head) : teamGit(overview, selected.project, now)) : null);

  const current = $derived(model.view?.items.find(i => i.id === model.current));
  const trail = $derived(crumbs({ context: model.contextLabel, page: model.page, place: model.places[model.current] ?? '', team: current?.kind === 'team', onGit: model.onGit, title: model.frameTitle || undefined }));
  const workspaceTag = $derived(selected && (model.view?.items.filter(i => !['setup', 'leaving'].includes(i.state)).length ?? 0) > 1 ? model.view?.items.find(i => i.id === selected.workspace) : undefined);

  const shown = $derived(uses.slice(0, MARKS));
  const view = (u: AgentUse) => resolveAgentState({ state: u.state, fresh: u.fresh, observedAt: u.observedAt, now, agentName: agentName(u.agentId) });
  function openUse(u: AgentUse) {
    if (model.paneable) void model.openPane(u.workspace, u.href);
    else void model.goTo(u.workspace, u.href);
  }
</script>

{#snippet gitLines()}
  {#if git}
    <p class="line"><Icon name="git" size={13} /><strong>{git.text}</strong></p>
    {#if git.provenance}<p class="note">{git.provenance[0].toUpperCase() + git.provenance.slice(1)}. It is a member's report, not checked against GitHub.</p>
    {:else}<p class="note">The branch checked out on this computer.</p>{/if}
  {:else}<p class="note">{selected ? 'No Git repository to show for this project.' : 'Choose a project to see its branch.'}</p>{/if}
{/snippet}
{#snippet agentList()}
  {#if uses.length}
    <ul class="list">
      {#each uses as u (u.key)}
        {@const v = view(u)}
        <li><button type="button" class="row" onclick={() => openUse(u)}>
          <AgentMark agentId={u.agentId} state={u.state} fresh={u.fresh} observedAt={u.observedAt} {now} decorative />
          <span class="what"><strong>{agentName(u.agentId)} <span class="state">{v.text}</span></strong><span class="where">{u.title} · {u.project}</span></span>
        </button></li>
      {/each}
    </ul>
  {:else}<p class="note">No agent is working on this project right now.</p>{/if}
{/snippet}
{#snippet peopleList()}
  <ul class="list">
    {#each people as p (p.key)}
      <li class="row person"><Avatar id={p.id} name={p.name} size="sm" />
        <span class="what"><strong>{p.name}</strong><span class="where">{describeParticipant(p)}</span></span></li>
    {/each}
  </ul>
{/snippet}

{#if !['settings', 'help', 'workspaces'].includes(model.page)}
  <ContextBar crumbs={trail} {running} {needsYou}>
    {#snippet tag()}{#if workspaceTag}<WorkspaceTag workspace={workspaceTag} />{/if}{/snippet}
    {#snippet notices()}
      {#if model.selectedProject && !model.projects.some(p => p.workspace === model.selectedProject?.workspace && p.project === model.selectedProject?.project)}
        <span role="status">This project is unavailable. Reconnect its workspace or choose another project.</span>
      {/if}
      {#each unavailable as e (e.workspace.id)}<span role="status">{e.workspace.name}: {e.error} Its work is excluded.</span>{/each}
    {/snippet}
      {#if selected && git}
        <HeaderPopover label="Git: {git.text}" testid="header-git">
          {#snippet trigger()}<Icon name="git" size={13} /><span class="git-text">{git.text.split(' · ')[0]}<span class="sha"> · {git.text.split(' · ')[1] ?? ''}</span>{#if git.provenance}<span class="age"> · {git.provenance.split(' · ')[0]}</span>{/if}</span>{/snippet}
          {@render gitLines()}
        </HeaderPopover>
      {/if}
      {#if uses.length}
        <HeaderPopover label="Agents on this project ({uses.length})" testid="header-agents">
          {#snippet trigger()}<span class="cluster">{#each shown as u (u.key)}<AgentMark agentId={u.agentId} state={u.state} fresh={u.fresh} observedAt={u.observedAt} {now} decorative />{/each}{#if uses.length > MARKS}<span class="more">+{uses.length - MARKS}</span>{/if}</span><span class="visually-hidden">Agents</span>{/snippet}
          {@render agentList()}
        </HeaderPopover>
      {/if}
      {#if selected?.workspace === 'personal'}
        <HeaderPopover label="Agents and connections" testid="header-tools">
          {#snippet trigger()}<span>Agents & connections</span>{/snippet}
          <ConnectedTools project={selected.project} />
        </HeaderPopover>
      {/if}
      {#if people.length}
        <HeaderPopover label="People on this project ({people.length})" testid="header-people">
          {#snippet trigger()}<span class="cluster">{#each people.slice(0, MARKS) as p (p.key)}<Avatar id={p.id} name={p.name} size="sm" />{/each}{#if people.length > MARKS}<span class="more">+{people.length - MARKS}</span>{/if}</span><span class="visually-hidden">People</span>{/snippet}
          {@render peopleList()}
        </HeaderPopover>
      {/if}
    {#snippet compact(counts)}
      <HeaderPopover label="Context: {running} running, {needsYou} {needsYou === 1 ? 'needs' : 'need'} you" testid="header-context-button">
        {#snippet trigger()}<span>Context</span>{#if running || needsYou}<span class="badge">{running + needsYou}</span>{/if}{/snippet}
        <p class="heading">Now</p><p class="line">{@render counts()}</p>
        <p class="heading">Git</p>{@render gitLines()}
        <p class="heading">Agents</p>{@render agentList()}
        {#if people.length}<p class="heading">People</p>{@render peopleList()}{/if}
        {#if selected?.workspace === 'personal'}<p class="heading">Agents & connections</p><ConnectedTools project={selected.project} />{/if}
      </HeaderPopover>
    {/snippet}
  </ContextBar>
{/if}

<style>
  .cluster { display: inline-flex; align-items: center; }
  .cluster > :global(* + *) { margin-left: -2px; }
  .more { margin-left: 6px; font-size: 12px; }
  .git-text { font-variant-numeric: tabular-nums; }
  .badge { min-width: 18px; padding: 0 5px; border-radius: 99px; background: var(--amber); color: #1d1b18; font-size: 11px; font-weight: 600; text-align: center; }
  .list { list-style: none; margin: 0; padding: 0; display: grid; gap: 2px; }
  .row { display: flex; align-items: center; gap: 10px; width: 100%; padding: 6px 8px; border: 0; border-radius: var(--radius-sm); background: transparent; color: var(--text); font: inherit; text-align: left; }
  button.row { cursor: pointer; }
  button.row:hover { background: var(--hover); }
  button.row:focus-visible { outline: 2px solid var(--accent); outline-offset: -2px; }
  .what { display: grid; min-width: 0; }
  .what strong { font-weight: 500; }
  .state { color: var(--text-2); font-weight: 400; }
  .where { color: var(--text-2); font-size: 12px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .line { display: flex; align-items: center; gap: 8px; margin: 4px 8px; flex-wrap: wrap; }
  .note { margin: 4px 8px; font-size: 12px; color: var(--text-2); line-height: 1.5; }
  .heading { margin: 8px 8px 2px; font-size: 11px; font-weight: 600; letter-spacing: 0.04em; text-transform: uppercase; color: var(--text-2); }
  @media (max-width: 1279px) { .sha, .age { display: none; } }
  @media (max-width: 899px) { .git-text { display: none; } }
</style>

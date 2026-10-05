<script lang="ts">
  import { untrack } from 'svelte';
  import { ApiError, api } from '../api';
  import { branchHref } from '../gitroute';
  import { shortSha } from '../gitui';
  import type { GitActionResult, GitMergePlan } from '../types';
  import ResultCard from './ResultCard.svelte';
  import Sheet from '../Sheet.svelte';
  import { sheets } from './sheets.svelte';
  import type { GitStore } from './store.svelte';

  // Merging is checked before it is offered and again when it is asked for. What is shown
  // here is the controller's own check: the commits you reviewed, Git's own simulation of
  // the merge, and every reason it would refuse. Nothing is merged without this screen.

  let { projectId, store, branchName, onmerged }: { projectId: string; store: GitStore; branchName: string; onmerged?: () => void } = $props();

  const o = $derived(store.overview);
  // The commits the user was looking at when they opened this: the merge is made of those,
  // and refused if either has moved since.
  const reviewed = untrack(() => ({
    branchSha: store.overview?.branches.find((b) => b.scope === 'local' && b.name === branchName)?.sha ?? '',
    targetSha: store.overview?.local.target.sha ?? '',
    target: store.overview?.local.target.name ?? '',
  }));

  let strategy = $state<'merge' | 'ff-only'>('merge');
  let plan = $state<GitMergePlan | null>(null);
  let planning = $state(false);
  let error = $state('');
  let result = $state<GitActionResult | null>(null);
  let merging = $state(false);

  const request = $derived({ branch: branchName, branchSha: reviewed.branchSha, target: reviewed.target, targetSha: reviewed.targetSha, strategy });

  async function check() {
    planning = true;
    error = '';
    try {
      plan = await api.git.mergePlan(projectId, request);
    } catch (err) {
      error = err instanceof ApiError ? err.message : String(err);
    } finally {
      planning = false;
    }
  }

  $effect(() => {
    void strategy;
    untrack(() => void check());
  });

  async function merge() {
    merging = true;
    error = '';
    try {
      result = await api.git.merge(projectId, request);
      store.lastResult = result;
      if (result.ok) onmerged?.();
      await store.reloadAll();
    } catch (err) {
      error = err instanceof ApiError ? err.message : String(err);
    } finally {
      merging = false;
    }
  }

  const moved = $derived(plan?.blockers.some((b) => b.code === 'branch_moved' || b.code === 'target_moved') ?? false);
  const targetBranch = $derived(o?.branches.find((b) => b.scope === 'local' && b.name === reviewed.target));
  const conflictLabel = $derived(
    !plan
      ? ''
      : plan.conflicts.method === 'simulation'
        ? plan.conflicts.result === 'clean'
          ? 'Git merged them in memory: no conflicts.'
          : 'Git merged them in memory and found conflicts.'
        : plan.conflicts.method === 'overlap'
          ? plan.conflicts.result === 'possible'
            ? 'A guess, not a check: files changed on both sides.'
            : 'Not checked: this Git cannot simulate a merge.'
          : 'Conflicts could not be checked.',
  );
</script>

<Sheet title="Merge {branchName}" onclose={() => sheets.close()}>
  {#if result}
    <ResultCard {result} />
  {:else if planning && !plan}
    <p class="muted">Checking the repository now…</p>
  {:else if plan}
    <p class="g-wrap">
      Merge <strong>{plan.branch}</strong> into <strong>{plan.target || reviewed.target}</strong>, on this computer only.
      <span class="muted">Nothing is pushed, and no pull request is changed.</span>
    </p>

    <dl class="g-kv">
      <dt>Commits</dt>
      <dd>{plan.ahead} from the branch{plan.behind ? `; ${plan.target} has ${plan.behind} the branch lacks` : ''}</dd>
      <dt>Files</dt>
      <dd>{plan.filesChanged} changed, <span class="add">+{plan.additions}</span> <span class="del">−{plan.deletions}</span></dd>
      <dt>Reviewed</dt>
      <dd class="mono">{shortSha(plan.branchSha)} into {shortSha(plan.targetSha)}</dd>
      <dt>Conflicts</dt>
      <dd>
        <span class:bad={plan.conflicts.result === 'conflicts'}>{conflictLabel}</span>
        {#if plan.conflicts.files?.length}<span class="g-small muted g-wrap"> {plan.conflicts.files.slice(0, 5).join(', ')}</span>{/if}
      </dd>
    </dl>

    <fieldset class="how">
      <legend class="g-small muted">How</legend>
      <label><input type="radio" bind:group={strategy} value="merge" /> Merge commit <span class="muted g-small">(keeps the branch’s commits)</span></label>
      <label><input type="radio" bind:group={strategy} value="ff-only" /> Fast-forward only <span class="muted g-small">(no new commit; refused if {reviewed.target} moved)</span></label>
    </fieldset>

    {#if plan.blockers.length}
      <div class="g-notice" data-tone="bad" role="alert">
        <strong>It will not merge</strong>
        <ul class="g-bullets">
          {#each plan.blockers as b (b.code + b.message)}<li class="g-wrap">{b.message}</li>{/each}
        </ul>
        {#if moved}
          <p class="g-small"><a href={branchHref(projectId, 'local', branchName)} onclick={() => sheets.close()}>Review it again</a> to see what changed.</p>
        {/if}
      </div>
    {/if}
    {#if plan.warnings.length}
      <div class="g-notice">
        <strong>Worth knowing</strong>
        <ul class="g-bullets">
          {#each plan.warnings as w (w)}<li class="g-wrap">{w}</li>{/each}
        </ul>
      </div>
    {/if}
    <p class="g-small muted">Checked just now. It is checked again the moment you confirm, and refused if anything moved.</p>
  {/if}
  {#if error}<p class="error" role="alert">{error}</p>{/if}

  {#snippet footer()}
    {#if result}
      {#if result.ok && targetBranch && targetBranch.upstream.state === 'ahead'}
        <button class="btn" onclick={() => sheets.open({ kind: 'push', branch: reviewed.target })}>Push {reviewed.target}…</button>
      {/if}
      <button class="btn primary" onclick={() => sheets.close()}>Done</button>
    {:else}
      <button class="btn" onclick={() => sheets.close()}>Cancel</button>
      <button class="btn primary" disabled={!plan?.canMerge || merging || planning} onclick={merge}>
        {merging ? 'Merging…' : `Merge into ${reviewed.target || 'target'}`}
      </button>
    {/if}
  {/snippet}
</Sheet>

<style>
  .add {
    color: var(--ok);
    font-family: var(--mono);
  }

  .del {
    color: var(--danger);
    font-family: var(--mono);
  }

  .bad {
    color: var(--danger);
  }

  .how {
    display: grid;
    gap: 6px;
    margin: 0;
    padding: 8px 10px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
  }

  .how label {
    display: flex;
    gap: 8px;
    align-items: baseline;
    min-height: 32px;
  }
</style>

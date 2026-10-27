<script lang="ts">
  import { untrack } from 'svelte';
  import { gitStore } from './git/store.svelte';
  import { branchHref } from './gitroute';
  import { safeURL } from './gitui';
  import { sheets } from './git/sheets.svelte';
  import Icon from './Icon.svelte';
  import { api } from "./api";
  import type { ProjectScope } from "./scope.svelte";
  import type { Handoff, Run } from "./types";
  let {
    run,
    scope,
    onmerged,
    hideReviewActions = false,
  }: {
    run: Run;
    scope: ProjectScope;
    /** The Review task panel shows these actions beside the task title. */
    hideReviewActions?: boolean;
    /** Runs once the work is merged: the task panel moves the task to Done. */
    onmerged?: () => void;
  } = $props();
  const current = $derived(
    scope.history.find((r) => r.id === run.id && r.version >= run.version) ??
      run,
  );
  const git = $derived(gitStore(run.projectId));
  const branch = $derived(!current.remote ? git.overview?.branches.find(b => b.scope === 'local' && (b.werkbord.runId === current.id || (!!current.branch && b.name === current.branch))) : undefined);
  /** The same test as the board's Merge…: its own local branch, ahead of the target and not merged yet. */
  const canMerge = $derived(!!branch && !branch.merged && branch.vsTarget.ahead > 0);
  const pr = $derived(git.github?.pullRequests.find(p => p.runId === current.id));
  const prURL = $derived(pr ? safeURL(pr.url) : '#');
  const reviewLookupFailed = $derived(!!git.error || (git.github?.reason === 'error' && !git.github.available));
  const unresolved = $derived([...(current.handoff?.knownIssues ?? []), ...(current.handoff?.blockers ?? []), ...(current.handoff?.questions ?? [])]);
  $effect(() => { const store = git; return untrack(() => { const stop = store.watch(); void store.loadGitHub(); return stop; }); });
  let editing = $state(false);
  let editVersion = $state(0);
  let draft = $state<Handoff | null>(null);
  let busy = $state(false);
  let error = $state("");
  let decisions = $state("");
  let tests = $state("");
  let issues = $state("");
  let blockers = $state("");
  let questions = $state("");
  function edit() {
    const h = current.handoff;
    if (!h) return;
    editVersion = current.version;
    draft = structuredClone($state.snapshot(h));
    decisions = h.decisions.join("\n");
    tests = h.tests.join("\n");
    issues = h.knownIssues.join("\n");
    blockers = h.blockers.join("\n");
    questions = h.questions.join("\n");
    editing = true;
  }
  const lines = (s: string) =>
    s
      .split("\n")
      .map((x) => x.trim())
      .filter(Boolean);
  async function generate() {
    busy = true;
    error = "";
    try {
      scope.upsertRun(await api.generateHandoff(run.projectId, run.id));
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      busy = false;
    }
  }
  async function save() {
    if (!draft) return;
    busy = true;
    error = "";
    try {
      scope.upsertRun(
        await api.saveHandoff({ ...current, version: editVersion }, {
          ...draft,
          decisions: lines(decisions),
          tests: lines(tests),
          knownIssues: lines(issues),
          blockers: lines(blockers),
          questions: lines(questions),
        }),
      );
      editing = false;
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      busy = false;
    }
  }

</script>

<section class="handoff card" aria-label="Run handoff">
  <div class="heading">
    <h2>Run result</h2>
    <button class="btn small" disabled={busy} onclick={generate}
      >{current.handoff ? "Refresh Git evidence" : "Capture handoff"}</button
    >{#if current.handoff}<button
        class="btn small"
        disabled={busy}
        onclick={edit}>Edit handoff</button
      >{/if}
  </div>
  <p class="text"><strong>{current.state === 'completed' ? 'Session finished — ready for your review.' : current.state === 'failed' ? 'The run failed. Your task and recorded activity are retained.' : 'The run stopped. Your task and recorded activity are retained.'}</strong></p>
  {#if current.reason}<p class="text">{current.reason}</p>{/if}
  <p class="text">Branch: <code>{branch?.name || current.branch || 'No branch recorded'}</code></p>
  {#if branch}{#if !hideReviewActions}<div class="review-acts"><a class="btn primary" href={branchHref(current.projectId, branch.scope, branch.name)}>Review changes</a>{#if canMerge}<button class="btn ok" onclick={() => sheets.open({ kind: 'merge', branch: branch.name, onmerged })}><Icon name="merge" size={14} />Merge…</button>{/if}</div>{/if}
  {#if canMerge}<p class="muted small">Merge… shows what will change and asks you to confirm before anything is merged into the target branch. A finished agent never merges by itself.</p>{:else if branch.merged}<p class="muted small">This branch is already merged.</p>{/if}
  {:else if pr && prURL !== '#'}{#if !hideReviewActions}<a class="btn primary" href={prURL} target="_blank" rel="noopener noreferrer">Review pull request #{pr.number}</a>{/if}
  {:else}<p class="muted">{reviewLookupFailed ? 'Could not check for a local diff or pull request.' : git.loading || git.githubLoading ? 'Checking available review evidence…' : current.remote ? 'Changes remain on the runner. No pull request is available here.' : 'No local diff or pull request is available here.'}</p>{/if}
  {#if git.error}<p class="error" role="alert">Could not load Git evidence: {git.error}</p>{/if}
  {#if git.github && !git.github.available}<p class="muted small">Pull request lookup unavailable: {git.github.message || 'Check GitHub access in Settings.'}</p>{/if}
  {#if unresolved.length}<p class="error" role="alert"><strong>Open items:</strong> {unresolved.join('\n')}</p>{/if}
  {#if current.handoff?.nextAction}<p class="next-action"><strong>Next:</strong> {current.handoff.nextAction}</p>{/if}
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  {#if editing && draft}
    <form
      onsubmit={(e) => {
        e.preventDefault();
        void save();
      }}
    >
      <label
        >Objective<textarea class="input" rows="2" bind:value={draft.objective}
        ></textarea></label
      >
      <label
        >Implementation summary<textarea
          class="input"
          rows="4"
          bind:value={draft.summary}></textarea></label
      >
      <label
        >Important decisions (one per line)<textarea
          class="input"
          rows="2"
          bind:value={decisions}></textarea></label
      >
      <label
        >Recorded test notes (one per line)<textarea
          class="input"
          rows="2"
          bind:value={tests}></textarea></label
      >
      <label
        >Results<textarea class="input" rows="2" bind:value={draft.results}
        ></textarea></label
      >
      <label
        >Known issues (one per line)<textarea
          class="input"
          rows="2"
          bind:value={issues}></textarea></label
      >
      <label
        >Blockers (one per line)<textarea
          class="input"
          rows="2"
          bind:value={blockers}></textarea></label
      >
      <label
        >Unanswered questions (one per line)<textarea
          class="input"
          rows="2"
          bind:value={questions}></textarea></label
      >
      <label
        >Recommended next action<textarea
          class="input"
          rows="2"
          bind:value={draft.nextAction}></textarea></label
      >
      <div class="heading">
        <button class="btn primary" disabled={busy}>Save handoff</button><button
          class="btn"
          type="button"
          onclick={() => (editing = false)}>Cancel</button
        >
      </div>
    </form>
  {:else if current.handoff}
    {@const h = current.handoff}
    <details>
      <summary>Objective and implementation</summary>
      <p class="text">{h.objective}</p>
      <p class="text">{h.summary}</p>
    </details>
    <details>
      <summary>Files changed ({h.filesChanged.length}) and Git state</summary>
      <p class="text">{h.gitState}</p>
      <ul>
        {#each h.filesChanged as f (f)}<li><code>{f}</code></li>{/each}
      </ul>
    </details>
    <details>
      <summary>Decisions, tests, and results</summary>
      <p class="text">Decisions: {h.decisions.join("\n") || "Not reported"}</p>
      <p class="text">Recorded test notes (unverified): {h.tests.join("\n") || "Not reported"}</p>
      <p class="text">{h.results}</p>
    </details>
    <details>
      <summary>Issues, blockers, and unanswered questions</summary>
      <p class="text">
        Known issues: {h.knownIssues.join("\n") || "None reported"}
      </p>
      <p class="text">Blockers: {h.blockers.join("\n") || "None reported"}</p>
      <p class="text">Questions: {h.questions.join("\n") || "None reported"}</p>
    </details>
  {:else}<p class="muted">Capture a handoff when another agent needs to continue.</p>{/if}

</section>

<style>
  .review-acts { display: flex; flex-wrap: wrap; gap: 8px; }
  .handoff,
  form {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    min-width: 0;
    gap: 14px;
    padding: 14px;
  }
  .heading {
    display: flex;
    gap: 10px;
    flex-wrap: wrap;
    align-items: center;
  }
  .heading h2 {
    margin-right: auto;
  }
  .text {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    font-size: 0.88rem;
    margin-top: 8px;
  }

  .next-action { font-size: 13px; }
  .small {
    font-size: 0.82rem;
  }
  details summary {
    cursor: pointer;
    min-height: 36px;
  }
  label {
    display: grid;
    gap: 6px;
    font-size: 0.88rem;
  }
  form {
    padding: 0;
  }
  ul {
    padding-left: 20px;
    overflow-wrap: anywhere;
  }
</style>

<script lang="ts">
  import { api } from '../api';
  import { app } from '../state.svelte';
  import type { GitHubStatusInfo } from '../types';
  import { createPlan, DEFAULT_REPO_NAME, repoNameProblem } from './createRepo';
  import SetupCard from './SetupCard.svelte';

  let { github, onadded }: { github: GitHubStatusInfo | null; onadded?: (fullName: string, pagesUrl: string) => void } = $props();

  let name = $state(DEFAULT_REPO_NAME);
  let isPrivate = $state(false);
  let confirming = $state(false);
  let busy = $state(false);
  let error = $state('');
  let made = $state<{ fullName: string; url: string; pagesUrl: string; pagesError: string } | null>(null);

  const login = $derived(github?.account?.login ?? '');
  const ready = $derived(github?.state === 'signed_in' && !github.missingScopes?.length);
  const problem = $derived(repoNameProblem(name));
  const plan = $derived(createPlan(login, name, isPrivate));

  async function create() {
    busy = true;
    error = '';
    try {
      const res = await api.githubCreateRepo({ name: name.trim(), description: 'Created with Werkbord', private: isPrivate });
      if (res.project) app.upsertProject(res.project);
      made = { fullName: res.repository.fullName, url: res.repository.url, pagesUrl: res.pagesUrl ?? '', pagesError: res.pagesError ?? '' };
      confirming = false;
      onadded?.(res.repository.fullName, made.pagesUrl);
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = false;
    }
  }
</script>

<SetupCard title="Create a new repository" status={made ? 'done' : 'todo'} summary={made ? made.fullName : ready ? `Under ${login}` : 'Needs GitHub'}>
  {#if made}
    <p>Created <a href={made.url} target="_blank" rel="noopener noreferrer">{made.fullName}</a> and added it as a project.</p>
    {#if made.pagesUrl}<p class="muted">Its page will appear at <code>{made.pagesUrl}</code> once something is published.</p>{/if}
    {#if made.pagesError}<p class="muted" role="status">{made.pagesError}</p>{/if}
  {:else if !ready}
    <p class="muted">Connect GitHub, with the extra permission it asks for, to create a repository from here.</p>
  {:else if !confirming}
    <p>A small example repository to try Werkbord on. You will see exactly what happens before anything is created.</p>
    <label class="field">
      <span>Name</span>
      <input bind:value={name} autocomplete="off" spellcheck="false" aria-invalid={problem ? 'true' : undefined} aria-describedby="repo-name-problem" />
    </label>
    <p id="repo-name-problem" class="error small" role={problem ? 'alert' : undefined}>{problem}</p>
    <fieldset class="vis">
      <legend>Visibility</legend>
      <label><input type="radio" bind:group={isPrivate} value={false} /> Public (GitHub Pages is free)</label>
      <label><input type="radio" bind:group={isPrivate} value={true} /> Private</label>
    </fieldset>
    <div class="row"><button class="btn primary" disabled={!!problem} onclick={() => (confirming = true)}>Review</button></div>
  {:else}
    <p>This will:</p>
    <ol class="plan">{#each plan as step (step)}<li>{step}</li>{/each}</ol>
    {#if isPrivate}<p class="muted small">If Pages is not available on your plan, the repository is still created and you can carry on without a page.</p>{/if}
    <p class="muted small">Nothing is ever deleted by Werkbord. You can remove the repository on GitHub whenever you like.</p>
    <div class="row">
      <button class="btn primary" disabled={busy} onclick={create}>{busy ? 'Creating…' : 'Create repository'}</button>
      <button class="btn" disabled={busy} onclick={() => (confirming = false)}>Back</button>
    </div>
  {/if}
  {#if error}<p class="error" role="alert">{error}</p>{/if}
</SetupCard>

<style>
  .row { display: flex; gap: 8px; flex-wrap: wrap; }
  .field { display: grid; gap: 4px; max-width: 360px; }
  .vis { border: 0; padding: 0; margin: 0; display: grid; gap: 4px; }
  .vis legend { font-size: 13px; font-weight: 500; padding: 0; margin-bottom: 4px; }
  .plan { margin: 0; padding-left: 20px; display: grid; gap: 4px; }
  .small { font-size: 0.82rem; }
</style>

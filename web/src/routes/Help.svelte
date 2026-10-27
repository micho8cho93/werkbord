<script lang="ts">
  import Icon from '../lib/Icon.svelte';
  import { app } from '../lib/state.svelte';
  import { router } from '../lib/router.svelte';
  import { api } from '../lib/api';
  import { forgetHere } from '../lib/setup/tours';
  import { helpHref, searchHelp } from '../lib/help';

  let query = $derived(new URLSearchParams(router.query).get('article') === 'mcp' ? 'mcp' : '');
  const projectId = $derived(router.projectId || app.lastProjectId);
  const project = $derived(app.project(projectId));
  const articles = $derived(searchHelp(query));
  // A search or a linked article narrows the list to what was asked for, so its steps are shown.
  const opened = $derived(query.trim() !== '' && articles.length <= 2);
  let replayed = $state(false);
  async function replayTours() {
    forgetHere();
    try { app.onboarding = await api.resetTours(); replayed = true; } catch { replayed = true; /* a phone keeps its own, and that is cleared */ }
  }
</script>

<section class="help" aria-labelledby="help-title">
  <header class="intro">
    <div>
      <h2 id="help-title">Find the next step</h2>
      <p class="lede">Help with tasks, agents, Git and connections.</p>
    </div>
    <label class="search-field">
      <span>Search Help and shortcuts</span>
      <input bind:value={query} type="search" placeholder="Try “merge” or “Ctrl+K”" autocomplete="off" />
    </label>
  </header>

  <p class="replay">
    <button class="btn" onclick={replayTours}>Replay the quick tours</button>
    {#if replayed}<span role="status">Each tour shows again the next time you open its page.</span>{/if}
  </p>

  <p class="result-count" aria-live="polite">{query.trim() ? `${articles.length} ${articles.length === 1 ? 'guide' : 'guides'} found` : `${articles.length} guides`}</p>

  {#if articles.length}
    <ol class="guides">
      {#each articles as article (article.id)}
        <li>
          <article aria-labelledby="guide-{article.id}">
            <div class="guide-heading">
              <h3 id="guide-{article.id}">{article.title}</h3>
              <p>{article.summary}</p>
            </div>
            <div class="guide-body">
              <a class="guide-link" href={helpHref(article.destination, projectId, project?.kind)}>
                {article.linkLabel}<Icon name="right" size={14} />
              </a>
              <details open={opened}>
                <summary>Steps, example and recovery</summary>
                <p class="example"><strong>Example</strong>{article.example}</p>
                <ol>
                  {#each article.steps as step, index (step)}
                    <li><span class="step-number" aria-hidden="true">{index + 1}</span><span>{step}</span></li>
                  {/each}
                </ol>
                <p class="recovery"><strong>If it goes wrong</strong>{article.recovery}</p>
              </details>
            </div>
          </article>
        </li>
      {/each}
    </ol>
  {:else}
    <div class="empty" role="status">
      <p>No bundled guide matches “{query}”.</p>
      <p>Try a task, section or shortcut such as “runner”, “Review” or “Ctrl+K”.</p>
    </div>
  {/if}
</section>

<style>
  .replay { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; margin: 0; font-size: 13px; }
  .help { max-width: 1040px; margin: 0 auto; }
  .intro { display: grid; grid-template-columns: minmax(0, 1fr) minmax(240px, 340px); gap: 28px; align-items: end; padding: 4px 0 18px; }
  h2 { margin: 0; font-size: 20px; font-weight: 600; }
  .lede { max-width: 66ch; margin: 7px 0 0; color: var(--text-2); font-size: 14px; line-height: 1.55; }
  .search-field { display: grid; gap: 6px; color: var(--text-2); font-size: 12px; }
  .search-field input { width: 100%; min-height: 42px; padding: 0 12px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface); color: var(--text); font: inherit; font-size: 14px; }
  .search-field input::placeholder { color: var(--text-2); opacity: 1; }
  .search-field input:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
  .result-count { margin: 0 0 4px; color: var(--text-2); font-size: 12px; }
  .guides { list-style: none; padding: 0; margin: 0; border-top: 1px solid var(--border); }
  .guides > li { border-bottom: 1px solid var(--border); }
  article { display: grid; grid-template-columns: minmax(190px, 0.72fr) minmax(0, 1.6fr); gap: 28px; padding: 20px 0 22px; }
  h3 { margin: 0 0 7px; font-size: 16px; font-weight: 600; }
  .guide-heading p, .guide-body p { margin: 0; color: var(--text-2); font-size: 13px; line-height: 1.55; }
  .guide-body { display: grid; gap: 12px; }
  .example { padding: 10px 12px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface-2); }
  .example strong, .recovery strong { display: block; margin-bottom: 3px; color: var(--text); font-weight: 600; }
  .guide-body ol { list-style: none; display: grid; gap: 8px; margin: 0; padding: 0; }
  .guide-body ol li { display: grid; grid-template-columns: 20px minmax(0, 1fr); gap: 8px; align-items: start; color: var(--text); font-size: 13px; line-height: 1.5; }
  .step-number { color: var(--text-2); font-family: var(--mono); font-size: 12px; font-variant-numeric: tabular-nums; }
  details > summary { cursor: pointer; min-height: 36px; display: flex; align-items: center; color: var(--text); font-size: 13px; font-weight: 500; }
  details[open] > summary { margin-bottom: 8px; }
  details > :not(summary) { margin-top: 10px; }
  .recovery { padding-top: 10px; border-top: 1px solid var(--border); }
  .guide-link { display: inline-flex; align-items: center; gap: 5px; justify-self: start; min-height: 36px; color: var(--accent); font-size: 13px; font-weight: 500; text-decoration-thickness: 1px; text-underline-offset: 3px; }
  .guide-link:focus-visible { outline: 2px solid var(--accent); outline-offset: 3px; border-radius: 2px; }
  .empty { padding: 18px 0; }
  .empty p { margin: 0 0 6px; color: var(--text-2); font-size: 13px; }
  .empty p:first-child { color: var(--text); }
  @media (max-width: 899px) {
    .intro { grid-template-columns: minmax(0, 1fr); gap: 16px; padding-top: 0; }
    .search-field input { min-height: 44px; font-size: 16px; }
    article { grid-template-columns: minmax(0, 1fr); gap: 12px; padding: 18px 0; }
    .guide-link { min-height: 44px; }
  }
  @media (pointer: coarse) {
    .guide-link { min-height: 44px; }
    .search-field input { min-height: 44px; }
  }
  @media (prefers-reduced-motion: reduce) {
    *, *::before, *::after { scroll-behavior: auto !important; animation-duration: 0.01ms !important; animation-iteration-count: 1 !important; transition-duration: 0.01ms !important; }
  }
</style>

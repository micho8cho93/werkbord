<script lang="ts">
  import { outcomeTitle, outcomeTone, safeURL, shortSha } from '../gitui';
  import type { GitActionResult } from '../types';

  // What an action did, with its two sides apart: what changed on THIS computer, and what
  // the REMOTE confirmed. A local change is never presented as something done on a remote,
  // and "confirmed" only appears when the remote itself was asked and agreed.

  let { result }: { result: GitActionResult } = $props();
  const tone = $derived(outcomeTone(result.outcome));
</script>

<div class="g-notice" data-tone={tone === 'ok' ? 'ok' : tone === 'neutral' ? 'neutral' : 'bad'} role="status">
  <strong>{outcomeTitle(result.outcome)}</strong>
  <p class="g-wrap">{result.message}</p>

  {#if result.blockers?.length}
    <ul class="g-bullets">
      {#each result.blockers as b (b.code + b.message)}<li class="g-wrap">{b.message}</li>{/each}
    </ul>
  {/if}

  {#if result.local || result.remote}
    <div class="sides">
      {#if result.local}
        <div class="g-scope">
          <h3>On this computer</h3>
          <p class="g-small g-wrap">
            {result.local.note ?? ''}
            {#if result.local.before || result.local.after}
              <span class="mono">{shortSha(result.local.before) || '—'} → {shortSha(result.local.after) || '—'}</span>
            {/if}
          </p>
        </div>
      {/if}
      {#if result.remote}
        <div class="g-scope">
          <h3>On {result.remote.remote || 'the remote'}</h3>
          <p class="g-small g-wrap">
            {#if result.remote.verified}
              <strong class="ok">Confirmed by the remote.</strong>
            {:else}
              <strong class="unconfirmed">Not confirmed by the remote.</strong>
            {/if}
            {result.remote.note ?? ''}
            {#if result.remote.sha}<span class="mono">at {shortSha(result.remote.sha)}</span>{/if}
          </p>
        </div>
      {/if}
    </div>
  {/if}

  {#if result.warnings?.length}
    <ul class="g-bullets">
      {#each result.warnings as w (w)}<li class="g-wrap g-small">{w}</li>{/each}
    </ul>
  {/if}

  {#if result.pullRequest}
    <p class="g-small">
      <a href={safeURL(result.pullRequest.url)} target="_blank" rel="noopener noreferrer">Pull request #{result.pullRequest.number} on GitHub</a>
    </p>
  {/if}

  {#if result.git}
    <details>
      <summary class="g-small muted">What Git said</summary>
      <pre class="g-small g-wrap">{result.git}</pre>
    </details>
  {/if}
</div>

<style>
  .sides {
    display: grid;
    gap: 8px;
    margin-top: 8px;
  }

  p {
    margin-top: 2px;
  }

  .ok {
    color: var(--ok);
  }

  .unconfirmed {
    color: var(--warn);
  }

  pre {
    margin: 4px 0 0;
    white-space: pre-wrap;
    font-family: var(--mono);
  }
</style>

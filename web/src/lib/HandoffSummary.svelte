<script lang="ts">
  import type { ReadableHandoff } from './handoff';
  let { handoff: h }: { handoff: ReadableHandoff } = $props();
  const groups = $derived([
    { label: 'Tests', lines: h.tests },
    { label: 'Known issues', lines: h.knownIssues },
    { label: 'Blockers', lines: h.blockers },
    { label: 'Questions', lines: h.questions },
  ].filter(g => g.lines.length));
</script>
<section class="handoff" aria-label="Agent handoff">
  <h3>Handoff</h3>
  {#if h.objective}<p>{h.objective}</p>{/if}
  {#if h.summary}<p>{h.summary}</p>{/if}
  {#if h.results}<p>{h.results}</p>{/if}
  {#if h.gitState}<div><h4>Git state</h4><p>{h.gitState}</p></div>{/if}
  {#each groups as group (group.label)}
    <div><h4>{group.label}</h4><ul>{#each group.lines as line, i (i)}<li>{line}</li>{/each}</ul></div>
  {/each}
  {#if h.decisions.length}<details><summary>Decisions ({h.decisions.length})</summary><ul>{#each h.decisions as line, i (i)}<li>{line}</li>{/each}</ul></details>{/if}
  {#if h.filesChanged.length}<details><summary>Files changed ({h.filesChanged.length})</summary><ul>{#each h.filesChanged as file, i (i)}<li><code>{file}</code></li>{/each}</ul></details>{/if}
  {#if h.nextAction}<p class="next"><strong>Next action</strong><br />{h.nextAction}</p>{/if}
</section>
<style>
  .handoff { display: grid; gap: 12px; padding: 16px; border: 1px solid var(--border); border-radius: var(--radius); background: var(--surface); min-width: 0; }
  h3 { font-size: 15px; } h4 { font-size: 13px; font-weight: 600; }
  p, li { white-space: pre-wrap; overflow-wrap: anywhere; }
  ul { margin: 4px 0 0; padding-left: 20px; display: grid; gap: 4px; }
  summary { cursor: pointer; min-height: 32px; }
  .next { padding-top: 12px; border-top: 1px solid var(--border); }
</style>

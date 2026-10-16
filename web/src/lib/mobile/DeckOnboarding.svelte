<script lang="ts">
  let { step, hasWorkflow, full, workflowName, liveDemo, preview, busy, onnext, onadd, onconfigure, onrecommend, onpreview, onfinish }:
    { step: number; hasWorkflow: boolean; full: boolean; workflowName: string; liveDemo: boolean; preview: boolean; busy: boolean;
      onnext: (step: number) => void; onadd: () => void; onconfigure: () => void; onrecommend: () => void; onpreview: () => void; onfinish: () => Promise<void> } = $props();
  const titles = ['Make the Control Deck yours', 'Choose your buttons', 'Hold to configure', 'Try a workflow', 'Inspect execution', 'Your deck is ready'];
</script>
<section class="guide" aria-label="Control Deck onboarding" aria-busy={busy}>
  <h2>{titles[step - 1]}</h2>
  <p class="muted">{step} of 6</p>
  {#if step === 1}
    <p>Make the Control Deck yours. Choose shortcuts and workflows for the actions you use most.</p>
    <button class="btn primary" onclick={() => { onnext(2); onadd(); }}>Choose buttons</button>
  {:else if step === 2}
    <p>Fill up to three empty positions. Choose at least one executable workflow; navigation buttons open pages. You can customise the full nine-button deck later.</p>
    <div class="actions"><button class="btn" onclick={onadd}>{full ? 'Replace a choice' : 'Add a button'}</button><button class="btn primary" disabled={!hasWorkflow || busy} onclick={() => onnext(3)}>Continue</button></div>
  {:else if step === 3}
    <p>Press and hold a workflow button to change its settings.</p>
    <p class="muted">{workflowName} is highlighted. With a keyboard, use Shift+F10.</p>
    <button class="btn" onclick={onconfigure}>Open workflow settings</button>
  {:else if step === 4}
    <p>{preview ? 'Interactive preview: tap the highlighted workflow to practise opening progress. No workflow will execute.' : 'Tap the highlighted workflow once to execute. Its progress indicator will appear above the deck.'}</p>
    {#if workflowName !== 'Find Bugs'}<p class="muted">We recommend Find Bugs for your first run.</p><button class="btn" onclick={onrecommend}>Choose and configure Find Bugs</button>{/if}
    <button class="btn" onclick={onconfigure}>Change repository or settings</button>
    {#if !liveDemo && !preview}<p class="muted">Live execution is unavailable with the current repository, connection or agent defaults. Reconnect or check settings, or try the preview.</p><button class="btn" onclick={onpreview}>Try interactive preview</button>{/if}
    <button class="btn" onclick={() => onnext(6)}>Skip execution for now</button>
  {:else if step === 5}
    <p>Tap the {preview ? 'interactive preview' : 'progress indicator above'} to inspect the current stage, completed stages, agent activity, repository, results and Stop control.</p>
    <p class="muted">Live work continues when you leave. Only reported stages and findings are shown.</p>
    <button class="btn" onclick={() => onnext(6)}>Inspect later</button>
  {:else}
    <p>Tap to run. Hold to configure. Edit Deck to rearrange.</p>
    <p class="muted">Change a workflow’s repository target to inspect other repositories. You can remove or replace every button.</p>
    <button class="btn primary" disabled={busy} onclick={() => void onfinish()}>{busy ? 'Saving…' : 'Finish onboarding'}</button>
  {/if}
</section>
<style>
  .guide { display: grid; gap: 10px; padding: 4px 0 12px; border-bottom: 1px solid var(--border); }
  h2 { margin: 0; font-size: 20px; font-weight: 600; letter-spacing: -.02em; color: var(--text); text-transform: none; }
  p { margin: 0; line-height: 1.5; font-size: 13px; }
  .actions { display: flex; flex-wrap: wrap; gap: 8px; }
  button { min-height: 44px; }
</style>

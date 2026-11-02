<script lang="ts">
  import { AGENT_MARKS, agentName } from './agent-marks';
  import { resolveAgentState } from './identity';

  // A coding agent's logo on a white tile, ringed by its state. The ring rotates only for a confirmed, fresh running state
  // (identity.js decides; identity.css draws). `fresh` is the caller's word: the controller for own work, the server for a peer.
  // Without a state it is just the mark. An agent with no mark of its own gets a neutral letter tile.
  let { agentId, name, state, fresh, observedAt, now = Date.now(), size = 'sm', decorative = false }: {
    agentId?: string; name?: string; state?: string; fresh?: boolean | null; observedAt?: string | number; now?: number; size?: 'sm' | 'md' | 'lg'; decorative?: boolean;
  } = $props();

  const label = $derived(agentName(agentId, name));
  const mark = $derived(agentId ? AGENT_MARKS[agentId] : undefined);
  const view = $derived(state === undefined ? null : resolveAgentState({ state, fresh, observedAt, now, agentName: label }));
</script>

<span class="wb-agent" data-size={size} data-ring={view?.ring} role={decorative ? undefined : 'img'} aria-label={decorative ? undefined : (view?.label ?? label)} aria-hidden={decorative ? 'true' : undefined}>
  <span class="wb-agent-tile" class:letter={!mark}>
    {#if mark}<svg viewBox="0 0 24 24" aria-hidden="true"><path d={mark.path} fill={mark.colour} /></svg>{:else}{label.slice(0, 1).toUpperCase()}{/if}
  </span>
</span>

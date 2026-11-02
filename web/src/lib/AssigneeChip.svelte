<script lang="ts">
  import AgentMark from './AgentMark.svelte';
  import Avatar from './Avatar.svelte';
  import { agentName as nameOf } from './agent-marks';
  import { resolveAgentState } from './identity';

  // Which agent is on a piece of work and which person holds it, as two separate groups, the agent first. The person group is always
  // shown when there is a holder; the agent group only when an agent is known. `role` is the label before the name ("Held by",
  // "Assigned to", "Review"). Nothing here says whether a person is available: people are shown by what is assigned to them.
  let { agentId, agentName: given, state, fresh, observedAt, now, holder, role = 'Held by' }: {
    agentId?: string; agentName?: string; state?: string; fresh?: boolean | null; observedAt?: string | number; now?: number;
    holder?: { id: string; name: string; self?: boolean }; role?: string;
  } = $props();

  const agent = $derived(agentId ? nameOf(agentId, given) : '');
  const view = $derived(agentId && state !== undefined ? resolveAgentState({ state, fresh, observedAt, now: now ?? Date.now(), agentName: agent }) : null);
</script>

{#if agentId || holder}
  <span class="wb-assign">
    {#if agentId}
      <span class="wb-assign-group"><AgentMark {agentId} name={given} {state} {fresh} {observedAt} {now} decorative /><span>{agent}{view ? ' ' + view.text : ''}</span></span>
    {/if}
    {#if holder}
      <span class="wb-assign-group"><Avatar id={holder.id} name={holder.name} self={holder.self} size="sm" /><span>{role} {holder.self ? 'you' : holder.name}</span></span>
    {/if}
  </span>
{/if}

// The shared identity resolver (agent state, initials, avatar tone), loaded once for Svelte. The Team console loads the same file.
import '../../../internal/uipresentation/static/identity.js';
export type { AgentRing, AgentStateInput, AgentStateRequest, AgentStateView } from '../../../internal/uipresentation/static/identity.js';
export const { resolveAgentState, ageLabel, initials, avatarTone } = globalThis.WerkbordIdentity;

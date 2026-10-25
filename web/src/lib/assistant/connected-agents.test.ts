import { describe, expect, it } from 'vitest';
import { connectedAgents, nativeCLILaunchers } from './connected-agents';
import type { CatalogAgent, AgentPreference } from '../selection';
const agent: CatalogAgent = {id:'codex',name:'Codex',installed:true,available:true,command:'codex',setup:'',adapter:false,assistant:true,coding:true,
  interactive:{supported:true,authorized:true,device:'local',authentication:'verified'}};
const preference: AgentPreference = {enabled:false,billing:'excluded',directCLI:true};
describe('connected interactive coding agents', () => {
  it('separates direct CLI eligibility from automatic selection, billing, ACP and Sally provider support', () => {
    expect(connectedAgents([agent],{codex:preference})).toEqual([agent]);
    expect(connectedAgents([{...agent,assistant:false}],{codex:preference})).toHaveLength(1);
    expect(connectedAgents([agent],{codex:{...preference,directCLI:false,enabled:true,billing:'subscription'}})).toEqual([]);
  });
  it.each([
    {installed:false}, {available:false}, {interactive:undefined},
    {interactive:{...agent.interactive!,supported:false}},
    {interactive:{...agent.interactive!,authorized:false}},
    {interactive:{...agent.interactive!,device:''}},
    {interactive:{...agent.interactive!,authentication:'signed_out' as const}},
    {interactive:{...agent.interactive!,authentication:'unknown' as const}},
  ])('excludes a missing eligibility gate: %j', patch => {
    expect(connectedAgents([{...agent,...patch}],{codex:preference})).toEqual([]);
  });
  it('accepts explicit sign-in declaration only when authentication is unknown', () => {
    const prefs = {codex:{...preference,cliSignedIn:true}};
    expect(connectedAgents([{...agent,interactive:{...agent.interactive!,authentication:'unknown'}}],prefs)).toHaveLength(1);
    expect(connectedAgents([{...agent,interactive:{...agent.interactive!,authentication:'signed_out'}}],prefs)).toEqual([]);
  });
  it('reflects new settings immediately and has no nonfunctional Phase 1 launchers', () => {
    const settings = {codex:{...preference}};
    expect(connectedAgents([agent],settings)).toHaveLength(1);
    settings.codex.directCLI = false;
    expect(connectedAgents([agent],settings)).toHaveLength(0);
    expect(nativeCLILaunchers.size).toBe(0);
  });
});

import { request } from './api';
import type { Agent, AgentOptions, ExecutionConfig } from './types';
export type SelectionMode = 'lowest_cost' | 'max_reasoning' | 'highest_intelligence' | 'manual';
export const SELECTION_MODES: {value: SelectionMode; label: string; description: string}[] = [
 {value:'lowest_cost',label:'Lowest cost',description:'Prefer included subscription usage, then estimated API cost. Choose enough capability for the task.'},
 {value:'max_reasoning',label:'Max reasoning',description:'Choose a suitable model and use its highest reported reasoning level.'},
 {value:'highest_intelligence',label:'Highest intelligence',description:'Prioritise the strongest suitable model and deep reasoning for the task.'},
 {value:'manual',label:'Manual',description:'Choose the agent, model and reasoning level yourself.'},
];
export function effectiveMode(c: Pick<ExecutionConfig,'mode'|'agent'>): SelectionMode { return c.mode ?? (c.agent ? 'manual' : 'lowest_cost'); }
export function modeLabel(mode: SelectionMode): string { return SELECTION_MODES.find(x=>x.value===mode)?.label ?? 'Lowest cost'; }
export interface ModelEconomics { input?:number; output?:number; intelligence?:number }
export interface AgentPreference { enabled:boolean; billing:'auto'|'subscription'|'metered'|'excluded'; models?:Record<string,ModelEconomics> }
export interface SelectionSettings { allowMetered:boolean; agents:Record<string,AgentPreference> }
export interface CatalogAgent extends Agent { command:string; setup:string; adapter:boolean; assistant:boolean; coding:boolean }
export interface SelectionDecision { agent:string; model:string; reasoning:string; runner?:string; mode:SelectionMode; method:string; reason:string }
export const selectionApi = {
 settings:()=>request<SelectionSettings>('GET','/api/settings/selection'),
 save:(value:SelectionSettings)=>request<SelectionSettings>('PUT','/api/settings/selection',value),
 catalog:()=>request<{agents:CatalogAgent[]}>('GET','/api/agents/catalog').then(r=>r.agents),
 options:(id:string)=>request<AgentOptions>('GET',`/api/agents/${encodeURIComponent(id)}/options`),
};

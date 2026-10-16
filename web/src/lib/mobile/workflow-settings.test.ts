import { render } from 'svelte/server';
import { describe, expect, it } from 'vitest';
import WorkflowSettings from './WorkflowSettings.svelte';
import { targetLabel, WORKFLOWS, place, initialLayout, type DeckSlot } from './layout';

const projects = [{id:'one',name:'One'}, {id:'two',name:'Two'}];
describe('workflow configuration', () => {
  for (const preset of WORKFLOWS) it(`configures ${preset.label} with inherited models and scoped capabilities`, () => {
    const slot: DeckSlot = {id:'button',button:preset.id,target:'all',output:'drafts',approval:'changes',maxRepositories:3};
    const html = render(WorkflowSettings, {props:{slot,onclose:()=>{},onsave:async()=>{},onrun:async()=>{}}}).body;
    expect(html).toContain(`Configure ${preset.label}`);
    for (const text of ['Current project', 'Selected repositories', 'All accessible repositories', 'Ask every time', 'Output', 'Approval', 'Maximum repositories', 'Run Once With Different Settings']) {
      expect(html).toContain(text);
    }
    expect(html).not.toContain('gpt-');
    expect(html).not.toContain('>Reasoning ');
    const saved = JSON.parse(JSON.stringify(place(initialLayout().slots, 6, slot)));
    expect(saved[6]).toEqual(slot);
    expect(targetLabel(slot,projects,'one')).toBe('All accessible repositories');
  });
  it('labels dynamic, explicit, legacy, missing and prompted scopes before execution', () => {
    const slot: DeckSlot = {id:'x',button:'find-bugs'};
    expect(targetLabel(slot,projects,'one')).toBe('Current: One');
    expect(targetLabel(slot,projects,'two')).toBe('Current: Two');
    expect(targetLabel({...slot,target:'selected',projectIds:['two','one']},projects,'one')).toBe('2 repos: Two, One');
    expect(targetLabel({...slot,projectId:'two'},projects,'one')).toBe('Two');
    expect(targetLabel({...slot,target:'selected',projectIds:['gone']},projects,'one')).toBe('Unavailable');
    expect(targetLabel({...slot,target:'ask'},projects,'one')).toBe('Ask every time');
  });
});

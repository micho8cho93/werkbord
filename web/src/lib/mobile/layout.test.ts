import { describe, expect, it } from 'vitest';
import { initialLayout, isWorkspaceButton, newSlot, place, rearrange, WORKFLOWS, workflow } from './layout';

describe('configurable Control Deck', () => {
  it('asks for a repository on tap and makes findings actionable without configuring a new button', () => {
    expect(newSlot('bugs', 'find-bugs')).toEqual({id:'bugs',button:'find-bugs',target:'ask',output:'drafts'});
    expect(newSlot('summary', 'sprint-summary')).toMatchObject({target:'ask',output:'report'});
    expect(newSlot('projects', 'projects')).toEqual({id:'projects',button:'projects'});
  });
  it('starts with the existing six positions and three empty sockets', () => {
    expect(initialLayout().slots.map(s => s?.button ?? null)).toEqual(['needs', 'calendar', 'projects', 'mywork', 'agents', 'analytics', null, null, null]);
  });
  it('adds, replaces and removes without filling other empty positions', () => {
    const added = place(initialLayout().slots, 8, { id: 'one', button: 'find-bugs' });
    expect(added[6]).toBeNull(); expect(added[7]).toBeNull(); expect(added[8]?.button).toBe('find-bugs');
    const replaced = place(added, 8, { id: 'two', button: 'git' });
    expect(replaced[8]?.button).toBe('git');
    expect(place(replaced, 8, null)).toEqual(initialLayout().slots);
    expect(added[8]?.button).toBe('find-bugs');
  });
  it('swaps occupied positions and preserves empty positions when moving', () => {
    const before = initialLayout().slots;
    const moved = rearrange(before, 0, 8);
    expect(moved[0]).toBeNull(); expect(moved[8]?.button).toBe('needs');
    expect(rearrange(before, 0, 1).slice(0, 2).map(s => s?.button)).toEqual(['calendar', 'needs']);
    expect(before[0]?.button).toBe('needs');
  });
  it('allows a completely empty deck and rejects positions outside the nine sockets', () => {
    let slots = initialLayout().slots;
    for (let i = 0; i < 9; i++) slots = place(slots, i, null);
    expect(slots).toEqual(Array(9).fill(null));
    expect(() => place(slots, 9, { id: 'extra', button: 'git' })).toThrow();
    expect(() => rearrange(slots, 0, -1)).toThrow();
    expect(() => rearrange(slots, 1.5, 2)).toThrow();
  });
  it('has six fixed workflow definitions with honest descriptions', () => {
    expect(WORKFLOWS.map(w => w.label)).toEqual(['Find Bugs', 'Improve Tests', 'Security Audit', 'Dependency Check', 'Refactor Codebase', 'Sprint Summary']);
    expect(workflow('git')).toBeUndefined();
    expect(workflow('sprint-summary')?.description).toContain('14 days');
  });
});

describe('a generated workspace on the Control Deck', () => {
  const id = 'pws_abcdefghij234567';
  it('is a plain button named by the workspace, with no workflow settings', () => {
    expect(isWorkspaceButton(id)).toBe(true);
    expect(workflow(id)).toBeUndefined();
    expect(newSlot('slot', id)).toEqual({ id: 'slot', button: id });
  });
  it('refuses anything that is not a workspace identifier', () => {
    for (const bad of ['pws_', 'pws_ABCDEFGHIJ234567', 'pcl_abcdefghij234567', 'pws_abcdefghij23456', 'find-bugs', '']) expect(isWorkspaceButton(bad)).toBe(false);
  });
  it('takes any empty position and survives rearranging', () => {
    const placed = place(initialLayout().slots, 7, newSlot('slot', id));
    expect(placed[7]?.button).toBe(id);
    expect(rearrange(placed, 7, 6)[6]?.button).toBe(id);
  });
});

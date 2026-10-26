import { describe, expect, it } from 'vitest';
import { messageParts } from './handoff';

describe('readable agent handoffs', () => {
  it('keeps surrounding prose and accepts both protocol names', () => {
    for (const name of ['werkbord-handoff', 'werkbord-handoff']) {
      const parts = messageParts(`Before\n<${name}>{"summary":"Fixed","tests":["Passed",42],"nextAction":"Review"}</${name}>\nAfter`);
      expect(parts[0]).toEqual({ text: 'Before\n' });
      expect(parts[1].handoff).toMatchObject({ summary: 'Fixed', tests: ['Passed'], nextAction: 'Review' });
      expect(parts[2]).toEqual({ text: '\nAfter' });
    }
  });
  it('leaves incomplete, malformed and unrelated agent output readable', () => {
    for (const text of ['Ordinary prose', '<werkbord-handoff>{"summary":"partial"', '<werkbord-handoff>bad json</werkbord-handoff>', '<werkbord-handoff>[]</werkbord-handoff>', '<werkbord-handoff>{"foo":"bar"}</werkbord-handoff>']) {
      expect(messageParts(text)).toEqual([{ text }]);
    }
  });
  it('does not hide an invalid block before a valid handoff or coerce structured values', () => {
    const invalid = '<werkbord-handoff>invalid</werkbord-handoff>';
    const parts = messageParts(invalid + '<werkbord-handoff>{"objective":"Inspect","results":{},"blockers":["Needs input",{}]}</werkbord-handoff>');
    expect(parts[0]).toEqual({ text: invalid });
    expect(parts[1].handoff).toMatchObject({ objective: 'Inspect', results: '', blockers: ['Needs input'] });
  });
});

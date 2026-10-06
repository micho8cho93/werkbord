import { describe, expect, it } from 'vitest';
import { messageParts } from './handoff';

describe('readable agent handoffs', () => {
  it('keeps surrounding prose and accepts both protocol names', () => {
    for (const name of ['devboard-handoff', 'werkbord-handoff']) {
      const parts = messageParts(`Before\n<${name}>{"summary":"Fixed","tests":["Passed",42],"nextAction":"Review"}</${name}>\nAfter`);
      expect(parts[0]).toEqual({ text: 'Before\n' });
      expect(parts[1].handoff).toMatchObject({ summary: 'Fixed', tests: ['Passed'], nextAction: 'Review' });
      expect(parts[2]).toEqual({ text: '\nAfter' });
    }
  });
  it('leaves incomplete, malformed and unrelated agent output readable', () => {
    for (const text of ['Ordinary prose', '<devboard-handoff>{"summary":"partial"', '<devboard-handoff>bad json</devboard-handoff>', '<devboard-handoff>[]</devboard-handoff>', '<devboard-handoff>{"foo":"bar"}</devboard-handoff>']) {
      expect(messageParts(text)).toEqual([{ text }]);
    }
  });
  it('does not hide an invalid block before a valid handoff or coerce structured values', () => {
    const invalid = '<devboard-handoff>invalid</devboard-handoff>';
    const parts = messageParts(invalid + '<devboard-handoff>{"objective":"Inspect","results":{},"blockers":["Needs input",{}]}</devboard-handoff>');
    expect(parts[0]).toEqual({ text: invalid });
    expect(parts[1].handoff).toMatchObject({ objective: 'Inspect', results: '', blockers: ['Needs input'] });
  });
});

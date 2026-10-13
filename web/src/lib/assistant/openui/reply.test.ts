import { describe, expect, it } from 'vitest';
import { emptyReplyPartsCache, growingReplyParts, replyParts } from './reply';

describe('OpenUI sections in assistant replies', () => {
  it('separates prose and a complete OpenUI Lang fence', () => {
    expect(replyParts('Here is the summary.\n\n```openui-lang\nroot = Stack([title])\ntitle = Heading("Plan")\n```\n\nThe totals are from the board.')).toEqual([
      { kind: 'text', text: 'Here is the summary.\n\n' },
      { kind: 'openui', text: 'root = Stack([title])\ntitle = Heading("Plan")\n', closed: true },
      { kind: 'text', text: '\nThe totals are from the board.' },
    ]);
  });

  it('accepts the shorter OpenUI fence label used by earlier generated replies', () => {
    expect(replyParts('```openui\nroot = Stack([])\n```')).toEqual([{ kind: 'openui', text: 'root = Stack([])\n', closed: true }]);
  });

  it('keeps an incomplete stream progressive and marks it unfinished', () => {
    expect(replyParts('Before\n```openui-lang\nroot = Stack([title])\ntitle = Head', true)).toEqual([
      { kind: 'text', text: 'Before\n' },
      { kind: 'openui', text: 'root = Stack([title])\ntitle = Head', closed: false },
    ]);
  });

  it('holds an unfinished opening fence out of the prose while streaming', () => {
    expect(replyParts('One\n```openui-la', true)).toEqual([
      { kind: 'text', text: 'One\n' },
      { kind: 'openui', text: '', closed: false },
    ]);
  });

  it('leaves ordinary fenced code blocks as prose for the safe Markdown reader', () => {
    const text = 'Example:\n```html\n<strong>safe text</strong>\n```';
    expect(replyParts(text)).toEqual([{ kind: 'text', text }]);
  });

  it('keeps a completed OpenUI section stable as later prose arrives', () => {
    const cache = emptyReplyPartsCache();
    const first = growingReplyParts('Summary.\n```openui\nroot = Stack([])\n```\nMore', true, cache);
    const prose = first[0];
    const ui = first[1];
    const next = growingReplyParts('Summary.\n```openui\nroot = Stack([])\n```\nMore words arrive.', true, cache);
    expect(next[0]).toBe(prose);
    expect(next[1]).toBe(ui);
    expect(next[2]).toMatchObject({ kind: 'text', text: '\nMore words arrive.' });
  });
});

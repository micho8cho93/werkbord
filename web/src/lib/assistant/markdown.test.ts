import { describe, expect, it } from 'vitest';
import { blocks, emptyMarkdownBlockCache, growingBlocks, inline } from './markdown';

describe('inline', () => {
  it('reads bold, italic and code', () => {
    expect(inline('a **b** and *c* and `d`')).toEqual([
      { t: 'text', v: 'a ' },
      { t: 'b', c: [{ t: 'text', v: 'b' }] },
      { t: 'text', v: ' and ' },
      { t: 'i', c: [{ t: 'text', v: 'c' }] },
      { t: 'text', v: ' and ' },
      { t: 'code', v: 'd' },
    ]);
  });
  it('leaves what does not close as the characters it is (a reply still arriving)', () => {
    expect(inline('half **bold')).toEqual([{ t: 'text', v: 'half **bold' }]);
    expect(inline('a ` b')).toEqual([{ t: 'text', v: 'a ` b' }]);
    expect(inline('2 * 3 * 4')).toEqual([{ t: 'text', v: '2 * 3 * 4' }]);
  });
  it('does not italicise snake_case', () => {
    expect(inline('use list_blocked_tickets and get_ticket')).toEqual([{ t: 'text', v: 'use list_blocked_tickets and get_ticket' }]);
  });
  it('keeps code literal: nothing inside it is formatted', () => {
    expect(inline('`**x**`')).toEqual([{ t: 'code', v: '**x**' }]);
  });
  it('turns nothing into markup: tags and links are just text', () => {
    const t = '<img src=x onerror=alert(1)> [click](javascript:alert(1)) <script>x</script>';
    expect(inline(t)).toEqual([{ t: 'text', v: t }]);
  });
});

describe('blocks', () => {
  it('reads paragraphs, lists, headings and quotes', () => {
    const b = blocks('# Title\n\nOne.\nStill one.\n\n- a\n- b\n\n3. c\n4. d\n\n> quoted\n> more');
    expect(b.map((x) => x.t)).toEqual(['h', 'p', 'ul', 'ol', 'quote']);
    expect(b[2]).toMatchObject({ t: 'ul', items: [[{ t: 'text', v: 'a' }], [{ t: 'text', v: 'b' }]] });
    expect(b[3]).toMatchObject({ t: 'ol', start: 3 });
    expect(b[1]).toMatchObject({ t: 'p' });
  });
  it('reads a code fence, with its language, literally', () => {
    expect(blocks('```json\n{"a": "**b**"}\n```\nafter')).toEqual([
      { t: 'code', lang: 'json', text: '{"a": "**b**"}' },
      { t: 'p', c: [{ t: 'text', v: 'after' }] },
    ]);
  });
  it('treats a fence that has not closed yet as code so far', () => {
    expect(blocks('Look:\n```\nline 1\nline 2')).toEqual([
      { t: 'p', c: [{ t: 'text', v: 'Look:' }] },
      { t: 'code', lang: '', text: 'line 1\nline 2' },
    ]);
  });
  it('copes with windows line endings, blank input and a very long line', () => {
    expect(blocks('a\r\n\r\nb')).toHaveLength(2);
    expect(blocks('')).toEqual([]);
    expect(blocks('x'.repeat(100000))).toHaveLength(1);
  });
  it('keeps completed paragraphs cached while parsing only the growing tail', () => {
    const cache = emptyMarkdownBlockCache();
    const first = growingBlocks('First paragraph.\n\nStill arriving', cache);
    const stable = cache.stableBlocks[0];
    const next = growingBlocks('First paragraph.\n\nStill arriving, with more words.\n\nLast line', cache);
    expect(next.map((b) => b.t)).toEqual(['p', 'p', 'p']);
    expect(cache.stableBlocks[0]).toBe(stable);
    expect(first[0]).toBe(stable);
  });
  it('never nests so deep that a hostile reply could cost anything', () => {
    const deep = '*'.repeat(200) + 'x' + '*'.repeat(200);
    const t0 = Date.now();
    blocks(deep);
    expect(Date.now() - t0).toBeLessThan(500);
  });
});

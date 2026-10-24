import { describe, expect, it } from 'vitest';
import { byteLength, normalizeHref, parseMarkdown, safeHref, schema, toMarkdown } from './markdown';

const roundTrip = (source: string) => toMarkdown(parseMarkdown(source));
function kinds(source: string): string[] {
  const found = new Set<string>();
  parseMarkdown(source).descendants((node) => { found.add(node.type.name); node.marks.forEach((m) => found.add(m.type.name)); });
  return [...found];
}

describe('document Markdown', () => {
  it.each([
    '# Title\n\nA paragraph with **bold**, *italic*, ~~strike~~ and `code`.',
    '## Second level\n\n### Third level',
    '- one\n- two\n  - nested\n- three',
    '1. first\n2. second\n3. third',
    '- [ ] open task\n- [x] finished task',
    '> a quoted line\n>\n> another',
    '```js\nconst a = 1;\n```',
    '---',
    'See [the docs](https://example.com/a) and [[Another page]].',
    'Line one\\\nline two',
  ])('is stable through the editor: %s', (source) => {
    const once = roundTrip(source);
    expect(roundTrip(once)).toBe(once);
    expect(once).toBe(source);
  });

  it('reads tasks as list items that are checked or not', () => {
    const doc = parseMarkdown('- [ ] a\n- [x] b\n- c');
    const states: unknown[] = [];
    doc.descendants((node) => { if (node.type.name === 'list_item') states.push(node.attrs.checked); });
    expect(states).toEqual([false, true, null]);
  });

  it('keeps wiki links as one inert node that names a title', () => {
    expect(kinds('Go to [[Project plan]] now')).toContain('wiki_link');
    const doc = parseMarkdown('[[Project plan]]');
    expect(doc.firstChild?.firstChild?.attrs.title).toBe('Project plan');
    for (const text of ['[[]]', '[[ ]]', '[[a\nb]]', `[[${'x'.repeat(121)}]]`]) expect(kinds(text)).not.toContain('wiki_link');
  });

  it('never keeps raw HTML or scripts: they are written out as plain text', () => {
    const source = 'Hello <script>alert(1)</script> <img src=x onerror=alert(1)> <b>bold</b>\n\n<div onclick="x()">block</div>';
    const names = kinds(source);
    expect(names.filter((n) => !['doc', 'paragraph', 'text'].includes(n))).toEqual([]);
    expect(roundTrip(source)).toContain('<script>');
    expect(JSON.stringify(parseMarkdown(source).toJSON())).not.toMatch(/"type":"(html|script|image)/);
  });

  it.each([
    '[click](javascript:alert(1))', '[click](data:text/html,<b>x</b>)', '[click](vbscript:x)', '[click](file:///etc/passwd)',
  ])('does not make a link of %s', (source) => {
    expect(kinds(source)).not.toContain('link');
    expect(parseMarkdown(source).textContent).toContain('click');
  });

  it('fetches no pictures: an image is written out as the link it names', () => {
    const names = kinds('![pic](https://example.com/a.png)');
    expect(names).not.toContain('image');
    expect(schema.nodes).not.toHaveProperty('image');
  });

  it('opens web and mail addresses only', () => {
    expect(safeHref('https://example.com')).toBe('https://example.com');
    expect(safeHref('mailto:a@example.com')).toBe('mailto:a@example.com');
    for (const bad of ['javascript:alert(1)', 'JaVaScRiPt:alert(1)', 'data:text/html,x', 'file:///x', '//example.com', 'https://a b', '', null, 4]) expect(safeHref(bad)).toBeNull();
    expect(normalizeHref('example.com/path')).toBe('https://example.com/path');
    expect(normalizeHref('a@example.com')).toBe('mailto:a@example.com');
    expect(normalizeHref('http://localhost:8080')).toBe('http://localhost:8080');
    for (const bad of ['', 'two words', 'javascript:alert(1)', 'nodot']) expect(normalizeHref(bad)).toBeNull();
  });

  it('survives text that looks like markup', () => {
    const source = 'a_b_c and 2 * 3 * 4, # not a heading, 1) paren and <tag> and \\* escaped';
    expect(roundTrip(roundTrip(source))).toBe(roundTrip(source));
    expect(parseMarkdown(source).textContent).toContain('2 * 3 * 4');
  });

  it('gives an empty document one paragraph to type into', () => {
    const doc = parseMarkdown('');
    expect(doc.childCount).toBe(1);
    expect(toMarkdown(doc)).toBe('');
    expect(schema.nodes.doc.validContent(doc.content)).toBe(true);
  });

  it('counts the stored size in bytes, not characters', () => {
    expect(byteLength('é')).toBe(2);
    expect(byteLength('日本')).toBe(6);
  });
});

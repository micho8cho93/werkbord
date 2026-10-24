// The rich text a workspace document is written in. It is stored as Markdown in a long text field and nothing else: no HTML,
// no scripts, no styles. The editor works on this schema, so whatever is stored is rebuilt from known nodes and marks, and a
// document can only ever contain what this file lists.

import MarkdownIt, { type MarkdownIt as Tokenizer, type StateCore, type StateInline } from 'markdown-it';
import { MarkdownParser, MarkdownSerializer } from 'prosemirror-markdown';
import { Schema, type MarkSpec, type Node, type NodeSpec } from 'prosemirror-model';
import { byteLength } from './size';
export { byteLength };

/** Only web addresses and mail addresses open from a document; anything else is kept as plain text. */
export function safeHref(href: unknown): string | null {
  return typeof href === 'string' && href.length <= 2000 && /^(https?:\/\/|mailto:)[^\s]+$/i.test(href) ? href : null;
}
/** What a person types into a link box: a bare address gets https. */
export function normalizeHref(input: string): string | null {
  const trimmed = input.trim();
  if (!trimmed || /\s/.test(trimmed)) return null;
  if (/^[a-z][a-z0-9+.-]*:/i.test(trimmed)) return safeHref(trimmed);
  return /^[^\s/@]+\.[^\s/@]{2,}(\/\S*)?$/.test(trimmed) ? safeHref(`https://${trimmed}`) : /^[^\s@]+@[^\s@]+\.[^\s@]{2,}$/.test(trimmed) ? safeHref(`mailto:${trimmed}`) : null;
}

const nodes: Record<string, NodeSpec> = {
  doc: { content: 'block+' },
  paragraph: { content: 'inline*', group: 'block', parseDOM: [{ tag: 'p' }], toDOM: () => ['p', 0] },
  blockquote: { content: 'block+', group: 'block', defining: true, parseDOM: [{ tag: 'blockquote' }], toDOM: () => ['blockquote', 0] },
  horizontal_rule: { group: 'block', parseDOM: [{ tag: 'hr' }], toDOM: () => ['hr'] },
  heading: {
    attrs: { level: { default: 1 } }, content: 'inline*', group: 'block', defining: true,
    parseDOM: [1, 2, 3, 4, 5, 6].map((level) => ({ tag: `h${level}`, attrs: { level } })),
    toDOM: (node) => [`h${node.attrs.level}`, 0],
  },
  code_block: {
    content: 'text*', marks: '', group: 'block', code: true, defining: true, attrs: { params: { default: '' } },
    parseDOM: [{ tag: 'pre', preserveWhitespace: 'full' }], toDOM: () => ['pre', ['code', 0]],
  },
  ordered_list: {
    attrs: { order: { default: 1 } }, content: 'list_item+', group: 'block',
    parseDOM: [{ tag: 'ol', getAttrs: (dom) => ({ order: Number((dom as HTMLElement).getAttribute('start')) || 1 }) }],
    toDOM: (node) => (node.attrs.order === 1 ? ['ol', 0] : ['ol', { start: node.attrs.order }, 0]),
  },
  bullet_list: { content: 'list_item+', group: 'block', parseDOM: [{ tag: 'ul' }], toDOM: () => ['ul', 0] },
  // checked is null for a plain item, false or true for a task.
  list_item: {
    attrs: { checked: { default: null } }, content: 'paragraph block*', defining: true,
    parseDOM: [{ tag: 'li', getAttrs: (dom) => { const task = (dom as HTMLElement).getAttribute('data-task'); return { checked: task === 'done' ? true : task === 'todo' ? false : null }; } }],
    toDOM: (node) => (node.attrs.checked === null ? ['li', 0] : ['li', { 'data-task': node.attrs.checked ? 'done' : 'todo' }, 0]),
  },
  text: { group: 'inline' },
  hard_break: { inline: true, group: 'inline', selectable: false, parseDOM: [{ tag: 'br' }], toDOM: () => ['br'] },
  // [[Page title]]: a link to another page of the same workspace. It names a title, never an address.
  wiki_link: {
    inline: true, group: 'inline', atom: true, attrs: { title: { default: '' } },
    parseDOM: [{ tag: 'span[data-wiki]', getAttrs: (dom) => ({ title: (dom as HTMLElement).getAttribute('data-wiki') ?? '' }) }],
    toDOM: (node) => ['span', { class: 'wiki-link', 'data-wiki': node.attrs.title }, node.attrs.title],
  },
};
const marks: Record<string, MarkSpec> = {
  link: {
    attrs: { href: {} }, inclusive: false,
    parseDOM: [{ tag: 'a[href]', getAttrs: (dom) => { const href = safeHref((dom as HTMLElement).getAttribute('href')); return href ? { href } : false; } }],
    toDOM: (mark) => ['a', { href: safeHref(mark.attrs.href) ?? undefined, target: '_blank', rel: 'noopener noreferrer' }, 0],
  },
  em: { parseDOM: [{ tag: 'i' }, { tag: 'em' }, { style: 'font-style=italic' }], toDOM: () => ['em', 0] },
  strong: { parseDOM: [{ tag: 'strong' }, { tag: 'b' }, { style: 'font-weight=bold' }, { style: 'font-weight=700' }], toDOM: () => ['strong', 0] },
  strike: { parseDOM: [{ tag: 's' }, { tag: 'del' }, { tag: 'strike' }], toDOM: () => ['s', 0] },
  code: { code: true, parseDOM: [{ tag: 'code' }], toDOM: () => ['code', 0] },
};
export const schema = new Schema({ nodes, marks });

function wikiLinks(md: Tokenizer) {
  md.inline.ruler.before('link', 'wiki_link', (state: StateInline, silent: boolean) => {
    const { src, pos } = state;
    if (src.charCodeAt(pos) !== 0x5b || src.charCodeAt(pos + 1) !== 0x5b) return false;
    const end = src.indexOf(']]', pos + 2);
    if (end === -1) return false;
    const title = src.slice(pos + 2, end).trim();
    if (!title || title.length > 120 || /[[\]\n]/.test(title)) return false;
    if (!silent) state.push('wiki_link', '', 0).content = title;
    state.pos = end + 2;
    return true;
  });
}
/** "- [ ] item" and "- [x] item": the marker moves from the text onto the list item. */
function taskLists(md: Tokenizer) {
  md.core.ruler.after('inline', 'task_lists', (state: StateCore) => {
    const tokens = state.tokens;
    for (let i = 2; i < tokens.length; i++) {
      if (tokens[i].type !== 'inline' || tokens[i - 1].type !== 'paragraph_open' || tokens[i - 2].type !== 'list_item_open') continue;
      const first = tokens[i].children?.[0];
      const marker = /^\[([ xX])\](?: +|$)/.exec(first?.type === 'text' ? first.content : '');
      if (!marker || !first) continue;
      first.content = first.content.slice(marker[0].length);
      tokens[i].content = tokens[i].content.slice(marker[0].length);
      tokens[i - 2].attrSet('checked', marker[1] === ' ' ? 'false' : 'true');
    }
    return true;
  });
}
function tokenizer() {
  // 'commonmark' has no raw HTML switched on by us and html:false drops any that is written: it is shown as text, never run.
  const md = new MarkdownIt('commonmark', { html: false, linkify: false, breaks: false }).enable(['strikethrough']).disable(['image']); // Pictures are not part of a document: nothing is fetched.
  md.use(wikiLinks).use(taskLists);
  md.validateLink = (url) => safeHref(url) !== null;
  return md;
}

export const parser = new MarkdownParser(schema, tokenizer(), {
  blockquote: { block: 'blockquote' },
  paragraph: { block: 'paragraph' },
  list_item: { block: 'list_item', getAttrs: (token) => { const checked = token.attrGet('checked'); return { checked: checked === null ? null : checked === 'true' }; } },
  bullet_list: { block: 'bullet_list' },
  ordered_list: { block: 'ordered_list', getAttrs: (token) => ({ order: Number(token.attrGet('start')) || 1 }) },
  heading: { block: 'heading', getAttrs: (token) => ({ level: Number(token.tag.slice(1)) }) },
  code_block: { block: 'code_block', noCloseToken: true },
  fence: { block: 'code_block', getAttrs: (token) => ({ params: (token.info || '').replace(/[^\w+#.-]/g, '').slice(0, 30) }), noCloseToken: true },
  hr: { node: 'horizontal_rule' },
  hardbreak: { node: 'hard_break' },
  wiki_link: { node: 'wiki_link', getAttrs: (token) => ({ title: token.content }), noCloseToken: true },
  em: { mark: 'em' },
  strong: { mark: 'strong' },
  s: { mark: 'strike' },
  link: { mark: 'link', getAttrs: (token) => ({ href: safeHref(token.attrGet('href')) ?? '' }) },
  code_inline: { mark: 'code', noCloseToken: true },
});

function backticks(node: Node, side: number): string {
  const ticks = /`+/g;
  let match: RegExpExecArray | null;
  let len = 0;
  if (node.isText) while ((match = ticks.exec(node.text ?? ''))) len = Math.max(len, match[0].length);
  let result = len > 0 && side > 0 ? ' `' : '`';
  for (let i = 0; i < len; i++) result += '`';
  if (len > 0 && side < 0) result += ' ';
  return result;
}

export const serializer = new MarkdownSerializer(
  {
    blockquote(state, node) { state.wrapBlock('> ', null, node, () => state.renderContent(node)); },
    code_block(state, node) {
      const fences = node.textContent.match(/`{3,}/gm);
      const fence = fences ? `${fences.sort().slice(-1)[0]}\`` : '```';
      state.write(`${fence}${node.attrs.params || ''}\n`);
      state.text(node.textContent, false);
      state.write('\n');
      state.write(fence);
      state.closeBlock(node);
    },
    heading(state, node) { state.write(`${state.repeat('#', node.attrs.level)} `); state.renderInline(node, false); state.closeBlock(node); },
    horizontal_rule(state, node) { state.write('---'); state.closeBlock(node); },
    bullet_list(state, node) { state.renderList(node, '  ', () => '- '); },
    ordered_list(state, node) {
      const start = node.attrs.order || 1;
      const width = String(start + node.childCount - 1).length;
      const space = state.repeat(' ', width + 2);
      state.renderList(node, space, (i) => `${state.repeat(' ', width - String(start + i).length)}${start + i}. `);
    },
    list_item(state, node) {
      if (node.attrs.checked !== null) state.write(node.attrs.checked ? '[x] ' : '[ ] ');
      state.renderContent(node);
    },
    paragraph(state, node) { state.renderInline(node); state.closeBlock(node); },
    text(state, node) { state.text(node.text ?? ''); },
    hard_break(state, node, parent, index) {
      for (let i = index + 1; i < parent.childCount; i++) if (parent.child(i).type !== node.type) { state.write('\\\n'); return; }
    },
    wiki_link(state, node) { state.write(`[[${String(node.attrs.title).replace(/[[\]\n]/g, ' ')}]]`); },
  },
  {
    em: { open: '*', close: '*', mixable: true, expelEnclosingWhitespace: true },
    strong: { open: '**', close: '**', mixable: true, expelEnclosingWhitespace: true },
    strike: { open: '~~', close: '~~', mixable: true, expelEnclosingWhitespace: true },
    link: {
      open: () => '[',
      close: (_state, mark) => `](${String(mark.attrs.href).replace(/[()"\s]/g, (c) => encodeURIComponent(c) === c ? `\\${c}` : encodeURIComponent(c))})`,
    },
    code: { open: (_state, _mark, parent, index) => backticks(parent.child(index), -1), close: (_state, _mark, parent, index) => backticks(parent.child(index - 1), 1), escape: false },
  },
);

export function parseMarkdown(markdown: string): Node {
  const text = markdown.replace(/\r\n?/g, '\n');
  try {
    // An empty document still has one paragraph to type into.
    return parser.parse(text) ?? schema.node('doc', null, [schema.node('paragraph')]);
  } catch {
    // Text this schema cannot read is still shown, as plain paragraphs, and never lost.
    const lines = text.split('\n').map((line) => (line ? schema.node('paragraph', null, [schema.text(line)]) : schema.node('paragraph')));
    return schema.node('doc', null, lines.length ? lines : [schema.node('paragraph')]);
  }
}
export function toMarkdown(doc: Node): string {
  return serializer.serialize(doc, { tightLists: true }).replace(/\n+$/, '');
}

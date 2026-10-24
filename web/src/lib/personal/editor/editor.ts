// The rich text editor behind documents and pages. ProseMirror edits the schema in markdown.ts; what is saved is the Markdown
// it serialises. Nothing here fetches, evaluates or injects markup: nodes are built from the schema, and links open only when a
// person asks, and only for web and mail addresses.

import { baseKeymap, chainCommands, createParagraphNear, lift, liftEmptyBlock, newlineInCode, setBlockType, splitBlock, toggleMark, wrapIn } from 'prosemirror-commands';
import { history, redo, undo } from 'prosemirror-history';
import { InputRule, inputRules, textblockTypeInputRule, wrappingInputRule } from 'prosemirror-inputrules';
import { keymap } from 'prosemirror-keymap';
import { type MarkType, type Node, type NodeType, Slice } from 'prosemirror-model';
import { liftListItem, sinkListItem, splitListItem, wrapInList } from 'prosemirror-schema-list';
import { EditorState, Plugin, PluginKey, TextSelection, type Command, type Transaction } from 'prosemirror-state';
import { Decoration, DecorationSet, EditorView, type NodeView } from 'prosemirror-view';
import { byteLength, normalizeHref, parseMarkdown, schema, toMarkdown } from './markdown';

const { nodes, marks } = schema;

/** What the toolbar shows: which formats the selection is in, and what can be undone. */
export interface ToolbarState {
  bold: boolean; italic: boolean; strike: boolean; code: boolean; link: boolean;
  block: 'paragraph' | 'h1' | 'h2' | 'h3' | 'bullet' | 'ordered' | 'task' | 'quote' | 'code';
  canUndo: boolean; canRedo: boolean; empty: boolean;
}
export type EditorCommand =
  | 'bold' | 'italic' | 'strike' | 'code' | 'paragraph' | 'h1' | 'h2' | 'h3' | 'bullet' | 'ordered' | 'task' | 'quote' | 'codeblock'
  | 'rule' | 'indent' | 'outdent' | 'undo' | 'redo' | 'unlink';

export interface EditorOptions {
  markdown: string;
  readOnly?: boolean;
  placeholder?: string;
  /** The most the stored Markdown may take, in bytes. Typing and pasting past it are refused. */
  maxBytes: number;
  onChange?: (markdown: string) => void;
  onState?: (state: ToolbarState) => void;
  /** A click on a [[link]]; Cmd or Ctrl click on a web link also comes here is not needed: those open themselves. */
  onWiki?: (title: string) => void;
  /** Pages that exist, so a [[link]] to a missing one is drawn differently. */
  wikiExists?: (title: string) => boolean;
  onLimit?: () => void;
  onLinkRequest?: () => void;
}
export interface EditorHandle {
  view: EditorView;
  markdown(): string;
  setMarkdown(markdown: string): void;
  run(command: EditorCommand): boolean;
  setLink(href: string): boolean;
  selectedLink(): string;
  focus(): void;
  setReadOnly(readOnly: boolean): void;
  refreshWiki(): void;
  destroy(): void;
}

function markActive(state: EditorState, type: MarkType): boolean {
  const { from, $from, to, empty } = state.selection;
  return empty ? !!type.isInSet(state.storedMarks ?? $from.marks()) : state.doc.rangeHasMark(from, to, type);
}
function blockOf(state: EditorState): ToolbarState['block'] {
  const { $from } = state.selection;
  for (let depth = $from.depth; depth >= 0; depth--) {
    const node = $from.node(depth);
    if (node.type === nodes.list_item) {
      const list = $from.node(depth - 1);
      return node.attrs.checked !== null ? 'task' : list.type === nodes.ordered_list ? 'ordered' : 'bullet';
    }
    if (node.type === nodes.blockquote) return 'quote';
    if (node.type === nodes.code_block) return 'code';
    if (node.type === nodes.heading) return node.attrs.level === 1 ? 'h1' : node.attrs.level === 2 ? 'h2' : 'h3';
  }
  return 'paragraph';
}
function toolbarState(state: EditorState): ToolbarState {
  return {
    bold: markActive(state, marks.strong), italic: markActive(state, marks.em), strike: markActive(state, marks.strike), code: markActive(state, marks.code),
    link: markActive(state, marks.link), block: blockOf(state), canUndo: undo(state), canRedo: redo(state),
    empty: state.doc.childCount === 1 && !state.doc.firstChild?.content.size && state.doc.firstChild?.type === nodes.paragraph,
  };
}

/** Switches the block under the selection to a type, or back to a paragraph when it already is one. */
function toggleBlock(type: NodeType, attrs?: Record<string, unknown>): Command {
  return (state, dispatch) => {
    const { $from } = state.selection;
    const here = $from.parent;
    const same = here.type === type && Object.entries(attrs ?? {}).every(([key, value]) => here.attrs[key] === value);
    return setBlockType(same ? nodes.paragraph : type, same ? undefined : attrs)(state, dispatch);
  };
}
function insideList(state: EditorState): { depth: number; list: NodeType } | null {
  const { $from } = state.selection;
  for (let depth = $from.depth; depth > 0; depth--) if ($from.node(depth).type === nodes.list_item) return { depth, list: $from.node(depth - 1).type };
  return null;
}
/** Bullet and numbered lists: wrap the selection, switch kind, or lift out of the list it is already in. */
function toggleList(type: NodeType): Command {
  return (state, dispatch) => {
    const found = insideList(state);
    if (found && found.list === type) {
      // Plain items of this kind lift out; tasks become plain items first so the kind button means what it says.
      const item = state.selection.$from.node(found.depth);
      if (item.attrs.checked !== null && dispatch) {
        const tr = state.tr.setNodeMarkup(state.selection.$from.before(found.depth), undefined, { ...item.attrs, checked: null });
        dispatch(tr);
        return true;
      }
      return liftListItem(nodes.list_item)(state, dispatch);
    }
    if (found) {
      dispatch?.(state.tr.setNodeMarkup(state.selection.$from.before(found.depth - 1), type, type === nodes.ordered_list ? { order: 1 } : undefined).scrollIntoView());
      return true;
    }
    return wrapInList(type)(state, dispatch);
  };
}
/** Task lists: bullets whose items have a box. Toggling again takes the boxes away and keeps the bullets. */
const toggleTask: Command = (state, dispatch) => {
  const found = insideList(state);
  if (!found) {
    return wrapInList(nodes.bullet_list)(state, dispatch && ((tr) => {
      const { $from } = tr.selection;
      for (let depth = $from.depth; depth > 0; depth--) {
        if ($from.node(depth).type === nodes.list_item) { tr.setNodeMarkup($from.before(depth), undefined, { checked: false }); break; }
      }
      dispatch(tr);
    }));
  }
  if (!dispatch) return true;
  const { $from } = state.selection;
  const item = $from.node(found.depth);
  const tr = state.tr;
  if (found.list === nodes.ordered_list) tr.setNodeMarkup($from.before(found.depth - 1), nodes.bullet_list);
  tr.setNodeMarkup($from.before(found.depth), undefined, { ...item.attrs, checked: item.attrs.checked === null ? false : null });
  dispatch(tr);
  return true;
};
const toggleQuote: Command = (state, dispatch) => {
  const { $from } = state.selection;
  for (let depth = $from.depth; depth > 0; depth--) if ($from.node(depth).type === nodes.blockquote) return lift(state, dispatch);
  return wrapIn(nodes.blockquote)(state, dispatch);
};
/** A divider, with a paragraph after it to carry on in when it ends the document. */
function withRule(tr: Transaction): Transaction {
  tr.replaceSelectionWith(nodes.horizontal_rule.create());
  const after = tr.selection.$to.after();
  if (tr.doc.resolve(Math.min(after, tr.doc.content.size)).nodeAfter === null || tr.doc.lastChild?.type === nodes.horizontal_rule) {
    tr.insert(tr.doc.content.size, nodes.paragraph.create());
    tr.setSelection(TextSelection.near(tr.doc.resolve(tr.doc.content.size - 1)));
  }
  return tr.scrollIntoView();
}
const insertRule: Command = (state, dispatch) => {
  dispatch?.(withRule(state.tr));
  return true;
};
const removeLink: Command = (state, dispatch) => {
  const { from, to, $from, empty } = state.selection;
  if (!markActive(state, marks.link)) return false;
  if (!dispatch) return true;
  if (!empty) return dispatch(state.tr.removeMark(from, to, marks.link)), true;
  // With the caret inside a link, take the whole link away.
  const start = $from.start();
  let a = $from.pos, b = $from.pos;
  const parent = $from.parent;
  parent.forEach((child, offset) => {
    const absolute = start + offset;
    if (marks.link.isInSet(child.marks) && absolute <= $from.pos && absolute + child.nodeSize >= $from.pos) { a = Math.min(a, absolute); b = Math.max(b, absolute + child.nodeSize); }
  });
  dispatch(state.tr.removeMark(a, b, marks.link));
  return true;
};

const commands: Record<EditorCommand, Command> = {
  bold: toggleMark(marks.strong), italic: toggleMark(marks.em), strike: toggleMark(marks.strike), code: toggleMark(marks.code),
  paragraph: setBlockType(nodes.paragraph),
  h1: toggleBlock(nodes.heading, { level: 1 }), h2: toggleBlock(nodes.heading, { level: 2 }), h3: toggleBlock(nodes.heading, { level: 3 }),
  bullet: toggleList(nodes.bullet_list), ordered: toggleList(nodes.ordered_list), task: toggleTask,
  quote: toggleQuote, codeblock: toggleBlock(nodes.code_block), rule: insertRule,
  indent: sinkListItem(nodes.list_item), outdent: liftListItem(nodes.list_item), undo, redo, unlink: removeLink,
};

/** "**bold**", "*italic*", "~~strike~~", "`code`": the typed marks become formatting as the closing mark is typed. */
function markRule(pattern: RegExp, type: MarkType, width: number): InputRule {
  return new InputRule(pattern, (state, match, start, end) => {
    // The last character typed is not in the document yet: only width - 1 characters of the closing mark are.
    const inner = match[1];
    const open = start + match[0].length - (inner.length + width * 2);
    const closing = open + width + inner.length;
    const tr = state.tr;
    tr.delete(closing, end).delete(open, open + width);
    tr.addMark(open, open + inner.length, type.create());
    tr.removeStoredMark(type);
    return tr;
  });
}
const taskRule = new InputRule(/^\[( |x)\]\s$/, (state, match, start, end) => {
  const { $from } = state.selection;
  const found = insideList(state);
  if (!found || $from.node(found.depth).childCount < 1 || $from.index(found.depth) !== 0) return null;
  const item = $from.node(found.depth);
  const tr = state.tr.delete(start, end);
  tr.setNodeMarkup(tr.mapping.map($from.before(found.depth)), undefined, { ...item.attrs, checked: match[1] === 'x' });
  return tr;
});
const wikiRule = new InputRule(/\[\[([^[\]\n]{1,120})\]\]$/, (state, match, start, end) => {
  const title = match[1].trim();
  return title ? state.tr.replaceWith(start, end, nodes.wiki_link.create({ title })) : null;
});
function rules() {
  return inputRules({
    rules: [
      textblockTypeInputRule(/^(#{1,3})\s$/, nodes.heading, (match) => ({ level: match[1].length })),
      wrappingInputRule(/^\s*>\s$/, nodes.blockquote),
      wrappingInputRule(/^(\d+)\.\s$/, nodes.ordered_list, (match) => ({ order: Number(match[1]) }), (match, node) => node.childCount + node.attrs.order === Number(match[1])),
      wrappingInputRule(/^\s*([-+*])\s$/, nodes.bullet_list),
      taskRule,
      textblockTypeInputRule(/^```$/, nodes.code_block),
      new InputRule(/^(?:---|___)\s$/, (state, _match, start, end) => withRule(state.tr.delete(start, end))),
      markRule(/(?:^|[^*])\*\*([^*\s](?:[^*]*[^*\s])?)\*\*$/, marks.strong, 2),
      markRule(/(?:^|[^*])\*([^*\s](?:[^*]*[^*\s])?)\*$/, marks.em, 1),
      markRule(/(?:^|[^~])~~([^~\s](?:[^~]*[^~\s])?)~~$/, marks.strike, 2),
      markRule(/(?:^|[^`])`([^`\s](?:[^`]*[^`\s])?)`$/, marks.code, 1),
      wikiRule,
    ],
  });
}

/** Task items draw a box that is outside the text, so typing never touches it. */
class ListItemView implements NodeView {
  dom: HTMLElement;
  contentDOM: HTMLElement;
  private box: HTMLInputElement | null = null;
  constructor(private node: Node, private view: EditorView, private getPos: () => number | undefined) {
    this.dom = document.createElement('li');
    this.contentDOM = document.createElement('div');
    this.contentDOM.className = 'task-content';
    this.build();
  }
  private build() {
    this.dom.replaceChildren();
    this.box = null;
    if (this.node.attrs.checked === null) {
      this.dom.removeAttribute('data-task');
      this.contentDOM.className = '';
      this.dom.append(this.contentDOM);
      return;
    }
    const label = document.createElement('span');
    label.className = 'task-box';
    label.contentEditable = 'false';
    const box = document.createElement('input');
    box.type = 'checkbox';
    box.setAttribute('aria-label', 'Done');
    box.addEventListener('change', () => {
      const pos = this.getPos();
      if (pos === undefined || !this.view.editable) { box.checked = this.node.attrs.checked === true; return; }
      this.view.dispatch(this.view.state.tr.setNodeMarkup(pos, undefined, { ...this.node.attrs, checked: box.checked }));
    });
    label.append(box);
    this.box = box;
    this.contentDOM.className = 'task-content';
    this.dom.append(label, this.contentDOM);
    this.sync();
  }
  private sync() {
    if (!this.box) return;
    this.dom.setAttribute('data-task', this.node.attrs.checked ? 'done' : 'todo');
    this.box.checked = this.node.attrs.checked === true;
    this.box.disabled = !this.view.editable;
  }
  update(node: Node) {
    if (node.type !== this.node.type) return false;
    const rebuild = (node.attrs.checked === null) !== (this.node.attrs.checked === null);
    this.node = node;
    if (rebuild) this.build(); else this.sync();
    return true;
  }
  ignoreMutation(mutation: { target: globalThis.Node }) { return !this.contentDOM.contains(mutation.target); }
  stopEvent(event: Event) { return this.box !== null && event.target === this.box; }
}

const placeholderKey = new PluginKey('placeholder');
function placeholder(text: string) {
  return new Plugin({
    key: placeholderKey,
    props: {
      decorations(state) {
        const first = state.doc.firstChild;
        if (state.doc.childCount !== 1 || !first || first.type !== nodes.paragraph || first.content.size) return null;
        return DecorationSet.create(state.doc, [Decoration.node(0, first.nodeSize, { class: 'is-empty', 'data-placeholder': text })]);
      },
    },
  });
}
const wikiKey = new PluginKey<{ exists: (title: string) => boolean }>('wiki');
/** A [[link]] to a page that does not exist is drawn as such; clicking either kind goes there (or offers to make it). */
function wikiPlugin(options: EditorOptions, current: { exists: (title: string) => boolean }) {
  return new Plugin({
    key: wikiKey,
    props: {
      decorations(state) {
        if (!options.wikiExists) return null;
        const found: Decoration[] = [];
        state.doc.descendants((node, pos) => {
          if (node.type === nodes.wiki_link && !current.exists(node.attrs.title)) found.push(Decoration.node(pos, pos + node.nodeSize, { class: 'wiki-missing' }));
        });
        return found.length ? DecorationSet.create(state.doc, found) : null;
      },
      handleClickOn(_view, _pos, node, _nodePos, event) {
        if (node.type !== nodes.wiki_link) return false;
        event.preventDefault();
        options.onWiki?.(node.attrs.title);
        return true;
      },
    },
  });
}
/** Pasted plain text is read as Markdown; pasted pages keep only what the schema allows. */
function pasteMarkdown() {
  return new Plugin({
    props: {
      clipboardTextParser(text, $context) {
        const doc = parseMarkdown(text);
        void $context;
        return new Slice(doc.content, 0, 0);
      },
    },
  });
}
/** Typing past the limit is refused, not truncated, so no one loses the end of a document they did not see cut. */
function limitPlugin(options: EditorOptions) {
  return new Plugin({
    filterTransaction(tr: Transaction) {
      if (!tr.docChanged) return true;
      // Every character takes at most four bytes, and Markdown marks add few: stay out of the way for short documents.
      if (tr.doc.content.size * 4 + 2000 < options.maxBytes) return true;
      const next = byteLength(toMarkdown(tr.doc));
      if (next <= options.maxBytes || next <= byteLength(toMarkdown(tr.before))) return true;
      options.onLimit?.();
      return false;
    },
  });
}

export function mountEditor(host: HTMLElement, options: EditorOptions): EditorHandle {
  const current = { exists: options.wikiExists ?? (() => true) };
  let readOnly = options.readOnly ?? false;
  let last = options.markdown;
  const linkHook: Command = () => { options.onLinkRequest?.(); return !!options.onLinkRequest; };
  const splitItem: Command = (state, dispatch) => {
    // A new item after a finished task is a new task, not a finished one.
    const found = insideList(state);
    const task = !!found && state.selection.$from.node(found.depth).attrs.checked !== null;
    return splitListItem(nodes.list_item, task ? { checked: false } : undefined)(state, dispatch);
  };
  const enter = chainCommands(newlineInCode, splitItem, createParagraphNear, liftEmptyBlock, splitBlock);
  const state = EditorState.create({
    doc: parseMarkdown(options.markdown),
    plugins: [
      rules(),
      history(),
      keymap({
        'Mod-z': undo, 'Mod-y': redo, 'Shift-Mod-z': redo,
        'Mod-b': commands.bold, 'Mod-i': commands.italic, 'Mod-e': commands.code, 'Shift-Mod-x': commands.strike, 'Mod-k': linkHook,
        'Mod-Alt-1': commands.h1, 'Mod-Alt-2': commands.h2, 'Mod-Alt-3': commands.h3, 'Mod-Alt-0': commands.paragraph,
        'Shift-Mod-7': commands.ordered, 'Shift-Mod-8': commands.bullet, 'Shift-Mod-9': commands.task,
        'Shift-Mod-b': commands.quote, 'Mod-Alt-c': commands.codeblock,
        Enter: enter,
        Tab: sinkListItem(nodes.list_item), 'Shift-Tab': liftListItem(nodes.list_item),
        'Shift-Enter': (s, d) => { d?.(s.tr.replaceSelectionWith(nodes.hard_break.create()).scrollIntoView()); return true; },
      }),
      keymap(baseKeymap),
      wikiPlugin(options, current),
      pasteMarkdown(),
      limitPlugin(options),
      ...(options.placeholder ? [placeholder(options.placeholder)] : []),
    ],
  });
  const view: EditorView = new EditorView(host, {
    state,
    editable: () => !readOnly,
    attributes: { class: 'doc-content', role: 'textbox', 'aria-multiline': 'true', 'aria-label': 'Document text', spellcheck: 'true' },
    nodeViews: { list_item: (node, v, getPos) => new ListItemView(node, v, getPos) },
    handleDOMEvents: {
      click(_v, event) {
        // Inside the editor a click places the caret; with Cmd or Ctrl, or when reading, the link opens as any link does.
        const link = (event.target as HTMLElement).closest?.('a');
        if (link && view.editable && !event.metaKey && !event.ctrlKey) event.preventDefault();
        return false;
      },
    },
    dispatchTransaction(tr) {
      const next = view.state.apply(tr);
      view.updateState(next);
      options.onState?.(toolbarState(next));
      if (tr.docChanged) {
        last = toMarkdown(next.doc);
        options.onChange?.(last);
      }
    },
  });
  options.onState?.(toolbarState(view.state));
  return {
    view,
    markdown: () => last,
    setMarkdown(markdown) {
      last = markdown;
      // A document replaced from outside is not typing: it starts a new editing history and never reaches onChange.
      const next = EditorState.create({ doc: parseMarkdown(markdown), plugins: view.state.plugins });
      view.updateState(next);
      options.onState?.(toolbarState(next));
    },
    run(command) {
      const ok = commands[command](view.state, view.dispatch, view);
      if (ok) view.focus();
      return ok;
    },
    setLink(href) {
      const safe = normalizeHref(href);
      if (!safe) return false;
      const { from, to, empty } = view.state.selection;
      let tr = view.state.tr;
      if (empty) {
        const label = safe.replace(/^(https?:\/\/|mailto:)/i, '');
        tr = tr.insertText(label, from).addMark(from, from + label.length, marks.link.create({ href: safe }));
      } else tr = tr.addMark(from, to, marks.link.create({ href: safe }));
      view.dispatch(tr);
      view.focus();
      return true;
    },
    selectedLink() {
      const { $from, from, to, empty } = view.state.selection;
      const mark = empty ? marks.link.isInSet($from.marks()) : view.state.doc.nodeAt(from)?.marks.find((m) => m.type === marks.link && view.state.doc.rangeHasMark(from, to, marks.link));
      return typeof mark?.attrs.href === 'string' ? mark.attrs.href : '';
    },
    focus: () => view.focus(),
    setReadOnly(value) { readOnly = value; view.setProps({ editable: () => !readOnly }); },
    refreshWiki() { view.dispatch(view.state.tr.setMeta(wikiKey, true)); },
    destroy: () => view.destroy(),
  };
}

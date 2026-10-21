import { afterEach, describe, expect, it, vi } from 'vitest';
import { appendedText, streamedText } from './stream-motion';

class TextNode {
  constructor(public data: string) {}
  get textContent() { return this.data; }
}
class Span {
  childNodes: (Span | TextNode)[] = [];
  replacements = 0;
  animations: object[] = [];
  private text = '';
  get textContent(): string { return this.text + this.childNodes.map(n => n.textContent).join(''); }
  set textContent(value: string) { this.text = value; }
  append(node: Span | TextNode) { this.childNodes.push(node); }
  replaceChildren(node: TextNode) { this.replacements++; this.text = ''; this.childNodes = [node]; }
  contains(node: object | null) { return node === this || this.childNodes.includes(node as TextNode); }
  getAnimations() { return this.animations; }
}
afterEach(() => vi.unstubAllGlobals());

describe('streamed text presentation', () => {
  it('recognises only real appends, including whitespace and Unicode', () => {
    expect(appendedText('Hello', 'Hello\n世界 😀')).toBe('\n世界 😀');
    expect(appendedText('old attempt', 'new attempt')).toBeNull();
    expect(appendedText('same', 'same')).toBe('');
  });

  it('shows received text immediately, leaves restored text still and preserves selection', async () => {
    const node = new Span();
    const callbacks = new Map<string, () => void>();
    const selection = { isCollapsed: true, anchorNode: null as object | null, focusNode: null as object | null, containsNode: () => false };
    let finish = () => {};
    const finished = new Promise<void>(resolve => { finish = resolve; });
    const animate = vi.fn(() => { node.animations = [{}]; return { finished }; });
    vi.stubGlobal('window', { WerkbordMotion: { animate } });
    vi.stubGlobal('document', {
      createTextNode: (text: string) => new TextNode(text), createElement: () => new Span(), getSelection: () => selection,
      addEventListener: (name: string, cb: () => void) => callbacks.set(name, cb), removeEventListener: (name: string) => callbacks.delete(name),
    });
    const action = streamedText(node as unknown as HTMLElement, { text: 'Restored reply', live: true });
    expect(animate).not.toHaveBeenCalled();
    action.update({ text: 'Restored reply with more', live: true });
    expect(node.textContent).toBe('Restored reply with more');
    expect(animate).toHaveBeenCalledOnce();
    selection.isCollapsed = false; selection.anchorNode = node.childNodes[0];
    node.animations = []; finish();
    for (let i=0; i<4; i++) await Promise.resolve();
    expect(node.replacements).toBe(0);
    selection.isCollapsed = true; callbacks.get('selectionchange')?.();
    expect(node.replacements).toBe(1);
    expect(node.textContent).toBe('Restored reply with more');
    action.update({ text: 'Replacement attempt', live: true });
    expect(node.textContent).toBe('Replacement attempt');
    expect(animate).toHaveBeenCalledOnce();
    action.destroy(); expect(callbacks.size).toBe(0);
  });
});

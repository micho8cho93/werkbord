// A small, safe reader for the assistant's replies: paragraphs, lists, headings, quotes, code, bold, italic and `code`. It
// produces a tree that the page draws element by element, never HTML, so nothing a model (or a ticket it read) says can inject
// markup. Links are deliberately not made clickable: a reply that was influenced by something the assistant read could point
// anywhere, and the person is better served by seeing the address as text. It copes with a reply that is still arriving: an
// unclosed fence is a code block so far, an unclosed ** is just text.

export type Inline = { t: 'text'; v: string } | { t: 'b'; c: Inline[] } | { t: 'i'; c: Inline[] } | { t: 'code'; v: string };

export type Block =
  | { t: 'p'; c: Inline[] }
  | { t: 'h'; level: 1 | 2 | 3; c: Inline[] }
  | { t: 'ul'; items: Inline[][] }
  | { t: 'ol'; start: number; items: Inline[][] }
  | { t: 'quote'; c: Inline[] }
  | { t: 'code'; lang: string; text: string };

const MAX_DEPTH = 3;

/** Inline formatting. Unbalanced markers stay as the characters they are. */
export function inline(src: string, depth = 0): Inline[] {
  const out: Inline[] = [];
  let text = '';
  const flush = () => {
    if (text) out.push({ t: 'text', v: text });
    text = '';
  };
  let i = 0;
  while (i < src.length) {
    const c = src[i];
    if (c === '`') {
      const j = src.indexOf('`', i + 1);
      if (j > i + 1) {
        flush();
        out.push({ t: 'code', v: src.slice(i + 1, j) });
        i = j + 1;
        continue;
      }
    } else if ((c === '*' || c === '_') && depth < MAX_DEPTH) {
      const double = src[i + 1] === c;
      const mark = double ? c + c : c;
      const j = src.indexOf(mark, i + mark.length);
      const inner = j > i + mark.length ? src.slice(i + mark.length, j) : '';
      // Emphasis wraps words: not spaces at its edges, and an underscore inside a word (snake_case) is just a character.
      const edgeOK = inner !== '' && !/^\s|\s$/.test(inner);
      const wordy = c === '_' && (/\w/.test(src[i - 1] ?? '') || /\w/.test(src[j + mark.length] ?? ''));
      if (edgeOK && !wordy) {
        flush();
        out.push({ t: double ? 'b' : 'i', c: inline(inner, depth + 1) });
        i = j + mark.length;
        continue;
      }
    }
    text += c;
    i++;
  }
  flush();
  return out;
}

const FENCE = /^ {0,3}(`{3,}|~{3,})\s*([A-Za-z0-9_+.-]*)\s*$/;
const BULLET = /^ {0,3}[-*+]\s+(.*)$/;
const ORDERED = /^ {0,3}(\d{1,9})[.)]\s+(.*)$/;
const HEADING = /^ {0,3}(#{1,6})\s+(.*?)\s*#*\s*$/;
const QUOTE = /^ {0,3}>\s?(.*)$/;

export function blocks(src: string): Block[] {
  const lines = src.replace(/\r\n?/g, '\n').split('\n');
  const out: Block[] = [];
  let para: string[] = [];
  const endPara = () => {
    if (para.length) out.push({ t: 'p', c: inline(para.join('\n')) });
    para = [];
  };
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const f = FENCE.exec(line);
    if (f) {
      endPara();
      const close = new RegExp(`^ {0,3}${f[1][0]}{${f[1].length},}\\s*$`);
      const body: string[] = [];
      i++;
      while (i < lines.length && !close.test(lines[i])) body.push(lines[i++]);
      out.push({ t: 'code', lang: f[2], text: body.join('\n') });
      continue;
    }
    if (line.trim() === '') {
      endPara();
      continue;
    }
    const h = HEADING.exec(line);
    if (h) {
      endPara();
      out.push({ t: 'h', level: Math.min(3, h[1].length) as 1 | 2 | 3, c: inline(h[2]) });
      continue;
    }
    const q = QUOTE.exec(line);
    if (q) {
      endPara();
      const body = [q[1]];
      while (i + 1 < lines.length && QUOTE.test(lines[i + 1])) body.push(QUOTE.exec(lines[++i])![1]);
      out.push({ t: 'quote', c: inline(body.join('\n')) });
      continue;
    }
    const b = BULLET.exec(line);
    if (b) {
      endPara();
      const items = [inline(b[1])];
      while (i + 1 < lines.length && BULLET.test(lines[i + 1])) items.push(inline(BULLET.exec(lines[++i])![1]));
      out.push({ t: 'ul', items });
      continue;
    }
    const o = ORDERED.exec(line);
    if (o) {
      endPara();
      const items = [inline(o[2])];
      while (i + 1 < lines.length && ORDERED.test(lines[i + 1])) items.push(inline(ORDERED.exec(lines[++i])![2]));
      out.push({ t: 'ol', start: Number(o[1]), items });
      continue;
    }
    para.push(line);
  }
  endPara();
  return out;
}

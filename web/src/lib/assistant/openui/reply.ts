export type ReplyPart =
  | { kind: 'text'; text: string }
  | { kind: 'openui'; text: string; closed: boolean };

const OPEN_FENCE = /^ {0,3}```openui(?:-lang)?[ \t]*$/;
const CLOSE_FENCE = /^ {0,3}```[ \t]*$/;
const OPEN_PREFIXES = ['```openui-lang', '```openui'];

export interface ReplyPartsCache {
  source: string;
  stableEnd: number;
  stableParts: ReplyPart[];
}

export function emptyReplyPartsCache(): ReplyPartsCache {
  return { source: '', stableEnd: 0, stableParts: [] };
}

function lastClosedOpenUIEnd(source: string): number {
  const lines = source.split('\n');
  let inUI = false;
  let offset = 0;
  let stable = 0;
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    if (!inUI && OPEN_FENCE.test(line)) inUI = true;
    else if (inUI && CLOSE_FENCE.test(line)) {
      inUI = false;
      stable = offset + line.length + (i < lines.length - 1 ? 1 : 0);
    }
    offset += line.length + (i < lines.length - 1 ? 1 : 0);
  }
  return stable;
}

/** Splits the OpenUI Lang fences from prose without interpreting or executing the model's text. */
export function replyParts(source: string, streaming = false): ReplyPart[] {
  const normalized = source.replace(/\r\n?/g, '\n');
  const lines = normalized.split('\n');
  const hadFinalNewline = normalized.endsWith('\n');
  const parts: ReplyPart[] = [];
  let prose = '';
  let ui = '';
  let inUI = false;

  const flushProse = () => {
    if (prose !== '') parts.push({ kind: 'text', text: prose });
    prose = '';
  };

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const newline = i < lines.length - 1 || hadFinalNewline ? '\n' : '';
    if (!inUI && OPEN_FENCE.test(line)) {
      flushProse();
      inUI = true;
      continue;
    }
    if (inUI && CLOSE_FENCE.test(line)) {
      parts.push({ kind: 'openui', text: ui, closed: true });
      ui = '';
      inUI = false;
      continue;
    }
    if (!inUI && streaming && i === lines.length - 1 && !hadFinalNewline) {
      const trimmed = line.trimStart();
      if (OPEN_PREFIXES.some((candidate) => candidate.startsWith(trimmed) && trimmed.length > 3)) {
        flushProse();
        parts.push({ kind: 'openui', text: '', closed: false });
        continue;
      }
    }
    if (inUI) ui += line + newline;
    else prose += line + newline;
  }

  if (inUI) parts.push({ kind: 'openui', text: ui, closed: false });
  else flushProse();
  return parts;
}

/** Keep completed prose/OpenUI sections stable while the last section is still arriving. */
export function growingReplyParts(source: string, streaming: boolean, cache: ReplyPartsCache): ReplyPart[] {
  const normalized = source.replace(/\r\n?/g, '\n');
  if (!streaming) {
    const complete = replyParts(normalized);
    cache.source = normalized;
    cache.stableEnd = normalized.length;
    cache.stableParts = complete;
    return complete;
  }
  if (!normalized.startsWith(cache.source)) {
    cache.source = '';
    cache.stableEnd = 0;
    cache.stableParts = [];
  }
  const end = lastClosedOpenUIEnd(normalized);
  if (end > cache.stableEnd) {
    cache.stableParts.push(...replyParts(normalized.slice(cache.stableEnd, end)));
    cache.stableEnd = end;
  }
  cache.source = normalized;
  const tail = replyParts(normalized.slice(cache.stableEnd), true);
  const last = cache.stableParts.at(-1);
  const first = tail[0];
  if (last?.kind === 'text' && first?.kind === 'text') {
    return [...cache.stableParts.slice(0, -1), { kind: 'text', text: last.text + first.text }, ...tail.slice(1)];
  }
  return [...cache.stableParts, ...tail];
}

export type ReplyPart =
  | { kind: 'text'; text: string }
  | { kind: 'openui'; text: string; closed: boolean };

const OPEN_FENCE = /^ {0,3}```openui(?:-lang)?[ \t]*$/;
const CLOSE_FENCE = /^ {0,3}```[ \t]*$/;
const OPEN_PREFIXES = ['```openui-lang', '```openui'];

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

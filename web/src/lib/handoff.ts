export interface ReadableHandoff {
  summary: string;
  objective: string;
  results: string;
  nextAction: string;
  gitState: string;
  filesChanged: string[];
  decisions: string[];
  tests: string[];
  knownIssues: string[];
  blockers: string[];
  questions: string[];
}
export type MessagePart = { text: string; handoff?: never } | { handoff: ReadableHandoff; text?: never };

/** Render completed protocol blocks as prose. Malformed or partial output stays visible. */
export function messageParts(text: string): MessagePart[] {
  const parts: MessagePart[] = [];
  const pattern = /<(devboard-handoff|werkbord-handoff)>\s*([\s\S]*?)\s*<\/\1>/g;
  let offset = 0;
  for (const match of text.matchAll(pattern)) {
    try {
      const value: unknown = JSON.parse(match[2]);
      if (!value || typeof value !== 'object' || Array.isArray(value)) continue;
      const record = value as Record<string, unknown>;
      const str = (key: string) => typeof record[key] === 'string' ? record[key] as string : '';
      const list = (key: string) => Array.isArray(record[key]) ? (record[key] as unknown[]).filter((v): v is string => typeof v === 'string') : [];
      if (!str('summary') && !str('results') && !str('objective')) continue;
      if (match.index! > offset) parts.push({ text: text.slice(offset, match.index) });
      parts.push({ handoff: { summary: str('summary'), objective: str('objective'), results: str('results'), nextAction: str('nextAction'), gitState: str('gitState'), filesChanged: list('filesChanged'), decisions: list('decisions'), tests: list('tests'), knownIssues: list('knownIssues'), blockers: list('blockers'), questions: list('questions') } });
      offset = match.index! + match[0].length;
    } catch { /* Keep the agent's words when it did not finish valid JSON. */ }
  }
  if (offset < text.length) parts.push({ text: text.slice(offset) });
  return parts;
}

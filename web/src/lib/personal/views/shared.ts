import type { Collection, Field, PersonalRecord, Values, View } from '../schema';

export const untitled = 'Untitled';

/** The record's title: the view's first field, or a name for one that has none. */
export function titleOf(view: View, record: PersonalRecord): string {
  const value = record.values[view.fields[0]];
  return typeof value === 'string' && value.trim() ? value : untitled;
}

/** A short plain-text taste of a Markdown body, for lists. */
export function excerpt(markdown: unknown, length = 110): string {
  if (typeof markdown !== 'string') return '';
  const plain = markdown
    .replace(/```[\s\S]*?```/g, ' ')
    .replace(/\[\[([^\]]+)\]\]/g, '$1')
    .replace(/\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/^[\s>#*+-]*(\[[ xX]\]\s*)?/gm, '')
    .replace(/[*_~`]/g, '')
    .replace(/\s+/g, ' ')
    .trim();
  return plain.length > length ? `${plain.slice(0, length).trimEnd()}…` : plain;
}

function localDate(): string {
  const now = new Date();
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`;
}
/** Every value a required field needs, so a record made from one gesture (a new page, a card) is complete. */
export function newRecordValues(collection: Collection, view: View, given: Values = {}): Values {
  const values: Values = { ...given };
  for (const field of collection.fields) {
    if (!field.required || field.key in values) continue;
    values[field.key] = field.type === 'select' ? (field.options?.[0] ?? '')
      : field.type === 'multiselect' ? []
      : field.type === 'boolean' ? false
      : field.type === 'number' ? (field.min ?? 0)
      : field.type === 'date' ? localDate()
      : field.type === 'datetime' ? new Date().toISOString().replace(/\.\d{3}Z$/, 'Z')
      : field.type === 'url' ? 'https://example.com'
      : '';
  }
  void view;
  return values;
}

/** The fields shown beside a document or card rather than in it: everything but the title, the body and the page tree. */
export function propertyFields(collection: Collection, view: View): Field[] {
  const hidden = new Set([view.fields[0], view.bodyField, view.parentField]);
  return view.fields.filter((key) => !hidden.has(key)).map((key) => collection.fields.find((f) => f.key === key)).filter((f): f is Field => !!f && f.type !== 'longtext');
}

export function display(value: unknown): string {
  return value === undefined ? '' : Array.isArray(value) ? value.join(', ') : typeof value === 'boolean' ? (value ? 'Yes' : 'No') : String(value);
}

/** RFC 3339 to a value a datetime-local input accepts, in the person's time; and back. */
export function toLocalInput(value: unknown): string {
  if (typeof value !== 'string') return '';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '';
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}
export function fromLocalInput(value: string): string | undefined {
  if (!value) return undefined;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? undefined : date.toISOString().replace(/\.\d{3}Z$/, 'Z');
}

export function relativeTime(iso: string, now = Date.now()): string {
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return '';
  const minutes = Math.round((now - then) / 60000);
  if (minutes < 1) return 'just now';
  if (minutes < 60) return `${minutes} min ago`;
  const hours = Math.round(minutes / 60);
  if (hours < 24) return `${hours} h ago`;
  const days = Math.round(hours / 24);
  if (days < 30) return `${days} d ago`;
  return new Date(then).toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
}

export function wordCount(markdown: unknown): number {
  return typeof markdown === 'string' ? (markdown.match(/[\p{L}\p{N}][\p{L}\p{N}'’-]*/gu) ?? []).length : 0;
}

/** For a box that has just been opened to type into: it takes the caret. */
export function autofocus(node: HTMLElement) { node.focus(); }

import type { Collection, Formula, PersonalRecord, Values, View } from './schema';

/** Mirrors the bounded server arithmetic for previews and derived displays. */
export function calculatedValues(collection: Collection, values: Values): Values {
  const out = { ...values };
  function evaluate(x: Formula, depth = 1): number | undefined {
    if (depth > 8) return;
    let result: number | undefined;
    if (x.op === 'literal') result = x.value;
    else if (x.op === 'field') {
      const f = collection.fields.find(f => f.key === x.field);
      result = f?.formula ? evaluate(f.formula, depth + 1) : typeof values[x.field!] === 'number' ? values[x.field!] as number : undefined;
    } else {
      const args = (x.args ?? []).map(a => evaluate(a, depth + 1));
      if (args.length < 2 || args.some(a => a === undefined)) return;
      const ns = args as number[];
      result = ns.slice(1).reduce((a, b) => x.op === 'add' ? a + b : x.op === 'subtract' ? a - b : x.op === 'multiply' ? a * b : x.op === 'divide' ? a / b : x.op === 'min' ? Math.min(a, b) : Math.max(a, b), ns[0]);
    }
    return result !== undefined && Number.isFinite(result) ? result : undefined;
  }
  for (const f of collection.fields) if (f.formula) {
    delete out[f.key];
    const n = evaluate(f.formula);
    if (n !== undefined) out[f.key] = n;
  }
  return out;
}
export function metricValue(view: View, records: PersonalRecord[]): number | undefined {
  if (view.metric?.operation === 'count') return records.length;
  const numbers = records.map(r => r.values[view.metric?.field ?? '']).filter((n): n is number => typeof n === 'number' && Number.isFinite(n));
  if (!numbers.length) return;
  if (view.metric?.operation === 'min') return Math.min(...numbers);
  if (view.metric?.operation === 'max') return Math.max(...numbers);
  const total = numbers.reduce((a, b) => a + b, 0);
  return Number.isFinite(total) ? view.metric?.operation === 'average' ? total / numbers.length : total : undefined;
}
/** Dates without times represent UTC calendar days; datetimes retain their instant. */
export function dateInstant(value: unknown): number | undefined {
  if (typeof value !== 'string' || !/^\d{4}-\d{2}-\d{2}(?:$|T)/.test(value)) return;
  const n = Date.parse(value.length === 10 ? `${value}T00:00:00Z` : value);
  return Number.isFinite(n) ? n : undefined;
}
export function countdown(value: unknown, now: number): string {
  const due = dateInstant(value);
  if (due === undefined) return 'No deadline';
  const days = Math.ceil((due - now) / 86400000);
  return days === 0 ? 'Due today' : days < 0 ? `${Math.abs(days)} ${Math.abs(days) === 1 ? 'day' : 'days'} overdue` : `${days} ${days === 1 ? 'day' : 'days'} remaining`;
}
/** Each saved record appears once, including missing parents and cyclic graphs. */
export function hierarchyRows(records: PersonalRecord[], parentField: string): {record: PersonalRecord; depth: number}[] {
  const byID = new Map(records.map(r => [r.id, r]));
  const children = new Map<string, PersonalRecord[]>();
  for (const r of records) {
    const parent = typeof r.values[parentField] === 'string' ? r.values[parentField] as string : '';
    const key = byID.has(parent) && parent !== r.id ? parent : '';
    children.set(key, [...(children.get(key) ?? []), r]);
  }
  const rows: {record: PersonalRecord; depth: number}[] = [], seen = new Set<string>();
  function walk(root: PersonalRecord) {
    const stack = [{record:root,depth:0}];
    while (stack.length) {
      const row = stack.pop()!;
      if (seen.has(row.record.id)) continue;
      seen.add(row.record.id); rows.push({...row,depth:Math.min(row.depth,8)});
      for (const child of [...(children.get(row.record.id) ?? [])].reverse()) stack.push({record:child,depth:row.depth+1});
    }
  }
  for (const root of children.get('') ?? []) walk(root);
  for (const r of records) walk(r);
  return rows;
}

/** The usage meter: what runs Werkbord started reported using, against budgets the person set. Never what a provider has left. */
export interface UsageWindow { tokens: number; runs: number; unreported: number; budget: number; percent?: number; easesAt?: string }
export interface AgentUsage { agent: string; fiveHour: UsageWindow; weekly: UsageWindow; held?: string }
export interface UsageMeter { at: string; mode: 'standard' | 'saver'; saverAt: number; agents: AgentUsage[] }
export interface UsageBudget { fiveHour?: number; weekly?: number }
export interface UsageSettings { mode: 'standard' | 'saver'; saverAt: number; budgets: Record<string, UsageBudget> }
export interface UsageView { settings: UsageSettings; meter: UsageMeter }

/** 1,234,000 reads 1.2M; small numbers stay whole. */
export function tokensLabel(n: number): string {
  if (n >= 1e9) return `${+(n / 1e9).toFixed(1)}B`;
  if (n >= 1e6) return `${+(n / 1e6).toFixed(1)}M`;
  if (n >= 1e4) return `${Math.round(n / 1e3)}k`;
  if (n >= 1e3) return `${+(n / 1e3).toFixed(1)}k`;
  return String(Math.round(n));
}

/** "2M", "500k", "1.5m" or "2,000,000" as a token count; empty is "no budget" (0); anything else is null. */
export function parseTokens(text: string): number | null {
  const t = text.trim().toLowerCase().replaceAll(',', '').replaceAll(' ', '');
  if (t === '') return 0;
  const m = /^(\d+(?:\.\d+)?)([kmb]?)$/.exec(t);
  if (!m) return null;
  const n = Math.round(parseFloat(m[1]) * ({ '': 1, k: 1e3, m: 1e6, b: 1e9 } as Record<string, number>)[m[2]]);
  return Number.isSafeInteger(n) && n <= 1e12 ? n : null;
}

/** A saved budget as a person would write it back: 2M, 500k, or the whole number when it is neither. */
export function budgetText(n: number | undefined): string {
  if (!n) return '';
  if (n % 1e9 === 0) return `${n / 1e9}B`;
  if (n % 1e6 === 0) return `${n / 1e6}M`;
  if (n % 1e3 === 0) return `${n / 1e3}k`;
  return String(n);
}

/** How full a window is, for colour and words: calm below the saver threshold, near at it, over past the budget. */
export function pressure(w: UsageWindow, saverAt: number): 'none' | 'ok' | 'near' | 'over' {
  if (w.percent === undefined) return 'none';
  if (w.percent >= 100) return 'over';
  return w.percent >= saverAt ? 'near' : 'ok';
}

/** One window, in words that claim only what was measured. */
export function windowLine(w: UsageWindow): string {
  const used = `${tokensLabel(w.tokens)} used`;
  const base = w.budget > 0 ? `${used} of ${tokensLabel(w.budget)}${w.percent !== undefined ? ` · ${w.percent}%` : ''}` : used;
  return w.unreported > 0 ? `${base} · ${w.unreported} ${w.unreported === 1 ? 'run' : 'runs'} reported no tokens` : base;
}

/** When the figure starts to fall: the oldest run counted leaves the window then. Not a reset, and not the provider's. */
export function easesLabel(at: string | undefined, now: number): string {
  if (!at) return '';
  const ms = Date.parse(at) - now;
  if (!(ms > 0)) return '';
  const min = Math.round(ms / 60000);
  if (min < 60) return `eases in ${Math.max(min, 1)} min`;
  const h = Math.round(min / 60);
  return h < 36 ? `eases in about ${h} h` : `eases in about ${Math.round(h / 24)} days`;
}

/** Whether there is anything to show: some usage, or a budget set. */
export function hasUsage(m: UsageMeter | undefined): boolean {
  return !!m && m.agents.some(a => a.fiveHour.runs > 0 || a.weekly.runs > 0 || a.fiveHour.budget > 0 || a.weekly.budget > 0);
}

/** The settings to save, from what was typed: a budget field that is not a number is reported, not guessed at. */
export function budgetsFrom(fields: Record<string, { fiveHour: string; weekly: string }>): { budgets: Record<string, UsageBudget>; problem: string } {
  const budgets: Record<string, UsageBudget> = {};
  for (const [agent, f] of Object.entries(fields)) {
    const five = parseTokens(f.fiveHour), week = parseTokens(f.weekly);
    if (five === null || week === null) return { budgets: {}, problem: `Enter a token count for ${agent}, like 2M or 500k.` };
    if (five > 0 || week > 0) budgets[agent] = { ...(five > 0 ? { fiveHour: five } : {}), ...(week > 0 ? { weekly: week } : {}) };
  }
  return { budgets, problem: '' };
}

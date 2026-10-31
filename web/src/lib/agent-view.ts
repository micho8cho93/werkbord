// What the Agents list shows, kept for the tab: the filter chosen and how far the list was scrolled. Opening a session leads away
// from the list (to its task, to Salie) and Back leads to it again; the list is then as it was left, and so it is after a reload.
// Nothing here is a session or knows one: the sessions are the controller's records (agent-sessions.ts).

export interface AgentView {
  filter: 'all' | 'live' | 'task' | 'standalone';
  /** Only used where the page chooses the project itself (a plain browser); in the window the window's project context does. */
  project: string;
  scroll: number;
}

const KEY = 'werkbord.agents.view';
const FILTERS: readonly AgentView['filter'][] = ['all', 'live', 'task', 'standalone'];

export function readAgentView(): AgentView {
  const none: AgentView = { filter: 'all', project: '', scroll: 0 };
  try {
    const v = JSON.parse(sessionStorage.getItem(KEY) ?? 'null') as Partial<AgentView> | null;
    if (!v || typeof v !== 'object') return none;
    return {
      filter: FILTERS.includes(v.filter as AgentView['filter']) ? (v.filter as AgentView['filter']) : 'all',
      project: typeof v.project === 'string' && v.project.length <= 120 ? v.project : '',
      scroll: typeof v.scroll === 'number' && Number.isFinite(v.scroll) && v.scroll >= 0 ? Math.min(v.scroll, 1e6) : 0,
    };
  } catch {
    return none;
  }
}

export function writeAgentView(view: AgentView): void {
  try { sessionStorage.setItem(KEY, JSON.stringify(view)); } catch { /* The list still works; it just starts at the top next time. */ }
}

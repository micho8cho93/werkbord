// One surface, many independent instances of the existing conversation client. Transcript caches
// remain in sessionStorage; only provider handles/settings live on the controller.
import { assistantProjectContext } from './project-context.svelte';
import { terminalApi, type TerminalSession } from './terminal';
import { app } from '../state.svelte';
import { RunSession, liveRun } from './run-session.svelte';
import { InteractionQueue, type Interaction } from './interactions';
import { embedded, surface } from '../embed';
import { onAssistantRequest, reportAssistant } from '../embed';
import { AssistantStore, type Phase } from './store.svelte';
import { FALLBACK_ORIGIN, type Origin } from './transition';
import type { Choice } from './choice';
import { SessionStreams } from './session-streams';

const INDEX = 'werkbord.assistant.tabs.v1';
const LEGACY = 'werkbord.assistant.v1';
const keyOf = (id: string) => `werkbord.assistant.session.${id}`;
export interface SalieTab { id: string; title: string; closed: boolean; store: AssistantStore; run?: RunSession; terminal?: TerminalSession }

export class AssistantSessions {
  tabs = $state<SalieTab[]>([]);
  activeId = $state('');
  mounted = $state(false);
  phase = $state<Phase>('closed');
  origin = $state<Origin>(FALLBACK_ORIGIN);
  trigger: HTMLElement | null = null;
  compact = $state(false);
  private queue = new InteractionQueue();
  private pending = $derived.by(() => {
    const requests: Interaction[] = app.questions.map(question => ({ key: `question:${question.projectId}:${question.id}`, kind: 'question', question }));
    for (const tab of this.tabs) if (!tab.run && !tab.terminal) for (const item of tab.store.waitingItems) {
      if (Date.parse(item.expiresAt) <= tab.store.now) continue;
      requests.push({ key: `proposal:${tab.store.session?.id}:${item.id}`, kind: 'proposal', tabId: tab.id, store: tab.store, item });
    }
    return this.queue.reconcile(requests);
  });
  get interactions(): Interaction[] { return this.pending; }
  get activeTab(): SalieTab { return this.tabs.find(t => t.id === this.activeId)!; }
  openRun(projectId: string, runId: string): void {
    if (embedded() && !surface) {
      window.parent.postMessage({ type: 'werkbord.frame', event: 'run_session', projectId, runId }, '*');
      return;
    }
    let tab = this.tabs.find(t => t.run?.projectId === projectId && t.run.runId === runId);
    if (!tab) { tab = this.add(); tab.run = new RunSession(projectId, runId); }
    tab.closed = false; tab.run!.attach(); this.activeId = tab.id; this.compact = false; this.show(); this.save();
  }
  private streams = new SessionStreams();
  private started = false;
  private dispose: (() => void) | undefined;

  constructor() { this.add(); }
  get active(): AssistantStore { return this.tabs.find(t => t.id === this.activeId)!.store; }
  get openTabs(): SalieTab[] { return this.tabs.filter(t => !t.closed); }
  get closedTabs(): SalieTab[] { return this.tabs.filter(t => t.closed); }
  get running(): boolean { return this.tabs.some(t => t.terminal ? t.terminal.state === 'running' : t.run ? !!t.run.run && liveRun(t.run.run) : t.store.running || t.store.session?.running); }
  get waiting(): number { return this.interactions.length; }
  get isOpen(): boolean { return this.mounted && this.phase !== 'closing' && this.phase !== 'closed'; }
  get draft(): string { return this.active.draft; }
  set draft(value: string) { this.active.draft = value; }

  private add(id = crypto.randomUUID(), title = '', closed = false): SalieTab {
    const tab = { id, title, closed, store: new AssistantStore(keyOf(id), this.streams.connect) };
    this.tabs.push(tab);
    this.activeId = id;
    return this.tabs.at(-1)!;
  }
  async newTerminal(projectId: string, agentId: string, device: string): Promise<void> {
    const session = await terminalApi.create(projectId, agentId, device);
    const tab = this.add(); tab.terminal = session; this.save();
  }
  async discoverTerminals(): Promise<void> {
    if (app.remote) return;
    try {
      const sessions = await terminalApi.list();
      for (const session of sessions) {
        const tab = this.tabs.find(t => t.terminal?.id === session.id);
        if (tab) tab.terminal = session;
        else { const active = this.activeId; const added = this.add(); added.closed = true; added.terminal = session; this.activeId = active; }
      }
      for (const tab of this.tabs) if (tab.terminal && !sessions.some(s => s.id === tab.terminal!.id)) tab.terminal.state = 'interrupted';
      this.save();
    } catch { /* The terminal view reports connection loss without guessing process state. */ }
  }
  newTab(): void {
    const tab = this.add();
    tab.store.init(true);
    if (!tab.run && !tab.terminal) void tab.store.prepare();
    this.save();
  }
  select(id: string): void {
    const tab = this.tabs.find(t => t.id === id && !t.closed);
    if (!tab) return;
    this.activeId = id;
    if (!tab.run && !tab.terminal) void tab.store.prepare();
    this.save();
  }
  closeTab(id: string): void {
    const tab = this.tabs.find(t => t.id === id);
    if (!tab) return;
    tab.closed = true;
    // Closing a tab only hides it: its stream, pending approvals and provider session stay alive.
    if (this.activeId === id) {
      const next = this.openTabs.at(-1);
      if (next) this.select(next.id);
      else this.newTab();
    }
    this.save();
  }
  reopen(id: string): void {
    const tab = this.tabs.find(t => t.id === id);
    if (!tab) return;
    tab.closed = false;
    this.select(id);
  }
  rename(id: string, title: string): void {
    const tab = this.tabs.find(t => t.id === id);
    if (tab) tab.title = title.trim().slice(0, 100);
    this.save();
  }
  title(tab: SalieTab): string {
    return tab.title || (tab.terminal ? `${tab.terminal.agentId} · ${tab.terminal.repository}` : tab.run ? `${tab.run.run?.agentId ?? 'Agent'} · ${tab.run.project?.name ?? tab.run.projectId}` : this.firstMessage(tab)) || 'New conversation';
  }
  private firstMessage(tab: SalieTab): string {
    const first = tab.store.transcript.items.find(i => i.kind === 'user');
    return first?.kind === 'user' ? first.text.slice(0, 80) : '';
  }
  init(): void {
    if (this.started) return;
    this.started = true;
    try {
      const saved = JSON.parse(sessionStorage.getItem(INDEX) ?? 'null');
      if (saved?.v === 1 && Array.isArray(saved.tabs)) {
        this.tabs = [];
        for (const t of saved.tabs) {
          if (!t || typeof t.id !== 'string' || !/^[\w-]{1,80}$/.test(t.id) || this.tabs.some(x => x.id === t.id)) continue;
          const tab = this.add(t.id, typeof t.title === 'string' ? t.title.slice(0, 100) : '', t.closed === true);
          if (t.run && typeof t.run.projectId === 'string' && typeof t.run.runId === 'string') tab.run = new RunSession(t.run.projectId, t.run.runId);
          if (t.terminal && typeof t.terminal.id === 'string' && typeof t.terminal.projectId === 'string') tab.terminal = t.terminal;
          if (typeof t.draft === 'string') tab.store.draft = t.draft.slice(0, 120_000);
          if (validChoice(t.choice)) tab.store.choice = t.choice;
        }
        const active = this.tabs.find(t => t.id === saved.activeId && !t.closed) ?? this.openTabs[0];
        if (active) this.activeId = active.id;
        else this.add();
      } else {
        const legacy = sessionStorage.getItem(LEGACY);
        if (legacy) {
          sessionStorage.setItem(keyOf(this.activeId), legacy);
          sessionStorage.removeItem(LEGACY);
        }
      }
    } catch { /* Storage unavailable or malformed: retain a usable empty tab. */ }
    for (const tab of this.tabs) { if (tab.run) tab.run.attach(); else if (!tab.terminal) tab.store.init(true); }
    void this.discoverTerminals();
    const wake = () => { void this.discoverTerminals(); for (const tab of this.tabs) tab.store.wake(); };
    const visible = () => { if (!document.hidden) wake(); };
    const pagehide = () => this.save();
    document.addEventListener('visibilitychange', visible);
    window.addEventListener('online', wake);
    window.addEventListener('pagehide', pagehide);
    const clock = setInterval(() => {
      for (const tab of this.tabs) { tab.store.now = Date.now(); tab.store.expireFiles(); }
    }, 15_000);
    const unlisten = onAssistantRequest(req => {
      if (req.projectContext !== undefined) assistantProjectContext.current = req.projectContext;
      if (req.action === 'close') { this.compact = false; this.hide(); }
      else if (req.action === 'compact') this.compact = !this.compact;
      else if (req.run) this.openRun(req.run.projectId, req.run.runId);
      else {
        if (req.catchUpPrompt !== undefined) { this.draft = req.catchUpPrompt; }
        if (req.action === 'toggle') this.toggle(req.origin, null);
        else this.show(req.origin, null);
      }
    });
    const stopEffects = $effect.root(() => {
      $effect(() => { reportAssistant({ open: this.mounted, waiting: this.waiting, running: this.running, compact: this.compact }); });
      $effect(() => { this.saveIndex(); });
    });
    this.dispose = () => {
      stopEffects(); unlisten(); clearInterval(clock);
      document.removeEventListener('visibilitychange', visible);
      window.removeEventListener('online', wake);
      window.removeEventListener('pagehide', pagehide);
    };
  }
  private saveIndex(): void {
    try {
      sessionStorage.setItem(INDEX, JSON.stringify({ v: 1, activeId: this.activeId, tabs: this.tabs.map(t => ({
        id: t.id, title: t.title || this.firstMessage(t), closed: t.closed, terminal: t.terminal, run: t.run ? { projectId: t.run.projectId, runId: t.run.runId } : undefined, choice: t.store.choice, draft: t.store.draft,
      })) }));
    } catch { /* Private mode/full storage must not prevent conversation use. */ }
  }
  save(): void {
    this.saveIndex();
    for (const tab of this.tabs) tab.store.saveNow();
  }
  show(origin?: Origin, trigger?: HTMLElement | null): void {
    if (origin) this.origin = origin;
    if (trigger !== undefined) this.trigger = trigger;
    this.compact = false;
    void this.discoverTerminals();
    this.mounted = true;
    this.phase = 'opening';
    if (!this.activeTab.run && !this.activeTab.terminal) void this.active.prepare();
  }
  hide(): void { if (this.mounted && this.phase !== 'closed') this.phase = 'closing'; }
  toggle(origin?: Origin, trigger?: HTMLElement | null): void { if (this.isOpen) this.hide(); else this.show(origin, trigger); }
  opened(): void { if (this.phase === 'opening') this.phase = 'open'; }
  closed(): void {
    if (this.phase !== 'closing') return;
    this.mounted = false; this.phase = 'closed';
    const trigger = this.trigger; this.trigger = null;
    this.save();
    queueMicrotask(() => trigger?.focus?.({ preventScroll: true }));
  }
  /** Page teardown/test cleanup, never used when hiding Salie or closing a tab. */
  destroy(): void {
    this.save(); this.dispose?.();
    for (const tab of this.tabs) { tab.store.disconnect(); tab.run?.disconnect(); }
  }
}
function validChoice(c: unknown): c is Choice {
  if (!c || typeof c !== 'object') return false;
  const x = c as Choice;
  return typeof x.provider === 'string' && typeof x.model === 'string' && typeof x.reasoning === 'string'
    && (x.mode === undefined || ['manual', 'lowest_cost', 'max_reasoning', 'highest_intelligence'].includes(x.mode));
}
export const assistant = new AssistantSessions();

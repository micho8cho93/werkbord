// One surface, many independent instances of the existing conversation client. Transcript caches
// remain in sessionStorage; only provider handles/settings live on the controller.
import { contextualWorkspaceDraft, personalWorkspaceDraft } from '../personal/conversation';
import { onAssistantRequest, reportAssistant } from '../embed';
import { AssistantStore, type Phase } from './store.svelte';
import { FALLBACK_ORIGIN, type Origin } from './transition';
import type { Choice } from './choice';
import { SessionStreams } from './session-streams';

const INDEX = 'werkbord.assistant.tabs.v1';
const LEGACY = 'werkbord.assistant.v1';
const keyOf = (id: string) => `werkbord.assistant.session.${id}`;
export interface SallyTab { id: string; title: string; closed: boolean; store: AssistantStore }

export class AssistantSessions {
  tabs = $state<SallyTab[]>([]);
  activeId = $state('');
  mounted = $state(false);
  phase = $state<Phase>('closed');
  origin = $state<Origin>(FALLBACK_ORIGIN);
  trigger: HTMLElement | null = null;
  private streams = new SessionStreams();
  private started = false;
  private dispose: (() => void) | undefined;

  constructor() { this.add(); }
  get active(): AssistantStore { return this.tabs.find(t => t.id === this.activeId)!.store; }
  get openTabs(): SallyTab[] { return this.tabs.filter(t => !t.closed); }
  get closedTabs(): SallyTab[] { return this.tabs.filter(t => t.closed); }
  get running(): boolean { return this.tabs.some(t => t.store.running || t.store.session?.running); }
  get waiting(): number { return this.tabs.reduce((n, t) => n + t.store.waiting, 0); }
  get isOpen(): boolean { return this.mounted && this.phase !== 'closing' && this.phase !== 'closed'; }
  get draft(): string { return this.active.draft; }
  set draft(value: string) { this.active.draft = value; }
  get personalEditing() { return this.active.personalEditing; }
  set personalEditing(value: AssistantStore['personalEditing']) { this.active.personalEditing = value; }

  private add(id = crypto.randomUUID(), title = '', closed = false): SallyTab {
    const tab = { id, title, closed, store: new AssistantStore(keyOf(id), this.streams.connect) };
    this.tabs.push(tab);
    this.activeId = id;
    return tab;
  }
  newTab(): void {
    const tab = this.add();
    tab.store.init(true);
    void tab.store.prepare();
    this.save();
  }
  select(id: string): void {
    const tab = this.tabs.find(t => t.id === id && !t.closed);
    if (!tab) return;
    this.activeId = id;
    void tab.store.prepare();
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
  title(tab: SallyTab): string {
    return tab.title || this.firstMessage(tab) || 'New conversation';
  }
  private firstMessage(tab: SallyTab): string {
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
    for (const tab of this.tabs) tab.store.init(true);
    const wake = () => { for (const tab of this.tabs) tab.store.wake(); };
    const visible = () => { if (!document.hidden) wake(); };
    const pagehide = () => this.save();
    document.addEventListener('visibilitychange', visible);
    window.addEventListener('online', wake);
    window.addEventListener('pagehide', pagehide);
    const clock = setInterval(() => {
      for (const tab of this.tabs) { tab.store.now = Date.now(); tab.store.expireFiles(); }
    }, 15_000);
    const unlisten = onAssistantRequest(req => {
      if (req.action === 'close') this.hide();
      else {
        if (req.personalWorkspace !== undefined) {
          this.draft = req.personalPrompt ? contextualWorkspaceDraft(req.personalWorkspace, req.personalPrompt) : personalWorkspaceDraft(req.personalWorkspace);
          this.personalEditing = req.personalWorkspace && req.personalRevision ? { workspaceId: req.personalWorkspace, revision: req.personalRevision } : undefined;
        }
        if (req.action === 'toggle') this.toggle(req.origin, null);
        else this.show(req.origin, null);
      }
    });
    const stopEffects = $effect.root(() => {
      $effect(() => { reportAssistant({ open: this.mounted, waiting: this.waiting, running: this.running }); });
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
        id: t.id, title: t.title || this.firstMessage(t), closed: t.closed, choice: t.store.choice, draft: t.store.draft,
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
    this.mounted = true;
    this.phase = 'opening';
    void this.active.prepare();
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
  /** Page teardown/test cleanup, never used when hiding Sally or closing a tab. */
  destroy(): void {
    this.save(); this.dispose?.();
    for (const tab of this.tabs) tab.store.disconnect();
  }
}
function validChoice(c: unknown): c is Choice {
  if (!c || typeof c !== 'object') return false;
  const x = c as Choice;
  return typeof x.provider === 'string' && typeof x.model === 'string' && typeof x.reasoning === 'string'
    && (x.mode === undefined || ['manual', 'lowest_cost', 'max_reasoning', 'highest_intelligence'].includes(x.mode));
}
export const assistant = new AssistantSessions();

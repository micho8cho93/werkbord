// The assistant, as the page holds it. One instance for the whole page: it is NOT tied to the screen that shows it, so
// closing the screen, or going to another view, changes nothing about a reply that is streaming, a change that is waiting for
// the person, or a file that is waiting to be sent. Those live here and on the server; the screen only looks at them.
//
// What the page remembers of a conversation, it keeps in the tab (sessionStorage), not on disk and not on the server: the
// server keeps no transcript by design (docs/ASSISTANT.md). After a reload the screen comes back as it was, the reply that was
// streaming goes on streaming, and what was waiting for the person is still waiting.

import { ApiError } from '../api';
import { onAssistantRequest, reportAssistant } from '../embed';
import { assistantApi } from './api';
import { modelName, pickChoice, reasoningLevels, summary, type Choice } from './choice';
import { Connection, type Link } from './stream';
import { FALLBACK_ORIGIN, type Origin } from './transition';
import {
  activeReply,
  adoptPending,
  apply,
  emptyTranscript,
  reconcileSeq,
  restore,
  serialize,
  settleStale,
  type ConfirmItem,
  type ReplyItem,
  type Transcript,
  type UserItem,
} from './transcript';
import type { AssistantEvent, AttachmentPolicy, ProviderInfo, SessionView } from './types';
import { explain, looksLikeText, size } from './words';

const SESSION_KEY = 'werkbord.assistant.v1';
const CHOICE_KEY = 'werkbord.assistant.choice';

/** What the screen can be showing, instead of a conversation. */
export type Status = 'idle' | 'loading' | 'ready' | 'no-provider' | 'disabled' | 'offline' | 'unauthorized';
export type Phase = 'closed' | 'opening' | 'open' | 'closing';

export interface Staged {
  key: string;
  name: string;
  bytes: number;
  file?: File;
  /** The server's id for it once it is held there. */
  id?: string;
  /** When the server will drop it, in ms. */
  expiresAt?: number;
  state: 'asking' | 'uploading' | 'ready' | 'error' | 'expired';
  error?: string;
}

function readStore(kind: 'session' | 'local', key: string): string | null {
  try {
    return (kind === 'session' ? sessionStorage : localStorage).getItem(key);
  } catch {
    return null;
  }
}
function writeStore(kind: 'session' | 'local', key: string, value: string | null): void {
  try {
    const s = kind === 'session' ? sessionStorage : localStorage;
    if (value === null) s.removeItem(key);
    else s.setItem(key, value);
  } catch {
    // Private mode, or a full store: the conversation simply is not kept across a reload.
  }
}

let counter = 0;
const uid = (p: string) => `${p}${Date.now().toString(36)}${(counter++).toString(36)}`;

export class AssistantStore {
  // ---- the screen ----
  mounted = $state(false);
  phase = $state<Phase>('closed');
  origin = $state<Origin>(FALLBACK_ORIGIN);
  /** The control that opened it, which gets focus back when it closes. */
  trigger: HTMLElement | null = null;

  // ---- what it can do ----
  status = $state<Status>('idle');
  providers = $state<ProviderInfo[]>([]);
  policy = $state<AttachmentPolicy | null>(null);
  choice = $state<Choice | null>(null);
  /** Something to tell the person about the last thing they tried. */
  problem = $state('');
  /** The provider, model and level chooser is showing. */
  pickerOpen = $state(false);

  // ---- the conversation ----
  session = $state<SessionView | null>(null);
  transcript = $state<Transcript>(emptyTranscript());
  link = $state<Link>('idle');
  draft = $state('');
  staged = $state<Staged[]>([]);
  sending = $state(false);
  /** The clock the screen reads, so a file's time running out shows without anyone touching anything. */
  now = $state(Date.now());

  private conn: Connection | null = null;
  private saveTimer: ReturnType<typeof setTimeout> | undefined;
  private started = false;

  // ---- derived ----
  get isOpen(): boolean {
    return this.mounted && this.phase !== 'closing' && this.phase !== 'closed';
  }
  get provider(): ProviderInfo | undefined {
    return this.providers.find((p) => p.id === (this.choice?.provider ?? this.session?.provider));
  }
  get running(): boolean {
    return this.sending || !!activeReply(this.transcript);
  }
  /** Changes waiting for the person, wherever they are looking. */
  get waiting(): number {
    return this.transcript.items.filter((i) => i.kind === 'confirm' && (i.state === 'waiting' || i.state === 'working')).length;
  }
  get summary(): string {
    return this.choice ? summary(this.provider, this.choice) : '';
  }
  get hasConversation(): boolean {
    return this.transcript.items.length > 0;
  }
  get canAttach(): boolean {
    return !!this.policy?.enabled;
  }
  get asking(): Staged[] {
    return this.staged.filter((s) => s.state === 'asking');
  }
  get readyFiles(): Staged[] {
    return this.staged.filter((s) => s.state === 'ready');
  }

  // ---- start up ----

  /** Called once when the page starts. Picks up a conversation this tab already had, and listens to the desktop shell. */
  init(): void {
    if (this.started) return;
    this.started = true;
    const saved = restore(readStore('session', SESSION_KEY));
    if (saved) {
      this.transcript = { items: saved.items, lastSeq: saved.lastSeq };
      void this.resume(saved.sessionId);
    }
    if (typeof document !== 'undefined') {
      document.addEventListener('visibilitychange', () => {
        if (!document.hidden) this.wake();
      });
      window.addEventListener('online', () => this.wake());
      window.addEventListener('pagehide', () => this.saveNow());
    }
    setInterval(() => {
      this.now = Date.now();
      this.expireFiles();
    }, 15_000);
    onAssistantRequest((req) => {
      if (req.action === 'close') this.hide();
      else if (req.action === 'open') this.show(req.origin, null);
      else if (this.isOpen) this.hide();
      else this.show(req.origin, null);
    });
    $effect.root(() => {
      $effect(() => {
        // `mounted`, not `isOpen`: the window must stay put until the surface has finished closing back into its button.
        reportAssistant({ open: this.mounted, waiting: this.waiting, running: this.running });
      });
    });
  }

  private wake(): void {
    if (!this.session) return;
    this.conn?.nudge();
  }

  // ---- the screen opening and closing ----

  show(origin?: Origin, trigger?: HTMLElement | null): void {
    if (origin) this.origin = origin;
    if (trigger !== undefined) this.trigger = trigger;
    this.mounted = true;
    this.phase = 'opening';
    void this.prepare();
  }

  hide(): void {
    if (!this.mounted || this.phase === 'closed') return;
    this.phase = 'closing';
  }

  toggle(origin?: Origin, trigger?: HTMLElement | null): void {
    if (this.isOpen) this.hide();
    else this.show(origin, trigger);
  }

  /** The opening finished. */
  opened(): void {
    if (this.phase === 'opening') this.phase = 'open';
  }

  /** The closing finished: the screen is gone, and focus goes back to what opened it. */
  closed(): void {
    if (this.phase !== 'closing') return;
    this.mounted = false;
    this.phase = 'closed';
    const t = this.trigger;
    this.trigger = null;
    queueMicrotask(() => t?.focus?.({ preventScroll: true }));
  }

  // ---- readiness ----

  /** Finds out what can be used on this computer. Safe to call again: "Check again" does. */
  async prepare(force = false): Promise<void> {
    if (this.status === 'loading') return;
    if (this.status === 'ready' && !force) {
      void this.refreshProviders();
      return;
    }
    this.status = 'loading';
    try {
      const r = await assistantApi.providers();
      this.providers = r.providers;
      this.policy = r.attachments;
      this.choice = this.session ? this.choiceOf(this.session) : pickChoice(r.providers, this.savedChoice());
      this.status = this.choice ? 'ready' : 'no-provider';
    } catch (e) {
      const status = e instanceof ApiError ? e.status : 0;
      this.status = status === 503 ? 'disabled' : status === 401 || status === 403 ? 'unauthorized' : 'offline';
      if (status > 0 && this.status === 'offline') this.problem = explain(e);
    }
  }

  private async refreshProviders(): Promise<void> {
    try {
      const r = await assistantApi.providers();
      this.providers = r.providers;
      this.policy = r.attachments;
      if (!this.choice && !this.session) {
        this.choice = pickChoice(r.providers, this.savedChoice());
        if (!this.choice) this.status = 'no-provider';
      }
    } catch {
      // What is on screen stays; a real problem shows when the person does something.
    }
  }

  private savedChoice(): Partial<Choice> | null {
    try {
      const v = JSON.parse(readStore('local', CHOICE_KEY) ?? 'null') as Partial<Choice> | null;
      return v && typeof v === 'object' ? v : null;
    } catch {
      return null;
    }
  }

  private choiceOf(s: SessionView): Choice {
    return { provider: s.provider, model: s.model ?? '', reasoning: s.reasoning ?? '' };
  }

  // ---- the conversation ----

  private async resume(id: string): Promise<void> {
    try {
      const v = await assistantApi.get(id);
      this.adopt(v);
      this.connect(v.id);
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) this.lost('That conversation is over (Werkbord was restarted, or it was removed). Your next message starts a new one.');
      else if (e instanceof ApiError && (e.status === 401 || e.status === 403)) this.status = 'unauthorized';
      else if (e instanceof ApiError && e.status === 503) this.status = 'disabled';
      else setTimeout(() => void this.resume(id), 4000); // the controller is not answering yet: try again, the transcript waits
    }
  }

  /** Takes in what the server says about the conversation, and brings the transcript in line with it. */
  private adopt(v: SessionView): void {
    this.session = v;
    // A server that was restarted counts events from the start again; the transcript must count with it.
    reconcileSeq(this.transcript, v.lastEvent);
    adoptPending(this.transcript, v.pending ?? []);
    settleStale(this.transcript, v.running);
    this.choice = this.choiceOf(v);
    // The files the server holds are the truth; ones it no longer holds ran out of time or were sent.
    const held = (v.attachments ?? []).map((a) => a.id);
    for (const s of this.staged) {
      if (s.state === 'ready' && s.id && !held.includes(s.id)) s.state = 'expired';
    }
    for (const a of v.attachments ?? []) {
      if (!this.staged.some((s) => s.id === a.id)) {
        this.staged.push({ key: uid('f'), name: a.name, bytes: a.bytes, id: a.id, expiresAt: Date.parse(a.expiresAt), state: 'ready' });
      }
    }
    this.save();
  }

  private connect(id: string): void {
    this.conn?.stop();
    this.conn = new Connection({
      sessionId: id,
      after: () => this.transcript.lastSeq,
      onEvent: (e) => this.onEvent(e),
      onLink: (l) => {
        this.link = l;
        if (l === 'gone') this.lost('That conversation is over (Werkbord was restarted, or it was removed). Your next message starts a new one.');
        else if (l === 'unavailable') this.status = 'disabled';
        else if (l === 'unauthorized') this.status = 'unauthorized';
      },
      onOpen: async () => {
        const v = await assistantApi.get(id);
        this.adopt(v);
      },
    });
    this.conn.start();
  }

  private onEvent(e: AssistantEvent): void {
    const r = apply(this.transcript, e);
    if (r === 'gap') {
      const reply = activeReply(this.transcript);
      if (reply) reply.notices.push('Part of this reply was missed while the connection was down.');
      void assistantApi.get(e.sessionId).then((v) => this.adopt(v), () => {});
      return;
    }
    if (r === 'ignored') return;
    if (this.session && e.type === 'turn_started') this.session.running = true;
    if (this.session && (e.type === 'turn_completed' || e.type === 'turn_failed' || e.type === 'turn_cancelled')) {
      this.session.running = false;
      this.session.lastError = e.type === 'turn_failed' ? e.error?.code : '';
      if (e.state) this.session.state = e.state;
    }
    this.save();
  }

  /** The conversation is gone from the server. What is on screen stays; nothing there can be acted on any more. */
  private lost(message: string): void {
    this.conn?.stop();
    this.conn = null;
    this.session = null;
    this.transcript.lastSeq = 0;
    for (const it of this.transcript.items) {
      if (it.kind === 'confirm' && (it.state === 'waiting' || it.state === 'working')) it.state = 'gone';
    }
    settleStale(this.transcript, false);
    for (const s of this.staged) if (s.state === 'ready' || s.state === 'uploading') s.state = 'expired';
    this.transcript.items.push({ kind: 'notice', id: uid('n'), text: message });
    this.link = 'idle';
    this.choice = pickChoice(this.providers, this.savedChoice()) ?? this.choice;
    this.save();
  }

  private async ensureSession(): Promise<boolean> {
    if (this.session) return true;
    if (!this.choice) {
      this.problem = 'Claude Code or Codex needs to be set up and signed in first.';
      return false;
    }
    try {
      const v = await assistantApi.create({ provider: this.choice.provider, model: this.choice.model || undefined, reasoning: this.choice.reasoning || undefined });
      this.transcript.lastSeq = 0;
      this.adopt(v);
      this.connect(v.id);
      return true;
    } catch (e) {
      this.problem = explain(e);
      if (e instanceof ApiError && e.status === 400) void this.prepare(true);
      return false;
    }
  }

  /** Sends what is in the box (and the files that are ready) as the next message. */
  async send(text = this.draft, opts: { again?: boolean } = {}): Promise<void> {
    text = text.trim();
    const files = opts.again ? [] : this.readyFiles;
    if ((text === '' && files.length === 0) || this.running) return;
    this.problem = '';
    this.sending = true;
    try {
      if (!(await this.ensureSession())) return;
      const id = this.session!.id;
      const user: UserItem | null = opts.again ? null : { kind: 'user', id: uid('u'), text, files: files.map((f) => ({ name: f.name, bytes: f.bytes })) };
      if (user) this.transcript.items.push(user);
      const before = this.draft;
      if (!opts.again) this.draft = '';
      try {
        const { turnId } = await assistantApi.send(id, text, files.map((f) => f.id!).filter(Boolean));
        if (!this.transcript.items.some((i) => i.kind === 'reply' && i.turnId === turnId)) {
          this.transcript.items.push({ kind: 'reply', id: turnId, turnId, text: '', attempt: 0, attemptStart: 0, steps: [], notices: [], status: 'working' });
        }
        this.staged = this.staged.filter((s) => !files.includes(s));
        if (this.session) this.session.running = true;
      } catch (e) {
        if (user) this.transcript.items = this.transcript.items.filter((i) => i !== user);
        if (!opts.again) this.draft = before;
        this.problem = explain(e);
        if (e instanceof ApiError && (e.status === 404 || e.status === 409)) void assistantApi.get(id).then((v) => this.adopt(v), () => {});
      }
      this.save();
    } finally {
      this.sending = false;
    }
  }

  /** Asks the assistant to try the last message again, after a failure that a retry can fix. */
  async retry(): Promise<void> {
    const items = this.transcript.items;
    const last = items.length - 1;
    let r = last;
    while (r >= 0 && items[r].kind !== 'reply') r--;
    if (r < 0 || (items[r] as ReplyItem).status === 'working') return;
    let u = r - 1;
    while (u >= 0 && items[u].kind !== 'user') u--;
    if (u < 0) return;
    const text = (items[u] as UserItem).text;
    const hadFiles = (items[u] as UserItem).files.length > 0;
    items.splice(r, 1);
    if (hadFiles) items.push({ kind: 'notice', id: uid('n'), text: 'Files from that message are not sent again. Attach them again if they matter.' });
    await this.send(text || 'Please try that again.', { again: true });
  }

  async cancel(): Promise<void> {
    if (!this.session) return;
    try {
      await assistantApi.cancel(this.session.id);
    } catch (e) {
      this.problem = explain(e);
    }
  }

  /** The person's answer to a proposed change. Only this carries it out. */
  async resolve(item: ConfirmItem, decision: 'approve' | 'decline'): Promise<void> {
    if (!this.session || item.state !== 'waiting') return;
    item.state = 'working';
    this.problem = '';
    try {
      await assistantApi.resolve(this.session.id, item.id, decision);
      // The event that follows carries the details; this settles the card even if the stream is down.
      if (item.state === 'working') item.state = decision === 'approve' ? 'confirmed' : 'declined';
    } catch (e) {
      if (e instanceof ApiError && e.status === 410) {
        item.state = 'expired';
      } else if (e instanceof ApiError && (e.status === 404 || e.code === 'not_pending')) {
        item.state = 'gone';
      } else if (e instanceof ApiError && e.status === 409) {
        item.state = 'failed';
        item.outcome = e.message;
      } else if (e instanceof ApiError && decision === 'approve' && e.status >= 400 && e.status < 500) {
        item.state = 'failed';
        item.outcome = e.message;
      } else {
        item.state = 'waiting';
        this.problem = explain(e);
      }
    }
    this.save();
  }

  // ---- choosing the provider, model and level ----

  /** Changes what the next message is answered by. Another provider starts a fresh conversation with it, and says so. */
  async configure(patch: Partial<Choice>): Promise<void> {
    if (!this.choice) return;
    const prev = this.choice;
    const switched = patch.provider !== undefined && patch.provider !== prev.provider;
    const next: Choice = { ...prev, ...patch };
    if (switched) {
      next.model = patch.model ?? '';
      next.reasoning = patch.reasoning ?? '';
    }
    this.choice = next;
    writeStore('local', CHOICE_KEY, JSON.stringify(next));
    this.problem = '';
    if (!this.session) return;
    const names = (id: string) => this.providers.find((p) => p.id === id)?.name ?? id;
    const hadFiles = this.readyFiles.length > 0;
    try {
      const v = await assistantApi.configure(this.session.id, { provider: switched ? next.provider : undefined, model: next.model, reasoning: next.reasoning });
      this.adopt(v);
      if (switched) {
        // Files authorized for the first provider were dropped by the controller; they leave the screen too.
        this.staged = this.staged.filter((s) => s.state === 'asking' || s.state === 'uploading');
        this.transcript.items.push({
          kind: 'notice',
          id: uid('n'),
          text: `Now using ${names(next.provider)}. This is a fresh conversation with it: it does not remember what came before.${hadFiles ? ' Files you had attached were removed; attach them again to share them with it.' : ''}`,
        });
        this.save();
      }
    } catch (e) {
      this.choice = prev;
      writeStore('local', CHOICE_KEY, JSON.stringify(prev));
      this.problem = explain(e);
    }
  }

  /** The reasoning levels the current model takes. */
  get levels(): string[] {
    return this.choice ? reasoningLevels(this.provider, this.choice.model) : [];
  }
  get modelLabel(): string {
    return this.choice ? modelName(this.provider, this.choice.model) : '';
  }

  // ---- files ----

  /** Takes the files the person picked. They are held here, in the page, and nothing is sent until the person allows it. */
  async stageFiles(files: File[]): Promise<void> {
    const policy = this.policy;
    if (!policy?.enabled) {
      this.problem = 'Files are turned off in this setup.';
      return;
    }
    const refused: string[] = [];
    for (const file of files) {
      const live = this.staged.filter((s) => s.state !== 'error' && s.state !== 'expired').length;
      if (live >= policy.maxFiles) {
        refused.push(`${file.name}: a message carries at most ${policy.maxFiles} files`);
        continue;
      }
      if (file.size > policy.maxBytes) {
        refused.push(`${file.name} is ${size(file.size)}; the most is ${size(policy.maxBytes)}`);
        continue;
      }
      if (file.size === 0) {
        refused.push(`${file.name} is empty`);
        continue;
      }
      const head = new Uint8Array(await file.slice(0, 4096).arrayBuffer());
      if (!looksLikeText(head)) {
        refused.push(`${file.name} is not a plain text file. PDFs and pictures cannot be shown to the assistant yet`);
        continue;
      }
      this.staged.push({ key: uid('f'), name: file.name, bytes: file.size, file, state: 'asking' });
    }
    this.problem = refused.length ? refused.join('. ') + '.' : '';
  }

  /** The person's yes: the waiting files are handed to Werkbord, which keeps them in memory for a short time. */
  async allowFiles(): Promise<void> {
    const todo = this.asking;
    if (todo.length === 0) return;
    for (const s of todo) s.state = 'uploading';
    if (!(await this.ensureSession())) {
      for (const s of todo) s.state = 'asking';
      return;
    }
    const id = this.session!.id;
    for (const s of todo) {
      try {
        const info = await assistantApi.attach(id, s.file!, s.name, true);
        s.id = info.id;
        s.expiresAt = Date.parse(info.expiresAt);
        s.file = undefined; // the page does not keep it either
        s.state = 'ready';
      } catch (e) {
        s.state = 'error';
        s.error = explain(e);
      }
    }
  }

  declineFiles(): void {
    this.staged = this.staged.filter((s) => s.state !== 'asking');
  }

  async removeFile(key: string): Promise<void> {
    const s = this.staged.find((x) => x.key === key);
    if (!s) return;
    this.staged = this.staged.filter((x) => x !== s);
    if (s.id && this.session && s.state === 'ready') {
      try {
        await assistantApi.detach(this.session.id, s.id);
      } catch {
        // It is gone from the screen; if the server still holds it, it runs out of time there.
      }
    }
  }

  private expireFiles(): void {
    for (const s of this.staged) if (s.state === 'ready' && s.expiresAt && s.expiresAt <= this.now) s.state = 'expired';
  }

  // ---- starting over ----

  /** Ends the conversation: a reply in progress is stopped and changes it left waiting are dropped. */
  async startOver(): Promise<void> {
    const s = this.session;
    this.conn?.stop();
    this.conn = null;
    if (s) {
      try {
        await assistantApi.remove(s.id);
      } catch {
        // Already gone, or the controller is not answering; either way this page is done with it.
      }
    }
    this.session = null;
    this.transcript = emptyTranscript();
    this.staged = [];
    this.draft = '';
    this.problem = '';
    this.link = 'idle';
    writeStore('session', SESSION_KEY, null);
  }

  // ---- keeping it for the life of the tab ----

  private save(): void {
    clearTimeout(this.saveTimer);
    this.saveTimer = setTimeout(() => this.saveNow(), 400);
  }

  private saveNow(): void {
    clearTimeout(this.saveTimer);
    if (!this.session) {
      if (this.transcript.items.length === 0) writeStore('session', SESSION_KEY, null);
      return;
    }
    writeStore('session', SESSION_KEY, serialize(this.session.id, this.transcript));
  }

  /** Test seam: the connection, to prove it is not torn down when the screen closes. */
  get connected(): boolean {
    return this.conn !== null;
  }
}

export const assistant = new AssistantStore();

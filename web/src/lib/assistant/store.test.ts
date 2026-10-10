import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { FakeServer, provider } from './fakeserver';
import type { AssistantStore } from './store.svelte';
import type { ConfirmItem, ReplyItem, UserItem } from './transcript';

// The page's side of the assistant, against a stand-in for the controller: the whole conversation, the screen opening and
// closing around it, files, choosing a provider, and coming back after a reload or a restart.

function memory(): Storage {
  const m = new Map<string, string>();
  return {
    get length() {
      return m.size;
    },
    clear: () => m.clear(),
    getItem: (k) => m.get(k) ?? null,
    key: (i) => [...m.keys()][i] ?? null,
    removeItem: (k) => void m.delete(k),
    setItem: (k, v) => void m.set(k, String(v)),
  };
}

let srv: FakeServer;
let session: Storage;
let local: Storage;
const pages: AssistantStore[] = [];

beforeEach(() => {
  srv = new FakeServer();
  session = memory();
  local = memory();
  vi.stubGlobal('sessionStorage', session);
  vi.stubGlobal('localStorage', local);
  vi.stubGlobal('window', { localStorage: local, addEventListener: () => {}, removeEventListener: () => {} });
  vi.stubGlobal('document', { addEventListener: () => {}, hidden: false });
  vi.stubGlobal('fetch', srv.fetch);
  vi.stubGlobal('setInterval', () => 0); // the page's minute-by-minute clock is not under test
});
afterEach(() => {
  // A page left connected by one test must not reconnect into the next test's server.
  for (const p of pages.splice(0)) (p as unknown as { conn: { stop(): void } | null }).conn?.stop();
  vi.unstubAllGlobals();
  vi.resetModules();
  vi.useRealTimers();
});

/** A fresh page: a new instance of the store, as a reload gives. */
async function page(): Promise<AssistantStore> {
  vi.resetModules();
  const { AssistantStore } = await import('./store.svelte');
  const a = new AssistantStore();
  pages.push(a);
  return a;
}
const replyOf = (a: AssistantStore) => a.transcript.items.find((i): i is ReplyItem => i.kind === 'reply')!;
const confirmOf = (a: AssistantStore) => a.transcript.items.find((i): i is ConfirmItem => i.kind === 'confirm')!;

/** Opens the screen and sends one message; the "provider" has been told nothing yet. */
async function talk(a: AssistantStore, text = 'what is blocked?') {
  a.show({ x: 10, y: 10 });
  await srv.until(() => a.status === 'ready', 'ready');
  await a.send(text);
  await srv.until(() => srv.streamCount === 1, 'the stream');
  return [...srv.sessions.values()][0];
}

describe('the screen opening and closing', () => {
  it('goes closed → opening → open → closing → closed, and gives focus back to what opened it', async () => {
    const a = await page();
    const trigger = { focus: vi.fn() } as unknown as HTMLElement;
    expect(a.phase).toBe('closed');
    a.show({ x: 40, y: 50 }, trigger);
    expect([a.mounted, a.phase, a.isOpen, a.origin]).toEqual([true, 'opening', true, { x: 40, y: 50 }]);
    a.opened();
    expect(a.phase).toBe('open');
    a.hide();
    expect([a.phase, a.isOpen, a.mounted]).toEqual(['closing', false, true]);
    a.closed();
    expect([a.phase, a.mounted]).toEqual(['closed', false]);
    await Promise.resolve();
    expect(trigger.focus).toHaveBeenCalled();
  });

  it('toggles, and can be turned round while it is still opening', async () => {
    const a = await page();
    a.toggle({ x: 1, y: 1 });
    expect(a.phase).toBe('opening');
    a.toggle();
    expect(a.phase).toBe('closing');
    a.show();
    expect(a.phase).toBe('opening');
    expect(a.mounted).toBe(true);
  });

  it('finds out what can be used only when it is first opened, not before', async () => {
    const a = await page();
    a.init();
    expect(srv.calls).toHaveLength(0);
    a.show();
    await srv.until(() => a.status === 'ready');
    expect(srv.called('GET', '/providers')).toHaveLength(1);
    expect(a.choice).toEqual({ provider: 'claude-code', model: '', reasoning: '' });
  });
});

describe('a conversation survives the screen closing', () => {
  it('keeps streaming, keeps its connection, and is there to see when the screen comes back', async () => {
    const a = await page();
    const s = await talk(a);
    srv.emit(s.id, { type: 'turn_started' });
    srv.emit(s.id, { type: 'delta', text: 'Two ', attempt: 1 });
    await srv.until(() => replyOf(a)?.text === 'Two ');

    a.hide();
    a.closed(); // the screen is gone; the page behind it is showing
    expect(a.mounted).toBe(false);
    expect(a.connected).toBe(true);
    expect(a.running).toBe(true);

    srv.emit(s.id, { type: 'delta', text: 'are blocked.', attempt: 1 });
    srv.emit(s.id, { type: 'turn_completed', state: 'idle' });
    await srv.until(() => replyOf(a).status === 'done');
    expect(replyOf(a).text).toBe('Two are blocked.');
    expect(a.running).toBe(false);
    expect(srv.called('POST', '/cancel')).toHaveLength(0); // closing never stops anything

    a.show();
    expect(a.transcript.items.map((i) => i.kind)).toEqual(['user', 'reply']);
  });

  it('keeps a change waiting for the person, and says so while the screen is closed', async () => {
    const a = await page();
    const s = await talk(a);
    srv.emit(s.id, { type: 'turn_started' });
    srv.emit(s.id, { type: 'confirmation_required', actionId: 'act_9', proposal: { actionId: 'act_9', operation: 'create_ticket', summary: 'Create task "A"', expiresAt: '2026-03-09T09:15:00Z' } });
    srv.emit(s.id, { type: 'turn_completed' });
    await srv.until(() => a.waiting === 1);
    a.hide();
    a.closed();
    expect(a.waiting).toBe(1); // what the sidebar button shows
    expect(confirmOf(a).state).toBe('waiting');
    expect(srv.called('POST', '/actions')).toHaveLength(0);
  });
});

describe('sending', () => {
  it('starts the conversation on the first message with the provider chosen, then sends it', async () => {
    const a = await page();
    await talk(a, 'hello');
    const created = srv.called('POST', '/sessions').find((c) => !c.path.includes('/messages'))!;
    expect(created.body).toEqual({ provider: 'claude-code' });
    expect(srv.called('POST', '/messages')[0].body).toEqual({ text: 'hello', attempts: undefined, attachments: [] });
    const [u] = a.transcript.items as UserItem[];
    expect(u).toMatchObject({ kind: 'user', text: 'hello' });
    expect(a.draft).toBe('');
    expect(a.running).toBe(true);
  });

  it('puts the words back and removes the bubble when the message was refused', async () => {
    const a = await page();
    a.show();
    await srv.until(() => a.status === 'ready');
    srv.fail.set('POST /api/assistant/sessions/ast_1/messages', 409);
    a.draft = 'keep these words';
    await a.send();
    expect(a.draft).toBe('keep these words');
    expect(a.transcript.items.filter((i) => i.kind === 'user')).toHaveLength(0);
    expect(a.problem).not.toBe('');
    expect(a.running).toBe(false);
  });

  it('does not send nothing, or a second message while one is being answered', async () => {
    const a = await page();
    const s = await talk(a, 'first');
    await a.send('   ');
    await a.send('second');
    expect(srv.called('POST', '/messages')).toHaveLength(1);
    srv.emit(s.id, { type: 'turn_started' });
    srv.emit(s.id, { type: 'turn_completed' });
    await srv.until(() => !a.running);
  });

  it('stops a reply on request', async () => {
    const a = await page();
    await talk(a);
    await a.cancel();
    expect(srv.called('POST', '/cancel')).toHaveLength(1);
  });

  it('tries the last message again after a failure, without a second bubble', async () => {
    const a = await page();
    const s = await talk(a, 'plan next week');
    srv.emit(s.id, { type: 'turn_started' });
    srv.emit(s.id, { type: 'turn_failed', error: { code: 'transient', message: 'The provider crashed.', retryable: true }, state: 'idle' });
    await srv.until(() => replyOf(a).status === 'failed');
    s.running = false;
    await a.retry();
    const sent = srv.called('POST', '/messages');
    expect(sent).toHaveLength(2);
    expect((sent[1].body as { text: string }).text).toBe('plan next week');
    expect(a.transcript.items.filter((i) => i.kind === 'user')).toHaveLength(1);
    expect(a.transcript.items.filter((i) => i.kind === 'reply')).toHaveLength(1);
    expect(replyOf(a).status).toBe('working');
  });
});

describe('the person’s answer to a proposed change', () => {
  async function waitingChange() {
    const a = await page();
    const s = await talk(a);
    srv.emit(s.id, { type: 'turn_started' });
    srv.emit(s.id, { type: 'confirmation_required', actionId: 'act_1', proposal: { actionId: 'act_1', operation: 'update_ticket', summary: 'Change ticket "A"', expiresAt: '2026-03-09T09:15:00Z' } });
    srv.emit(s.id, { type: 'turn_completed' });
    await srv.until(() => a.waiting === 1);
    return { a, s };
  }

  it('is carried out only by the person pressing confirm, with their decision in the request', async () => {
    const { a } = await waitingChange();
    await a.resolve(confirmOf(a), 'approve');
    expect(srv.called('POST', '/actions/act_1')[0].body).toEqual({ decision: 'approve' });
    expect(confirmOf(a).state).toBe('confirmed');
    expect(a.waiting).toBe(0);
  });

  it('records a no', async () => {
    const { a } = await waitingChange();
    await a.resolve(confirmOf(a), 'decline');
    expect(srv.called('POST', '/actions/act_1')[0].body).toEqual({ decision: 'decline' });
    expect(confirmOf(a).state).toBe('declined');
  });

  it('is not pressed twice', async () => {
    const { a } = await waitingChange();
    const item = confirmOf(a);
    await Promise.all([a.resolve(item, 'approve'), a.resolve(item, 'approve')]);
    expect(srv.called('POST', '/actions/act_1')).toHaveLength(1);
  });

  it.each([
    [410, 'expired'],
    [404, 'gone'],
    [409, 'failed'],
  ] as const)('shows what a %i means', async (status, state) => {
    const { a } = await waitingChange();
    srv.fail.set('POST /api/assistant/sessions/ast_1/actions/act_1', status);
    await a.resolve(confirmOf(a), 'approve');
    expect(confirmOf(a).state).toBe(state);
  });

  it('puts the change back as waiting if the controller could not be reached, so it can be pressed again', async () => {
    const { a } = await waitingChange();
    vi.stubGlobal('fetch', () => Promise.reject(new TypeError('Failed to fetch')));
    await a.resolve(confirmOf(a), 'approve');
    expect(confirmOf(a).state).toBe('waiting');
    expect(a.problem).toMatch(/cannot be reached/);
  });
});

describe('choosing the provider, model and level', () => {
  it('changes the model mid-conversation and keeps the conversation', async () => {
    const a = await page();
    await talk(a);
    await a.configure({ model: 'opus', reasoning: 'high' });
    expect(srv.called('PATCH', '/sessions/ast_1')[0].body).toEqual({ provider: undefined, model: 'opus', reasoning: 'high' });
    expect(a.choice).toEqual({ provider: 'claude-code', model: 'opus', reasoning: 'high' });
    expect(JSON.parse(local.getItem('werkbord.assistant.choice')!)).toMatchObject({ model: 'opus' });
    expect(a.transcript.items.some((i) => i.kind === 'notice')).toBe(false);
  });

  it('moves to the other provider, says it is a fresh conversation, and takes back files authorized for the first', async () => {
    const a = await page();
    await talk(a);
    await a.stageFiles([new File(['notes'], 'n.txt')]);
    await a.allowFiles();
    expect(a.readyFiles).toHaveLength(1);
    await a.configure({ provider: 'codex' });
    expect(srv.called('PATCH', '/sessions/ast_1')[0].body).toEqual({ provider: 'codex', model: '', reasoning: '' });
    expect(a.choice).toEqual({ provider: 'codex', model: '', reasoning: '' });
    const note = a.transcript.items.find((i) => i.kind === 'notice') as { text: string };
    expect(note.text).toMatch(/Now using Codex/);
    expect(note.text).toMatch(/does not remember what came before/);
    expect(note.text).toMatch(/Files you had attached were removed/);
    expect(a.staged).toHaveLength(0);
  });

  it('goes back to what it was if the controller refuses', async () => {
    const a = await page();
    await talk(a);
    srv.fail.set('PATCH /api/assistant/sessions/ast_1', 409);
    await a.configure({ model: 'opus' });
    expect(a.choice?.model).toBe('');
    expect(a.problem).not.toBe('');
  });

  it('can be chosen before there is a conversation', async () => {
    const a = await page();
    a.show();
    await srv.until(() => a.status === 'ready');
    await a.configure({ provider: 'codex', model: 'gpt-a' });
    await a.send('hi');
    expect(srv.called('POST', '/sessions')[0].body).toEqual({ provider: 'codex', model: 'gpt-a' });
  });

  it('remembers the choice on this device for the next conversation', async () => {
    const a = await page();
    a.show();
    await srv.until(() => a.status === 'ready');
    await a.configure({ provider: 'codex' });
    const b = await page();
    b.show();
    await srv.until(() => b.status === 'ready');
    expect(b.choice?.provider).toBe('codex');
  });
});

describe('files', () => {
  const text = (name: string, body = 'hello') => new File([body], name, { type: 'text/plain' });

  it('are held in the page and sent nowhere until the person says yes', async () => {
    const a = await page();
    a.show();
    await srv.until(() => a.status === 'ready');
    await a.stageFiles([text('a.md')]);
    expect(a.asking).toHaveLength(1);
    expect(srv.called('POST', '/attachments')).toHaveLength(0);
    expect(srv.called('POST', '/sessions')).toHaveLength(0); // not even a conversation was begun
    a.declineFiles();
    expect(a.staged).toHaveLength(0);
    expect(srv.calls.filter((c) => c.path.includes('attachments'))).toHaveLength(0);
  });

  it('are handed over with the authorization on the request, then ride along with the next message', async () => {
    const a = await page();
    a.show();
    await srv.until(() => a.status === 'ready');
    await a.stageFiles([text('plan é.md', 'ship it')]);
    await a.allowFiles();
    const up = srv.called('POST', '/attachments')[0];
    expect(up.headers.get('X-Attachment-Authorized')).toBe('true');
    expect(decodeURIComponent(up.headers.get('X-Attachment-Name')!)).toBe('plan é.md');
    expect(up.rawBody).toBe('ship it');
    expect(a.readyFiles).toHaveLength(1);
    expect(a.readyFiles[0].file).toBeUndefined(); // the page does not keep its copy

    await a.send('what do you think?');
    const sent = srv.called('POST', '/messages')[0].body as { attachments: string[] };
    expect(sent.attachments).toEqual([a.transcript.items.length ? srv.attachments[0]?.id ?? sent.attachments[0] : '']);
    expect(sent.attachments).toHaveLength(1);
    expect((a.transcript.items[0] as UserItem).files).toEqual([{ name: 'plan é.md', bytes: 7 }]);
    expect(a.staged).toHaveLength(0);
  });

  it('are refused before they go anywhere if too big, empty, not text, or too many', async () => {
    const a = await page();
    a.show();
    await srv.until(() => a.status === 'ready');
    const pdf = new File([new Uint8Array([0x25, 0x50, 0x44, 0x46, 0x00, 0x01])], 'x.pdf');
    await a.stageFiles([new File(['a'.repeat(262145)], 'big.txt'), new File([], 'empty.txt'), pdf]);
    expect(a.staged).toHaveLength(0);
    expect(a.problem).toMatch(/big\.txt is 257 KB; the most is 256 KB/);
    expect(a.problem).toMatch(/empty\.txt is empty/);
    expect(a.problem).toMatch(/x\.pdf is not a plain text file. PDFs and pictures cannot be shown/);

    await a.stageFiles([1, 2, 3, 4, 5].map((i) => text(`f${i}.txt`)));
    expect(a.staged).toHaveLength(4);
    expect(a.problem).toMatch(/f5\.txt: a message carries at most 4 files/);
  });

  it('can be taken back, which tells the controller to forget them', async () => {
    const a = await page();
    a.show();
    await srv.until(() => a.status === 'ready');
    await a.stageFiles([text('a.md')]);
    await a.allowFiles();
    const id = a.staged[0].id!;
    await a.removeFile(a.staged[0].key);
    expect(a.staged).toHaveLength(0);
    expect(srv.called('DELETE', `/attachments/${id}`)).toHaveLength(1);
  });

  it('show as removed when their time runs out, and are not sent', async () => {
    const a = await page();
    a.show();
    await srv.until(() => a.status === 'ready');
    await a.stageFiles([text('a.md')]);
    await a.allowFiles();
    a.staged[0].expiresAt = Date.now() - 1;
    a.now = Date.now();
    (a as unknown as { expireFiles(): void }).expireFiles();
    expect(a.staged[0].state).toBe('expired');
    expect(a.readyFiles).toHaveLength(0);
    await a.send('go');
    expect((srv.called('POST', '/messages')[0].body as { attachments: string[] }).attachments).toEqual([]);
  });

  it('show the reason when the controller refuses one', async () => {
    const a = await page();
    a.show();
    await srv.until(() => a.status === 'ready');
    srv.fail.set('POST /api/assistant/sessions/ast_1/attachments', 413);
    await a.stageFiles([text('a.md')]);
    await a.allowFiles();
    expect(a.staged[0].state).toBe('error');
    expect(a.staged[0].error).toBeTruthy();
  });

  it('are not offered where the setup turns them off', async () => {
    srv.policy.enabled = false;
    const a = await page();
    a.show();
    await srv.until(() => a.status === 'ready');
    expect(a.canAttach).toBe(false);
    await a.stageFiles([text('a.md')]);
    expect(a.staged).toHaveLength(0);
    expect(a.problem).toMatch(/turned off/);
  });
});

describe('what the screen shows when there is nothing to talk to', () => {
  it('asks to sign in when no provider is ready, and finds out again on request', async () => {
    srv.providers = [provider('claude-code', 'Claude Code', { available: false, signedIn: false, detail: 'not signed in', guidance: 'Run `claude auth login`.' }), provider('codex', 'Codex', { installed: false, available: false })];
    const a = await page();
    a.show();
    await srv.until(() => a.status === 'no-provider');
    expect(a.choice).toBeNull();
    srv.providers = [provider('claude-code', 'Claude Code')];
    await a.prepare(true);
    expect(a.status).toBe('ready');
  });

  it.each([
    [503, 'disabled'],
    [401, 'unauthorized'],
    [500, 'offline'],
  ] as const)('shows %i as %s', async (status, want) => {
    srv.fail.set('GET /api/assistant/providers', status);
    const a = await page();
    a.show();
    await srv.until(() => a.status === want);
  });

  it('shows the controller being unreachable, and recovers when it is back', async () => {
    vi.stubGlobal('fetch', () => Promise.reject(new TypeError('Failed to fetch')));
    const a = await page();
    a.show();
    await srv.until(() => a.status === 'offline');
    vi.stubGlobal('fetch', srv.fetch);
    await a.prepare(true);
    expect(a.status).toBe('ready');
  });
});

describe('coming back after a reload', () => {
  async function conversation(leave = true) {
    const a = await page();
    const s = await talk(a, 'what is blocked?');
    srv.emit(s.id, { type: 'turn_started' });
    srv.emit(s.id, { type: 'delta', text: 'Two tasks are ', attempt: 1 });
    await srv.until(() => replyOf(a)?.text === 'Two tasks are ');
    (a as unknown as { saveNow(): void }).saveNow();
    if (leave) {
      (a as unknown as { conn: { stop(): void } }).conn.stop(); // this tab is gone; the next page is the one under test
      srv.dropStreams();
    }
    return { a, s };
  }

  it('picks up the conversation, and the reply goes on streaming into it', async () => {
    const { s } = await conversation();
    srv.dropStreams();
    const b = await page(); // the tab was reloaded
    b.init();
    await srv.until(() => srv.streamCount === 1, 'the new stream');
    expect(b.transcript.items.map((i) => i.kind)).toEqual(['user', 'reply']);
    expect(replyOf(b).text).toBe('Two tasks are ');
    expect(replyOf(b).status).toBe('working'); // the server still says it is running
    srv.emit(s.id, { type: 'delta', text: 'blocked.', attempt: 1 });
    srv.emit(s.id, { type: 'turn_completed', state: 'idle' });
    await srv.until(() => replyOf(b).status === 'done');
    expect(replyOf(b).text).toBe('Two tasks are blocked.');
    expect(srv.called('GET', '/events').at(-1)!.headers.get('Last-Event-ID')).toBe(String(replyOf(b).text ? 2 : 0));
  });

  it('shows a change that is still waiting, even if the transcript did not know of it', async () => {
    const { s } = await conversation();
    srv.dropStreams();
    const act = srv.propose(s.id);
    srv.session(s.id)!.running = false;
    const b = await page();
    b.init();
    await srv.until(() => b.waiting === 1);
    expect(confirmOf(b).id).toBe(act.id);
    expect(replyOf(b).status).toBe('done'); // it stopped while the page was away
  });

  it('closes a change that was settled while it was away', async () => {
    const a = await page();
    const s = await talk(a);
    srv.emit(s.id, { type: 'confirmation_required', actionId: 'act_1', proposal: { actionId: 'act_1', operation: 'create_ticket', summary: 'x', expiresAt: '' } });
    await srv.until(() => a.waiting === 1);
    (a as unknown as { saveNow(): void }).saveNow();
    srv.dropStreams();
    const b = await page();
    b.init();
    await srv.until(() => confirmOf(b)?.state === 'gone');
    expect(b.waiting).toBe(0);
  });

  it('counts events afresh when the controller was restarted, so the next reply is not thrown away', async () => {
    const { a, s } = await conversation(false);
    expect(a.transcript.lastSeq).toBeGreaterThan(0);
    srv.restart();
    await srv.until(() => srv.streamCount === 1 && a.transcript.lastSeq === 0, 'reconnected and recounted');
    srv.session(s.id)!.running = true;
    srv.emit(s.id, { type: 'turn_started', turnId: 't2' });
    srv.emit(s.id, { type: 'delta', turnId: 't2', text: 'Back again.', attempt: 1 });
    await srv.until(() => a.transcript.items.some((i) => i.kind === 'reply' && i.turnId === 't2' && i.text === 'Back again.'));
  });

  it('says plainly that the conversation is over when the controller no longer has it, and starts a new one on the next message', async () => {
    await conversation();
    srv.sessions.clear();
    srv.dropStreams();
    const b = await page();
    b.init();
    await srv.until(() => b.session === null && b.transcript.items.some((i) => i.kind === 'notice'));
    const note = b.transcript.items.find((i) => i.kind === 'notice') as { text: string };
    expect(note.text).toMatch(/That conversation is over/);
    expect(replyOf(b).status).not.toBe('working');
    b.show();
    await srv.until(() => b.status === 'ready');
    await b.send('hello again');
    expect(srv.called('POST', '/sessions').filter((c) => !c.path.includes('messages'))).toHaveLength(2);
  });

  it('starts clean when nothing was kept, or what was kept is not trustworthy', async () => {
    session.setItem('werkbord.assistant.v1', '{"v":1,"sessionId":"../../etc","lastSeq":1,"items":[]}');
    const a = await page();
    a.init();
    expect(srv.calls).toHaveLength(0);
    expect(a.transcript.items).toHaveLength(0);
  });

  it('keeps trying while the controller is still coming up', async () => {
    const { a } = await conversation();
    void a;
    srv.dropStreams();
    vi.useFakeTimers();
    vi.stubGlobal('fetch', () => Promise.reject(new TypeError('Failed to fetch')));
    const b = await page();
    b.init();
    await vi.advanceTimersByTimeAsync(10);
    vi.stubGlobal('fetch', srv.fetch);
    await vi.advanceTimersByTimeAsync(4500);
    vi.useRealTimers();
    await srv.until(() => srv.streamCount === 1);
    expect(b.transcript.items.length).toBeGreaterThan(0);
  });
});

describe('starting over', () => {
  it('ends the conversation on the controller and clears the page of it', async () => {
    const a = await page();
    await talk(a);
    (a as unknown as { saveNow(): void }).saveNow();
    expect(session.getItem('werkbord.assistant.v1')).not.toBeNull();
    await a.startOver();
    expect(srv.called('DELETE', '/sessions/ast_1')).toHaveLength(1);
    expect([a.session, a.transcript.items.length, a.staged.length, a.link]).toEqual([null, 0, 0, 'idle']);
    expect(session.getItem('werkbord.assistant.v1')).toBeNull();
    expect(a.connected).toBe(false);
  });
});

describe('a reply that was partly missed', () => {
  it('says so and reloads what the controller holds', async () => {
    const a = await page();
    const s = await talk(a);
    srv.emit(s.id, { type: 'turn_started' });
    await srv.until(() => !!replyOf(a));
    const before = srv.called('GET', '/sessions/ast_1').length;
    srv.emit(s.id, { type: 'gap', seq: 0 });
    await srv.until(() => replyOf(a).notices.length === 1);
    expect(replyOf(a).notices[0]).toMatch(/missed/);
    await srv.until(() => srv.called('GET', '/sessions/ast_1').length > before);
  });
});

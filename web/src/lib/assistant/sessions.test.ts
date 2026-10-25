import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { tick } from 'svelte';
import { FakeServer } from './fakeserver';
import type { AssistantSessions } from './sessions.svelte';
import type { ReplyItem } from './transcript';

function memory(): Storage {
  const map = new Map<string, string>();
  return { get length() { return map.size; }, clear: () => map.clear(), getItem: k => map.get(k) ?? null,
    key: i => [...map.keys()][i] ?? null, removeItem: k => { map.delete(k); }, setItem: (k, v) => { map.set(k, v); } };
}
let srv: FakeServer;
let storage: Storage;
const managers: AssistantSessions[] = [];
beforeEach(() => {
  srv = new FakeServer(); storage = memory();
  vi.stubGlobal('sessionStorage', storage);
  vi.stubGlobal('localStorage', memory());
  vi.stubGlobal('window', { addEventListener() {}, removeEventListener() {} });
  vi.stubGlobal('document', { addEventListener() {}, removeEventListener() {}, hidden: false });
  vi.stubGlobal('fetch', srv.fetch);
});
afterEach(() => {
  for (const manager of managers.splice(0)) manager.destroy();
  vi.unstubAllGlobals(); vi.resetModules();
});
async function manager() {
  const { AssistantSessions } = await import('./sessions.svelte');
  const m = new AssistantSessions(); managers.push(m); m.init(); return m;
}
const reply = (m: AssistantSessions, id: string) => m.tabs.find(t => t.id === id)!.store.transcript.items.find(i => i.kind === 'reply') as ReplyItem;

describe('Salie session manager', () => {
  it('keeps five streams, settings, cursors, drafts and confirmations independent while switching and hiding', async () => {
    const m = await manager();
    const sessions: {tab: string; session: string; turn: string}[] = [];
    for (let i = 0; i < 5; i++) {
      if (i) m.newTab();
      const store = m.active;
      await store.prepare();
      await store.configure({ provider: i % 2 ? 'codex' : 'claude-code', model: `model-${i}`, reasoning: i % 2 ? 'low' : 'high', mode: 'manual' });
      await store.send(`Conversation ${i}`);
      const r = store.transcript.items.find(x => x.kind === 'reply') as ReplyItem;
      sessions.push({tab: m.activeId, session: store.session!.id, turn: r.turnId});
      store.draft = `Draft ${i}`;
    }
    await srv.until(() => srv.streamCount === 1);
    const subscriptions = srv.called('POST', '/events').length;
    m.show(); m.opened(); m.hide(); m.closed();
    for (let i = 0; i < sessions.length; i++) {
      const s = sessions[i];
      m.select(s.tab);
      srv.emit(s.session, {type:'delta', turnId:s.turn, text:`Reply ${i}`, attempt:1});
      srv.emit(s.session, {type:'confirmation_required', turnId:s.turn, actionId:`act_${i}`, proposal:{actionId:`act_${i}`,operation:'create_ticket',summary:`Task ${i}`,expiresAt:'2099-01-01T00:00:00Z'}});
      srv.emit(s.session, {type:'turn_completed', turnId:s.turn, state:'awaiting_confirmation'});
    }
    await srv.until(() => sessions.every(s => reply(m,s.tab)?.status === 'done'));
    expect(new Set(sessions.map(s => s.session)).size).toBe(5);
    for (let i = 0; i < sessions.length; i++) {
      const store = m.tabs.find(t => t.id === sessions[i].tab)!.store;
      expect(reply(m,sessions[i].tab).text).toBe(`Reply ${i}`);
      expect(store.choice).toMatchObject({provider:i%2?'codex':'claude-code',model:`model-${i}`,reasoning:i%2?'low':'high'});
      expect(store.draft).toBe(`Draft ${i}`);
      expect(store.waitingItems.map(x => x.id)).toEqual([`act_${i}`]);
      expect(store.connected).toBe(true);
    }
    expect(m.waiting).toBe(5);
    expect(srv.called('POST', '/events')).toHaveLength(subscriptions);
    expect(srv.called('POST', '/events').at(-1)?.body).toMatchObject({sessions:expect.arrayContaining(sessions.map(s=>({id:s.session,after:expect.any(Number)})))});
    expect(srv.called('POST','/cancel')).toHaveLength(0);
    expect(srv.calls.filter(c => c.method === 'DELETE')).toHaveLength(0);
  });

  it('keeps model-selection modes separate across provider changes and reload', async () => {
    const m = await manager(); await m.active.prepare();
    await m.active.configure({mode:'max_reasoning'}); await m.active.send('Reason deeply');
    const first = m.activeId;
    m.newTab(); await m.active.prepare();
    await m.active.configure({mode:'manual',provider:'codex',model:'gpt-a',reasoning:'low'});
    await m.active.send('Manual request'); const second = m.activeId;
    m.select(first); expect(m.active.choice?.mode).toBe('max_reasoning');
    m.save(); m.destroy();
    const restored = await manager();
    await srv.until(()=>restored.tabs.every(t=>!!t.store.session));
    expect(restored.active.choice?.mode).toBe('max_reasoning');
    restored.select(second);
    expect(restored.active.choice).toMatchObject({mode:'manual',provider:'codex',model:'gpt-a',reasoning:'low'});
  });

  it('keeps a closed tab streaming and reopens it with the same session and title', async () => {
    const m = await manager(); await m.active.prepare(); await m.active.send('first');
    const tab = m.activeId, session = m.active.session!.id;
    const turn = reply(m,tab).turnId;
    await srv.until(() => srv.streamCount === 1);
    m.rename(tab,'Planning'); m.closeTab(tab);
    expect(m.activeId).not.toBe(tab); expect(m.running).toBe(true);
    srv.emit(session,{type:'delta',turnId:turn,text:'Continued in background',attempt:1});
    await srv.until(() => reply(m,tab).text === 'Continued in background');
    m.reopen(tab);
    expect(m.active.session!.id).toBe(session);
    expect(m.title(m.openTabs.find(t => t.id === tab)!)).toBe('Planning');
    expect(m.active.connected).toBe(true);
  });

  it('restores active and closed tabs, bounded transcripts, independent unsent choices and drafts after reload', async () => {
    const m = await manager(); await m.active.prepare();
    await m.active.configure({provider:'codex',model:'gpt-a',reasoning:'low',mode:'manual'});
    await m.active.send('Saved text');
    const first = m.activeId, session = m.active.session!.id;
    m.rename(first,'Saved planning'); m.closeTab(first);
    await m.active.prepare(); await m.active.configure({mode:'max_reasoning'});
    m.active.draft = 'Unsent draft'; const second = m.activeId;
    await tick(); m.save(); m.destroy();
    const restored = await manager();
    await srv.until(() => restored.closedTabs[0]?.store.session?.id === session);
    expect(restored.activeId).toBe(second);
    expect(restored.active.draft).toBe('Unsent draft');
    expect(restored.active.choice?.mode).toBe('max_reasoning');
    restored.reopen(first);
    expect(restored.active.transcript.items.some(i => i.kind === 'user' && i.text === 'Saved text')).toBe(true);
    expect(restored.active.choice).toMatchObject({provider:'codex',model:'gpt-a',reasoning:'low'});
    expect(srv.calls.some(c => c.path.includes('transcript'))).toBe(false);
  });

  it('migrates the previous single conversation cache and tolerates a corrupt index', async () => {
    storage.setItem('werkbord.assistant.v1',JSON.stringify({v:1,sessionId:'ast_old',lastSeq:0,items:[{kind:'user',id:'u1',text:'Legacy conversation',files:[]}]}));
    const m = await manager();
    expect(m.active.transcript.items[0]).toMatchObject({text:'Legacy conversation'});
    expect(storage.getItem('werkbord.assistant.v1')).toBeNull();
    storage.setItem('werkbord.assistant.tabs.v1','{invalid');
    const clean = await manager(); expect(clean.openTabs).toHaveLength(1);
  });
});

it('deduplicates attached run tabs, restores them and never creates a provider session for them', async () => {
  const { api } = await import('../api');
  const { app } = await import('../state.svelte');
  vi.spyOn(api, 'getRun').mockResolvedValue({id:'run-one',projectId:'project-one',state:'running',version:1} as Awaited<ReturnType<typeof api.getRun>>);
  vi.spyOn(api, 'runEvents').mockResolvedValue({events:[],hasMore:false});
  const stop = vi.spyOn(api, 'stopRun');
  const watch = vi.spyOn(app,'watchRun');
  const m = await manager();
  m.openRun('project-one','run-one'); const id = m.activeId;
  m.openRun('project-one','run-one');
  expect(m.activeId).toBe(id); expect(m.tabs.filter(t => t.run)).toHaveLength(1);
  expect(watch).toHaveBeenCalledTimes(1);
  m.closeTab(id); m.openRun('project-one','run-one'); expect(m.activeId).toBe(id); expect(m.activeTab.closed).toBe(false);
  m.hide(); m.closed(); expect(stop).not.toHaveBeenCalled();
  m.save(); m.destroy();
  const restored = await manager();
  expect(restored.tabs.filter(t => t.run)).toHaveLength(1);
  expect(restored.activeTab.run?.runId).toBe('run-one');
  expect(srv.calls.filter(c => c.method === 'POST' && c.path === '/sessions')).toHaveLength(0);
  vi.restoreAllMocks();
});

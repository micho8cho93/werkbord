import { describe, expect, it, vi } from 'vitest';
import { handle, isPlace, type FrameRef, type Deps } from './frames';

const personal = {} as Window, team = {} as Window;
const frames: FrameRef[] = [{ id: 'personal', origin: 'http://127.0.0.1:7420', window: personal }, { id: 'team:main', origin: 'http://127.0.0.1:7431', window: team }];
const deps = (): Deps => ({ relay: vi.fn().mockResolvedValue('ok'), reply: vi.fn(), remember: vi.fn().mockResolvedValue(undefined), ready: vi.fn() });
const data = { type: 'werkbord.native.request', id: 'request1', method: 'Info', args: [] };

describe('frame authority', () => {
  it('rejects forged sources, origins and invalid calls without relay', async () => {
    const d = deps();
    for (const e of [
      { source: {}, origin: frames[0].origin, data },
      { source: personal, origin: frames[1].origin, data },
      { source: team, origin: 'https://evil.example', data },
      { source: team, origin: frames[1].origin, data: { ...data, args: ['x'.repeat(8193)] } },
      { source: team, origin: frames[1].origin, data: { ...data, method: 'eval()' } },
    ]) await handle(frames, e, d);
    expect(d.relay).not.toHaveBeenCalled();
  });
  it('uses the shell identity even if a Team frame claims to be Personal', async () => {
    const d = deps();
    await handle(frames, { source: team, origin: frames[1].origin, data: { ...data, workspace: 'personal' } }, d);
    expect(d.relay).toHaveBeenCalledWith('team:main', 'Info', []);
    expect(d.reply).toHaveBeenCalledWith(frames[1], { type: 'werkbord.native.result', id: 'request1', ok: true, result: 'ok' });
  });
  it('remembers only internal routes from known frames', async () => {
    const d = deps();
    for (const place of ['https://evil.example', '?tab=//evil', '?tab=../personal', '?token=SECRET', '?tab=board&join=SECRET', '#/p/prj_1?invite=SECRET', '?%74oken=SECRET', '?tab=board&project=tpj_1']) await handle(frames, { source: team, origin: frames[1].origin, data: { type: 'werkbord.frame', event: 'place', place } }, d);
    expect(d.remember).toHaveBeenCalledExactlyOnceWith('team:main', '?tab=board&project=tpj_1');
    expect(isPlace('#/p/prj_1/task/tsk_1')).toBe(true);
  });
});

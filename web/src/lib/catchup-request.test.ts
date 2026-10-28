import { describe, expect, it, vi } from 'vitest';
vi.mock('./theme.svelte', () => ({ look: {}, theme: {} }));
vi.mock('./planningBridge', () => ({ followPlanning: () => () => {} }));
import { parseAssistantRequest } from './embed';
describe('explicit catch-up context from the shell', () => {
  it('accepts bounded context only on a general open request', () => {
    expect(parseAssistantRequest({ type: 'werkbord.assistant', action: 'open', catchUpPrompt: 'Explain recorded events' })?.catchUpPrompt).toBe('Explain recorded events');
    for (const invalid of [{ action: 'close' }, { personalWorkspace: 'pws_aaaaaaaaaaaaaaaa' }, { run: { projectId: 'p', runId: 'r' } }, { catchUpPrompt: 'x'.repeat(24001) }]) expect(parseAssistantRequest({ type: 'werkbord.assistant', action: 'open', catchUpPrompt: 'data', ...invalid })).toBeNull();
  });
});

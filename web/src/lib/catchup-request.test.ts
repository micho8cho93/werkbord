import { describe, expect, it, vi } from 'vitest';
vi.mock('./theme.svelte', () => ({ look: {}, theme: {} }));
vi.mock('./planningBridge', () => ({ followPlanning: () => () => {} }));
import { parseAssistantRequest } from './embed';
describe('explicit catch-up context from the shell', () => {
  it('accepts bounded context only on a general open request', () => {
    expect(parseAssistantRequest({ type: 'werkbord.assistant', action: 'open', catchUpPrompt: 'Explain recorded events' })?.catchUpPrompt).toBe('Explain recorded events');
    for (const invalid of [{ action: 'close' }, { personalWorkspace: 'pws_aaaaaaaaaaaaaaaa' }, { run: { projectId: 'p', runId: 'r' } }, { catchUpPrompt: 'x'.repeat(24001) }]) expect(parseAssistantRequest({ type: 'werkbord.assistant', action: 'open', catchUpPrompt: 'data', ...invalid })).toBeNull();
  });

  it('sends a prompt at once only when the shell says the person asked for it, and never without a prompt', () => {
    expect(parseAssistantRequest({ type: 'werkbord.assistant', action: 'open', catchUpPrompt: 'Plan my day', sendNow: true })).toMatchObject({ catchUpPrompt: 'Plan my day', sendNow: true });
    expect(parseAssistantRequest({ type: 'werkbord.assistant', action: 'open', catchUpPrompt: 'Plan my day' })?.sendNow).toBeUndefined();
    expect(parseAssistantRequest({ type: 'werkbord.assistant', action: 'open', sendNow: true })).toBeNull();
    expect(parseAssistantRequest({ type: 'werkbord.assistant', action: 'open', catchUpPrompt: 'x', sendNow: 'yes' })).toBeNull();
  });
});

import { describe, expect, it, vi } from 'vitest';
import type { App, PremiumStatus } from './types';

const native = vi.hoisted(() => ({ app: {} as App }));
vi.mock('./native', () => ({ ask: (f: (app: App) => unknown) => f(native.app) }));
import { plan } from './plan.svelte';

const premium: PremiumStatus = { active: true, id: 'pk_a', holder: 'Ada', canCheck: true, buyUrl: '' };

describe('the person’s plan', () => {
  it('is Free until the app says otherwise, and Free again when there is no app behind the window', async () => {
    expect(plan.premium).toBe(false);
    native.app.Premium = async () => { throw new Error('This page is not connected to the Werkbord app.'); };
    await plan.refresh();
    expect(plan.premium).toBe(false);
    expect(plan.loaded).toBe(true);
    native.app.Premium = async () => premium;
    await plan.refresh();
    expect(plan.premium).toBe(true);
  });

  it('keeps the app’s words when a key is refused, and clears them on the next try', async () => {
    plan.status = { active: false, canCheck: true, buyUrl: '' };
    native.app.UnlockPremium = async () => { throw new Error('this unlock key is not valid: it was not issued for Werkbord or it was changed'); };
    expect(await plan.unlock('wbpremium-nope')).toBe(false);
    expect(plan.error).toContain('not valid');
    expect(plan.premium).toBe(false);
    native.app.UnlockPremium = async (key: string) => { expect(key).toBe('wbpremium-good'); return premium; };
    expect(await plan.unlock('wbpremium-good')).toBe(true);
    expect(plan.error).toBe('');
    expect(plan.premium).toBe(true);
    expect(plan.busy).toBe(false);
  });
});

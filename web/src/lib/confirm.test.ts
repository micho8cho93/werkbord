import { describe, expect, it } from 'vitest';
import { confirmAction, confirmState } from './confirm.svelte';

const ask = (title: string) => confirmAction({ title, message: 'm', confirmLabel: 'Go' });

describe('asking the person to confirm, in the page', () => {
  it('goes ahead only when the person says so, and clears the question', async () => {
    const yes = ask('One?');
    expect(confirmState.current?.title).toBe('One?');
    expect(confirmState.current?.danger).toBe(false);
    confirmState.current?.settle(true);
    expect(await yes).toBe(true);
    expect(confirmState.current).toBeNull();
    const no = ask('Two?');
    confirmState.current?.settle(false);
    expect(await no).toBe(false);
  });
  it('declines a question that a newer one replaces, so nothing is confirmed by accident', async () => {
    const first = ask('First?');
    const second = ask('Second?');
    expect(await first).toBe(false);
    expect(confirmState.current?.title).toBe('Second?');
    confirmState.current?.settle(true);
    expect(await second).toBe(true);
  });
});

describe('the question stays on screen until it is answered', () => {
  it('is cleared when answered through the state it is shown from, not only through the object that made it', async () => {
    const answer = ask('Shown?');
    const shown = confirmState.current;
    expect(shown).not.toBeNull();
    shown!.settle(false);
    expect(confirmState.current).toBeNull();
    expect(await answer).toBe(false);
  });
});

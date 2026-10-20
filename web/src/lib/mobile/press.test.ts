import { afterEach, describe, expect, it, vi } from 'vitest';
import { KeyPress } from './press';
afterEach(() => vi.useRealTimers());
describe('workflow key gestures', () => {
  it('lets an ordinary tap activate exactly once', () => {
    vi.useFakeTimers(); const hold = vi.fn(); const press = new KeyPress(hold);
    press.start(0,0); vi.advanceTimersByTime(120); press.end();
    expect(press.consumeClick()).toBe(false); expect(hold).not.toHaveBeenCalled();
  });
  it('configures on hold and suppresses the following synthetic click', () => {
    vi.useFakeTimers(); const hold = vi.fn(); const press = new KeyPress(hold);
    press.start(0,0); vi.advanceTimersByTime(550); press.end();
    expect(hold).toHaveBeenCalledTimes(1); expect(press.consumeClick()).toBe(true);
    expect(press.consumeClick()).toBe(false);
  });
  it('cancels long press when scrolling or dragging and never launches on release', () => {
    vi.useFakeTimers(); const hold = vi.fn(), drag = vi.fn(); const press = new KeyPress(hold,drag);
    press.start(0,0); press.move(50,0); vi.advanceTimersByTime(600); press.end();
    expect(drag).toHaveBeenCalledWith(50,0); expect(hold).not.toHaveBeenCalled(); expect(press.consumeClick()).toBe(true);
  });
  it('cleans up cancelled gestures and accepts the next tap', () => {
    vi.useFakeTimers(); const hold = vi.fn(); const press = new KeyPress(hold);
    press.start(0,0); press.cancel(); vi.advanceTimersByTime(600);
    expect(hold).not.toHaveBeenCalled(); expect(press.consumeClick()).toBe(true);
    press.start(0,0); press.end(); expect(press.consumeClick()).toBe(false);
  });
  it('keeps the click of a finished tap when touch fires pointerleave after pointerup', () => {
    vi.useFakeTimers(); const hold = vi.fn(); const press = new KeyPress(hold);
    press.start(0,0); vi.advanceTimersByTime(80); press.end(); press.leave();
    expect(press.consumeClick()).toBe(false); expect(hold).not.toHaveBeenCalled();
  });
  it('still abandons a press when the finger slides off the key', () => {
    vi.useFakeTimers(); const hold = vi.fn(); const press = new KeyPress(hold);
    press.start(0,0); press.leave(); vi.advanceTimersByTime(600);
    expect(hold).not.toHaveBeenCalled(); expect(press.consumeClick()).toBe(true);
  });
});

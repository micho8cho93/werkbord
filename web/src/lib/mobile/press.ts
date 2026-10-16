/** Pointer gestures share one suppression guard: holding, scrolling and dragging
 * must never turn into a workflow launch on the following synthetic click. */
export class KeyPress {
  private timer?: ReturnType<typeof setTimeout>;
  private x = 0;
  private y = 0;
  private active = false;
  private handled = false;
  constructor(private hold: () => void, private drag?: (x: number, y: number) => void) {}
  start(x: number, y: number, hold = true) {
    this.cancel(); this.x = x; this.y = y; this.active = true; this.handled = false;
    if (hold) this.timer = setTimeout(() => { this.handled = true; this.hold(); }, 550);
  }
  move(x: number, y: number) {
    if (!this.active || Math.hypot(x - this.x, y - this.y) < 9) return;
    clearTimeout(this.timer); this.handled = true;
    this.drag?.(x, y);
  }
  end() { clearTimeout(this.timer); this.active = false; }
  cancel() { this.end(); this.handled = true; }
  consumeClick() { const handled = this.handled; this.handled = false; return handled; }
}

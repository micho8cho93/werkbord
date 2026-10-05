// Which action sheet is open. One at a time, from anywhere on the Git screens.

export type SheetState =
  /** `onmerged` runs once the merge is done: the board uses it to move the card to Done. */
  | { kind: 'merge'; branch: string; onmerged?: () => void }
  | { kind: 'push'; branch: string }
  | { kind: 'pr'; branch: string }
  | { kind: 'delete'; branch: string }
  | { kind: 'clean'; worktreeId: string; label: string; branch: string; head: string };

class Sheets {
  current = $state<SheetState | null>(null);

  open(s: SheetState): void {
    this.current = s;
  }

  close(): void {
    this.current = null;
  }
}

export const sheets = new Sheets();

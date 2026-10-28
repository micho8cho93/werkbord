/**
 * Asking the person to confirm, inside the page.
 *
 * The browser's own confirm() cannot be used: the desktop app shows this page in a WKWebView, which answers "Cancel" to every
 * confirm() the host app has not handled, so the action behind it silently never ran. A question asked here works the same in the
 * desktop window, a browser and on a phone, and can be tested.
 */

export interface ConfirmOptions {
  /** What is being asked, as the dialog's heading. */
  title: string;
  /** What will happen. */
  message: string;
  /** The button that goes ahead, named for what it does ("Delete project"), never "OK". */
  confirmLabel: string;
  /** The action cannot simply be undone, or signs something out: the dialog starts on Cancel. */
  danger?: boolean;
}

export interface PendingConfirm extends Required<ConfirmOptions> {
  /** Tells this question from the one that replaced it. Not an object identity: state wraps what it holds in a proxy. */
  id: number;
  settle(ok: boolean): void;
}

/** The question on screen, if any. ConfirmDialog shows it. */
export const confirmState = $state<{ current: PendingConfirm | null }>({ current: null });

let sequence = 0;

/** Asks, and answers true only if the person chose to go ahead. A second question replaces the first, which is then declined. */
export function confirmAction(options: ConfirmOptions): Promise<boolean> {
  confirmState.current?.settle(false);
  return new Promise<boolean>((resolve) => {
    const id = ++sequence;
    const pending: PendingConfirm = {
      danger: false,
      ...options,
      id,
      settle(ok) {
        if (confirmState.current?.id === id) confirmState.current = null;
        resolve(ok);
      },
    };
    confirmState.current = pending;
  });
}

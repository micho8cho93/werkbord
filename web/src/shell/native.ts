// The shell's one way to the app: the Go side Wails puts at window.go.main.App. Everything it returns has already been
// validated by the app (internal/workspace); nothing here makes a request to any workspace.

import type { App } from './types';

declare global {
  interface Window {
    go?: { main?: { App?: App } };
    runtime?: { EventsOn?(event: string, cb: (...data: unknown[]) => void): () => void };
  }
}

export function app(): App {
  const a = window.go?.main?.App;
  if (!a) throw new Error('This page is not connected to the Werkbord app.');
  return a;
}

/** Calls the app and turns whatever it rejects with into an Error with a sentence in it. */
export async function ask<T>(f: (a: App) => Promise<T>): Promise<T> {
  try {
    return await f(app());
  } catch (e) {
    throw e instanceof Error ? e : new Error(String(e));
  }
}

/** Menu and system events the app sends the shell ("shell:go" with where to go). Returns the unsubscribe. */
export function onApp(event: string, cb: (...data: unknown[]) => void): () => void {
  return window.runtime?.EventsOn?.(event, cb) ?? (() => {});
}

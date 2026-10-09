export function createNativeBridge(namespace: string, messages?: { unavailable?: string; timeout?: string }): (method: string, args?: unknown[], timeoutMs?: number) => Promise<unknown>;

/** Whether this page is inside a frame (of the Werkbord desktop app's shell). */
export function isEmbedded(w?: unknown): boolean;
/** A place inside a workspace's own page: a fragment (#/…) or a query (?tab=…), nothing that could name another address. */
export function isWorkspaceHref(href: unknown): boolean;
/** Calls handler with a place the shell that framed this page asks it to go to. Returns a function that stops listening. */
export function onNavigate(handler: (href: string) => void, w?: unknown): () => void;

export function reportFrame(place: () => string, w?: unknown): () => void;
/** Asks the shell to show the person's own Werkbord at a place inside it. False outside the desktop app. */
export function openWorkspace(target: 'personal', place: string, w?: unknown): boolean;
/** Calls handler with the theme the person chose in the shell's sidebar. Returns a function that stops listening. */
export function onTheme(handler: (theme: 'light' | 'dark') => void, w?: unknown): () => void;

// A product-neutral call transport between a web page and its native app. Native applications enforce origins and exported methods.
//
// A page reaches its app in one of two ways. In the app's own window it talks to the Wails runtime directly
// (window.webkit.messageHandlers.external), and the answer comes back to window.wails.Callback. A page the Werkbord
// desktop app shows in a frame, beside a person's other workspaces, cannot: the runtime answers only the window's own
// page. It asks the page that framed it instead, with postMessage, and that page (the app's shell) checks which frame
// asked, allows only what that kind of workspace may ask, and calls the runtime. The page's code is the same either way.

const REQUEST = 'werkbord.native.request';
const RESULT = 'werkbord.native.result';
const NAVIGATE = 'werkbord.navigate';

// A place inside a workspace's own page: a fragment or a query, nothing that could name another address.
const HREF = /^[#?][A-Za-z0-9/_.:=&%?#-]{0,299}$/;

/** Whether this page is inside a frame of the desktop shell (or any frame: only the shell's answers matter). */
export function isEmbedded(w = globalThis.window) {
  return !!w && !!w.parent && w.parent !== w;
}

/** Valid for a link the shell may send a framed page: a place inside it. */
export function isWorkspaceHref(href) {
  return typeof href === 'string' && HREF.test(href) && !href.includes('//') && !href.includes('..');
}

/**
 * Calls handler(href) when the shell that framed this page asks it to go somewhere inside itself.
 * Only the parent window is believed, and only a place inside the page (see isWorkspaceHref).
 */
export function onNavigate(handler, w = globalThis.window) {
  if (!isEmbedded(w)) return () => {};
  const listener = (event) => {
    if (event.source !== w.parent) return;
    const m = event.data;
    if (!m || m.type !== NAVIGATE || !isWorkspaceHref(m.href)) return;
    handler(m.href);
  };
  w.addEventListener('message', listener);
  return () => w.removeEventListener('message', listener);
}

export function createNativeBridge(namespace, messages = {}) {
  const pending = new Map();
  let sequence = 0;
  let installed = false;
  let relayInstalled = false;
  const prefix = 'native' + Math.random().toString(36).slice(2) + '-';

  function listen(w) {
    if (installed) return;
    installed = true;
    w.wails = w.wails || {};
    const previous = w.wails.Callback;
    w.wails.Callback = message => {
      let answer;
      try { answer = JSON.parse(message); } catch (_) { /* Another listener's message. */ }
      const entry = answer?.callbackid ? pending.get(answer.callbackid) : undefined;
      if (!entry) { previous?.(message); return; }
      settle(answer.callbackid, entry, answer);
    };
  }

  function settle(id, entry, answer) {
    pending.delete(id);
    clearTimeout(entry.timer);
    if (answer.error) entry.reject(new Error(String(answer.error)));
    else entry.resolve(answer.result);
  }

  function listenForRelay(w) {
    if (relayInstalled) return;
    relayInstalled = true;
    w.addEventListener('message', event => {
      if (event.source !== w.parent) return;
      const m = event.data;
      if (!m || m.type !== RESULT || typeof m.id !== 'string') return;
      const entry = pending.get(m.id);
      if (entry) settle(m.id, entry, m.ok ? { result: m.result } : { error: m.error || 'The app refused.' });
    });
  }

  function wait(id, timeoutMs, send) {
    return new Promise((resolve, reject) => {
      const entry = { resolve, reject };
      if (timeoutMs > 0) entry.timer = setTimeout(() => {
        pending.delete(id);
        reject(new Error(messages.timeout || 'The native app did not answer.'));
      }, timeoutMs);
      pending.set(id, entry);
      try { send(); }
      catch (error) { pending.delete(id); clearTimeout(entry.timer); reject(error); }
    });
  }

  return function call(method, args = [], timeoutMs = 5000) {
    const w = globalThis.window;
    if (isEmbedded(w)) {
      listenForRelay(w);
      const id = prefix + ++sequence;
      // The shell checks who is asking; the page it is addressed to is the one that framed this one.
      return wait(id, timeoutMs, () => w.parent.postMessage({ type: REQUEST, id, method, args }, '*'));
    }
    const handler = w?.webkit?.messageHandlers?.external;
    if (typeof handler?.postMessage !== 'function') return Promise.reject(new Error(messages.unavailable || 'The native app is not available.'));
    listen(w);
    const id = prefix + ++sequence;
    return wait(id, timeoutMs, () => handler.postMessage('C' + JSON.stringify({ name: namespace + '.' + method, args, callbackID: id })));
  };
}

/** Report only a validated local route. Tokens and enrollment secrets never leave the frame. */
export function reportFrame(place, w = globalThis.window) {
  if (!isEmbedded(w)) return () => {};
  const send = () => {
    const href = place();
    if (isWorkspaceHref(href) && !/(token|join|invite)=/i.test(href)) w.parent.postMessage({ type: 'werkbord.frame', event: 'place', place: href }, '*');
  };
  w.parent.postMessage({ type: 'werkbord.frame', event: 'ready' }, '*');
  send();
  // 'werkbord:place' is raised by a page that settles its own address (history.replaceState raises no event of its own).
  const events = ['hashchange', 'popstate', 'werkbord:place'];
  for (const name of events) w.addEventListener(name, send);
  return () => { for (const name of events) w.removeEventListener(name, send); };
}

/**
 * Asks the shell to show another workspace, at a place inside it: a Team ticket's "Open in Individual" opens the person's
 * own task. Only 'personal' is a target, and only a place (see isWorkspaceHref); the shell checks both again.
 */
export function openWorkspace(target, place, w = globalThis.window) {
  if (!isEmbedded(w) || target !== 'personal' || !isWorkspaceHref(place)) return false;
  w.parent.postMessage({ type: 'werkbord.frame', event: 'open', target, place }, '*');
  return true;
}

/**
 * Calls handler('light' | 'dark') when the shell that framed this page says which theme the person chose (the window's one
 * sidebar is where it is chosen). Only the parent window is believed, and only those two words.
 */
export function onTheme(handler, w = globalThis.window) {
  if (!isEmbedded(w)) return () => {};
  const listener = (event) => {
    if (event.source !== w.parent) return;
    const m = event.data;
    if (!m || m.type !== 'werkbord.theme' || (m.theme !== 'light' && m.theme !== 'dark')) return;
    handler(m.theme);
  };
  w.addEventListener('message', listener);
  return () => w.removeEventListener('message', listener);
}

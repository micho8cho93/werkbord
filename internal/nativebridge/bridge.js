// A product-neutral WebKit/Wails call transport. Native applications enforce origins and exported methods.
export function createNativeBridge(namespace, messages = {}) {
  const pending = new Map();
  let sequence = 0;
  let installed = false;
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
      pending.delete(answer.callbackid);
      clearTimeout(entry.timer);
      if (answer.error) entry.reject(new Error(String(answer.error)));
      else entry.resolve(answer.result);
    };
  }
  return function call(method, args = [], timeoutMs = 5000) {
    const w = globalThis.window;
    const handler = w?.webkit?.messageHandlers?.external;
    if (typeof handler?.postMessage !== 'function') return Promise.reject(new Error(messages.unavailable || 'The native app is not available.'));
    listen(w);
    return new Promise((resolve, reject) => {
      const id = prefix + ++sequence;
      const entry = { resolve, reject };
      if (timeoutMs > 0) entry.timer = setTimeout(() => {
        pending.delete(id);
        reject(new Error(messages.timeout || 'The native app did not answer.'));
      }, timeoutMs);
      pending.set(id, entry);
      try { handler.postMessage('C' + JSON.stringify({ name: namespace + '.' + method, args, callbackID: id })); }
      catch (error) { pending.delete(id); clearTimeout(entry.timer); reject(error); }
    });
  };
}

<script lang="ts">
  import { onMount } from 'svelte';
  import { Terminal } from '@xterm/xterm';
  import { FitAddon } from '@xterm/addon-fit';
  import '@xterm/xterm/css/xterm.css';
  import { terminalApi, type TerminalSession } from './terminal';
  import { agentName } from '../format';
  import { app } from '../state.svelte';
  let { session }: { session: TerminalSession } = $props();
  let host: HTMLDivElement;
  let connection = $state('Connecting');
  let error = $state('');
  let reconnect = $state(0);
  let processState = $state<TerminalSession['state']>('running');
  let busy = $state(false);
  async function stop() {
    busy = true;
    try { await terminalApi.stop(session); processState = 'stopped'; session.state = 'stopped'; }
    catch (e) { error = e instanceof Error ? e.message : String(e); }
    finally { busy = false; }
  }
  onMount(() => {
    processState = session.state;
    const terminal = new Terminal({ cursorBlink: true, scrollback: 5000, fontFamily: 'Geist Mono Variable, monospace', fontSize: 13, theme: { background: '#0B0B0C', foreground: '#F0F0EC', cursor: '#7C93FF', selectionBackground: '#2447FF80' }, allowProposedApi: false });
    const fit = new FitAddon(); terminal.loadAddon(fit); terminal.open(host);
    // No OSC clipboard or link handlers: escape sequences cannot write the clipboard or open URLs.
    let disposed = false;
    let abort: AbortController | undefined;
    let after = 0;
    let sending = Promise.resolve();
    let queued = 0;
    const input = terminal.onData(data => {
      if (processState !== 'running' || connection !== 'Connected') return;
      if (new TextEncoder().encode(data).length > 16384 || queued >= 8) { error = 'Input is too large or still being delivered. Paste a smaller block.'; return; }
      queued++;
      sending = sending.then(async () => { await terminalApi.input(session, data); }).catch(e => { error = String(e); }).finally(() => { queued--; });
    });
    let resizeTimer: ReturnType<typeof setTimeout>;
    const resize = () => {
      clearTimeout(resizeTimer);
      resizeTimer = setTimeout(() => {
        if (disposed || !host.clientWidth || !host.clientHeight) return;
        fit.fit();
        if (processState === 'running') void terminalApi.resize(session, Math.max(2, Math.min(500, terminal.cols)), Math.max(2, Math.min(300, terminal.rows))).catch(e => { error = String(e); });
      }, 100);
    };
    const observer = new ResizeObserver(resize); observer.observe(host); resize();
    const endEffect = $effect.root(() => {
      $effect(() => {
        const attempt = reconnect; void attempt;
        abort?.abort(); const controller = new AbortController(); abort = controller;
        connection = 'Connecting'; error = '';
        void (async () => {
          try {
            while (!disposed && !controller.signal.aborted) {
              const out = await terminalApi.output(session, after, controller.signal);
              if (disposed || controller.signal.aborted) return;
              processState = out.session.state; session.state = processState;
              // Terminal device-status queries can emit onData synchronously while output is rendered.
              connection = processState === 'running' ? 'Connected' : processState === 'interrupted' ? 'Interrupted after device restart' : 'Process ended';
              if (out.reset) { terminal.reset(); terminal.writeln('[Earlier output is no longer buffered]'); }
              if (out.data) { const bytes = Uint8Array.from(atob(out.data), c => c.charCodeAt(0)); await new Promise<void>(resolve => terminal.write(bytes, resolve)); }
              after = out.next;
              if (processState !== 'running' && (!out.data || atob(out.data).length < 65536)) break;
            }
          } catch (e) { if (!controller.signal.aborted && !disposed) { connection = 'Disconnected'; error = e instanceof Error ? e.message : String(e); } }
        })();
      });
    });
    terminal.focus();
    return () => { disposed = true; endEffect(); abort?.abort(); observer.disconnect(); clearTimeout(resizeTimer); input.dispose(); terminal.dispose(); };
  });
</script>
<div class="terminal-session">
  <div class="toolbar">
    <span class="identity">{agentName(app.agents, session.agentId)} · {session.repository}</span>
    <span role="status">{connection}</span>
    {#if connection === 'Disconnected'}<button class="btn small" onclick={() => reconnect++}>Reconnect</button>{/if}
    <button class="btn small" disabled={busy || processState !== 'running'} onclick={stop}>Terminate</button>
  </div>
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  <div class="terminal" bind:this={host}></div>
</div>
<style>
  .terminal-session{display:flex;flex-direction:column;flex:1;min-height:0;min-width:0;background:#0B0B0C}
  .toolbar{display:flex;align-items:center;gap:12px;padding:8px 12px;background:var(--surface);border-bottom:1px solid var(--border);font-size:12px;flex-wrap:wrap}
  .identity{flex:1;min-width:120px;color:var(--text)}
  [role='status']{color:var(--text-2)}
  .terminal{flex:1;min-height:120px;min-width:0;padding:8px;overflow:hidden}
  .error{margin:0;padding:8px 12px;background:var(--surface);color:var(--danger-text);font-size:13px}
  .terminal :global(.xterm){height:100%}
</style>

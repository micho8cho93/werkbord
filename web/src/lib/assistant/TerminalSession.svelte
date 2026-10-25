<script lang="ts">
  import { onMount } from 'svelte';
  import { Terminal } from '@xterm/xterm';
  import { FitAddon } from '@xterm/addon-fit';
  import '@xterm/xterm/css/xterm.css';
  import { terminalApi, terminalInputClient, type TerminalSession } from './terminal';
  import { agentName } from '../format';
  import { api } from '../api';
  import { isPhone } from '../phones/capability';
  import type { PhoneEntry } from '../types';
  import { app } from '../state.svelte';
  let { session }: { session: TerminalSession } = $props();
  let host: HTMLDivElement;
  let connection = $state('Connecting');
  let error = $state('');
  let reconnect = $state(0);
  let processState = $state<TerminalSession['state']>('running');
  let busy = $state(false);
  let inputUncertain = $state(false);
  let phones = $state<PhoneEntry[]>([]);
  let sharing = $state(false);
  let ctrl = $state(false);
  let textSize = $state(13);
  let sendKey: (data: string) => void = () => {};
  let resumeInput = $state(() => {});
  let changeSize: () => void = () => {};
  async function share() {
    sharing = !sharing;
    if (sharing) try { phones = await api.phones(); } catch(e) { error=String(e); }
  }
  async function grant(phoneId: string, checkbox: HTMLInputElement) {
    const enabled = checkbox.checked;
    try { await terminalApi.grant(session,phoneId,enabled); session.phones = [...(session.phones ?? []).filter(p=>p!==phoneId), ...(enabled ? [phoneId] : [])]; }
    catch(e) { error=String(e); }
    finally { checkbox.checked = session.phones?.includes(phoneId) ?? false; }
  }
  async function stop() {
    busy = true;
    try { await terminalApi.stop(session); processState = 'stopped'; session.state = 'stopped'; connection = 'Process ended'; }
    catch (e) { error = e instanceof Error ? e.message : String(e); }
    finally { busy = false; }
  }
  onMount(() => {
    // Metadata refreshes must not interrupt a connection to the same process.
    const target = session;
    processState = session.state;
    const terminal = new Terminal({ cursorBlink: true, scrollback: 5000, fontFamily: 'Geist Mono Variable, monospace', fontSize: 13, theme: { background: '#0B0B0C', foreground: '#F0F0EC', cursor: '#7C93FF', selectionBackground: '#2447FF80' }, allowProposedApi: false });
    const fit = new FitAddon(); terminal.loadAddon(fit); terminal.open(host);
    // No OSC clipboard or link handlers: escape sequences cannot write the clipboard or open URLs.
    let disposed = false;
    let abort: AbortController | undefined;
    let after = 0;
    const client = terminalInputClient(target);
    inputUncertain = client.uncertain;
    const bytes = (data: string) => new TextEncoder().encode(data).length;
    const flush = async () => {
      client.sending = true;
      try {
        while (client.pending.length && !client.uncertain) {
          const data = client.pending.shift()!;
          await terminalApi.input(target, data, client.id, ++client.sequence, after);
        }
      } catch {
        client.uncertain = true; inputUncertain = true; client.pending = [];
      } finally { client.sending = false; }
    };
    const send = (data: string) => {
      if (client.uncertain) { inputUncertain = true; return; }
      if (processState !== 'running' || connection !== 'Connected') return;
      if (ctrl && data.length === 1) { data = String.fromCharCode(data.toUpperCase().charCodeAt(0) & 31); ctrl = false; }
      if (bytes(data) > 16384 || client.pending.length >= 8) { error = 'Input is too large or still being delivered. Paste a smaller block.'; return; }
      // Coalesce keystrokes behind the in-flight request: a mobile round trip must not drop fast typing.
      const last = client.pending.length - 1;
      if (last >= 0 && bytes(client.pending[last]) + bytes(data) <= 16384) client.pending[last] += data;
      else client.pending.push(data);
      if (!client.sending) void flush();
    };
    sendKey = data => { if (data) send(data); terminal.focus(); };
    // Historical terminal queries were already answered by an earlier viewer. Consume them on replay.
    let replaying = false;
    const replies = [
      terminal.parser.registerCsiHandler({ final: 'n' }, () => replaying),
      terminal.parser.registerCsiHandler({ prefix: '?', final: 'n' }, () => replaying),
      terminal.parser.registerCsiHandler({ final: 'c' }, () => replaying),
      terminal.parser.registerCsiHandler({ prefix: '>', final: 'c' }, () => replaying),
      terminal.parser.registerCsiHandler({ prefix: '=', final: 'c' }, () => replaying),
      terminal.parser.registerCsiHandler({ intermediates: '$', final: 'p' }, () => replaying),
      terminal.parser.registerCsiHandler({ prefix: '?', intermediates: '$', final: 'p' }, () => replaying),
      terminal.parser.registerCsiHandler({ final: 't' }, params => replaying && [14,16,18].includes(params[0] as number)),
      terminal.parser.registerDcsHandler({ intermediates: '$', final: 'q' }, () => replaying),
      ...[4,10,11,12].map(id => terminal.parser.registerOscHandler(id, data => replaying && data.includes('?'))),
    ];
    const input = terminal.onData(send);
    let resizeTimer: ReturnType<typeof setTimeout>;
    const resize = () => {
      clearTimeout(resizeTimer);
      resizeTimer = setTimeout(() => {
        if (disposed || !host.clientWidth || !host.clientHeight) return;
        terminal.options.fontSize = textSize; fit.fit();
        if (processState === 'running') void terminalApi.resize(target, Math.max(2, Math.min(500, terminal.cols)), Math.max(2, Math.min(300, terminal.rows))).catch(e => { error = String(e); });
      }, 100);
    };
    changeSize = resize;
    const observer = new ResizeObserver(resize); observer.observe(host); resize();
    const wake = () => { if (!document.hidden && navigator.onLine) { reconnect++; resize(); } };
    const offline = () => { abort?.abort(); connection = 'Disconnected'; error = 'Network unavailable. Output reconnects when this device is online.'; };
    const viewport = window.visualViewport;
    viewport?.addEventListener('resize',resize);
    window.addEventListener('online',wake);
    window.addEventListener('offline',offline);
    document.addEventListener('visibilitychange',wake);
    const retry = setInterval(() => { if (!document.hidden && navigator.onLine && connection === 'Disconnected') reconnect++; },3000);
    const endEffect = $effect.root(() => {
      $effect(() => {
        const attempt = reconnect; void attempt;
        if (!navigator.onLine) { offline(); return; }
        abort?.abort(); const controller = new AbortController(); abort = controller;
        connection = 'Connecting'; if (!client.uncertain) error = '';
        void (async () => {
          try {
            let probe = true;
            while (!disposed && !controller.signal.aborted) {
              const out = await terminalApi.output(target, after, controller.signal, probe); probe = false;
              if (disposed || controller.signal.aborted) return;
              processState = out.session.state; session.state = processState;
              // Terminal device-status queries can emit onData synchronously while output is rendered.
              connection = processState === 'running' ? 'Connected' : processState === 'interrupted' ? 'Interrupted after device restart' : 'Process ended';
              if (out.reset) { terminal.reset(); terminal.writeln('[Earlier output is no longer buffered]'); }
              after = out.next; replaying = out.replay === true;
              if (out.data) { const bytes = Uint8Array.from(atob(out.data), c => c.charCodeAt(0)); await new Promise<void>(resolve => terminal.write(bytes, resolve)); }
              if (processState !== 'running' && (!out.data || atob(out.data).length < 65536)) break;
            }
          } catch (e) { if (!controller.signal.aborted && !disposed) { connection = 'Disconnected'; if (!client.uncertain) error = e instanceof Error ? e.message : String(e); } }
        })();
      });
    });
    // Recovering a stream never retries a mutation or resets its byte offset.
    const allowInput = () => { client.id = crypto.randomUUID(); client.sequence = 0; client.uncertain = false; inputUncertain = false; error = ''; };
    resumeInput = allowInput;
    if (!isPhone()) terminal.focus();
    return () => { disposed = true; endEffect(); abort?.abort(); observer.disconnect(); clearInterval(retry); viewport?.removeEventListener('resize',resize); window.removeEventListener('online',wake); window.removeEventListener('offline',offline); document.removeEventListener('visibilitychange',wake); clearTimeout(resizeTimer); input.dispose(); replies.forEach(reply => reply.dispose()); terminal.dispose(); };
  });
</script>
<div class="terminal-session">
  <div class="toolbar">
    <span class="identity">{agentName(app.agents, session.agentId)} · {session.repository}</span>
    <span role="status">{connection}</span>
    {#if connection === 'Disconnected'}<button class="btn small" onclick={() => reconnect++}>Reconnect</button>{/if}
    {#if !isPhone()}<button class="btn small" onclick={share}>Phone access</button>{/if}
    <button class="btn small" disabled={busy || processState !== 'running'} onclick={stop}>Terminate</button>
  </div>
  {#if sharing}<div class="sharing">Allow terminal control for this session. CLI permissions apply.
    {#each phones as phone (phone.id)}<label><input type="checkbox" checked={session.phones?.includes(phone.id) ?? false} onchange={e=>grant(phone.id,e.currentTarget)} />{phone.name}</label>{/each}
    {#if !phones.length}<p>Enrol a phone in Settings → Phones first.</p>{/if}
  </div>{/if}
  {#if inputUncertain}<div class="error" role="alert">Input delivery is uncertain. Check the output before enabling new input. Nothing will be resent.
    <button class="btn small" onclick={resumeInput}>I checked the output · Enable new input</button>
  </div>{/if}
  {#if error}<div class="error" role="alert">{error}</div>{/if}
  <div class="terminal" bind:this={host}></div>
  <div class="terminal-keys" aria-label="Terminal controls">
    <button class="btn small" onclick={()=>sendKey('')}>Keyboard</button>
    {#each [['Escape','\x1b'],['Tab','\t'],['Arrow up','\x1b[A'],['Arrow down','\x1b[B'],['Arrow left','\x1b[D'],['Arrow right','\x1b[C']] as [label,data] (label)}
      <button class="btn small" aria-label={label} onpointerdown={e=>e.preventDefault()} onclick={()=>sendKey(data)}>{label.replace('Arrow ','')}</button>
    {/each}
    <button class="btn small" aria-pressed={ctrl} onpointerdown={e=>e.preventDefault()} onclick={()=>{ctrl=!ctrl;sendKey('');}}>Ctrl</button>
    <label>Text <select aria-label="Terminal text size" bind:value={textSize} onchange={()=>changeSize()}>{#each [12,13,14,16,18] as size (size)}<option value={size}>{size}</option>{/each}</select></label>
  </div>
</div>
<style>
  .sharing{padding:8px 12px;background:var(--surface);font-size:13px}.sharing label{display:flex;align-items:center;gap:8px;min-height:44px}
 .terminal-keys{display:flex;flex-wrap:wrap;gap:4px;padding:4px 8px;background:var(--surface);border-top:1px solid var(--border)}.terminal-keys button{min-height:44px;min-width:44px}.terminal-keys label{display:flex;align-items:center;gap:4px;font-size:12px}.terminal-keys select{min-height:44px;font-size:16px}
 @media(max-width:899px){.toolbar button{min-height:44px}.toolbar{gap:6px;padding:4px 8px}.terminal{padding:4px;min-height:60px}.identity{overflow-wrap:anywhere}}
 .terminal-session{display:flex;flex-direction:column;flex:1;min-height:0;min-width:0;background:#0B0B0C}
  .toolbar{display:flex;align-items:center;gap:12px;padding:8px 12px;background:var(--surface);border-bottom:1px solid var(--border);font-size:12px;flex-wrap:wrap}
  .identity{flex:1;min-width:120px;color:var(--text)}
  [role='status']{color:var(--text-2)}
  .terminal{flex:1;min-height:120px;min-width:0;padding:8px;overflow:hidden}
  .error{margin:0;padding:8px 12px;background:var(--surface);color:var(--danger-text);font-size:13px}
  .terminal :global(.xterm){height:100%}
</style>

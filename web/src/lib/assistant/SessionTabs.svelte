<script lang="ts">
  import { tick } from 'svelte';
  import Icon from '../Icon.svelte';
  import { embedded, openSettingsInShell } from '../embed';
  import { modeLabel, selectionApi, type CatalogAgent } from '../selection';
  import { connectedAgents, nativeCLILaunchers } from './connected-agents';
  import { assistant, type SallyTab } from './sessions.svelte';
  // The Phase 1 controller advertises no supported native interactive CLI transport.
  // Keep the catalogue in Settings; never render a launch action without a launcher.
  let menu = $state(false);
  let connected = $state<CatalogAgent[]>([]);
  let refresh = 0;
  async function toggleMenu() {
    menu = !menu;
    const version = ++refresh;
    connected = [];
    if (!menu) return;
    try {
      const [catalog, settings] = await Promise.all([selectionApi.catalog(), selectionApi.settings()]);
      if (version === refresh && menu) connected = connectedAgents(catalog, settings.agents).filter(a => nativeCLILaunchers.has(a.id));
    } catch { /* Fail closed. Settings remains reachable when detection is unavailable. */ }
  }
  let editing = $state('');
  let name = $state('');
  let container: HTMLDivElement;
  let plus: HTMLButtonElement;
  function revealActive() { document.getElementById(`sally-tab-${assistant.activeId}`)?.scrollIntoView({block:'nearest',inline:'nearest'}); }
  $effect(() => { const id = assistant.activeId; void tick().then(() => { if (assistant.activeId === id) revealActive(); }); });
  function rename(tab: SallyTab) { editing = tab.id; name = assistant.title(tab); }
  function finish() { assistant.rename(editing, name); editing = ''; }
  function status(tab: SallyTab) { return tab.store.waiting ? 'Waiting for confirmation' : tab.store.running || tab.store.session?.running ? 'Running' : 'Idle'; }
  function keydown(event: KeyboardEvent) {
    if (event.key === 'Escape' && (menu || editing)) {
      event.preventDefault(); event.stopPropagation(); menu = false; editing = ''; plus?.focus();
    }
  }
  function move(event: KeyboardEvent, id: string) {
    const tabs = assistant.openTabs;
    const i = tabs.findIndex(t => t.id === id);
    const next = event.key === 'ArrowRight' ? tabs[(i + 1) % tabs.length] : event.key === 'ArrowLeft' ? tabs[(i + tabs.length - 1) % tabs.length] : event.key === 'Home' ? tabs[0] : event.key === 'End' ? tabs.at(-1) : undefined;
    if (next) { event.preventDefault(); assistant.select(next.id); document.getElementById(`sally-tab-${next.id}`)?.focus(); }
  }
  function manage(event: MouseEvent) {
    menu = false; assistant.hide();
    if (embedded()) { event.preventDefault(); openSettingsInShell('#/settings?agents'); }
  }
</script>
<svelte:window onresize={revealActive} onkeydowncapture={keydown} onpointerdown={e=>{if(container&&!container.contains(e.target as Node))menu=false;}} />
<div class="tabs-bar" bind:this={container}>
  <div class="tabs" role="tablist" aria-label="Sally conversations">
    {#each assistant.openTabs as tab (tab.id)}
      <div class="tab" class:active={tab.id === assistant.activeId}>
        <button id="sally-tab-{tab.id}" role="tab" aria-selected={tab.id===assistant.activeId} tabindex={tab.id===assistant.activeId?0:-1} aria-controls="sally-panel" title="{assistant.title(tab)} · {tab.store.provider?.name ?? 'Sally'} · {modeLabel(tab.store.choice?.mode ?? 'lowest_cost')} · {status(tab)}" onclick={()=>assistant.select(tab.id)} onkeydown={e=>move(e,tab.id)} ondblclick={()=>rename(tab)}>
          <Icon name="assistant" size={14} />
          <span class="identity"><span class="title">{assistant.title(tab)}</span><span class="detail">{tab.store.provider?.name ?? 'Sally'} · {modeLabel(tab.store.choice?.mode ?? 'lowest_cost')}</span></span>
          <span class="status" class:running={tab.store.running || tab.store.session?.running} class:waiting={tab.store.waiting>0} aria-label={status(tab)}></span>
        </button>
        <button class="dismiss" aria-label="Close {assistant.title(tab)} tab" title="Close tab; conversation keeps running" onclick={()=>assistant.closeTab(tab.id)}><Icon name="close" size={12} /></button>
      </div>
    {/each}
  </div>
  <div class="tab-menu">
    <button class="new" bind:this={plus} aria-label="New tab" aria-expanded={menu} aria-haspopup="dialog" onclick={toggleMenu} data-testid="assistant-new"><Icon name="plus" size={16}/></button>
    {#if menu}
      <div class="menu" role="dialog" aria-label="New tab">
        <span class="group">Sally</span>
        <button onclick={()=>{assistant.newTab();menu=false;}}>New Sally Conversation</button>
        {#if connected.length}
          <span class="group">Coding Agents</span>
          {#each connected as agent (agent.id)}
            <button onclick={()=>{nativeCLILaunchers.get(agent.id)?.(agent.interactive!.device);menu=false;}}>{agent.name}</button>
          {/each}
        {/if}
        <a href="#/settings?agents" onclick={manage}>Manage Coding Agents →</a>
        <div class="separator"></div>
        <button onclick={()=>{rename(assistant.openTabs.find(t=>t.id===assistant.activeId)!);menu=false;}}>Rename current tab</button>
        {#if assistant.closedTabs.length}
          <span class="group">Reopen closed tabs</span>
          {#each [...assistant.closedTabs].reverse() as tab (tab.id)}
            <button title={assistant.title(tab)} onclick={()=>{assistant.reopen(tab.id);menu=false;}}>{assistant.title(tab)} · {status(tab)}</button>
          {/each}
        {/if}
      </div>
    {/if}
  </div>
</div>
{#if editing}
  <form class="rename" onsubmit={e=>{e.preventDefault();finish();}}>
    <label>Conversation title <input class="input" maxlength="100" bind:value={name} /></label>
    <button class="btn small" type="submit">Save</button><button class="btn quiet small" type="button" onclick={()=>editing=''}>Cancel</button>
  </form>
{/if}
<style>
  .tabs-bar{display:flex;align-items:stretch;gap:4px;padding:0 12px;border-bottom:1px solid var(--border);flex:none;min-width:0}
  .tabs{display:flex;overflow-x:auto;min-width:0;flex:1;scrollbar-width:thin}
  .tab{display:flex;flex:0 1 190px;min-width:132px;border-bottom:2px solid transparent;align-items:center}
  .tab.active{border-bottom-color:var(--accent);background:var(--surface)}
  button{border:0;background:transparent;color:var(--text-2);cursor:pointer;font:inherit}
  button:hover,a:hover{background:var(--surface-2);color:var(--text)}
  button:focus-visible,a:focus-visible{outline:2px solid var(--accent);outline-offset:-2px}
  .tab>[role='tab']{display:flex;align-items:center;gap:7px;min-width:0;flex:1;text-align:left;padding:8px}
  .tab>[role='tab'] :global(svg){flex:none}
  .identity{display:grid;min-width:0;gap:2px;flex:1}.title{font-size:12px;color:var(--text)}.detail{font-size:11px}
  .title,.detail{white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
  .dismiss{padding:6px;display:flex;align-items:center;align-self:stretch}
  .status{width:5px;height:5px;border-radius:50%;background:var(--text-2);flex:none}.status.running{background:var(--accent)}.status.waiting{background:var(--warn)}
  .tab-menu{position:relative;display:flex}.new{min-width:36px;display:grid;place-items:center}
  .menu{position:absolute;right:0;top:100%;z-index:5;width:260px;max-height:60vh;overflow:auto;background:var(--surface);border:1px solid var(--border);border-radius:var(--radius-sm);padding:6px}
  .menu button,.menu a{display:block;padding:9px 10px;text-align:left;width:100%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:13px}
  .menu a{font-size:12px;color:var(--text-2);box-sizing:border-box}.group{display:block;font-size:11px;color:var(--text-2);padding:8px 10px 4px}.separator{border-top:1px solid var(--border);margin:5px 0}
  .rename{display:flex;align-items:center;gap:8px;padding:8px 16px;flex:none}.rename label{display:flex;align-items:center;gap:8px;font-size:12px;min-width:0}.rename input{min-width:0}
  @media(max-width:640px){.tab{min-width:150px}.new,.dismiss{min-width:44px}.menu button,.menu a{min-height:44px}.rename{flex-wrap:wrap}}
</style>

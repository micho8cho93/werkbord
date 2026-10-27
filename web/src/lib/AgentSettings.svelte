<script lang="ts">
 import { onMount } from 'svelte';
 import { selectionApi, type SelectionSettings, type CatalogAgent, type AgentPreference, type ModelEconomics } from './selection';
 import type { AgentOptions } from './types';
 let catalogue=$state<CatalogAgent[]>([]);
 let settings=$state<SelectionSettings>({allowMetered:false,agents:{}});
 let original=$state('');
 let loading=$state(true);
 let saving=$state(false);
 let error=$state('');
 let saved=$state(false);
 let query=$state('');
 let options=$state<Record<string,AgentOptions>>({});
 let probing=$state('');
 const filtered=$derived(catalogue.filter(a=>a.name.toLowerCase().includes(query.toLowerCase())));
 const dirty=$derived(JSON.stringify(settings)!==original);
 const enabled=$derived(Object.values(settings.agents).filter(a=>a.enabled).length);
 function pref(id:string):AgentPreference{return settings.agents[id] ?? {enabled:false,billing:'auto'};}
 function change(id:string,patch:Partial<AgentPreference>){settings.agents[id]={...pref(id),...patch};saved=false;}
 function economics(id:string,model:string,field:keyof ModelEconomics,value:string){
  const p=pref(id);const modelValue={...p.models?.[model]};
  if(value==='')delete modelValue[field];else modelValue[field]=Number(value);
  change(id,{models:{...p.models,[model]:modelValue}});
 }
 async function load(){loading=true;error='';try{[catalogue,settings]=await Promise.all([selectionApi.catalog(),selectionApi.settings()]);original=JSON.stringify(settings);}catch(e){error=e instanceof Error?e.message:String(e);}finally{loading=false;}}
 async function inspect(id:string){if(options[id]||probing)return;probing=id;try{options[id]=await selectionApi.options(id);}catch(e){error=e instanceof Error?e.message:String(e);}finally{probing='';}}
 async function save(){if(!original)return;saving=true;error='';try{settings=await selectionApi.save(settings);original=JSON.stringify(settings);saved=true;}catch(e){error=e instanceof Error?e.message:String(e);}finally{saving=false;}}
 onMount(()=>{void load();});
</script>
<section class="pn agents-settings">
 <div class="head"><h3>Agents</h3><p class="muted">Use your installed CLIs and their existing sign-ins. Enable the agents automatic selection may use. Each conversation or run keeps its initial selection.</p></div>
 <label class="metered"><input type="checkbox" bind:checked={settings.allowMetered} disabled={loading||saving} /> Allow paid API usage when subscription usage is unavailable</label>
 <p class="hint">Lowest cost prefers included subscription usage. Paid API usage needs this permission and a confirmed billing type. Charges and remaining allowance may not be reported.</p>
 <div class="actions"><button class="btn primary" disabled={!original||loading||saving||!dirty} onclick={save}>{saving?'Saving…':'Save agent settings'}</button><button class="btn quiet" disabled={loading||saving||dirty} onclick={load}>Check installations again</button>{#if saved&&!dirty}<span class="ok" role="status">Saved</span>{/if}</div>
 {#if error}<p class="error" role="alert">{error}</p>{/if}
 <label class="search">Find an agent <input class="input" type="search" bind:value={query} placeholder="Search CLI names" /></label>
 <p class="hint">{catalogue.length} integrations · {enabled} enabled for automatic selection</p>
 {#if loading}<p role="status">Checking installed agents…</p>{/if}
 <ul class="catalogue">
 {#each filtered as a(a.id)}
  <li>
   <div class="agent-heading"><label><input type="checkbox" checked={pref(a.id).enabled} disabled={saving} onchange={e=>change(a.id,{enabled:e.currentTarget.checked})} aria-label="Use {a.name} in automatic selection" /><strong>{a.name}</strong></label><span class:ok={a.available}>{a.available?'Installed':a.installed?'Needs attention':'Not installed'}</span></div>
   <p class="hint">Coding runs · {a.assistant?'Salie chat':'Salie unavailable: tool isolation not verified'}{a.adapter?' · Uses a separate adapter':''}{a.version?` · ${a.version}`:''}</p>
   {#if !a.available}<p class="hint">{a.detail || a.guidance || a.setup}</p>{/if}
   <details ontoggle={e=>{if(e.currentTarget.open&&a.installed)void inspect(a.id);}}>
    <summary>Setup, billing and model controls</summary>
    <div class="agent-options">
     {#if a.setup}<p class="hint">{a.setup}</p>{/if}
     {#if a.docsUrl}<a href={a.docsUrl} target="_blank" rel="noopener noreferrer">Installation and CLI documentation</a>{/if}
     <label>Billing <select class="select" value={pref(a.id).billing} onchange={e=>change(a.id,{billing:e.currentTarget.value as AgentPreference['billing']})}><option value="auto">Detect automatically</option><option value="subscription">Included in my subscription</option><option value="metered">Metered API usage</option><option value="excluded">Exclude from automatic selection</option></select></label>
     <label><input type="checkbox" checked={pref(a.id).directCLI===true} onchange={e=>change(a.id,{directCLI:e.currentTarget.checked})} /> Allow Werkbord to start this CLI on this device</label>
     <label><input type="checkbox" checked={pref(a.id).cliSignedIn===true} onchange={e=>change(a.id,{cliSignedIn:e.currentTarget.checked})} /> I have signed in to this CLI on this device</label>
     <p class="hint">Authentication: {a.signIn==='signed_in'?'signed in (reported by CLI)':a.signIn==='signed_out'?'signed out (reported by CLI)':pref(a.id).cliSignedIn?'confirmed by you; not independently verified':'unknown; use the CLI’s login guidance'}. Native interactive tabs are not available yet. These choices do not enable automatic selection or grant execution permission.</p>
     <p class="hint">Confirm subscription billing only when this CLI uses your included allowance. Werkbord cannot verify remaining allowance for every provider.</p>
     {#if probing===a.id}<p role="status">Asking the CLI for its model controls…</p>{/if}
     {#if options[a.id]}
      {@const o=options[a.id]}
      <p class="hint">Models: {o.modelsSource==='agent'?'reported by CLI':o.modelsSource==='configured'?'configured':'built-in aliases'}. Reasoning: {o.reasoning.filter(x=>x.id!=='default').map(x=>x.name).join(', ')||'managed by the agent; no controls reported'}.</p>
      {#if o.note}<p class="hint">{o.note}</p>{/if}
      <p class="hint">Optional model estimates: USD per million tokens. Unknown API prices are excluded from automatic selection. Capability tiers are routing estimates; override them when needed.</p>
      {#each o.models.filter(x=>x.id!=='default') as m(m.id)}
       <div class="model-row"><strong>{m.name}</strong>
        <label>Input $/M <input type="number" class="input" min="0" max="1000000" step="any" value={pref(a.id).models?.[m.id]?.input??''} oninput={e=>economics(a.id,m.id,'input',e.currentTarget.value)} /></label>
        <label>Output $/M <input type="number" class="input" min="0" max="1000000" step="any" value={pref(a.id).models?.[m.id]?.output??''} oninput={e=>economics(a.id,m.id,'output',e.currentTarget.value)} /></label>
        <label>Capability <select class="select" value={pref(a.id).models?.[m.id]?.intelligence??0} onchange={e=>economics(a.id,m.id,'intelligence',e.currentTarget.value)}><option value="0">Estimate from model family</option><option value="1">Lightweight</option><option value="2">General purpose</option><option value="3">Strongest</option></select></label>
       </div>
      {/each}
     {:else if !a.installed}<p class="hint">Install the CLI to discover its model and reasoning controls.</p>{/if}
    </div>
   </details>
  </li>
 {/each}
 </ul>
 {#if !loading&&filtered.length===0}<p>No matching agents.</p>{/if}
</section>
<style>
 .agents-settings{display:grid;gap:12px;min-width:0}.catalogue{list-style:none;padding:0;margin:0;display:grid}.catalogue li{padding:14px 0;border-top:1px solid var(--border);display:grid;gap:6px}.agent-heading,.actions{display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap}.actions{justify-content:flex-start}.agent-heading label,.metered{display:flex;align-items:center;gap:8px;min-height:36px}.agent-heading span,.hint{font-size:12px;color:var(--text-2)}.agent-heading span.ok,.ok{color:var(--ok-text)}.agent-options{display:grid;gap:10px;padding:12px 0}.agent-options label,.search{display:grid;gap:4px;min-width:0}summary{cursor:pointer;min-height:36px;align-content:center;font-size:13px}.model-row{display:grid;grid-template-columns:minmax(100px,1fr) 90px 90px minmax(140px,1fr);gap:8px;align-items:end;padding:8px 0}.model-row strong{font-size:13px;overflow-wrap:anywhere}.model-row label{font-size:12px}@media(max-width:699px){.model-row{grid-template-columns:1fr 1fr}.model-row strong{grid-column:1/-1}.agent-heading label,.metered,summary{min-height:44px}}
</style>

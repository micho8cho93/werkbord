<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from './api';
  import { app } from './state.svelte';
  import type { RoutingRule } from './types';
  let rules = $state<RoutingRule[]>([]);
  let saving = $state(false);
  let error = $state('');
  let saved = $state(false);
  onMount(() => { api.routingRules().then(r=>rules=r, e=>error=String(e)); });
  async function save() {
    saving=true; error=''; saved=false;
    try { await api.saveRoutingRules(rules); saved=true; }
    catch(e) { error=e instanceof Error?e.message:String(e); }
    finally { saving=false; }
  }
</script>
<section class="pn rules">
  <div>
    <h3>Routing rules <span class="chip">{rules.length}</span></h3>
    <p class="muted">Rules match title or description in order, without a model choosing machines. Explicit task and run model choices take precedence. Resource requirements still apply.</p>
    {#if !rules.length}<p class="muted">No rules. Execution uses your defaults and available runner capacity.</p>{/if}
    {#each rules as rule, i (i)}
      <div class="rule">
        <label>Rule name<input class="input" bind:value={rule.name} maxlength="120" /></label>
        <label>Contains text<input class="input" bind:value={rule.contains} placeholder="documentation, review, security…" /></label>
        <label class="check"><input type="checkbox" bind:checked={rule.retry} />Only after an earlier attempt</label>
        <label>Agent<select class="select" bind:value={rule.agent} onchange={e=>{if(!e.currentTarget.value){rule.model="";rule.reasoning=""}}}><option value="">Keep resolved agent</option>{#each app.agents as a (a.id)}<option value={a.id}>{a.name}</option>{/each}</select></label>
        <label>Model<input class="input" disabled={!rule.agent} bind:value={rule.model} placeholder="Agent default unless set" /></label>
        <label>Reasoning<input class="input" disabled={!rule.agent} bind:value={rule.reasoning} placeholder="Keep defaults unless set" /></label>
        <label>Minimum logical CPUs<input class="input" type="number" min="0" max="1024" bind:value={rule.minCpu} /></label>
        <label>Minimum available RAM (GB)<input class="input" type="number" min="0" step="0.5" value={(rule.minRamBytes ?? 0)/1024**3} oninput={e=>rule.minRamBytes=Math.round(Number(e.currentTarget.value)*1024**3)} /></label>
        <button class="btn small" onclick={()=>rules=rules.filter((_,n)=>n!==i)}>Remove rule</button>
      </div>
    {/each}
    <div class="actions"><button class="btn" disabled={saving || rules.length>=50} onclick={()=>rules=[...rules,{name:'',contains:'',retry:false,agent:'',model:'',reasoning:'',minCpu:0,minRamBytes:0}]}>Add rule</button><button class="btn primary" disabled={saving} onclick={save}>{saving?'Saving…':'Save rules'}</button>{#if saved}<span role="status">Saved</span>{/if}</div>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
  </div>
</section>
<style>
  .rules { padding: 16px; } h3 { font-size: 15px; display: flex; align-items: center; gap: 8px; } p { margin-block: 12px; font-size: 13px; color: var(--text-2); }
  .rule { display: grid; grid-template-columns: repeat(2,minmax(0,1fr)); gap: 12px; border-top: 1px solid var(--border); padding-block: 16px; }
  label { display: grid; gap: 6px; font-size: 13px; font-weight: 500; } .check,.actions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  @media(max-width:480px) { .rule { grid-template-columns: 1fr; } }
</style>

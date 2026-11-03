<script lang="ts">
  import Icon from '../Icon.svelte';
  import AgentMark from '../AgentMark.svelte';
  import Avatar from '../Avatar.svelte';
  import WorkspaceTag from '../WorkspaceTag.svelte';
  import { FLAG_LABEL, STATE_LABEL, type Row } from '../../shell/workqueue';
  import type { Entry } from '../../shell/types';

  // One row of work as the Work page draws it, in the window and in the web app: the title and where it sits, why it is worth a look,
  // the agent on it, its state at the right. What a press does, and which row is open beside the list, is the page's to say.
  let { row, index = 0, live, current = false, where, now, onopen, sortable = false }: {
    row: Row;
    index?: number;
    /** What the work is doing now, in words; left out for finished work. */
    live?: string;
    /** This row's task is the one open beside the list. */
    current?: boolean;
    /** The workspace to name on the row, when there is more than one to tell apart. */
    where?: Entry;
    now: number;
    onopen: (workspace: string, href: string) => void;
    /** The row can be put in another place by dragging its grip, or with Alt and the arrow keys: the page says what that does. */
    sortable?: boolean;
  } = $props();
</script>

  <li data-flag={row.flag} class="wb-rise" class:sortable style:--i={index} data-order-item={sortable ? row.item.id : undefined} data-order-label={sortable ? row.item.title : undefined}>
  {#if sortable}<button type="button" class="order-handle" data-order-handle aria-label="Reorder “{row.item.title}”. Alt plus Up or Down arrow moves it." title="Drag to reorder"><Icon name="grip" size={14} /></button>{/if}<button class="task" data-state={row.state} data-flag={row.flag} aria-current={current ? 'true' : undefined} onclick={() => onopen(row.workspace, row.item.href)}>
    <span class="task-content"><strong>{row.item.title}</strong><span class="context"><span class="proj">{row.item.project}</span>{#if row.item.plan}<span class="sep" aria-hidden="true">›</span><span class="plan">{row.item.plan}</span>{/if}{#if where}<WorkspaceTag workspace={where} />{/if}{#if live}<span class="detail" class:blocked={row.state === 'blocked'}>{live}</span>{/if}</span></span>
    {#if row.asking}<span class="ask-chip">Decision</span>{:else if row.flag}<span class="flag" data-flag={row.flag}>{FLAG_LABEL[row.flag]}</span>{/if}
    {#if row.item.agent && row.item.group !== 'done'}<AgentMark agentId={row.item.agent.id} state={row.item.agent.state} fresh={row.item.agent.fresh} observedAt={row.item.agent.observedAt} {now} />{/if}
    <span class="state" data-state={row.state}>{STATE_LABEL[row.state]}</span>
    {#if row.item.owner && row.item.group !== 'done'}<span class="holder" title={row.item.mine ? 'Held by you' : 'Held by ' + row.item.owner}><Avatar id={row.item.owner} name={row.item.mine ? 'Held by you' : 'Held by ' + row.item.owner} self={row.item.mine} size="sm" /></span>{/if}
    <span class="task-state" data-state={row.state} aria-hidden="true">{#if row.state === 'done'}<Icon name="check" size={14} />{/if}</span>
    <span class="open-icon"><Icon name="right" size={14} /></span>
  </button></li>

<style>
 .sortable{display:flex;align-items:center;gap:2px;padding-left:4px;}.sortable .task{flex:1;min-width:0;}
 .task{display:flex;align-items:center;gap:12px;width:100%;text-align:left;padding:14px 8px;background:transparent;border:0;color:var(--text);cursor:pointer;border-radius:5px;}
 .task:hover{background:var(--hover);}
 .task-content{display:grid;gap:4px;min-width:0;flex:1;}
 strong{font-size:14px;font-weight:500;line-height:1.5;}
 .context,.detail{font-size:12px;color:var(--text-2);overflow-wrap:anywhere;}
 .blocked{color:var(--block-text);}
 .task-state{display:grid;place-items:center;flex:none;width:22px;height:22px;color:var(--text-2);}
 .open-icon{color:var(--text-2);opacity:0;}
 .task:hover .open-icon,.task:focus-visible .open-icon{opacity:1;}
 @media(max-width:900px){.task{padding:14px 0;}.open-icon{opacity:1;}}
 .context{display:flex;flex-wrap:wrap;align-items:center;gap:2px 8px;}
 .context .sep{opacity:.6;}
 .context .plan{color:var(--text-2);}
 .context .detail::before{content:'·';margin-right:8px;}
 .task-state[data-state=action]::before{content:'';width:10px;height:10px;border-radius:50%;background:var(--amber);}
 .task-state[data-state=blocked]::before{content:'';width:10px;height:10px;border-radius:50%;border:2px dashed var(--block);}
 .task-state[data-state=done]{color:var(--ok);}
 /* Overview: trays for what is running, what is next, the plans, Git activity and participants. */
 .holder{display:inline-flex;}
 .state{font-size:12px;color:var(--text-2);white-space:nowrap;}
 .state[data-state=action]{color:var(--warn-text);font-weight:500;}
 .state[data-state=blocked]{color:var(--block-text,var(--text-2));}
 .state[data-state=running]{color:var(--accent);}
 .state[data-state=ready],.state[data-state=done]{opacity:.8;}
 .ask-chip{font-size:11px;padding:1px 8px;border:1px solid var(--border);border-radius:99px;color:var(--text-2);}
 /* Attention: the edge of a row says why it is worth a look; the chips above the queue count them and take you to the first. */
 .task:focus-visible{outline:2px solid var(--accent);outline-offset:-2px;}
 [data-flag=ask]{--chip:var(--amber);}
 [data-flag=failed]{--chip:var(--danger);}
 [data-flag=review]{--chip:var(--ok);}
 [data-flag=high]{--chip:var(--block,var(--accent));}
 li{position:relative;}
 .task{transition:background-color 140ms,box-shadow 200ms var(--ease);}
 .task[data-flag]{box-shadow:inset 3px 0 0 var(--chip);}
 .task[data-flag=ask]{background:color-mix(in srgb,var(--amber) 8%,var(--surface));}
 .task:hover{background:color-mix(in srgb,var(--chip,var(--accent)) 6%,var(--surface));}
 .task[aria-current=true]{background:color-mix(in srgb,var(--accent) 9%,var(--surface));box-shadow:inset 3px 0 0 var(--accent);}
 .open-icon{transition:opacity 140ms,transform 200ms var(--ease);}
 .task:hover .open-icon,.task:focus-visible .open-icon{transform:translateX(3px);}
 .flag{font-size:11px;font-weight:500;padding:1px 8px;border-radius:99px;white-space:nowrap;color:var(--text);background:color-mix(in srgb,var(--chip) 16%,var(--surface));border:1px solid color-mix(in srgb,var(--chip) 40%,var(--border));}
 .ask-chip{color:var(--text);background:color-mix(in srgb,var(--amber) 20%,var(--surface));border-color:color-mix(in srgb,var(--amber) 55%,var(--border));font-weight:500;}
 @media(prefers-reduced-motion:no-preference){
 }
</style>

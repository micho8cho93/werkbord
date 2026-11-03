<script lang="ts">
  import type { Snippet } from 'svelte';
  import Avatar from '../Avatar.svelte';
  import AgentMark from '../AgentMark.svelte';
  import QuestionCard from '../QuestionCard.svelte';
  import WorkspaceTag from '../WorkspaceTag.svelte';
  import WorkRow from './WorkRow.svelte';
  import { agentName } from '../agent-marks';
  import { timeAgo } from '../format';
  import type { Question } from '../types';
  import type { Entry, QueueItem } from '../../shell/types';
  import type { Decision } from '../../shell/decisions';
  import { describeParticipant, type Participant } from '../../shell/context-header';
  import type { Row } from '../../shell/workqueue';

  // The Work page's Overview, drawn once for the window's All projects and for the web app's Work pages: what waits on the person, a
  // catch-up, what is running, what is next, the plans, and room for what one side adds. It is handed rows and says nothing about
  // where they came from: each side reads its own workspaces and says what pressing a row, a decision or "See all" does.
  const SHOWN = 3;
  let { waiting, decide, running, upNext, ready, plans, plansEmpty, people, showWhere, now, whereOf, detail, isOpen, onopen, seeAll, brief, attention, extra, compact = false }: {
    waiting: Decision[];
    /** What the window or the web app does with a question: can it be answered here, how, and what opening it means. */
    decide: {
      askable: (d: Decision) => boolean;
      /** The workspace that asked has reported recently, so what is shown is current. */
      fresh: (d: Decision) => boolean;
      openable: (d: Decision) => boolean;
      answerWith: (q: Question, answer: string) => Promise<Question>;
      onSettled: (q: Question) => void;
      notify: (message: string) => void;
      open: (d: Decision) => void;
    };
    running: Row[];
    upNext: Row[];
    ready: number;
    plans: { workspace: Entry; item: QueueItem }[];
    plansEmpty: string;
    people: Participant[];
    showWhere: boolean;
    now: number;
    whereOf: (workspace: string) => Entry | undefined;
    detail: (workspace: string, item: QueueItem) => string | undefined;
    isOpen: (workspace: string, href: string) => boolean;
    onopen: (workspace: string, href: string) => void;
    seeAll: () => void;
    /** The catch-up card: it depends on where Salie can be asked from, so each side draws it. */
    brief?: Snippet;
    /** The questions and notices under the cards. */
    attention?: Snippet;
    /** More trays one side has (the window's Git activity). */
    extra?: Snippet;
    /** The list sits beside an open task, so it is one column and the decisions come first. */
    compact?: boolean;
  } = $props();
  const shownDecisions = $derived(waiting.slice(0, SHOWN));
</script>

{#snippet rowOf(row: Row, i: number)}
  <WorkRow {row} index={i} live={row.item.group === 'done' ? undefined : detail(row.workspace, row.item)} current={isOpen(row.workspace, row.item.href)} where={showWhere ? whereOf(row.workspace) : undefined} {now} {onopen} />
{/snippet}
{#snippet decisionBlock()}
    <aside class="decision" class:urgent={waiting.length > 0} aria-labelledby="decision-h" data-testid="next-decision">
      <h2 id="decision-h" class="section-title">{waiting.length ? 'Waiting for you' : 'Your next decision'}{#if waiting.length}<span class="pill wb-pop" aria-label="{waiting.length} waiting">{waiting.length}</span>{/if}</h2>
      <div aria-live="polite" class="decision-list">
      {#if shownDecisions.length}
        <p class="decision-count">{waiting.length === 1 ? '1 waiting' : `${waiting.length} waiting`}, oldest first</p>
        {#each shownDecisions as d, i (d.key)}
          {@const here = decide.fresh(d)}
          {@const openable = decide.openable(d)}
          <article class="decision-card wb-rise" style:--i={i} data-testid="decision" data-kind={d.question?.kind}>
            {#if !(decide.askable(d) && d.question)}<p class="decision-prompt">{d.prompt}</p>{/if}
            <dl class="decision-facts">
              <div><dt>Task</dt><dd>{d.task || 'Task'}</dd></div>
              <div><dt>Project</dt><dd>{d.project || 'Project'}{#if showWhere} <WorkspaceTag workspace={d.workspace} />{/if}</dd></div>
              {#if d.agent}<div><dt>Agent</dt><dd class="agent-fact"><AgentMark agentId={d.agent} state="needs_input" fresh={here} observedAt={d.askedAt} decorative /><span>{agentName(d.agent)}</span></dd></div>{/if}
              {#if d.askedAt}<div><dt>Asked</dt><dd>{timeAgo(d.askedAt, now)}</dd></div>{/if}
            </dl>
            {#if !here}<p class="decision-note" role="status">{d.workspace.name} is not answering. This is as last reported; it stays here until the workspace confirms it was answered.</p>{/if}
            {#if decide.askable(d) && d.question}
              <QuestionCard question={{ ...d.question, state: 'pending' }} showMeta={false} revealContext={d.question.kind === 'approval'} answerWith={decide.answerWith} onSettled={decide.onSettled} notify={decide.notify} />
              {#if openable}<button class="btn quiet small" type="button" onclick={() => decide.open(d)}>{d.workspace.kind === 'personal' ? 'Open session' : 'Open in ' + d.workspace.name}</button>{/if}
            {:else}
              <button class="btn primary" type="button" disabled={!(here && openable)} onclick={() => decide.open(d)}>{d.workspace.kind === 'personal' ? 'Open session' : 'Open in ' + d.workspace.name}</button>
              {#if here && !openable}<p class="decision-note">{d.workspace.kind === 'personal' ? 'Your own Werkbord is not ready to open this session.' : 'This workspace did not say where to answer. Open it to find the question.'}</p>{/if}
            {/if}
          </article>
        {/each}
        {#if waiting.length > SHOWN}<p class="decision-more">{waiting.length - SHOWN} more waiting, oldest first. Answer these and the next appears.</p>{/if}
      {:else}
        <p class="decision-empty">No agent is waiting on you.</p>
      {/if}
      </div>
    </aside>
{/snippet}
{#snippet plansTray(limit: number, id: string)}
  <section class="tray" aria-labelledby={id}>
    <header><h2 {id} class="section-title">Plans</h2><span class="tray-note">{plans.length}</span></header>
    <ul class="tray-card rows plans">{#each (limit ? plans.slice(0, limit) : plans) as row (row.workspace.id + row.item.href)}<li><button class="task" onclick={() => onopen(row.workspace.id, row.item.href)}><span class="task-content"><strong>{row.item.title}</strong><span class="context">{row.item.project}{#if showWhere}{' · ' + row.workspace.name}{/if}</span></span><span class="plan-progress">{row.item.detail}</span></button></li>{:else}<li class="empty">{plansEmpty}</li>{/each}</ul>
  </section>
{/snippet}
<div class="work-overview" class:compact>
    <div class="work-grid overview">
      <div class="work-main">
        {@render decisionBlock()}
        {@render brief?.()}
        {@render attention?.()}

      </div>
      <div class="work-side">
        <section class="tray" aria-labelledby="running-h">
          <header><h2 id="running-h" class="section-title">Running now<span class="head-count" data-testid="running-count">{running.length}</span></h2></header>
          <ul class="tray-card rows">{#each running.slice(0, 5) as row, i (row.workspace + row.item.href)}{@render rowOf(row, i)}{:else}<li class="empty">No agent is working right now.</li>{/each}</ul>
        </section>
        <section class="tray" aria-labelledby="next-h">
          <header><h2 id="next-h" class="section-title">Up next<span class="head-count">· {ready} ready</span></h2><button type="button" class="link" onclick={seeAll}>See all</button></header>
          <ul class="tray-card rows">{#each upNext as row, i (row.workspace + row.item.href)}{@render rowOf(row, i)}{:else}<li class="empty">No tasks ready to pick up.</li>{/each}</ul>
        </section>
        {@render plansTray(3, 'plans-o-h')}
        {@render extra?.()}
        {#if people.length}
          <section class="tray" aria-labelledby="people-h" data-testid="participants">
            <header><h2 id="people-h" class="section-title">Participants<span class="head-count">{people.length}</span></h2></header>
            <ul class="tray-card rows">{#each people as p (p.key)}<li class="person"><Avatar id={p.id} name={p.name} size="sm" /><span class="task-content"><strong>{p.name}</strong><span class="context">{describeParticipant(p)}</span></span></li>{/each}</ul>
          </section>
        {/if}
      </div>
    </div>
</div>

<style>
 p{margin:6px 0 0;}
 h2{font-size:14px;margin:0;font-weight:600;}
 ul{list-style:none;padding:0;margin:0;}
 li+li{border-top:1px solid var(--border);}
 .task{display:flex;align-items:center;gap:12px;width:100%;text-align:left;padding:14px 8px;background:transparent;border:0;color:var(--text);cursor:pointer;border-radius:5px;}
 .task:hover{background:var(--hover);}
 .task-content{display:grid;gap:4px;min-width:0;flex:1;}
 strong{font-size:14px;font-weight:500;line-height:1.5;}
 .context{font-size:12px;color:var(--text-2);overflow-wrap:anywhere;}
 .empty{padding:16px 28px;color:var(--text-2);font-size:13px;line-height:1.6;}
 .plans{margin-top:12px;}
 .plan-progress{font-size:12px;color:var(--text-2);}
 @media(max-width:900px){.plan-progress{max-width:110px;}.task{padding:14px 0;}}
 .work-grid{display:grid;grid-template-columns:minmax(0,1fr) minmax(18rem,26rem);gap:24px;align-items:start;}
 .work-side{position:sticky;top:16px;display:grid;gap:16px;min-width:0;align-content:start;}
 .work-main{min-width:0;display:grid;gap:16px;align-content:start;}
 .context{display:flex;flex-wrap:wrap;align-items:center;gap:2px 8px;}
 /* Overview: trays for what is running, what is next, the plans, Git activity and participants. */
 .head-count{margin-left:8px;font-size:var(--fs-meta);font-weight:500;color:var(--text-2);font-variant-numeric:tabular-nums;}
 .agent-fact{display:flex;align-items:center;gap:8px;}
 .person{display:flex;align-items:center;gap:10px;padding:10px 14px;}
 .rows{overflow:clip;}
 .rows .empty{padding:14px 16px;}
 .rows .task{padding:12px 14px;}
 .link{margin-left:auto;border:0;background:none;color:var(--accent);font:inherit;font-size:var(--fs-meta);cursor:pointer;padding:0;}
 .link:hover{text-decoration:underline;}
 .decision.urgent{background:var(--tray-ask);border-color:var(--tray-ask-edge);}
 .decision{border:1px solid var(--border);border-radius:var(--radius);background:var(--surface);padding:16px;display:grid;gap:10px;}
 .decision-count{margin:0;font-size:12px;color:var(--text-2);}
 .decision-prompt{margin:0;font-size:14px;line-height:1.5;overflow-wrap:anywhere;display:-webkit-box;-webkit-line-clamp:6;line-clamp:6;-webkit-box-orient:vertical;overflow:hidden;}
 .decision-facts{margin:0;display:grid;gap:6px;font-size:12px;}
 .decision-facts div{display:grid;grid-template-columns:64px 1fr;gap:8px;}
 .decision-facts dt{color:var(--text-2);}
 .decision-facts dd{margin:0;overflow-wrap:anywhere;}
 .decision-note{margin:0;font-size:12px;color:var(--text-2);line-height:1.5;}
 .decision-empty{margin:0;font-size:13px;color:var(--text-2);}
 .decision .btn{justify-self:start;}
 @media(max-width:1100px){.work-grid{grid-template-columns:minmax(0,1fr);}.work-side{position:static;}}
 .compact .work-grid{grid-template-columns:minmax(0,1fr);}
 .compact .work-side{position:static;}
 /* Attention: the edge of a row says why it is worth a look; the chips above the queue count them and take you to the first. */
 .task:focus-visible{outline:2px solid var(--accent);outline-offset:-2px;}
 li{position:relative;}
 .task{transition:background-color 140ms,box-shadow 200ms var(--ease);}
 .task:hover{background:color-mix(in srgb,var(--chip,var(--accent)) 6%,var(--surface));}
 .decision{transition:border-color 200ms,box-shadow 300ms;}
 .decision.urgent{border-color:color-mix(in srgb,var(--warn) 55%,var(--border));}
 .decision h2{display:flex;align-items:center;gap:8px;}
 .pill{min-width:22px;padding:1px 7px;border-radius:99px;background:var(--amber);color:#1d1b18;font-size:12px;font-weight:600;text-align:center;font-variant-numeric:tabular-nums;}
 .decision-list{display:grid;gap:14px;}
 .decision-card{display:grid;gap:10px;padding-top:14px;border-top:1px solid var(--border);}
 .decision-card:first-of-type{padding-top:0;border-top:0;}
 .decision-more{margin:0;font-size:12px;color:var(--text-2);}
 .decision-card :global(.ask){margin:0;}
 .decision-card .btn.quiet{justify-self:start;}
 .compact .work-grid{grid-template-columns:minmax(0,1fr);gap:16px;}
 .compact .work-side{position:static;}
 .compact .decision{position:static;order:-1;}
 .compact .task{padding:12px 8px;}
 @media(prefers-reduced-motion:no-preference){
  .decision.urgent{animation:wb-breathe 2.6s ease-in-out infinite;}
 }
</style>

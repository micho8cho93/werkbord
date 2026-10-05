<script lang="ts">
  import { tick } from 'svelte';
  import Icon, { type IconName } from './Icon.svelte';
  import { activitySummary, attentionCount, filterProjects, shortPath, stepIndex } from './projects';
  import { GLOBAL_VIEWS, PROJECT_SECTIONS, globalHref, projectHref, router, switchedTo, taskHref } from './router.svelte';
  import { app } from './state.svelte';
  import { theme } from './theme.svelte';
  import { TASK_STATE_LABELS } from './types';

  // Jump to: one box for everywhere. Projects, the sections of the project you are in, its
  // tasks, and the two actions you take most. Arrow keys move, Enter goes, Esc closes.

  interface Item {
    id: string;
    group: string;
    label: string;
    sub?: string;
    icon?: IconName;
    count?: number;
    current?: boolean;
    run: () => void;
  }

  let query = $state('');
  let highlight = $state(0);
  let input = $state<HTMLInputElement>();
  let opener: Element | null = null;

  const currentId = $derived(router.projectId || app.lastProjectId);
  const current = $derived(app.project(currentId));
  const scope = $derived(currentId ? app.scope(currentId) : undefined);
  const activity = (id: string) => app.overview?.projects.find((p) => p.projectId === id);
  const go = (href: string) => () => (location.hash = href);

  const matchesText = (q: string, ...texts: (string | undefined)[]) => !q || texts.some((t) => t?.toLowerCase().includes(q));

  const items = $derived.by((): Item[] => {
    const q = query.trim().toLowerCase();
    const out: Item[] = [];

    for (const p of filterProjects(app.projects, query)) {
      const a = activity(p.id);
      out.push({
        id: `p-${p.id}`,
        group: 'Projects',
        label: p.name,
        sub: activitySummary(a) || shortPath(p.repoPath),
        count: attentionCount(a),
        current: p.id === currentId,
        run: () => {
          app.enter(p.id);
          router.go(switchedTo(router.location, p.id));
        },
      });
    }

    if (current) {
      for (const s of PROJECT_SECTIONS) {
        if (matchesText(q, s.label, 'go to')) {
          out.push({ id: `s-${s.id}`, group: `Go to · ${current.name}`, label: s.label, icon: s.id, run: go(projectHref(current.id, s.id)) });
        }
      }
    }
    for (const g of GLOBAL_VIEWS) {
      if (matchesText(q, g.label, 'go to')) out.push({ id: `g-${g.id}`, group: 'Go to', label: g.label, icon: g.id, run: go(globalHref(g.id)) });
    }

    if (current && scope && q) {
      const tasks = scope.tasks
        .filter((t) => t.title.toLowerCase().includes(q))
        .sort((a, b) => Number(a.state === 'done') - Number(b.state === 'done'))
        .slice(0, 8);
      for (const t of tasks) {
        out.push({ id: `t-${t.id}`, group: `Tasks · ${current.name}`, label: t.title, sub: TASK_STATE_LABELS[t.state], run: go(taskHref(current.id, t.id)) });
      }
    }

    if (current && matchesText(q, 'new task', 'add task', 'create')) {
      out.push({ id: 'a-new', group: 'Actions', label: 'New task', sub: 'N', icon: 'plus', run: () => (app.newTaskOpen = true) });
    }
    if (matchesText(q, 'theme', 'dark', 'light')) {
      out.push({ id: 'a-theme', group: 'Actions', label: theme.dark ? 'Switch to light' : 'Switch to dark', icon: theme.dark ? 'sun' : 'moon', run: () => theme.toggle() });
    }
    return out;
  });

  // Opening: start at the project you are in, with the box ready. Closing returns focus to where it was.
  let wasOpen = false;
  $effect(() => {
    const open = app.switcherOpen;
    if (open && !wasOpen) {
      opener = document.activeElement;
      query = '';
      highlight = 0;
      for (const p of app.projects) app.warm(p.id); // so that whichever is chosen is already there
      void tick().then(() => input?.focus());
    } else if (!open && wasOpen && opener instanceof HTMLElement) {
      opener.focus();
    }
    wasOpen = open;
  });

  // Typing starts again at the top; a shorter list must not leave the highlight past its end.
  $effect(() => {
    void query;
    highlight = 0;
  });
  $effect(() => {
    if (highlight >= items.length) highlight = Math.max(0, items.length - 1);
  });

  function close() {
    app.switcherOpen = false;
  }

  function choose(item: Item) {
    close();
    item.run();
  }

  function onKey(e: KeyboardEvent) {
    switch (e.key) {
      case 'Escape':
        e.preventDefault();
        close();
        break;
      case 'ArrowDown':
      case 'ArrowUp':
        e.preventDefault();
        highlight = stepIndex(highlight, e.key === 'ArrowDown' ? 1 : -1, items.length);
        document.getElementById(`jt-${items[highlight]?.id}`)?.scrollIntoView({ block: 'nearest' });
        break;
      case 'Enter': {
        e.preventDefault();
        const it = items[highlight];
        if (it) choose(it);
        break;
      }
    }
  }

</script>

{#if app.switcherOpen}
  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
  <div class="scrim" onclick={close}></div>
  <div class="palette" role="dialog" aria-modal="true" aria-label="Jump to" tabindex="-1" onkeydown={onKey}>
    <div class="head">
      <Icon name="search" />
      <label class="visually-hidden" for="jt-search">Jump to a project, section or task</label>
      <input
        id="jt-search"
        bind:this={input}
        class="search"
        type="search"
        placeholder="Jump to a project, section or task"
        autocomplete="off"
        autocapitalize="off"
        spellcheck="false"
        role="combobox"
        aria-expanded="true"
        aria-controls="jt-list"
        aria-activedescendant={items[highlight] ? `jt-${items[highlight].id}` : undefined}
        bind:value={query}
      />
      <button class="key esc" type="button" onclick={close} aria-label="Close">esc</button>
    </div>

    {#if items.length}
      <ul id="jt-list" class="list" role="listbox" aria-label="Results">
        {#each items as it, i (it.id)}
          {#if i === 0 || items[i - 1].group !== it.group}
            <li class="group" role="presentation">{it.group}</li>
          {/if}
          <li id="jt-{it.id}" role="option" aria-selected={i === highlight} class="row" data-active={i === highlight}>
            <button type="button" class="pick" onclick={() => choose(it)} onmousemove={() => (highlight = i)} tabindex="-1">
              {#if it.icon}<span class="ic"><Icon name={it.icon} /></span>{:else if it.group === 'Projects'}<span class="sq" class:here={it.current}></span>{:else}<span class="ic"><Icon name="board" /></span>{/if}
              <span class="it">{it.label}</span>
              {#if it.current}<span class="chip">current</span>{/if}
              {#if it.sub}<span class="sub">{it.sub}</span>{/if}
              {#if it.count}<span class="num pend">{it.count}</span>{/if}
            </button>
          </li>
        {/each}
      </ul>
    {:else}
      <p class="none">Nothing matches “{query}”.</p>
    {/if}
    <p class="hint">
      <span><kbd class="key">↑</kbd><kbd class="key">↓</kbd> move</span>
      <span><kbd class="key">↵</kbd> open</span>
      <span><kbd class="key">N</kbd> new task</span>
    </p>
  </div>
{/if}

<style>
  .scrim {
    position: fixed;
    inset: 0;
    z-index: 40;
    background: var(--scrim);
    backdrop-filter: blur(2px);
  }

  .palette {
    position: fixed;
    z-index: 41;
    inset: 12vh auto auto 50%;
    width: min(36rem, calc(100vw - 32px));
    max-height: min(70vh, 34rem);
    transform: translateX(-50%);
    display: grid;
    grid-template-rows: auto minmax(0, 1fr) auto;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 10px;
    box-shadow: var(--win-sh);
    overflow: hidden;
    animation: rise 0.18s var(--ease);
  }

  @keyframes rise {
    from {
      opacity: 0;
      transform: translate(-50%, -6px);
    }
  }

  .head {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 0 12px 0 14px;
    border-bottom: 1px solid var(--border);
    color: var(--text-2);
  }

  .search {
    flex: 1;
    min-width: 0;
    height: 48px;
    border: 0;
    background: transparent;
    font-size: 15px;
    color: var(--text);
    outline: none;
  }

  .search::-webkit-search-cancel-button {
    display: none;
  }

  .esc {
    cursor: pointer;
  }

  .list {
    list-style: none;
    margin: 0;
    padding: 6px;
    overflow-y: auto;
    overscroll-behavior: contain;
  }

  .group {
    padding: 10px 10px 4px;
    font-family: var(--mono);
    font-size: 10px;
    font-weight: 500;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--text-2);
  }

  .pick {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    min-height: 36px;
    padding: 6px 10px;
    border: 0;
    border-radius: var(--radius-sm);
    background: transparent;
    text-align: left;
    font-size: 13px;
  }

  .row[data-active='true'] .pick {
    background: var(--surface-2);
    box-shadow: var(--press-sh);
  }

  .ic {
    display: inline-flex;
    color: var(--text-2);
  }

  .sq {
    width: 7px;
    height: 7px;
    margin: 0 4.5px;
    flex: none;
    background: var(--text-2);
  }

  .sq.here {
    background: var(--text);
  }

  .it {
    font-weight: 500;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .sub {
    margin-left: auto;
    padding-left: 12px;
    font-family: var(--mono);
    font-size: 11px;
    color: var(--text-2);
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    flex-shrink: 1;
  }

  .num {
    margin-left: 6px;
  }

  .it + .num,
  .chip + .num {
    margin-left: auto;
  }

  .none {
    padding: 24px;
    text-align: center;
    color: var(--text-2);
  }

  .hint {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 8px 14px;
    border-top: 1px solid var(--border);
    font-family: var(--mono);
    font-size: 11px;
    color: var(--text-2);
    background: var(--bg);
  }

  .hint {
    gap: 16px;
  }

  .hint span {
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }

  /* Phone: a sheet from the bottom, in thumb reach, without the keyboard hints. */
  @media (max-width: 899px) {
    .palette {
      inset: auto 0 0 0;
      width: auto;
      transform: none;
      max-height: 80dvh;
      border-radius: 12px 12px 0 0;
      padding-bottom: env(safe-area-inset-bottom);
      animation: none;
    }

    .hint {
      display: none;
    }
  }
</style>

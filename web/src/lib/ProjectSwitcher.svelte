<script lang="ts">
  import { tick } from 'svelte';
  import ProjectAvatar from './ProjectAvatar.svelte';
  import { activitySummary, attentionCount, filterProjects, stepIndex } from './projects';
  import { globalHref, router, switchedTo } from './router.svelte';
  import { app } from './state.svelte';

  let query = $state('');
  let highlight = $state(0);
  let input = $state<HTMLInputElement>();
  let opener: Element | null = null;

  const matches = $derived(filterProjects(app.projects, query));
  const current = $derived(router.projectId || app.lastProjectId);
  const activity = (id: string) => app.overview?.projects.find((p) => p.projectId === id);

  // Opening: start at the project you are in, with the search box ready. Closing returns focus to where it was.
  let wasOpen = false;
  $effect(() => {
    const open = app.switcherOpen;
    if (open && !wasOpen) {
      opener = document.activeElement;
      query = '';
      highlight = Math.max(0, app.projects.findIndex((p) => p.id === current));
      for (const p of app.projects) app.warm(p.id); // so that whichever is chosen is already there
      void tick().then(() => input?.focus());
    } else if (!open && wasOpen && opener instanceof HTMLElement) {
      opener.focus();
    }
    wasOpen = open;
  });

  // A search that matches fewer projects must not leave the highlight past the end.
  $effect(() => {
    if (highlight >= matches.length) highlight = Math.max(0, matches.length - 1);
  });

  function close() {
    app.switcherOpen = false;
  }

  /** Switching keeps the section you are in where it makes sense, needs no reload, and shows the new project's data at once. */
  function choose(id: string) {
    app.enter(id);
    router.go(switchedTo(router.location, id));
    close();
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
        highlight = stepIndex(highlight, e.key === 'ArrowDown' ? 1 : -1, matches.length);
        document.getElementById(`sw-${matches[highlight]?.id}`)?.scrollIntoView({ block: 'nearest' });
        break;
      case 'Enter': {
        e.preventDefault();
        const p = matches[highlight];
        if (p) choose(p.id);
        break;
      }
    }
  }
</script>

{#if app.switcherOpen}
  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
  <div class="scrim" onclick={close}></div>
  <div class="sheet" role="dialog" aria-modal="true" aria-label="Switch project" tabindex="-1" onkeydown={onKey}>
    <div class="head">
      <label class="visually-hidden" for="sw-search">Find a project</label>
      <input
        id="sw-search"
        bind:this={input}
        class="input search"
        type="search"
        placeholder="Switch project…"
        autocomplete="off"
        autocapitalize="off"
        spellcheck="false"
        role="combobox"
        aria-expanded="true"
        aria-controls="sw-list"
        aria-activedescendant={matches[highlight] ? `sw-${matches[highlight].id}` : undefined}
        bind:value={query}
      />
      <button class="btn small quiet close" onclick={close} aria-label="Close">Esc</button>
    </div>

    {#if matches.length}
      <ul id="sw-list" class="list" role="listbox" aria-label="Projects">
        {#each matches as p, i (p.id)}
          {@const a = activity(p.id)}
          {@const n = attentionCount(a)}
          <li
            id="sw-{p.id}"
            role="option"
            aria-selected={p.id === current}
            class="row"
            data-active={i === highlight}
            data-current={p.id === current}
          >
            <button class="pick" onclick={() => choose(p.id)} onmousemove={() => (highlight = i)}>
              <ProjectAvatar id={p.id} name={p.name} size={34} />
              <span class="what">
                <span class="name">{p.name}{#if p.id === current}<span class="here">current</span>{/if}</span>
                <span class="sub">{activitySummary(a) || p.repoPath}</span>
              </span>
              {#if n > 0}<span class="count" aria-label="{n} need you">{n}</span>{/if}
            </button>
          </li>
        {/each}
      </ul>
    {:else}
      <p class="none muted">{app.projects.length ? 'No project matches.' : 'No projects yet.'}</p>
    {/if}

    <a class="all" href={globalHref('projects')} onclick={close}>All projects and registering a repository →</a>
  </div>
{/if}

<style>
  .scrim {
    position: fixed;
    inset: 0;
    z-index: 30;
    background: rgb(0 0 0 / 0.38);
  }

  /* Phone: a sheet from the bottom, in thumb reach. */
  .sheet {
    position: fixed;
    z-index: 31;
    inset: auto 0 0 0;
    display: grid;
    gap: 8px;
    max-height: min(80dvh, 34rem);
    padding: 12px 12px calc(env(safe-area-inset-bottom) + 12px);
    grid-template-rows: auto minmax(0, 1fr) auto;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 16px 16px 0 0;
    box-shadow: 0 -8px 30px rgb(0 0 0 / 0.25);
  }

  .head {
    display: flex;
    gap: 8px;
  }

  .search {
    flex: 1;
    min-height: 44px;
  }

  .close {
    display: none;
  }

  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    overflow-y: auto;
    overscroll-behavior: contain;
  }

  .row[data-active='true'] .pick {
    background: var(--surface-2);
  }

  .pick {
    display: flex;
    align-items: center;
    gap: 12px;
    width: 100%;
    min-height: 56px;
    padding: 8px 10px;
    border: 0;
    border-radius: var(--radius-sm);
    background: transparent;
    text-align: left;
  }

  .what {
    display: grid;
    min-width: 0;
    flex: 1;
  }

  .name {
    display: flex;
    align-items: center;
    gap: 8px;
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .here {
    padding: 1px 7px;
    border-radius: 999px;
    background: color-mix(in srgb, var(--accent) 16%, transparent);
    color: var(--accent);
    font-size: 0.7rem;
    font-weight: 650;
  }

  .sub {
    font-size: 0.8rem;
    color: var(--text-2);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .count {
    min-width: 22px;
    height: 22px;
    padding: 0 6px;
    border-radius: 999px;
    background: var(--warn);
    color: #1a1204;
    font-size: 0.75rem;
    font-weight: 700;
    line-height: 22px;
    text-align: center;
  }

  .none {
    padding: 20px 8px;
    text-align: center;
  }

  .all {
    padding: 6px 8px;
    font-size: 0.85rem;
    font-weight: 550;
    text-decoration: none;
  }

  /* Desktop: a palette near the top, opened with Ctrl/⌘ K. */
  @media (min-width: 900px) {
    .sheet {
      inset: 12vh auto auto 50%;
      width: min(30rem, calc(100vw - 32px));
      transform: translateX(-50%);
      border-radius: 14px;
      padding-bottom: 12px;
      box-shadow: 0 18px 60px rgb(0 0 0 / 0.35);
    }

    .close {
      display: inline-flex;
    }
  }
</style>

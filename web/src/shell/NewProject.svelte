<script lang="ts">
  import Sheet from '../lib/Sheet.svelte';
  import Icon from '../lib/Icon.svelte';
  import { model } from './model.svelte';
  import { sectionColour, sectionItems } from './nav';

  // New project, step one: where it lives. Personal is the person's own Werkbord on this Mac (a folder, a GitHub repository, or work
  // with no code); a team is shared with the people on it. The choice is made here because a project cannot move between them without
  // a transfer. Step two is that place's own form, with nothing else around it.
  const places = $derived(sectionItems(model.view?.items));
  function close() { model.newProjectOpen = false; }
</script>

<Sheet title="New project" onclose={close} width="30rem">
  <div class="dlg" data-testid="new-project-dialog">
    <p class="lead">Where should it live?</p>
    <ul class="places">
      {#each places as place (place.id)}
        {@const ready = place.state === 'ready'}
        <li>
          <button class="place" type="button" disabled={!ready} onclick={() => model.startNewProject(place.id)} data-testid="new-project-{place.id}" data-autofocus={place === places[0] ? '' : undefined}>
            <span class="mark" style:--c={sectionColour(place.kind, place.id)} aria-hidden="true"><Icon name={place.kind === 'personal' ? 'folder' : 'people'} size={18} /></span>
            <span class="what">
              <span class="name">{place.kind === 'personal' ? 'Personal' : place.name}</span>
              <span class="hint">
                {#if !ready}Not connected right now. {place.detail || 'Reconnect to add a project here.'}
                {:else if place.kind === 'personal'}Just for you, on this Mac. Your agents work on a folder, a GitHub repository, or plain work with no code.
                {:else}Shared with the people on this team. You choose who joins once it exists.{/if}
              </span>
            </span>
            <span class="go" aria-hidden="true"><Icon name="right" size={16} /></span>
          </button>
        </li>
      {/each}
    </ul>
    <div class="actions"><button class="btn" type="button" onclick={close}>Cancel</button></div>
  </div>
</Sheet>

<style>
  .dlg { display: grid; gap: 12px; }
  .lead { margin: 0; font-size: 13.5px; color: var(--text-2); }
  .places { list-style: none; margin: 0; padding: 0; display: grid; gap: 8px; }
  .place { display: flex; align-items: center; gap: 12px; width: 100%; padding: 12px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface); color: var(--text); font: inherit; text-align: left; cursor: pointer; transition: border-color 120ms, background 120ms, transform 160ms var(--ease); }
  .place:hover:not(:disabled) { border-color: color-mix(in srgb, var(--accent) 45%, var(--border)); background: color-mix(in srgb, var(--accent) 5%, var(--surface)); }
  .place:active:not(:disabled) { transform: scale(.99); }
  .place:disabled { cursor: default; opacity: .6; }
  .place:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
  .mark { flex: none; display: grid; place-items: center; width: 36px; height: 36px; border-radius: 10px; color: var(--c); background: color-mix(in srgb, var(--c) 14%, var(--surface)); }
  .what { flex: 1; min-width: 0; display: grid; gap: 2px; }
  .name { font-size: 14px; font-weight: 600; overflow-wrap: anywhere; }
  .hint { font-size: 12.5px; color: var(--text-2); line-height: 1.4; }
  .go { color: var(--text-2); display: grid; }
  .actions { display: flex; justify-content: flex-end; }
</style>

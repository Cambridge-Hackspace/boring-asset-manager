<script lang="ts">
  // Saved searches by the asset list's search box (docs/data-model.md#saved-searches):
  // saving the search shown under a name, and opening or deleting those saved.
  import { onMount, tick } from 'svelte';
  import ConfirmButton from './ConfirmButton.svelte';
  import ErrorMessage from './ErrorMessage.svelte';
  import { messageOf } from '../lib/errors';
  import { deleteSavedSearch, listSavedSearches, saveSearch, savedSearchHref, savedSearchNote, type SavedSearch } from '../lib/savedSearches';
  import { session } from '../lib/session.svelte';

  // search is the search the list shows now; onopen searches with a saved one.
  let { search, onopen }: { search: string; onopen: (search: string) => void } = $props();

  let list = $state<SavedSearch[]>([]);
  let saving = $state(false);
  let name = $state('');
  let shared = $state(false);
  let error = $state('');
  let nameInput: HTMLInputElement | undefined = $state();

  const canSave = $derived(!!session.user && !!search.trim());
  const canShare = session.can('searches.share');

  // Saved searches are beside the list, not part of it: if they cannot be
  // read, there are none to show, and the list itself is unaffected.
  async function load() {
    try {
      list = await listSavedSearches();
    } catch {
      list = [];
    }
  }
  onMount(load);

  async function open() {
    saving = true;
    error = '';
    await tick();
    nameInput?.focus();
  }

  async function save(e: SubmitEvent) {
    e.preventDefault();
    error = '';
    try {
      await saveSearch(name, search, shared);
      saving = false;
      name = '';
      shared = false;
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function remove(s: SavedSearch) {
    error = '';
    try {
      await deleteSavedSearch(s.id);
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }

  function choose(e: MouseEvent, s: SavedSearch) {
    if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    e.preventDefault();
    onopen(s.search);
  }
</script>

{#if canSave || list.length}
  <div class="mb-4 space-y-2">
    <div class="flex flex-wrap items-start gap-3">
      {#if canSave && !saving}<button type="button" class="btn-secondary" onclick={open}>Save as…</button>{/if}
      {#if list.length}
        <details class="min-w-48 flex-1">
          <summary class="inline-flex min-h-11 cursor-pointer items-center font-medium">Saved searches ({list.length})</summary>
          <ul class="divide-y divide-zinc-300 text-sm dark:divide-zinc-700">
            {#each list as s (s.id)}
              <li class="flex flex-wrap items-center gap-2 py-1">
                <a class="inline-flex min-h-11 items-center underline" href={savedSearchHref(s)} title={s.search} onclick={(e) => choose(e, s)}>{s.name}</a>
                {#if savedSearchNote(s)}<span class={s.problem ? 'chip-red' : 'text-xs text-zinc-600 dark:text-zinc-400'} title={s.problem?.message}>{savedSearchNote(s)}</span>{/if}
                {#if s.mine}<span class="ml-auto"><ConfirmButton label="Delete" confirmLabel="Delete {s.name}" question="Delete the saved search {s.name}?" onconfirm={() => remove(s)} /></span>{/if}
              </li>
            {/each}
          </ul>
        </details>
      {/if}
    </div>
    {#if saving}
      <form class="flex flex-wrap items-end gap-3" aria-label="Save as" onsubmit={save}>
        <div>
          <label class="label" for="saved-search-name">Title</label>
          <input id="saved-search-name" class="input" required maxlength="100" bind:this={nameInput} bind:value={name} />
        </div>
        {#if canShare}
          <label class="inline-flex min-h-11 items-center gap-2"><input type="checkbox" class="h-5 w-5" bind:checked={shared} /> Share with everyone who can read assets</label>
        {/if}
        <button type="submit" class="btn-secondary">Save</button>
        <button type="button" class="btn-secondary" onclick={() => (saving = false)}>Cancel</button>
      </form>
    {/if}
    <ErrorMessage {error} />
  </div>
{/if}

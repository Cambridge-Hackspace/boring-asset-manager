<script lang="ts">
  // The person's saved searches on the home page, beside their favorites
  // (docs/data-model.md#saved-searches): their own, then those shared.
  import { onMount } from 'svelte';
  import { listSavedSearches, savedSearchHref, savedSearchNote, type SavedSearch } from '../lib/savedSearches';
  import { session } from '../lib/session.svelte';

  let list = $state<SavedSearch[]>([]);

  onMount(() => {
    if (!session.can('assets.read')) return;
    // Beside the home page's other cards: if they cannot be read, the card is left out.
    listSavedSearches()
      .then((l) => (list = l))
      .catch(() => (list = []));
  });
</script>

{#if list.length}
  <section class="card" aria-labelledby="saved-searches-heading">
    <h2 id="saved-searches-heading" class="mb-2 text-lg font-bold"><span class="shadow-[inset_0_-0.38em_0_var(--color-highlight)] dark:shadow-none">Saved searches</span></h2>
    <ul class="divide-y divide-zinc-300 text-sm dark:divide-zinc-700">
      {#each list as s (s.id)}
        <li class="flex flex-wrap items-center gap-2">
          <a class="flex min-h-11 items-center underline" href={savedSearchHref(s)} title={s.search}>{s.name}</a>
          {#if savedSearchNote(s)}<span class={s.problem ? 'chip-red' : 'text-xs text-zinc-600 dark:text-zinc-400'} title={s.problem?.message}>{savedSearchNote(s)}</span>{/if}
        </li>
      {/each}
    </ul>
  </section>
{/if}

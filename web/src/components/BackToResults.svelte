<!--
  The way back to all of a search's results, on the page a search went
  straight to (docs/data-model.md#search): shown when the address carries
  the search.
-->
<script lang="ts">
  import { router } from '../lib/router.svelte';
  import { allResultsHref } from '../lib/search';

  const search = $derived(router.query.get('search')?.trim() ?? '');
</script>

{#if search}
  <p class="mb-2 text-sm">
    <a class="inline-flex min-h-11 items-center underline" href={allResultsHref(search)}>All results for “{search}”</a>
  </p>
{/if}

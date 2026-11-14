<!--
  Going straight to a search's one result (docs/data-model.md#search): the
  instance's setting, which each person may override on their account.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { SearchSettings } from '../../lib/models';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  let direct = $state(false);
  let error = $state('');
  let status = $state('');
  let busy = $state(false);
  // Nothing can be changed until the setting is read, so a change is not
  // overwritten as it arrives.
  let loaded = $state(false);

  onMount(async () => {
    try {
      direct = (await request<SearchSettings>('GET', '/settings/search')).direct;
      loaded = true;
    } catch (err) {
      error = messageOf(err);
    }
  });

  async function save(event: SubmitEvent) {
    event.preventDefault();
    error = status = '';
    busy = true;
    try {
      direct = (await request<SearchSettings>('PUT', '/settings/search', { direct })).direct;
      status = 'Saved.';
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }
</script>

<section class="card mt-8 max-w-3xl space-y-3" aria-labelledby="search-settings-heading">
  <h2 id="search-settings-heading" class="text-lg font-semibold">Searching</h2>
  <p class="text-sm text-zinc-600 dark:text-zinc-400">
    When a search finds exactly one asset, it can open on its scan page, or its own page where no scan template applies, instead of
    being listed. Each person can choose otherwise on their account.
  </p>
  <ErrorMessage {error} />
  <form class="flex flex-wrap items-end gap-3" onsubmit={save}>
    <label class="flex min-h-11 items-center gap-2 text-sm">
      <input type="checkbox" bind:checked={direct} disabled={!loaded} />
      <span>Open a search's one result straight away</span>
    </label>
    <button type="submit" class="btn-secondary" disabled={busy || !loaded}>Save search settings</button>
    {#if status}<p role="status" class="text-sm text-green-700 dark:text-green-400">{status}</p>{/if}
  </form>
</section>

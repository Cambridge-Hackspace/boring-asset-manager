<script lang="ts">
  // /offline: the copy of what you can see that this device keeps, to use
  // with no signal (docs/installing.md#offline): finding things in it,
  // whether it is kept here, with thumbnails or not, and when it was made.
  import { onMount } from 'svelte';
  import { ago, categoryPaths, searchCopy, size } from '../lib/offline/copy';
  import { offline } from '../lib/offline/offline.svelte';
  import { session } from '../lib/session.svelte';
  import ErrorMessage from '../components/ErrorMessage.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import QueuedChanges from '../components/QueuedChanges.svelte';

  let { params: _params }: { params: Record<string, string> } = $props();

  const link = 'inline-flex min-h-11 items-center underline';
  let query = $state('');
  let status = $state('');
  const copy = $derived(offline.copy);
  const found = $derived(copy ? searchCopy(copy, query) : []);

  onMount(() => void offline.load());

  async function setKeep(keep: boolean) {
    status = '';
    offline.setPrefs({ keep });
    if (!keep) {
      await offline.clear(false);
      status = 'The copy is removed from this device.';
    } else if (!session.offline) {
      await offline.refresh(session.me);
      if (offline.copy) status = 'A copy is kept on this device.';
    }
  }

  async function setThumbnails(thumbnails: boolean) {
    status = '';
    offline.setPrefs({ thumbnails });
    if (offline.keepsHere && !session.offline) await offline.refresh(session.me);
  }

  async function refresh() {
    status = '';
    await offline.refresh(session.me);
    if (offline.copy && !offline.error) status = 'Refreshed.';
  }
</script>

<PageHeader title="Offline copy" lede="What you can see, kept on this device, so that scanning, finding things and the shopping list work with no signal." />

<div class="max-w-3xl space-y-6">
  {#if copy}
    <section class="space-y-3" aria-labelledby="find-heading">
      <h2 id="find-heading" class="text-lg font-semibold">Find something</h2>
      <div>
        <label class="label" for="offline-search">Name, code, category or place</label>
        <input id="offline-search" class="input w-full" type="search" autocomplete="off" bind:value={query} />
      </div>
      {#if query.trim()}
        {#if found.length}
          <ul class="divide-y divide-zinc-200 dark:divide-zinc-800">
            {#each found as a (a.id)}
              <li class="py-1">
                <a class={link} href="/scan/{encodeURIComponent(a.codes?.[0] ?? a.id)}">{a.name}</a>
                <span class="text-sm text-zinc-600 dark:text-zinc-400">
                  {[categoryPaths(copy, a).join('; '), a.where, a.stock ? `${a.stock.quantity}${a.stock.unit ? ` ${a.stock.unit}` : ''}` : ''].filter(Boolean).join(' · ')}
                </span>
              </li>
            {/each}
          </ul>
        {:else}
          <p role="status">Nothing in the copy matches.</p>
        {/if}
      {/if}
    </section>
  {/if}

  <QueuedChanges />

  <section class="card space-y-3" aria-labelledby="kept-heading">
    <h2 id="kept-heading" class="text-lg font-semibold">On this device</h2>
    {#if copy}
      <p role="status">
        {copy.assets.length} assets, made {ago(copy.made_at)} ({new Date(copy.made_at).toLocaleString()}), {size(offline.kept!.bytes)}.
        {#if offline.settings}It is refreshed every {offline.settings.refresh_minutes} minutes while the app is open and online.{/if}
      </p>
    {:else if offline.settings?.enabled === false}
      <p>This instance keeps no copies for use offline.</p>
    {:else}
      <p>No copy is kept here.</p>
    {/if}
    <ErrorMessage error={offline.error} />
    {#if offline.settings?.enabled !== false && !session.offline}
      <label class="flex min-h-11 items-center gap-2">
        <input type="checkbox" checked={offline.keepsHere} onchange={(e) => setKeep(e.currentTarget.checked)} />
        Keep a copy on this device
      </label>
      <p class="text-xs text-zinc-600 dark:text-zinc-400">
        The installed app keeps one unless you turn this off; a browser, only if you turn it on. It holds only what you can see, and is removed when you sign out.
      </p>
      {#if offline.settings?.thumbnails !== false}
        <label class="flex min-h-11 items-center gap-2">
          <input type="checkbox" checked={offline.keepsThumbnails} onchange={(e) => setThumbnails(e.currentTarget.checked)} />
          Keep thumbnails of photos too
        </label>
      {/if}
      <div class="flex flex-wrap gap-2">
        {#if offline.keepsHere}<button type="button" class="btn-secondary" disabled={offline.refreshing} onclick={refresh}>Refresh now</button>{/if}
      </div>
    {/if}
    {#if status}<p role="status" class="text-sm text-green-700 dark:text-green-400">{status}</p>{/if}
  </section>
</div>

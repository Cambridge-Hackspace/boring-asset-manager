<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { amount } from '../../lib/assets';
  import { messageOf } from '../../lib/errors';
  import { UNCATEGORIZED, type AssetWithDetails, type Category } from '../../lib/models';
  import { navigate, router } from '../../lib/router.svelte';
  import { session } from '../../lib/session.svelte';
  import { exportUrl } from '../../lib/importing';
  import { flattenTree } from '../../lib/tree';
  import CategoryBadge from '../../components/CategoryBadge.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';
  import LabelActions from '../../components/LabelActions.svelte';

  let { params: _params }: { params: Record<string, string> } = $props();

  let assets = $state<AssetWithDetails[]>([]);
  let categories = $state<Category[]>([]);
  let loading = $state(true);
  let error = $state('');
  let search = $state(router.query.get('search') ?? '');
  let categoryId = $state(router.query.get('category') ?? '');
  // Archived assets are listed apart, without codes or labels to print.
  let archived = $state(router.query.get('archived') === 'true');
  let selected = $state<string[]>([]);
  // The filters the list shows, which the export follows.
  let applied = $state({ search: router.query.get('search') ?? '', categoryId: router.query.get('category') ?? '' });

  const categoryChoices = $derived(flattenTree(categories.filter((c) => c.id !== UNCATEGORIZED)));
  const uncategorized = $derived(categories.find((c) => c.id === UNCATEGORIZED));
  const allSelected = $derived(assets.length > 0 && assets.every((a) => selected.includes(a.asset.id)));

  onMount(async () => {
    try {
      categories = (await request<{ categories: Category[] }>('GET', '/categories')).categories ?? [];
    } catch (err) {
      error = messageOf(err);
    }
    await load();
  });

  async function load() {
    loading = true;
    error = '';
    const q = new URLSearchParams();
    if (search.trim()) q.set('search', search.trim());
    if (categoryId) q.set('category_id', categoryId);
    if (archived) q.set('archived', 'true');
    applied = { search, categoryId };
    try {
      assets = (await request<{ assets: AssetWithDetails[] }>('GET', `/assets?${q}`)).assets ?? [];
      selected = selected.filter((id) => assets.some((a) => a.asset.id === id));
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
  }

  // Keep the filters in the address, so they survive reloads and links.
  function applyFilters(event?: SubmitEvent) {
    event?.preventDefault();
    const q = new URLSearchParams();
    if (search.trim()) q.set('search', search.trim());
    if (categoryId) q.set('category', categoryId);
    if (archived) q.set('archived', 'true');
    const query = q.toString();
    navigate('/assets' + (query ? `?${query}` : ''), true);
    void load();
  }

  // Switching between active and archived assets clears the selection.
  function switchList() {
    selected = [];
    applyFilters();
  }

  function toggleAll() {
    selected = allSelected ? [] : assets.map((a) => a.asset.id);
  }
</script>

<div class="mb-4 flex flex-wrap items-center justify-between gap-2">
  <h1 class="text-2xl font-bold">Assets</h1>
  <div class="flex flex-wrap gap-2">
    {#if !archived}<a class="btn-secondary" href={exportUrl(applied.search, applied.categoryId)} download>Export CSV</a>{/if}
    {#if session.can('assets:write')}
      <a class="btn-secondary" href="/assets/import">Import CSV</a>
      <a class="btn-primary" href="/assets/new">New asset</a>
    {/if}
  </div>
</div>

<form class="mb-4 flex flex-wrap items-end gap-3" role="search" onsubmit={applyFilters}>
  <div class="min-w-48 flex-1">
    <label class="label" for="asset-search">Search</label>
    <input id="asset-search" class="input" type="search" placeholder="Name or code" bind:value={search} />
  </div>
  <div>
    <label class="label" for="asset-category">Category</label>
    <select id="asset-category" class="input" bind:value={categoryId} onchange={() => applyFilters()}>
      <option value="">All categories</option>
      {#each categoryChoices as row (row.category.id)}
        <option value={row.category.id}>{' '.repeat(row.depth * 3)}{row.category.name}</option>
      {/each}
      {#if uncategorized}<option value={UNCATEGORIZED}>{uncategorized.name}</option>{/if}
    </select>
  </div>
  <div>
    <label class="label" for="asset-show">Show</label>
    <select id="asset-show" class="input" bind:value={archived} onchange={switchList}>
      <option value={false}>Active assets</option>
      <option value={true}>Archived assets</option>
    </select>
  </div>
  <button type="submit" class="btn-secondary">Search</button>
</form>

<ErrorMessage {error} />

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading assets…</p>
{:else if assets.length === 0}
  <p class="text-sm text-zinc-600 dark:text-zinc-400">{archived ? 'No archived assets match.' : 'No assets match.'}</p>
{:else}
  <p class="sr-only" role="status">{assets.length} assets, {selected.length} selected.</p>
  <div class="overflow-x-auto">
    <table class="w-full text-left text-sm">
      <thead class="border-b border-zinc-300 dark:border-zinc-700">
        <tr>
          {#if !archived}
            <th scope="col" class="w-8 py-2 pr-2">
              <input type="checkbox" aria-label="Select all assets" checked={allSelected} onchange={toggleAll} />
            </th>
          {/if}
          <th scope="col" class="py-2 pr-4">Name</th>
          <th scope="col" class="py-2 pr-4">Categories</th>
          <th scope="col" class="py-2 pr-4">Stock</th>
          <th scope="col" class="py-2">{archived ? 'Archived' : 'Codes'}</th>
        </tr>
      </thead>
      <tbody>
        {#each assets as a (a.asset.id)}
          <tr class="border-b border-zinc-200 dark:border-zinc-800">
            {#if !archived}
              <td class="py-2 pr-2">
                <input type="checkbox" aria-label="Select {a.asset.name}" value={a.asset.id} bind:group={selected} />
              </td>
            {/if}
            <td class="py-2 pr-4 font-medium"><a class="hover:underline" href="/assets/{encodeURIComponent(a.asset.id)}">{a.asset.name}</a></td>
            <td class="py-2 pr-4">
              <span class="flex flex-wrap gap-1">
                {#each a.categories as c (c.id)}<CategoryBadge category={c} />{/each}
              </span>
            </td>
            <td class="py-2 pr-4 whitespace-nowrap">
              {#if a.asset.stock}
                {amount(a.asset.stock.quantity, a.asset.stock.unit)}
                {#if a.asset.stock.is_running_low}<span class="ml-1 rounded bg-amber-100 px-1.5 py-0.5 text-xs font-medium text-amber-900 dark:bg-amber-900 dark:text-amber-100">Running low</span>{/if}
              {/if}
            </td>
            {#if archived}
              <td class="py-2 whitespace-nowrap">{a.asset.archived_at ? new Date(a.asset.archived_at).toLocaleDateString() : ''}</td>
            {:else}
              <td class="py-2 font-mono text-xs">{a.generator_keys.map((k) => k.assigned_code).join(', ')}</td>
            {/if}
          </tr>
        {/each}
      </tbody>
    </table>
  </div>

  {#if selected.length && !archived}
    <div class="mt-6">
      <p class="mb-2 text-sm font-medium">{selected.length} selected</p>
      {#key selected.join(',')}
        <LabelActions assetIds={selected} />
      {/key}
    </div>
  {/if}
{/if}

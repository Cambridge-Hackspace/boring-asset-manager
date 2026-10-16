<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import { UNCATEGORIZED, type Category } from '../../lib/models';
  import { session } from '../../lib/session.svelte';
  import { flattenTree } from '../../lib/tree';
  import CategoryBadge from '../../components/CategoryBadge.svelte';
  import ConflictMark from '../../components/ConflictMark.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  let { params: _params }: { params: Record<string, string> } = $props();

  let categories = $state<Category[]>([]);
  // Categories in scan template ties, with how many assets each (README §6.4).
  let conflicts = $state<Record<string, number>>({});
  let loading = $state(true);
  let error = $state('');

  onMount(async () => {
    try {
      categories = (await request<{ categories: Category[] }>('GET', '/categories')).categories ?? [];
      conflicts = await request<{ categories: Record<string, number> }>('GET', '/categories/scan-conflicts')
        .then((r) => r.categories ?? {})
        .catch(() => ({}));
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
  });

  const tree = $derived(flattenTree(categories.filter((c) => c.id !== UNCATEGORIZED)));
  const uncategorized = $derived(categories.find((c) => c.id === UNCATEGORIZED));
</script>

<div class="mb-4 flex flex-wrap items-center justify-between gap-2">
  <div>
    <h1 class="text-2xl font-bold">Categories</h1>
    <p class="text-sm text-zinc-600 dark:text-zinc-400">Subcategories inherit their parents’ fields.</p>
  </div>
  {#if session.can('categories.manage')}
    <a class="btn-primary" href="/categories/new">New category</a>
  {/if}
</div>

<ErrorMessage {error} />

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading categories…</p>
{:else}
  <ul class="space-y-1" aria-label="Category tree">
    {#each tree as row (row.category.id)}
      <li class="flex flex-wrap items-center gap-2" style="padding-left: {row.depth * 1.5}rem">
        {#if row.depth > 0}<span aria-hidden="true" class="text-zinc-400">└</span>{/if}
        <CategoryBadge category={row.category} href="/categories/{encodeURIComponent(row.category.id)}" />
        {#if row.category.is_running_low}
          <span class="rounded bg-amber-100 px-1.5 py-0.5 text-xs font-medium text-amber-900 dark:bg-amber-900 dark:text-amber-100">Running low</span>
        {:else if row.category.is_consumable}
          <span class="text-xs text-zinc-600 dark:text-zinc-400">Consumable</span>
        {/if}
        {#if conflicts[row.category.id]}
          <a href="/assets?conflict=scan_template" class="text-xs"><ConflictMark label="Ties with another category over the scan template of {conflicts[row.category.id]} {conflicts[row.category.id] === 1 ? 'asset' : 'assets'}" /></a>
        {/if}
        {#if row.depth > 0}<span class="sr-only">(level {row.depth + 1})</span>{/if}
      </li>
    {:else}
      <li class="text-sm text-zinc-600 dark:text-zinc-400">No categories yet.</li>
    {/each}
  </ul>
  {#if uncategorized}
    <p class="mt-6 flex items-center gap-2 text-sm text-zinc-600 dark:text-zinc-400">
      <CategoryBadge category={uncategorized} href="/categories/{UNCATEGORIZED}" /> holds assets with no other category.
    </p>
  {/if}
{/if}

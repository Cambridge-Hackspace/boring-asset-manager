<script lang="ts" module>
  /** The columns a list of results may show beside each asset's name. */
  export type ResultColumn = 'where' | 'categories' | 'stock' | 'codes' | 'archived';
  export const resultColumns: { value: ResultColumn; label: string }[] = [
    { value: 'where', label: 'Where' },
    { value: 'categories', label: 'Categories' },
    { value: 'stock', label: 'Stock' },
    { value: 'codes', label: 'Codes' },
  ];
</script>

<script lang="ts">
  // A search's results (docs/data-model.md#search), as the asset list and a
  // flow's Search step (docs/flows.md#search-search) both show them: each
  // asset's name, why it matched where that was not as typed, its scan
  // page, and the columns asked for.
  import { stack } from '../lib/stack';
  import { amount } from '../lib/assets';
  import type { AssetWithDetails } from '../lib/models';
  import { configuredPage } from '../lib/search';
  import { personName } from '../lib/people';
  import { dueText, reviewChip, reviewLabel } from '../lib/review';
  import AssetThumb from './AssetThumb.svelte';
  import CategoryBadge from './CategoryBadge.svelte';
  import ConditionBadge from './ConditionBadge.svelte';
  import ConflictMark from './ConflictMark.svelte';
  import FavoriteStar from './FavoriteStar.svelte';

  let {
    assets,
    matched = {},
    columns,
    selected = $bindable([]),
    selectable = false,
    star = false,
    scanLinks = false,
    hrefFor = (a) => `/assets/${encodeURIComponent(a.asset.id)}`,
    linkPlaces = true,
  }: {
    assets: AssetWithDetails[];
    matched?: Record<string, string>;
    columns: ResultColumn[];
    selected?: string[];
    /** A box to select each, and all. */
    selectable?: boolean;
    /** A star to make each a favorite, in a column of its own. */
    star?: boolean;
    /** A link to each one's scan page, where a scan template applies, in a column of its own. */
    scanLinks?: boolean;
    /** Where each one's name leads. */
    hrefFor?: (a: AssetWithDetails) => string;
    /** Whether where each one is leads to that place's page. */
    linkPlaces?: boolean;
  } = $props();

  const scanColumn = $derived(scanLinks && assets.some((a) => configuredPage(a)));
  const allSelected = $derived(assets.length > 0 && assets.every((a) => selected.includes(a.asset.id)));

  function toggleAll() {
    selected = allSelected ? [] : assets.map((a) => a.asset.id);
  }
</script>

<div class="overflow-x-auto">
  <table class="w-full text-left text-sm" use:stack>
    <thead class="border-b border-zinc-300 dark:border-zinc-700">
      <tr>
        {#if selectable}
          <th scope="col" class="w-8 py-2 pr-2">
            <label class="-my-2 inline-flex min-h-11 min-w-11 items-center justify-center"><input type="checkbox" class="h-5 w-5" aria-label="Select all assets" checked={allSelected} onchange={toggleAll} /></label>
          </th>
        {/if}
        {#if star}<th scope="col" class="w-8 py-2 pr-1"><span class="sr-only">Favorite</span></th>{/if}
        <th scope="col" class="py-2 pr-4">Name</th>
        {#if scanColumn}<th scope="col" class="py-2 pr-4"><span class="sr-only">Scan page</span></th>{/if}
        {#each columns as column (column)}
          <th scope="col" class="py-2 pr-4">{column === 'archived' ? 'Archived' : resultColumns.find((c) => c.value === column)?.label}</th>
        {/each}
      </tr>
    </thead>
    <tbody>
      {#each assets as a (a.asset.id)}
        <tr class="border-b border-zinc-200 dark:border-zinc-800">
          {#if selectable}
            <td class="py-2 pr-2">
              <label class="-my-2 inline-flex min-h-11 min-w-11 items-center justify-center"><input type="checkbox" class="h-5 w-5" aria-label="Select {a.asset.name}" value={a.asset.id} bind:group={selected} /></label>
            </td>
          {/if}
          <!-- The star has a column of its own, so the name's cell is named by the name alone. -->
          {#if star}<td class="py-2 pr-1"><span class="-my-2 inline-block align-middle"><FavoriteStar kind="asset" id={a.asset.id} name={a.asset.name} /></span></td>{/if}
          <td class="py-2 pr-4 font-medium">
            <span class="mr-2 inline-block align-middle empty:hidden"><AssetThumb asset={a} /></span><a class="inline-flex min-h-11 items-center align-middle hover:underline" href={hrefFor(a)}>{a.asset.name}</a> <ConditionBadge condition={a.asset.condition} />{#if a.review}{' '}<span class={a.review.overdue ? 'chip-red' : reviewChip(a.review.state)} title={dueText(a.review) || undefined}>{reviewLabel(a.review.state)}{a.review.overdue ? ', review overdue' : a.review.due ? ', review due' : ''}</span>{/if}{#if a.scan_template?.conflict}<ConflictMark />{/if}{#if a.label_template?.conflict}<ConflictMark label="Label template chosen by a tie between categories" />{/if}
            {#if a.loan}<span class="block text-xs font-normal text-zinc-600 dark:text-zinc-400">{a.loan.borrower.id ? `Lent to ${personName(a.loan.borrower)}` : 'Lent out'}{a.loan.overdue ? ', overdue' : ''}</span>{/if}
            {#if matched[a.asset.id]}<span class="block text-xs font-normal text-zinc-600 dark:text-zinc-400" data-testid="match-why">Found as {matched[a.asset.id]}</span>{/if}
          </td>
          <!-- The scan page has a column of its own too, for the same reason. -->
          {#if scanColumn}<td class="py-2 pr-4">{#if configuredPage(a)}<a class="-my-2 inline-flex min-h-11 items-center text-sm underline" href={configuredPage(a)} title="Open {a.asset.name} on its scan page">Scan page</a>{/if}</td>{/if}
          {#each columns as column (column)}
            {#if column === 'where'}
              <td class="py-2 pr-4">
                {#if a.location.length}
                  {@const place = a.location[a.location.length - 1]!}
                  {#if linkPlaces}
                    <a class="inline-flex min-h-11 items-center hover:underline" href="/assets/{encodeURIComponent(place.id)}" title={a.location.map((p) => p.name).join(' › ')}>{place.name}</a>
                  {:else}
                    {a.location.map((p) => p.name).join(' › ')}
                  {/if}
                {/if}
              </td>
            {:else if column === 'categories'}
              <td class="py-2 pr-4">
                <span class="flex flex-wrap gap-1">
                  {#each a.categories as c (c.id)}<CategoryBadge category={c} />{/each}
                </span>
              </td>
            {:else if column === 'stock'}
              <td class="py-2 pr-4 whitespace-nowrap">
                {#if a.asset.stock}
                  {amount(a.asset.stock.quantity, a.asset.stock.unit)}
                  {#if a.asset.stock.is_running_low}<span class="ml-1 rounded bg-amber-100 px-1.5 py-0.5 text-xs font-medium text-amber-900 dark:bg-amber-900 dark:text-amber-100">Running low</span>{/if}
                {/if}
              </td>
            {:else if column === 'archived'}
              <td class="py-2 pr-4 whitespace-nowrap">{a.asset.archived_at ? new Date(a.asset.archived_at).toLocaleDateString() : ''}</td>
            {:else}
              <td class="py-2 pr-4 font-mono text-xs">{a.generator_keys.map((k) => k.assigned_code).join(', ')}</td>
            {/if}
          {/each}
        </tr>
      {/each}
    </tbody>
  </table>
</div>

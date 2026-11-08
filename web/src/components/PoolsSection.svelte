<script lang="ts">
  // The pools an asset's stock counts in (docs/stock-and-lending.md#pools),
  // each with its total and threshold, and where else its stock is kept:
  // "140 pcs here, 300 pcs in laser bay". Only the places the viewer may
  // see are listed, and totaled.
  import { amount } from '../lib/assets';
  import type { AssetPool } from '../lib/models';
  import { placeName, poolLine } from '../lib/pools';

  let { pools, hereId, linked = true }: { pools: AssetPool[]; hereId: string; linked?: boolean } = $props();
</script>

{#each pools as pool (pool.category_id)}
  {@const places = pool.places.filter((p) => p.asset_id === hereId || p.quantity > 0)}
  <div class="mt-3 text-sm" data-pool={pool.category_id}>
    <p>
      <span class="font-medium">{poolLine(pool.name, pool)}</span>
    </p>
    <ul class="mt-1 space-y-0.5" aria-label="Where {pool.name} is kept">
      {#each places as p (p.asset_id)}
        <li>
          {#if p.asset_id === hereId}
            {amount(p.quantity, pool.unit)} here
          {:else}
            {amount(p.quantity, pool.unit)} in
            {#if linked}<a class="underline" href="/assets/{encodeURIComponent(p.asset_id)}">{placeName(p)}</a>{:else}{placeName(p)}{/if}{#if placeName(p) !== p.name}<span class="text-zinc-600 dark:text-zinc-400">{` (${p.name})`}</span>{/if}
          {/if}
        </li>
      {/each}
    </ul>
  </div>
{/each}

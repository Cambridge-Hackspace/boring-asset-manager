<script lang="ts">
  // The contracts covering an asset (docs/contracts.md), on its page:
  // "Under maintenance contract with X until June 2027", each a link to the
  // contract. Those naming one of its categories say so.
  import type { AssetWithDetails } from '../lib/models';
  import { contractEnded, costText, coverSentence } from '../lib/contracts';

  let { asset }: { asset: AssetWithDetails } = $props();

  const covers = $derived(asset.contracts ?? []);
  const categoryName = (id: string) => asset.categories.find((c) => c.id === id)?.name;
</script>

{#if covers.length}
  <section aria-labelledby="contracts-heading">
    <h2 id="contracts-heading" class="section-title mb-3">Contracts</h2>
    <ul class="space-y-1 text-sm" aria-label="Contracts covering it">
      {#each covers as c (c.id)}
        {@const through = c.category_id ? categoryName(c.category_id) : undefined}
        <li class={contractEnded(c) ? 'text-zinc-600 dark:text-zinc-400' : ''}>
          <a class="inline-flex min-h-11 items-center underline" href="/contracts/{encodeURIComponent(c.id)}">{coverSentence(c)}</a>
          <span class="text-zinc-600 dark:text-zinc-400"
            >{c.name}{c.reference ? ` · ${c.reference}` : ''}{costText(c) ? ` · ${costText(c)}` : ''}{c.category_id ? ` · through ${through ?? 'a category above it'}` : ''}</span
          >
        </li>
      {/each}
    </ul>
  </section>
{/if}

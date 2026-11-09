<script lang="ts">
  // Starting orders from what is running low (docs/purchase-orders.md#starting-from-the-running-low-list):
  // each thing low with a preferred catalog entry, by the entry's vendor,
  // with enough packs to bring it back to its threshold, ticked. Creating
  // makes a draft order for each vendor with something ticked.
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { OrderSuggestions, PurchaseOrder, SuggestedLine } from '../../lib/models';
  import { onOrderText } from '../../lib/orders';
  import { navigate } from '../../lib/router.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  type Choice = { line: SuggestedLine; on: boolean; quantity: number };

  let groups = $state<{ vendor_id: string; vendor_name: string; choices: Choice[] }[]>([]);
  let onOrder = $state<SuggestedLine[]>([]);
  let without = $state<SuggestedLine[]>([]);
  let loading = $state(true);
  let error = $state('');
  let busy = $state(false);

  const chosen = $derived(groups.reduce((n, g) => n + g.choices.filter((c) => c.on).length, 0));

  onMount(async () => {
    try {
      const s = await request<OrderSuggestions>('GET', '/purchase-orders/suggestions');
      groups = (s.vendors ?? []).map((v) => ({ vendor_id: v.vendor_id, vendor_name: v.vendor_name, choices: v.lines.map((l) => ({ line: l, on: true, quantity: l.quantity })) }));
      onOrder = s.on_order ?? [];
      without = s.without_entry ?? [];
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
  });

  const href = (l: SuggestedLine) => (l.asset_id ? `/assets/${encodeURIComponent(l.asset_id)}` : `/categories/${encodeURIComponent(l.category_id ?? '')}`);
  const level = (l: SuggestedLine) => `${l.level}${l.unit ? ` ${l.unit}` : ''}${l.threshold ? ` of ${l.threshold}` : ''}`;

  async function create(event: SubmitEvent) {
    event.preventDefault();
    error = '';
    busy = true;
    try {
      for (const g of groups) {
        const picked = g.choices.filter((c) => c.on && c.quantity > 0);
        if (!picked.length) continue;
        await request<{ order: PurchaseOrder }>('POST', '/purchase-orders', {
          vendor_id: g.vendor_id,
          lines: picked.map((c) => ({ catalog_entry_id: c.line.catalog_entry_id, quantity: Number(c.quantity) })),
        });
      }
      navigate('/orders?state=draft');
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }
</script>

<nav aria-label="Breadcrumb" class="mb-2 text-sm">
  <a class="underline" href="/orders">Orders</a>
</nav>
<h1 class="mb-1 text-3xl font-bold">Start orders</h1>
<p class="mb-4 text-sm text-zinc-600 dark:text-zinc-400">What is running low, by the vendor of its preferred catalog entry, with enough packs to bring it back to its threshold. Each vendor gets a draft order.</p>
<ErrorMessage {error} />

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading…</p>
{:else}
  {#if groups.length === 0}
    <p class="text-sm text-zinc-600 dark:text-zinc-400">Nothing running low has a preferred catalog entry from a vendor you order from.</p>
  {:else}
    <form class="max-w-3xl space-y-6" aria-label="Orders to start" onsubmit={create}>
      {#each groups as g (g.vendor_id)}
        <fieldset class="card space-y-2">
          <legend class="text-lg font-semibold">{g.vendor_name}</legend>
          {#each g.choices as c, i (i)}
            <div class="flex flex-wrap items-center gap-3">
              <label class="flex min-h-11 grow items-center gap-2 text-sm">
                <input type="checkbox" bind:checked={c.on} />
                <span>
                  <span class="font-medium">{c.line.name}</span>: {level(c.line)}.
                  {#if c.line.part_number}<span class="font-mono">{c.line.part_number}</span>{/if}
                  {c.line.description}{c.line.pack_size || c.line.pack_unit ? ` (${[c.line.pack_size ?? '', c.line.pack_unit ?? ''].join(' ').trim()})` : ''}{c.line.unit_price
                    ? `, ${c.line.unit_price} ${c.line.currency ?? ''} a pack`
                    : ''}
                </span>
              </label>
              <div class="w-24">
                <label class="label" for="start-{g.vendor_id}-{i}">Packs</label>
                <input id="start-{g.vendor_id}-{i}" class="input" type="number" min="1" max="1000000" disabled={!c.on} bind:value={c.quantity} />
              </div>
            </div>
          {/each}
        </fieldset>
      {/each}
      <button type="submit" class="btn-primary" disabled={busy || chosen === 0}>Create drafts</button>
    </form>
  {/if}

  {#if onOrder.length}
    <section class="mt-6 max-w-3xl" aria-labelledby="on-order-heading">
      <h2 id="on-order-heading" class="section-title mb-2">Already on order</h2>
      <ul class="text-sm">
        {#each onOrder as l, i (i)}
          <li class="flex min-h-11 flex-wrap items-center gap-x-2">
            <a class="inline-flex min-h-11 items-center underline" href={href(l)}>{l.name}</a>
            {#if l.order}<a class="inline-flex min-h-11 items-center underline" href="/orders/{encodeURIComponent(l.order.id)}">{onOrderText(l.order)}</a>{/if}
          </li>
        {/each}
      </ul>
    </section>
  {/if}
  {#if without.length}
    <section class="mt-6 max-w-3xl" aria-labelledby="without-heading">
      <h2 id="without-heading" class="section-title mb-2">No preferred catalog entry</h2>
      <p class="mb-1 text-sm text-zinc-600 dark:text-zinc-400">Running low, but not from a vendor you order from: choose a preferred entry on its Catalog tab.</p>
      <ul class="text-sm">
        {#each without as l, i (i)}
          <li><a class="inline-flex min-h-11 items-center underline" href={href(l)}>{l.name}</a></li>
        {/each}
      </ul>
    </section>
  {/if}
{/if}

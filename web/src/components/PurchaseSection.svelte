<script lang="ts">
  // Where an asset came from (docs/data-model.md#purchases), on its page:
  // read-only, changed in the asset's form.
  import { attachmentUrl, type AssetWithDetails } from '../lib/models';
  import { hasPurchase, priceText, warrantyEnded } from '../lib/purchases';

  let { asset, canEdit = false }: { asset: AssetWithDetails; canEdit?: boolean } = $props();

  const p = $derived(asset.purchase);
</script>

{#if hasPurchase(p) || canEdit}
  <section aria-labelledby="purchase-heading">
    <h2 id="purchase-heading" class="section-title mb-3">Purchase</h2>
    {#if p && hasPurchase(p)}
      <dl class="grid grid-cols-[auto_1fr] gap-x-6 gap-y-3 text-sm">
        {#if p.vendor_id}
          <dt class="text-zinc-600 dark:text-zinc-400">Vendor</dt>
          <dd><a class="inline-flex min-h-11 items-center underline" href="/vendors/{encodeURIComponent(p.vendor_id)}">{p.vendor_name}</a></dd>
        {/if}
        {#if p.order_date}
          <dt class="text-zinc-600 dark:text-zinc-400">Ordered</dt>
          <dd>{p.order_date}</dd>
        {/if}
        {#if p.received_date}
          <dt class="text-zinc-600 dark:text-zinc-400">Received</dt>
          <dd>{p.received_date}</dd>
        {/if}
        {#if p.price}
          <dt class="text-zinc-600 dark:text-zinc-400">Price</dt>
          <dd>{priceText(p)}</dd>
        {/if}
        {#if p.order_number}
          <dt class="text-zinc-600 dark:text-zinc-400">Order number</dt>
          <dd>{p.order_number}</dd>
        {/if}
        {#if p.warranty_end}
          <dt class="text-zinc-600 dark:text-zinc-400">Warranty</dt>
          <dd>{warrantyEnded(p.warranty_end) ? 'Ended' : 'Ends'} {p.warranty_end}</dd>
        {/if}
        {#if p.invoice_attachment_id}
          <dt class="text-zinc-600 dark:text-zinc-400">Invoice</dt>
          <dd>
            <a class="inline-flex min-h-11 items-center underline" href={attachmentUrl('asset', asset.asset.id, p.invoice_attachment_id)} download={p.invoice_name}>{p.invoice_name}</a>
          </dd>
        {/if}
      </dl>
      {#if p.prices_hidden}
        <p class="mt-3 text-sm text-zinc-600 dark:text-zinc-400" data-prices-hidden>Its price, order number and invoice are hidden: seeing them needs See prices.</p>
      {/if}
    {:else}
      <p class="text-sm text-zinc-600 dark:text-zinc-400">Nothing is recorded of where it came from. Edit it to add its vendor, price and warranty.</p>
    {/if}
  </section>
{/if}

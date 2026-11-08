<script lang="ts">
  // /orders/:id/receive: a delivery against an order
  // (docs/purchase-orders.md#receiving). Each line still to come shows the
  // packs arriving, all of them unless changed, and what that does: how
  // much it restocks an asset by, or how many assets it makes in a
  // category, like one already there. Every line or none is received.
  import { onMount, untrack } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { AssetWithDetails, Category, PurchaseOrder } from '../../lib/models';
  import { followQuantity, linePack, outstanding, receiptDrafts, receiptEffect, receiptRequests, type ReceiptDraft } from '../../lib/orders';
  import { navigate } from '../../lib/router.svelte';
  import { session } from '../../lib/session.svelte';
  import { addToSpool, queueNewLabels, rememberQueueNewLabels } from '../../lib/spool';
  import { todayOf } from '../../lib/contracts';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  let { params }: { params: Record<string, string> } = $props();
  const id = untrack(() => params.id ?? '');

  let order = $state<PurchaseOrder | null>(null);
  let drafts = $state<ReceiptDraft[]>([]);
  // What each line is for: an asset's unit, a category's unit (when it
  // weighs) and the assets in it to make others like.
  let units = $state<Record<string, string>>({});
  let weighs = $state<Record<string, string>>({});
  let likes = $state<Record<string, { id: string; name: string }[]>>({});
  let receivedOn = $state(todayOf());
  const canSpool = session.can('labels.print');
  let queueLabels = $state(canSpool && queueNewLabels(session.user?.id ?? ''));
  let loading = $state(true);
  let error = $state('');
  let busy = $state(false);

  const arriving = $derived(drafts.filter((d) => d.on).length);

  onMount(async () => {
    try {
      const o = (await request<{ order: PurchaseOrder }>('GET', `/purchase-orders/${encodeURIComponent(id)}`)).order;
      order = o;
      const ds = receiptDrafts(o);
      for (const d of ds) {
        const l = d.line;
        if (l.asset_id) {
          const a = await request<{ asset: AssetWithDetails }>('GET', `/assets/${encodeURIComponent(l.asset_id)}`).catch(() => null);
          units[l.id] = a?.asset.asset.stock?.unit ?? '';
        } else if (l.category_id) {
          const c = await request<{ category: Category }>('GET', `/categories/${encodeURIComponent(l.category_id)}`)
            .then((r) => r.category)
            .catch(() => null);
          if (c?.is_consumable && c.unit) weighs[l.id] = c.unit;
          const found = await request<{ assets: AssetWithDetails[] }>('GET', `/assets?category_id=${encodeURIComponent(l.category_id)}`)
            .then((r) => r.assets ?? [])
            .catch(() => []);
          // The newest first: the one to make them like, unless changed.
          const sorted = [...found].filter((a) => !a.asset.archived_at).sort((a, b) => b.asset.created_at.localeCompare(a.asset.created_at));
          likes[l.id] = sorted.slice(0, 50).map((a) => ({ id: a.asset.id, name: a.asset.name }));
          if (sorted[0]) d.like = sorted[0].asset.id;
          if (weighs[l.id] && l.pack_size && l.pack_unit.toLowerCase() === weighs[l.id]!.toLowerCase()) d.each = String(l.pack_size);
        }
      }
      drafts = ds;
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
  });

  function setQuantity(i: number, value: string) {
    drafts[i] = followQuantity(drafts[i]!, Math.max(0, Math.min(outstanding(drafts[i]!.line), Number(value) || 0)));
  }

  async function receive(event: SubmitEvent) {
    event.preventDefault();
    error = '';
    busy = true;
    try {
      const res = await request<{ created_asset_ids: string[] }>('POST', `/purchase-orders/${encodeURIComponent(id)}/receive`, {
        received_on: receivedOn,
        lines: receiptRequests(drafts),
      });
      if (canSpool) {
        rememberQueueNewLabels(session.user?.id ?? '', queueLabels);
        // The receipt stands either way; a spool that cannot take them now
        // can be added to later.
        if (queueLabels && res.created_asset_ids?.length) await addToSpool({ asset_ids: res.created_asset_ids }).catch(() => undefined);
      }
      navigate(`/orders/${encodeURIComponent(id)}`);
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }
</script>

<nav aria-label="Breadcrumb" class="mb-2 text-sm">
  <a class="underline" href="/orders">Orders</a>
  {#if order}› <a class="underline" href="/orders/{encodeURIComponent(order.id)}">{order.reference}</a>{/if}
</nav>
<h1 class="mb-1 text-3xl font-bold">Receive {order?.reference ?? 'an order'}</h1>
{#if order}<p class="mb-4 text-sm text-zinc-600 dark:text-zinc-400">From {order.vendor_name}. Untick what has not come.</p>{/if}
<ErrorMessage {error} />

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading…</p>
{:else if order && drafts.length === 0}
  <p class="text-sm text-zinc-600 dark:text-zinc-400">Nothing is still to come of this order.</p>
{:else if order}
  <form class="max-w-3xl space-y-4" aria-label="The delivery" onsubmit={receive}>
    <div class="max-w-48">
      <label class="label" for="receive-on">Received</label>
      <input id="receive-on" class="input" type="date" required bind:value={receivedOn} />
    </div>
    {#each drafts as d, i (d.line.id)}
      <fieldset class="card space-y-3">
        <legend class="font-semibold">
          {#if d.line.part_number}<span class="font-mono">{d.line.part_number}</span>{' '}{/if}{d.line.description}{linePack(d.line) ? ` (${linePack(d.line)})` : ''}
        </legend>
        <label class="flex items-center gap-2 text-sm"><input type="checkbox" bind:checked={d.on} /> Came in this delivery</label>
        {#if d.on}
          <div class="flex flex-wrap items-end gap-3">
            <div class="w-28">
              <label class="label" for="receive-{i}-packs">Packs</label>
              <input id="receive-{i}-packs" class="input" type="number" min="1" max={outstanding(d.line)} value={d.quantity} oninput={(e) => setQuantity(i, e.currentTarget.value)} />
              <p class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">of {outstanding(d.line)} to come</p>
            </div>
            {#if d.line.asset_id}
              <div class="w-36">
                <label class="label" for="receive-{i}-restock">Restocks by</label>
                <input id="receive-{i}-restock" class="input" type="number" min="0" required bind:value={d.restock} />
              </div>
            {:else if d.line.category_id}
              <div class="w-28">
                <label class="label" for="receive-{i}-assets">Assets</label>
                <input id="receive-{i}-assets" class="input" type="number" min="0" max="500" bind:value={d.assets} />
              </div>
              <div class="min-w-48 grow">
                <label class="label" for="receive-{i}-like">Like</label>
                <select id="receive-{i}-like" class="input" bind:value={d.like}>
                  <option value="">None: name them</option>
                  {#each likes[d.line.id] ?? [] as a (a.id)}<option value={a.id}>{a.name}</option>{/each}
                </select>
              </div>
              {#if !d.like}
                <div class="min-w-48 grow">
                  <label class="label" for="receive-{i}-name">Name</label>
                  <input id="receive-{i}-name" class="input" maxlength="255" bind:value={d.name} />
                </div>
              {/if}
              {#if weighs[d.line.id]}
                <div class="w-32">
                  <label class="label" for="receive-{i}-each">Each holds, in {weighs[d.line.id]}</label>
                  <input id="receive-{i}-each" class="input" type="number" min="0" required bind:value={d.each} />
                </div>
              {/if}
              <div class="w-44">
                <label class="label" for="receive-{i}-warranty">Warranty ends (optional)</label>
                <input id="receive-{i}-warranty" class="input" type="date" bind:value={d.warranty_end} />
              </div>
            {/if}
          </div>
          <p class="text-sm" aria-live="polite">{receiptEffect(d, units[d.line.id] ?? '')}{d.line.category_id && d.like ? ' Like the one chosen, with the next codes in its series and its place.' : ''}</p>
        {/if}
      </fieldset>
    {/each}
    {#if canSpool}
      <label class="flex items-center gap-2 text-sm"><input type="checkbox" bind:checked={queueLabels} /> Add the new assets’ labels to the print queue</label>
    {/if}
    <div class="flex flex-wrap gap-2">
      <button type="submit" class="btn-primary" disabled={busy || arriving === 0}>Receive</button>
      <a class="btn-secondary" href="/orders/{encodeURIComponent(order.id)}">Cancel</a>
    </div>
  </form>
{/if}

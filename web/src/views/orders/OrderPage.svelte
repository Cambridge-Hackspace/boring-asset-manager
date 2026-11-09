<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { request } from '../../lib/api';
  import { entryName, packText } from '../../lib/catalog';
  import { messageOf } from '../../lib/errors';
  import { renderMarkdown } from '../../lib/markdown';
  import type { CatalogEntry, OrderRequest, PurchaseOrder, Vendor } from '../../lib/models';
  import {
    describeOrderChange,
    describeOrderEvent,
    describeReceipt,
    lineDrafts,
    linePack,
    lineRequests,
    moneyText,
    orderOverdue,
    orderProblem,
    orderRequest,
    stateLabel,
    type LineDraft,
    type OrderEvent,
  } from '../../lib/orders';
  import { actorName } from '../../lib/people';
  import { currencies } from '../../lib/purchases';
  import { navigate, router } from '../../lib/router.svelte';
  import { session } from '../../lib/session.svelte';
  import { currentTab } from '../../lib/tabs';
  import AttachmentsPanel from '../../components/AttachmentsPanel.svelte';
  import ConfirmButton from '../../components/ConfirmButton.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';
  import TabLinks from '../../components/TabLinks.svelte';
  import StartOrders from './StartOrders.svelte';

  // /orders/new and /orders/:id (docs/purchase-orders.md); /orders/new?from=running-low
  // starts orders from what is running low.
  let { params }: { params: Record<string, string> } = $props();

  // The app remounts the page when the path changes, so these never change.
  const id = untrack(() => params.id ?? '');
  const isNew = !id;
  const fromLow = isNew && untrack(() => router.query.get('from')) === 'running-low';

  let order = $state<PurchaseOrder | null>(null);
  let vendors = $state<Vendor[]>([]);
  let catalog = $state<CatalogEntry[]>([]);
  let history = $state<OrderEvent[]>([]);
  let olderHistory = $state(false);
  let loading = $state(true);
  let error = $state('');
  let saved = $state('');
  let busy = $state(false);
  let editing = $state(isNew);

  let form = $state<OrderRequest>(orderRequest(null, untrack(() => router.query.get('vendor_id') ?? '')));
  let lines = $state<LineDraft[]>([]);
  let chosenEntry = $state('');

  // A placed order keeps its vendor and currency; a received or canceled
  // one changes only its reference and notes.
  const placed = $derived(!!order && order.state !== 'draft');
  const closed = $derived(order?.state === 'received' || order?.state === 'canceled');
  const pricesHidden = $derived(!!order?.prices_hidden);
  const problem = $derived(orderProblem(form, lines));
  const canReceive = $derived(
    !!order && (order.state === 'ordered' || order.state === 'partly_received') && session.canAny('stock.record', 'assets.write'),
  );

  const tabs = [
    { key: 'lines', label: 'Lines' },
    { key: 'details', label: 'Details' },
    { key: 'attachments', label: 'Attachments' },
    { key: 'history', label: 'History' },
  ];
  const tab = $derived(currentTab(router.search, tabs.map((t) => t.key)));

  onMount(() => {
    if (!fromLow) void load();
  });

  async function loadCatalog(vendorId: string) {
    catalog = vendorId
      ? await request<{ entries: CatalogEntry[] }>('GET', `/catalog?vendor_id=${encodeURIComponent(vendorId)}`).then(
          (r) => r.entries ?? [],
          () => [],
        )
      : [];
  }

  async function load() {
    error = '';
    try {
      vendors = await request<{ vendors: Vendor[] }>('GET', '/vendors').then(
        (r) => r.vendors ?? [],
        () => [],
      );
      if (isNew) {
        await loadCatalog(form.vendor_id);
        return;
      }
      const r = await request<{ order: PurchaseOrder }>('GET', `/purchase-orders/${encodeURIComponent(id)}`);
      fill(r.order);
      if (r.order.can_manage) await loadCatalog(r.order.vendor_id);
      await loadHistory();
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
  }

  function fill(o: PurchaseOrder) {
    order = o;
    form = orderRequest(o);
    lines = lineDrafts(o);
  }

  async function loadHistory(before?: string) {
    const query = before ? `?before=${encodeURIComponent(before)}` : '';
    const page = await request<{ events: OrderEvent[]; more: boolean }>('GET', `/purchase-orders/${encodeURIComponent(id)}/history${query}`);
    history = before ? [...history, ...(page.events ?? [])] : (page.events ?? []);
    olderHistory = page.more;
  }

  async function showOlder() {
    error = '';
    try {
      await loadHistory(history[history.length - 1]?.id);
    } catch (err) {
      error = messageOf(err);
    }
  }

  function addEntry() {
    const e = catalog.find((x) => x.id === chosenEntry);
    if (!e) return;
    // A price in the order's currency comes with it; another currency's does not.
    const price = e.price && (!form.currency || form.currency === e.currency) ? e.price : '';
    if (price && !form.currency && e.currency) form.currency = e.currency;
    lines = [
      ...lines,
      {
        catalog_entry_id: e.id,
        linked_name: e.linked_name,
        part_number: e.part_number,
        description: e.description,
        pack_size: e.pack_size,
        pack_unit: e.pack_unit,
        quantity: 1,
        unit_price: price,
        received: 0,
      },
    ];
    chosenEntry = '';
  }

  function addLine() {
    lines = [...lines, { part_number: '', description: '', pack_size: null, pack_unit: '', quantity: 1, unit_price: '', received: 0 }];
  }

  function body(): OrderRequest {
    return {
      ...form,
      currency: form.currency.trim().toUpperCase(),
      vendor_reference: form.vendor_reference.trim(),
      lines: lineRequests(lines, pricesHidden),
    };
  }

  async function save(event: SubmitEvent) {
    event.preventDefault();
    error = saved = '';
    if (problem) {
      error = problem;
      return;
    }
    busy = true;
    try {
      if (isNew) {
        const res = await request<{ order: PurchaseOrder }>('POST', '/purchase-orders', body());
        navigate(`/orders/${encodeURIComponent(res.order.id)}`, true);
        return;
      }
      const res = await request<{ order: PurchaseOrder }>('PUT', `/purchase-orders/${encodeURIComponent(id)}`, body());
      fill(res.order);
      editing = false;
      saved = 'Saved.';
      await loadHistory().catch(() => {});
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }

  // Places, or cancels, the order.
  async function act(action: 'place' | 'cancel', done: string) {
    error = saved = '';
    busy = true;
    try {
      const res = await request<{ order: PurchaseOrder }>('POST', `/purchase-orders/${encodeURIComponent(id)}/${action}`, {});
      fill(res.order);
      saved = done;
      await loadHistory().catch(() => {});
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }

  async function remove() {
    try {
      await request('DELETE', `/purchase-orders/${encodeURIComponent(id)}`);
      navigate('/orders');
    } catch (err) {
      error = messageOf(err);
    }
  }

  function cancelEdit() {
    if (order) fill(order);
    editing = false;
  }

  function attachmentsChanged(done: string) {
    saved = done;
    void loadHistory().catch(() => {});
  }

  const when = (iso: string) => new Date(iso).toLocaleString();
</script>

{#if fromLow}
  <StartOrders />
{:else}
  <nav aria-label="Breadcrumb" class="mb-2 text-sm">
    <a class="underline" href="/orders">Orders</a>
  </nav>

  <div class="mb-4">
    <h1 class="text-3xl font-bold">{isNew ? 'New order' : (order?.reference ?? 'Order')}</h1>
    {#if order}
      <p class="mt-1 text-sm text-zinc-600 dark:text-zinc-400">
        {stateLabel(order.state)}, to <a class="underline" href="/vendors/{encodeURIComponent(order.vendor_id)}">{order.vendor_name}</a>{order.ordered_on
          ? `, ordered ${order.ordered_on}`
          : ''}{order.expected_on && order.state !== 'received' ? `, expected ${order.expected_on}` : ''}{order.received_on
          ? `, received ${order.received_on}`
          : ''}.
        {#if orderOverdue(order)}<span class="font-medium text-amber-900 dark:text-amber-200">Overdue.</span>{/if}
      </p>
    {/if}
  </div>
  <ErrorMessage {error} />
  {#if saved}<p class="mb-2 text-sm text-green-700 dark:text-green-400" role="status">{saved}</p>{/if}

  {#snippet orderForm()}
    <form class="card max-w-4xl space-y-4" aria-label="The order" onsubmit={save}>
      {#if !closed}
        <div class="grid gap-4 sm:grid-cols-2">
          <div>
            <label class="label" for="order-vendor">Vendor</label>
            <select id="order-vendor" class="input" required disabled={placed} bind:value={form.vendor_id} onchange={() => void loadCatalog(form.vendor_id)}>
              <option value="">Choose a vendor</option>
              {#each vendors as v (v.id)}<option value={v.id}>{v.name}</option>{/each}
            </select>
          </div>
          {#if !pricesHidden}
            <div>
              <label class="label" for="order-currency">Currency</label>
              <input id="order-currency" class="input uppercase" maxlength="3" list="order-currencies" placeholder="GBP" disabled={placed && !!order?.currency} bind:value={form.currency} />
              <datalist id="order-currencies">{#each currencies as c (c)}<option value={c}></option>{/each}</datalist>
            </div>
          {/if}
          {#if placed}
            <div>
              <label class="label" for="order-ordered">Ordered</label>
              <input id="order-ordered" class="input" type="date" bind:value={form.ordered_on} />
            </div>
          {/if}
          <div>
            <label class="label" for="order-expected">Expected (optional)</label>
            <input id="order-expected" class="input" type="date" bind:value={form.expected_on} />
          </div>
        </div>
        <div>
          <label class="label" for="order-deliver">Deliver to</label>
          <textarea id="order-deliver" class="input" rows="3" maxlength="1000" placeholder="The address it is delivered to; a new order starts with the last one’s" bind:value={form.deliver_to}></textarea>
        </div>
      {/if}
      <div>
        <label class="label" for="order-reference">Vendor’s reference (optional)</label>
        <input id="order-reference" class="input font-mono" maxlength="255" placeholder="Their order or confirmation number" bind:value={form.vendor_reference} />
      </div>
      <div>
        <label class="label" for="order-notes">Notes (optional)</label>
        <textarea id="order-notes" class="input" rows="4" aria-describedby="order-notes-help" bind:value={form.notes}></textarea>
        <p id="order-notes-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">Printed on the order. Markdown: **bold**, *italics*, lists, and [links](https://example.org).</p>
      </div>

      {#if !closed}
        <fieldset class="space-y-3">
          <legend class="text-lg font-semibold">Lines</legend>
          {#if pricesHidden}<p class="text-sm text-zinc-600 dark:text-zinc-400">Its prices are hidden from you, and stay as they are.</p>{/if}
          {#if lines.length === 0}
            <p class="text-sm text-zinc-600 dark:text-zinc-400">No lines yet.</p>
          {/if}
          {#each lines as l, i (i)}
            <div class="grid gap-2 border-b border-zinc-200 pb-3 sm:grid-cols-[1fr_2fr_5rem_6rem_5rem_6rem_auto] sm:items-end dark:border-zinc-800" role="group" aria-label="Line {i + 1}">
              <div>
                <label class="label" for="line-{i}-part">Part number</label>
                <input id="line-{i}-part" class="input font-mono" maxlength="255" bind:value={l.part_number} />
              </div>
              <div>
                <label class="label" for="line-{i}-description">Description</label>
                <input id="line-{i}-description" class="input" maxlength="2000" bind:value={l.description} />
                {#if l.linked_name}<p class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">For {l.linked_name}{l.received ? `; ${l.received} come` : ''}</p>{/if}
              </div>
              <div>
                <label class="label" for="line-{i}-pack">Pack</label>
                <input id="line-{i}-pack" class="input" type="number" min="1" value={l.pack_size ?? ''} oninput={(e) => (l.pack_size = e.currentTarget.value ? Number(e.currentTarget.value) : null)} />
              </div>
              <div>
                <label class="label" for="line-{i}-unit">Of</label>
                <input id="line-{i}-unit" class="input" maxlength="64" bind:value={l.pack_unit} />
              </div>
              <div>
                <label class="label" for="line-{i}-quantity">Packs</label>
                <input id="line-{i}-quantity" class="input" type="number" min={Math.max(1, l.received)} max="1000000" required bind:value={l.quantity} />
              </div>
              {#if !pricesHidden}
                <div>
                  <label class="label" for="line-{i}-price">Price a pack</label>
                  <input id="line-{i}-price" class="input" inputmode="decimal" placeholder="4.50" bind:value={l.unit_price} />
                </div>
              {/if}
              <button type="button" class="btn-secondary" disabled={l.received > 0} aria-label="Remove line {i + 1}" onclick={() => (lines = lines.filter((_, j) => j !== i))}>Remove</button>
            </div>
          {/each}
          <div class="flex flex-wrap items-end gap-2">
            {#if catalog.length}
              <div>
                <label class="label" for="order-entry">From the vendor’s catalog</label>
                <select id="order-entry" class="input" bind:value={chosenEntry}>
                  <option value="">Choose an entry</option>
                  {#each catalog as e (e.id)}
                    <option value={e.id}>{entryName(e)}{e.description && e.part_number ? ` ${e.description}` : ''}{packText(e) ? ` (${packText(e)})` : ''}, for {e.linked_name}</option>
                  {/each}
                </select>
              </div>
              <button type="button" class="btn-secondary" disabled={!chosenEntry} onclick={addEntry}>Add from catalog</button>
            {/if}
            <button type="button" class="btn-secondary" onclick={addLine}>Add a line</button>
          </div>
        </fieldset>
      {/if}

      <div class="flex flex-wrap gap-2">
        <button type="submit" class="btn-primary" disabled={busy}>{isNew ? 'Create draft' : 'Save'}</button>
        {#if !isNew}<button type="button" class="btn-secondary" onclick={cancelEdit}>Cancel</button>{/if}
      </div>
    </form>
  {/snippet}

  {#if loading}
    <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading…</p>
  {:else if isNew}
    {#if session.can('vendors.manage')}{@render orderForm()}{/if}
  {:else if order}
    <div class="mb-4 flex flex-wrap gap-2">
      {#if order.can_manage && order.state === 'draft'}
        <button type="button" class="btn-primary" disabled={busy || order.lines.length === 0} onclick={() => act('place', 'Placed with the vendor.')}>Place order</button>
      {/if}
      {#if canReceive}
        <a class="btn-primary" href="/orders/{encodeURIComponent(order.id)}/receive">Receive…</a>
      {/if}
      <a class="btn-secondary" href="/api/v1/purchase-orders/{encodeURIComponent(order.id)}/export.pdf" download>PDF</a>
      {#if order.can_manage && !editing}
        <button type="button" class="btn-secondary" onclick={() => (editing = true)}>Edit</button>
      {/if}
      {#if order.can_manage && (order.state === 'ordered' || order.state === 'partly_received')}
        <ConfirmButton label="Cancel order" question="Cancel this order? What came stays; the rest is not expected." onconfirm={() => act('cancel', 'Canceled.')} />
      {/if}
      {#if order.can_manage}
        <ConfirmButton
          label="Delete"
          question={order.state === 'draft' ? 'Delete this draft?' : 'Delete this order, with its files? What it received stays received, and its history is lost.'}
          onconfirm={remove}
        />
      {/if}
    </div>

    {#if editing && order.can_manage}
      {@render orderForm()}
    {:else}
      <TabLinks label="Parts of this order" {tabs} />

      <section class="mt-6 max-w-4xl" id="tab-lines" aria-labelledby="lines-heading" hidden={tab !== 'lines'}>
        <h2 id="lines-heading" class="section-title mb-3">Lines</h2>
        {#if order.lines.length === 0}
          <p class="text-sm text-zinc-600 dark:text-zinc-400">No lines yet.</p>
        {:else}
          <div class="overflow-x-auto">
            <table class="w-full text-left text-sm" aria-label="Lines">
              <thead>
                <tr class="border-b border-zinc-300 dark:border-zinc-700">
                  <th scope="col" class="py-2 pr-4 font-semibold">Line</th>
                  <th scope="col" class="py-2 pr-4 font-semibold">For</th>
                  <th scope="col" class="py-2 pr-4 font-semibold">Pack</th>
                  <th scope="col" class="py-2 pr-4 font-semibold">Packs</th>
                  <th scope="col" class="py-2 pr-4 font-semibold">Come</th>
                  <th scope="col" class="py-2 pr-4 font-semibold">Price a pack</th>
                  <th scope="col" class="py-2 font-semibold">Total</th>
                </tr>
              </thead>
              <tbody>
                {#each order.lines as l (l.id)}
                  <tr class="border-b border-zinc-200 dark:border-zinc-800">
                    <td class="py-2 pr-4">{#if l.part_number}<span class="font-mono">{l.part_number}</span>{' '}{/if}{l.description}</td>
                    <td class="pr-4">
                      {#if l.asset_id}<a class="inline-flex min-h-11 items-center underline" href="/assets/{encodeURIComponent(l.asset_id)}">{l.linked_name}</a>
                      {:else if l.category_id}<a class="inline-flex min-h-11 items-center underline" href="/categories/{encodeURIComponent(l.category_id)}">{l.linked_name}</a>
                      {:else}<span class="text-zinc-600 dark:text-zinc-400">Nothing kept</span>{/if}
                    </td>
                    <td class="pr-4">{linePack(l)}</td>
                    <td class="pr-4">{l.quantity}</td>
                    <td class="pr-4">{l.quantity_received}</td>
                    <td class="pr-4">{moneyText(l.unit_price, order.currency)}</td>
                    <td>{moneyText(l.total, order.currency)}</td>
                  </tr>
                {/each}
              </tbody>
              {#if order.total}
                <tfoot>
                  <tr>
                    <th scope="row" colspan="6" class="py-2 pr-4 text-right font-semibold">Total</th>
                    <td class="py-2 font-semibold">{moneyText(order.total, order.currency)}</td>
                  </tr>
                </tfoot>
              {/if}
            </table>
          </div>
        {/if}
        {#if order.prices_hidden}<p class="mt-2 text-sm text-zinc-600 dark:text-zinc-400">Its prices are hidden from you.</p>{/if}
      </section>

      <section class="mt-6 max-w-2xl space-y-4" id="tab-details" aria-labelledby="details-heading" hidden={tab !== 'details'}>
        <h2 id="details-heading" class="section-title">Details</h2>
        <dl class="grid gap-x-6 gap-y-3 text-sm sm:grid-cols-[max-content_1fr]">
          <dt class="font-semibold">Vendor</dt>
          <dd><a class="inline-flex min-h-11 items-center underline" href="/vendors/{encodeURIComponent(order.vendor_id)}">{order.vendor_name}</a></dd>
          <dt class="font-semibold">State</dt>
          <dd>{stateLabel(order.state)}</dd>
          <dt class="font-semibold">Vendor’s reference</dt>
          <dd>{#if order.vendor_reference}<span class="font-mono">{order.vendor_reference}</span>{:else}<span class="text-zinc-600 dark:text-zinc-400">None</span>{/if}</dd>
          <dt class="font-semibold">Ordered</dt>
          <dd>{order.ordered_on || 'Not yet'}</dd>
          <dt class="font-semibold">Expected</dt>
          <dd>{order.expected_on || 'Not said'}</dd>
          <dt class="font-semibold">Received</dt>
          <dd>{order.received_on || 'Not yet'}</dd>
          <dt class="font-semibold">Deliver to</dt>
          <dd class="whitespace-pre-line">{order.deliver_to || 'Not said'}</dd>
        </dl>
        {#if order.notes.trim()}
          <section aria-labelledby="notes-heading">
            <h3 id="notes-heading" class="mb-1 text-lg font-semibold">Notes</h3>
            <!-- Written by people, so rendered with everything escaped, and links only to the app, https or email (lib/markdown.ts). -->
            <div class="notes">
              <!-- eslint-disable-next-line svelte/no-at-html-tags -->
              {@html renderMarkdown(order.notes)}
            </div>
          </section>
        {/if}
      </section>

      <div class="mt-6 max-w-2xl" id="tab-attachments" hidden={tab !== 'attachments'}>
        <AttachmentsPanel owner="purchase_order" ownerId={id} ownerName={order.reference} canEdit={order.can_manage} onchange={attachmentsChanged} />
      </div>

      <section class="mt-6 max-w-2xl" id="tab-history" aria-labelledby="history-heading" hidden={tab !== 'history'}>
        <h2 id="history-heading" class="section-title mb-3">History</h2>
        {#if history.length === 0}
          <p class="text-sm text-zinc-600 dark:text-zinc-400">No history yet.</p>
        {:else}
          <ul class="space-y-2 text-sm">
            {#each history as e (e.id)}
              <li>
                <span class="font-medium">{describeOrderEvent(e)}</span>
                {#if e.payload.changes?.length || e.payload.order_lines?.length}
                  <ul class="list-disc pl-5">
                    {#each e.payload.changes ?? [] as c, i (i)}<li>{describeOrderChange(c)}</li>{/each}
                    {#if e.kind === 'purchase_order.received'}
                      {#each describeReceipt(e) as line, i (i)}<li>{line}</li>{/each}
                    {/if}
                  </ul>
                {/if}
                <span class="block text-xs text-zinc-600 dark:text-zinc-400">
                  {when(e.created_at)}{e.payload.actor ? ` · ${actorName(e.payload.actor, session.user?.id)}` : ''}
                </span>
              </li>
            {/each}
          </ul>
          {#if olderHistory}
            <button type="button" class="btn-secondary mt-3" onclick={showOlder}>Show older</button>
          {/if}
        {/if}
      </section>
    {/if}
  {/if}
{/if}

<style>
  .notes :global(p),
  .notes :global(ul),
  .notes :global(ol) {
    margin-bottom: 0.75rem;
    font-size: 0.875rem;
    line-height: 1.5;
  }
  .notes :global(ul) {
    list-style: disc;
    padding-left: 1.25rem;
  }
  .notes :global(ol) {
    list-style: decimal;
    padding-left: 1.25rem;
  }
</style>

<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { PurchaseOrder } from '../../lib/models';
  import { moneyText, orderOverdue, orderStates, stateLabel } from '../../lib/orders';
  import { navigate, router } from '../../lib/router.svelte';
  import { session } from '../../lib/session.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  // /orders: the purchase orders someone may see (docs/purchase-orders.md),
  // newest first, by state, and a search through them.
  let { params: _params }: { params: Record<string, string> } = $props();

  let orders = $state<PurchaseOrder[]>([]);
  let loading = $state(true);
  let error = $state('');
  let search = $state(router.query.get('q') ?? '');
  let stateFilter = $state(router.query.get('state') ?? '');
  // The search and state the list shows.
  let applied = $state('');

  onMount(() => void load());

  // Each load is numbered, so a slow answer to an older one cannot replace
  // the list a newer one asked for.
  let loads = 0;

  function query(): string {
    const q = search.trim();
    const parts = [stateFilter ? `state=${encodeURIComponent(stateFilter)}` : '', q ? `q=${encodeURIComponent(q)}` : ''].filter(Boolean);
    return parts.length ? `?${parts.join('&')}` : '';
  }

  async function load() {
    const mine = ++loads;
    loading = true;
    error = '';
    const qs = query();
    applied = qs;
    try {
      const found = (await request<{ orders: PurchaseOrder[] }>('GET', `/purchase-orders${qs}`)).orders ?? [];
      if (mine !== loads) return;
      orders = found;
    } catch (err) {
      if (mine === loads) error = messageOf(err);
    } finally {
      if (mine === loads) loading = false;
    }
  }

  // Keep the search and state in the address, so they survive reloads and links.
  function apply(event?: SubmitEvent) {
    event?.preventDefault();
    navigate('/orders' + query(), true);
    void load();
  }
</script>

<div class="mb-4 flex flex-wrap items-center justify-between gap-2">
  <div>
    <h1 class="text-2xl font-bold">Orders</h1>
    <p class="text-sm text-zinc-600 dark:text-zinc-400">Purchase orders to vendors, newest first.</p>
  </div>
  {#if session.can('vendors.manage')}
    <div class="flex flex-wrap gap-2">
      <a class="btn-secondary" href="/orders/new?from=running-low">Start orders from what is low</a>
      <a class="btn-primary" href="/orders/new">New order</a>
    </div>
  {/if}
</div>

<form class="mb-4 flex flex-wrap items-end gap-3" role="search" onsubmit={apply}>
  <div>
    <label class="label" for="order-state">State</label>
    <select
      id="order-state"
      class="input"
      value={stateFilter}
      onchange={(e) => {
        stateFilter = e.currentTarget.value;
        apply();
      }}
    >
      <option value="">Any</option>
      <option value="open">Still to come</option>
      {#each orderStates as s (s.key)}<option value={s.key}>{s.label}</option>{/each}
    </select>
  </div>
  <div class="min-w-48 flex-1">
    <label class="label" for="order-search">Search</label>
    <input id="order-search" class="input" type="search" placeholder="Number, reference, vendor or line" bind:value={search} />
  </div>
  <button type="submit" class="btn-secondary">Search</button>
</form>

<ErrorMessage {error} />

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading orders…</p>
{:else if orders.length === 0}
  <p class="text-sm text-zinc-600 dark:text-zinc-400">{applied ? 'No orders match.' : 'No orders yet.'}</p>
{:else}
  <div class="overflow-x-auto">
    <table class="w-full text-left text-sm" aria-label="Orders">
      <thead>
        <tr class="border-b border-zinc-300 dark:border-zinc-700">
          <th scope="col" class="py-2 pr-4 font-semibold">Order</th>
          <th scope="col" class="py-2 pr-4 font-semibold">Vendor</th>
          <th scope="col" class="py-2 pr-4 font-semibold">State</th>
          <th scope="col" class="py-2 pr-4 font-semibold">Ordered</th>
          <th scope="col" class="py-2 pr-4 font-semibold">Expected</th>
          <th scope="col" class="py-2 font-semibold">Total</th>
        </tr>
      </thead>
      <tbody>
        {#each orders as o (o.id)}
          <tr class="border-b border-zinc-200 dark:border-zinc-800">
            <td class="pr-4"><a class="inline-flex min-h-11 min-w-11 items-center font-mono font-medium underline" href="/orders/{encodeURIComponent(o.id)}">{o.reference}</a></td>
            <td class="pr-4">{o.vendor_name}</td>
            <td class="pr-4">{stateLabel(o.state)}</td>
            <td class="pr-4">{o.ordered_on}</td>
            <td class="pr-4">
              {o.expected_on}{#if orderOverdue(o)}<span class="ml-1 font-medium text-amber-900 dark:text-amber-200">(overdue)</span>{/if}
            </td>
            <td>{#if o.prices_hidden}<span class="text-zinc-600 dark:text-zinc-400">Hidden</span>{:else}{moneyText(o.total, o.currency)}{/if}</td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}

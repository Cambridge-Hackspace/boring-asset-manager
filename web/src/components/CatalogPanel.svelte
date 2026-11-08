<script lang="ts">
  // What vendors sell (docs/vendors.md#catalogs): a vendor's catalog on its
  // page, or what each vendor sells of a category or an asset on theirs.
  // Entries are changed by those who manage their vendor.
  import { onMount } from 'svelte';
  import { request } from '../lib/api';
  import { entryHref, entryName, entryPrice, entryProblem, entryRequest, packText } from '../lib/catalog';
  import { messageOf } from '../lib/errors';
  import type { AssetWithDetails, CatalogEntry, CatalogEntryRequest, Category, PurchaseOrder, Vendor } from '../lib/models';
  import { onOrderText } from '../lib/orders';
  import { currencies } from '../lib/purchases';
  import { session } from '../lib/session.svelte';
  import ConfirmButton from './ConfirmButton.svelte';
  import ErrorMessage from './ErrorMessage.svelte';

  let {
    vendor,
    category,
    asset,
    canAdd = false,
    onchange,
  }: {
    /** The vendor whose catalog this is; or one of the others, what it is for. */
    vendor?: Vendor;
    category?: { id: string; name: string };
    asset?: { id: string; name: string };
    /** Whether to offer adding an entry. */
    canAdd?: boolean;
    /** After a change: what was done. */
    onchange?: (done: string) => void;
  } = $props();

  const onVendor = $derived(!!vendor);
  let entries = $state<CatalogEntry[]>([]);
  let loaded = $state(false);
  let error = $state('');
  let busy = $state(false);
  // What the form chooses from: vendors on a category's or an asset's page,
  // categories and assets on a vendor's.
  let vendors = $state<Vendor[]>([]);
  let categories = $state<Category[]>([]);
  let assetSearch = $state('');
  let found = $state<{ id: string; name: string }[] | null>(null);
  let chosenAsset = $state<{ id: string; name: string } | null>(null);

  // The entry being made (id '') or changed, or none.
  let editing = $state<{ id: string; draft: CatalogEntryRequest; pricesHidden: boolean } | null>(null);
  let packSize = $state('');
  const problem = $derived(editing ? entryProblem({ ...editing.draft, pack_size: packSize.trim() ? Number(packSize) : null }) : '');

  function query(): string {
    if (vendor) return `vendor_id=${encodeURIComponent(vendor.id)}`;
    if (category) return `category_id=${encodeURIComponent(category.id)}`;
    return `asset_id=${encodeURIComponent(asset?.id ?? '')}`;
  }

  async function load() {
    try {
      entries = (await request<{ entries: CatalogEntry[] }>('GET', `/catalog?${query()}`)).entries ?? [];
    } catch (err) {
      error = messageOf(err);
    } finally {
      loaded = true;
    }
  }

  // The open purchase orders with a line for a category or an asset
  // (docs/purchase-orders.md), so its page says what is on order.
  let onOrder = $state<PurchaseOrder[]>([]);

  onMount(() => {
    void load();
    if (category || asset) {
      const q = category ? `category_id=${encodeURIComponent(category.id)}` : `asset_id=${encodeURIComponent(asset?.id ?? '')}`;
      request<{ orders: PurchaseOrder[] }>('GET', `/purchase-orders?state=open&${q}`)
        .then((r) => (onOrder = r.orders ?? []))
        .catch(() => {});
    }
  });

  /** How an entry is named on this page: by what it is for on a vendor's, or by vendor on the others. */
  const label = (e: CatalogEntry) => (onVendor ? `${entryName(e)} for ${e.linked_name}` : `${entryName(e)} from ${e.vendor_name}`);

  async function startAdding() {
    error = '';
    chosenAsset = null;
    found = null;
    packSize = '';
    editing = {
      id: '',
      draft: entryRequest({ vendor_id: vendor?.id, category_id: category?.id, asset_id: asset?.id, preferred: entries.length === 0 }),
      pricesHidden: !session.can('purchases.read'),
    };
    try {
      if (onVendor && categories.length === 0) {
        categories = (await request<{ categories: Category[] }>('GET', '/categories')).categories?.filter((c) => c.id !== 'uncategorized') ?? [];
        categories.sort((a, b) => a.name.localeCompare(b.name));
      }
      if (!onVendor && vendors.length === 0) {
        vendors = (await request<{ vendors: Vendor[] }>('GET', '/vendors')).vendors ?? [];
      }
    } catch (err) {
      error = messageOf(err);
    }
  }

  function startEditing(e: CatalogEntry) {
    error = '';
    packSize = e.pack_size === null ? '' : String(e.pack_size);
    chosenAsset = e.asset_id ? { id: e.asset_id, name: e.linked_name } : null;
    found = null;
    editing = { id: e.id, draft: entryRequest(e), pricesHidden: !!e.prices_hidden };
    if (onVendor && categories.length === 0) {
      void request<{ categories: Category[] }>('GET', '/categories')
        .then((r) => (categories = (r.categories ?? []).filter((c) => c.id !== 'uncategorized').sort((a, b) => a.name.localeCompare(b.name))))
        .catch((err) => (error = messageOf(err)));
    }
  }

  async function findAssets() {
    const q = assetSearch.trim();
    if (!q) return;
    try {
      const list = (await request<{ assets: AssetWithDetails[] }>('GET', `/assets?search=${encodeURIComponent(q)}`)).assets ?? [];
      found = list.slice(0, 20).map((a) => ({ id: a.asset.id, name: a.asset.name }));
    } catch (err) {
      error = messageOf(err);
    }
  }

  function chooseAsset(a: { id: string; name: string } | null) {
    if (!editing) return;
    chosenAsset = a;
    editing.draft.asset_id = a?.id ?? '';
    if (a) editing.draft.category_id = '';
    found = null;
    assetSearch = '';
  }

  async function save(event: SubmitEvent) {
    event.preventDefault();
    if (!editing || problem) return;
    error = '';
    busy = true;
    const body = { ...editing.draft, pack_size: packSize.trim() ? Number(packSize) : null };
    try {
      if (editing.id) {
        await request('PUT', `/catalog/${encodeURIComponent(editing.id)}`, body);
        onchange?.('Catalog entry saved.');
      } else {
        await request('POST', '/catalog', body);
        onchange?.('Catalog entry added.');
      }
      editing = null;
      await load();
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }

  async function prefer(e: CatalogEntry) {
    error = '';
    try {
      await request('PUT', `/catalog/${encodeURIComponent(e.id)}`, { ...entryRequest(e), preferred: true });
      onchange?.(`${entryName(e)} from ${e.vendor_name} is preferred.`);
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function remove(e: CatalogEntry) {
    error = '';
    try {
      await request('DELETE', `/catalog/${encodeURIComponent(e.id)}`);
      onchange?.('Catalog entry removed.');
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }
</script>

<section aria-labelledby="catalog-heading" class="space-y-4">
  <h2 id="catalog-heading" class="section-title">Catalog</h2>
  <ErrorMessage {error} />
  {#if onOrder.length}
    <p class="flex flex-wrap gap-x-3 text-sm">
      {#each onOrder as o (o.id)}<a class="inline-flex min-h-11 items-center underline" href="/orders/{encodeURIComponent(o.id)}">{onOrderText(o)}</a>{/each}
    </p>
  {/if}
  {#if !loaded}
    <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading…</p>
  {:else if entries.length === 0}
    <p class="text-sm text-zinc-600 dark:text-zinc-400">
      {#if onVendor}Nothing is recorded of what it sells yet.{:else}No vendor’s part is recorded for it yet.{/if}
    </p>
  {:else}
    <ul class="divide-y divide-zinc-300 text-sm dark:divide-zinc-700" aria-label="Catalog entries">
      {#each entries as e (e.id)}
        {@const r = entryHref(e, vendor)}
        <li class="space-y-1 py-3" aria-label={label(e)}>
          <div class="flex flex-wrap items-center gap-x-3">
            {#if onVendor}
              <a class="inline-flex min-h-11 items-center font-medium underline" href={e.category_id ? `/categories/${encodeURIComponent(e.category_id)}` : `/assets/${encodeURIComponent(e.asset_id ?? '')}`}>{e.linked_name}</a>
            {:else}
              <a class="inline-flex min-h-11 items-center font-medium underline" href="/vendors/{encodeURIComponent(e.vendor_id)}">{e.vendor_name}</a>
            {/if}
            {#if e.part_number}<span class="font-mono">{e.part_number}</span>{/if}
            {#if e.preferred}<span class="rounded bg-[var(--color-highlight)] px-1.5 py-0.5 text-xs font-semibold text-zinc-900">Preferred</span>{/if}
          </div>
          {#if e.description}<p>{e.description}</p>{/if}
          <p class="text-zinc-600 dark:text-zinc-400">
            {#if entryPrice(e)}{entryPrice(e)}{#if e.price_checked}, checked {e.price_checked}{/if}{:else if packText(e)}{packText(e)}{/if}
            {#if e.prices_hidden}<span data-prices-hidden>Its price is hidden: seeing it needs See prices.</span>{/if}
          </p>
          <div class="flex flex-wrap items-center gap-2">
            {#if r.external}
              <a class="inline-flex min-h-11 items-center underline" href={r.href} rel="noopener noreferrer" target="_blank" aria-label="Product page of {label(e)}">Product page</a>
            {/if}
            {#if e.can_manage}
              {#if !e.preferred && !onVendor}
                <button type="button" class="btn-secondary" aria-label="Prefer {label(e)}" onclick={() => prefer(e)}>Make preferred</button>
              {/if}
              <button type="button" class="btn-secondary" aria-label="Edit {label(e)}" onclick={() => startEditing(e)}>Edit</button>
              <ConfirmButton label="Remove" question="Remove {label(e)}?" onconfirm={() => remove(e)} />
            {/if}
          </div>
        </li>
      {/each}
    </ul>
  {/if}

  {#if editing}
    <form class="card space-y-4" aria-label={editing.id ? 'Change the catalog entry' : 'Add a catalog entry'} onsubmit={save}>
      {#if !onVendor}
        <div>
          <label class="label" for="catalog-vendor">Vendor</label>
          <select id="catalog-vendor" class="input" required bind:value={editing.draft.vendor_id}>
            <option value="">Choose a vendor</option>
            {#each vendors as v (v.id)}<option value={v.id}>{v.name}</option>{/each}
            {#if editing.id && !vendors.some((v) => v.id === editing?.draft.vendor_id)}
              <option value={editing.draft.vendor_id}>{entries.find((x) => x.id === editing?.id)?.vendor_name}</option>
            {/if}
          </select>
        </div>
      {:else}
        <fieldset class="space-y-2">
          <legend class="label">What it is for</legend>
          <div>
            <label class="label" for="catalog-category">A category, for things counted by items</label>
            <select id="catalog-category" class="input" value={editing.draft.category_id} onchange={(ev) => { if (editing) { editing.draft.category_id = ev.currentTarget.value; if (ev.currentTarget.value) { editing.draft.asset_id = ''; chosenAsset = null; } } }}>
              <option value="">None</option>
              {#each categories as c (c.id)}<option value={c.id}>{c.name}</option>{/each}
            </select>
          </div>
          <p class="text-sm">
            {#if chosenAsset}Or the asset <strong>{chosenAsset.name}</strong>.
              <button type="button" class="inline-flex min-h-11 items-center underline" onclick={() => chooseAsset(null)}>Not an asset</button>
            {:else}Or an asset, for things counted by quantity:{/if}
          </p>
          <div class="flex flex-wrap items-end gap-2">
            <div class="grow">
              <label class="label" for="catalog-asset-search">Find an asset</label>
              <input id="catalog-asset-search" class="input" type="search" placeholder="By name or code" bind:value={assetSearch}
                onkeydown={(ev) => { if (ev.key === 'Enter') { ev.preventDefault(); void findAssets(); } }} />
            </div>
            <button type="button" class="btn-secondary" disabled={!assetSearch.trim()} onclick={findAssets}>Find</button>
          </div>
          {#if found}
            {#if found.length === 0}
              <p class="text-sm text-zinc-600 dark:text-zinc-400">No asset matches.</p>
            {:else}
              <ul class="text-sm" aria-label="Assets found">
                {#each found as a (a.id)}
                  <li><button type="button" class="inline-flex min-h-11 items-center underline" onclick={() => chooseAsset(a)}>Choose {a.name}</button></li>
                {/each}
              </ul>
            {/if}
          {/if}
        </fieldset>
      {/if}
      <div class="grid gap-4 sm:grid-cols-2">
        <div>
          <label class="label" for="catalog-part">Part number</label>
          <input id="catalog-part" class="input font-mono" maxlength="255" bind:value={editing.draft.part_number} />
        </div>
        <div>
          <label class="label" for="catalog-url">Product page (optional)</label>
          <input id="catalog-url" class="input" type="url" maxlength="2048" placeholder="https://" bind:value={editing.draft.url} />
        </div>
        <div class="sm:col-span-2">
          <label class="label" for="catalog-description">Description (optional with a part number)</label>
          <input id="catalog-description" class="input" maxlength="2000" bind:value={editing.draft.description} />
        </div>
        <div>
          <label class="label" for="catalog-pack-size">Pack size (optional)</label>
          <input id="catalog-pack-size" class="input" inputmode="numeric" placeholder="100" bind:value={packSize} />
        </div>
        <div>
          <label class="label" for="catalog-pack-unit">Pack unit (optional)</label>
          <input id="catalog-pack-unit" class="input" maxlength="64" placeholder="screws" bind:value={editing.draft.pack_unit} />
        </div>
        {#if editing.pricesHidden}
          <p class="text-sm text-zinc-600 dark:text-zinc-400 sm:col-span-2">Its price is hidden from you, and stays as it is.</p>
        {:else}
          <div>
            <label class="label" for="catalog-price">Price of a pack (optional)</label>
            <input id="catalog-price" class="input" inputmode="decimal" placeholder="12.50" bind:value={editing.draft.price} />
          </div>
          <div>
            <label class="label" for="catalog-currency">Currency</label>
            <input id="catalog-currency" class="input font-mono uppercase" maxlength="3" list="catalog-currencies" placeholder="GBP" bind:value={editing.draft.currency} />
            <datalist id="catalog-currencies">{#each currencies as c (c)}<option value={c}></option>{/each}</datalist>
          </div>
          <div>
            <label class="label" for="catalog-checked">Price checked on (optional)</label>
            <input id="catalog-checked" class="input" type="date" bind:value={editing.draft.price_checked} />
          </div>
        {/if}
      </div>
      <label class="flex min-h-11 items-center gap-2 text-sm">
        <input type="checkbox" bind:checked={editing.draft.preferred} /> Preferred: reorder from this one
      </label>
      {#if problem}<p class="text-sm text-amber-900 dark:text-amber-200">{problem}</p>{/if}
      <div class="flex flex-wrap gap-2">
        <button type="submit" class="btn-primary" disabled={busy || !!problem}>{editing.id ? 'Save entry' : 'Add entry'}</button>
        <button type="button" class="btn-secondary" onclick={() => (editing = null)}>Cancel</button>
      </div>
    </form>
  {:else if canAdd}
    <button type="button" class="btn-secondary" onclick={startAdding}>Add a catalog entry</button>
  {/if}
</section>

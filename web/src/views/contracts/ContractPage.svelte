<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { request } from '../../lib/api';
  import {
    billings,
    contractEnded,
    contractKinds,
    contractProblem,
    contractRequest,
    costText,
    describeContractChange,
    describeContractEvent,
    kindLabel,
    type ContractEvent,
  } from '../../lib/contracts';
  import { messageOf } from '../../lib/errors';
  import { renderMarkdown } from '../../lib/markdown';
  import { UNCATEGORIZED, type AssetWithDetails, type Category, type Contract, type ContractRequest, type NamedThing, type Vendor } from '../../lib/models';
  import { actorName } from '../../lib/people';
  import { currencies } from '../../lib/purchases';
  import { navigate, router } from '../../lib/router.svelte';
  import { session } from '../../lib/session.svelte';
  import { currentTab } from '../../lib/tabs';
  import AttachmentsPanel from '../../components/AttachmentsPanel.svelte';
  import CategoryPicker from '../../components/CategoryPicker.svelte';
  import ConfirmButton from '../../components/ConfirmButton.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';
  import TabLinks from '../../components/TabLinks.svelte';

  // /contracts/new and /contracts/:id (docs/contracts.md)
  let { params }: { params: Record<string, string> } = $props();

  // The app remounts the page when the path changes, so these never change.
  const id = untrack(() => params.id ?? '');
  const isNew = !id;

  let contract = $state<Contract | null>(null);
  // Whether the requester may change it: vendors.manage in its vendor's scopes.
  let canManage = $state(false);
  let vendors = $state<Vendor[]>([]);
  let categories = $state<Category[]>([]);
  let history = $state<ContractEvent[]>([]);
  let olderHistory = $state(false);
  let loading = $state(true);
  let error = $state('');
  let saved = $state('');
  let busy = $state(false);
  // Whether its details show as a form, to change them.
  let editing = $state(isNew);

  // The details form, and what it covers as chosen.
  let form = $state<ContractRequest>(contractRequest(null, untrack(() => router.query.get('vendor_id') ?? '')));
  let categoryIds = $state<string[]>([]);
  let assets = $state<NamedThing[]>([]);
  let assetSearch = $state('');
  let found = $state<NamedThing[] | null>(null);

  const problem = $derived(contractProblem(form));
  const categoryChoices = $derived(categories.filter((c) => c.id !== UNCATEGORIZED));

  onMount(() => {
    void load();
    window.addEventListener('beforeunload', warnUnsaved);
    return () => window.removeEventListener('beforeunload', warnUnsaved);
  });

  // The page saves in two places; what each held when loaded or last saved,
  // to say which have changes not saved, and warn before leaving.
  const sectionNames: Record<string, string> = { details: 'its details', covers: 'what it covers' };
  let savedAs = $state<Record<string, string>>({});
  const sectionState = $derived<Record<string, string>>({
    details: editing ? JSON.stringify(form) : (savedAs.details ?? ''),
    covers: JSON.stringify([[...categoryIds].sort(), assets.map((a) => a.id).sort()]),
  });
  const unsaved = $derived(isNew ? [] : Object.keys(savedAs).filter((k) => savedAs[k] !== sectionState[k]));

  // The page in tabs, kept in the address, like a vendor's.
  const tabs = $derived.by(() => {
    const all = [
      { key: 'details', label: 'Details' },
      { key: 'covers', label: 'Covers' },
      { key: 'attachments', label: 'Attachments' },
      { key: 'history', label: 'History' },
    ];
    const changed = new Set(unsaved);
    return all.map((t) => ({ key: t.key, label: changed.has(t.key) ? `${t.label} (not saved)` : t.label }));
  });
  const tab = $derived(currentTab(router.search, tabs.map((t) => t.key)));

  function warnUnsaved(event: BeforeUnloadEvent) {
    if (unsaved.length) event.preventDefault();
  }

  function fill(c: Contract) {
    form = contractRequest(c);
    categoryIds = [...(c.category_ids ?? [])];
    assets = [...(c.assets ?? [])];
  }

  function markSaved(...keys: string[]) {
    for (const k of keys) savedAs[k] = sectionState[k]!;
  }

  async function load() {
    error = '';
    try {
      vendors = await request<{ vendors: Vendor[] }>('GET', '/vendors').then((r) => r.vendors ?? [], () => []);
      categories = await request<{ categories: Category[] }>('GET', '/categories').then((r) => r.categories ?? [], () => []);
      if (isNew) return;
      const r = await request<{ contract: Contract; can_manage: boolean }>('GET', `/contracts/${encodeURIComponent(id)}`);
      contract = r.contract;
      canManage = r.can_manage;
      fill(r.contract);
      await loadHistory();
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
      if (!isNew) {
        savedAs.details = JSON.stringify(form);
        markSaved('covers');
      }
    }
  }

  // Loads the newest page of history, or with before, the page older than it.
  async function loadHistory(before?: string) {
    const query = before ? `?before=${encodeURIComponent(before)}` : '';
    const page = await request<{ events: ContractEvent[]; more: boolean }>('GET', `/contracts/${encodeURIComponent(id)}/history${query}`);
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

  // The contract's details as the API takes them; what it covers is left
  // out, and so kept, unless covers is set.
  function body(details: ContractRequest, covers: boolean): ContractRequest {
    const out: ContractRequest = { ...details, name: details.name.trim(), reference: details.reference.trim(), notice_days: Number(details.notice_days) || 0 };
    if (covers || isNew) {
      out.category_ids = categoryIds;
      out.asset_ids = assets.map((a) => a.id);
    }
    return out;
  }

  async function saveDetails(event: SubmitEvent) {
    event.preventDefault();
    error = saved = '';
    if (problem) {
      error = problem;
      return;
    }
    busy = true;
    try {
      if (isNew) {
        const res = await request<{ contract: Contract }>('POST', '/contracts', body(form, true));
        navigate(`/contracts/${encodeURIComponent(res.contract.id)}`, true);
        return;
      }
      const res = await request<{ contract: Contract }>('PUT', `/contracts/${encodeURIComponent(id)}`, body(form, false));
      contract = res.contract;
      form = contractRequest(res.contract);
      editing = false;
      savedAs.details = JSON.stringify(form);
      saved = 'Saved.';
      await loadHistory().catch(() => {});
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }

  async function saveCovers(event: SubmitEvent) {
    event.preventDefault();
    if (!contract) return;
    error = saved = '';
    busy = true;
    try {
      const res = await request<{ contract: Contract }>('PUT', `/contracts/${encodeURIComponent(id)}`, body(contractRequest(contract), true));
      contract = res.contract;
      categoryIds = [...(res.contract.category_ids ?? [])];
      assets = [...(res.contract.assets ?? [])];
      markSaved('covers');
      saved = 'What it covers is saved.';
      await loadHistory().catch(() => {});
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }

  async function findAssets(event: SubmitEvent) {
    event.preventDefault();
    error = '';
    try {
      const list = (await request<{ assets: AssetWithDetails[] }>('GET', `/assets?search=${encodeURIComponent(assetSearch.trim())}`)).assets ?? [];
      found = list.filter((a) => !assets.some((x) => x.id === a.asset.id)).slice(0, 20).map((a) => ({ id: a.asset.id, name: a.asset.name }));
    } catch (err) {
      error = messageOf(err);
    }
  }

  function addAsset(a: NamedThing) {
    assets = [...assets, a];
    found = found?.filter((x) => x.id !== a.id) ?? null;
  }

  function edit() {
    if (contract) form = contractRequest(contract);
    editing = true;
    savedAs.details = JSON.stringify(form);
  }

  // After its attachments change: what was done, and its history as it is now.
  function attachmentsChanged(done: string) {
    saved = done;
    void loadHistory().catch(() => {});
  }

  async function remove() {
    try {
      await request('DELETE', `/contracts/${encodeURIComponent(id)}`);
      navigate('/contracts');
    } catch (err) {
      error = messageOf(err);
    }
  }

  const when = (iso: string) => new Date(iso).toLocaleString();
  const categoryName = (cid: string) => categories.find((c) => c.id === cid)?.name ?? contract?.categories.find((c) => c.id === cid)?.name ?? cid;
</script>

<nav aria-label="Breadcrumb" class="mb-2 text-sm">
  <a class="underline" href="/contracts">Contracts</a>
</nav>

<div class="mb-4">
  <h1 class="text-3xl font-bold">{isNew ? 'New contract' : (contract?.name ?? 'Contract')}</h1>
  {#if contract}
    <p class="mt-1 text-sm text-zinc-600 dark:text-zinc-400">
      {kindLabel(contract.kind)} with <a class="underline" href="/vendors/{encodeURIComponent(contract.vendor_id)}">{contract.vendor_name}</a>{contract.end_date
        ? contractEnded(contract)
          ? `, ended ${contract.end_date}`
          : `, until ${contract.end_date}`
        : ''}.
    </p>
  {/if}
</div>
<ErrorMessage {error} />
{#if saved}<p class="mb-2 text-sm text-green-700 dark:text-green-400" role="status">{saved}</p>{/if}
{#if unsaved.length}
  <p class="mb-2 text-sm text-amber-900 dark:text-amber-200">
    <span aria-hidden="true">⚠ </span>Not saved yet: {unsaved.map((k) => sectionNames[k]).join(', ')}. Each part of this page saves with its own button.
  </p>
{/if}

{#snippet detailForm()}
  <form class="card max-w-2xl space-y-4" aria-label="Its details" onsubmit={saveDetails}>
    <div class="grid gap-4 sm:grid-cols-2">
      <div>
        <label class="label" for="contract-name">Name</label>
        <input id="contract-name" class="input" required maxlength="255" bind:value={form.name} />
      </div>
      <div>
        <label class="label" for="contract-vendor">Vendor</label>
        <select id="contract-vendor" class="input" required bind:value={form.vendor_id}>
          <option value="">Choose a vendor</option>
          {#each vendors as v (v.id)}<option value={v.id}>{v.name}</option>{/each}
        </select>
      </div>
      <div>
        <label class="label" for="contract-kind">Kind</label>
        <select id="contract-kind" class="input" bind:value={form.kind}>
          {#each contractKinds as k (k.key)}<option value={k.key}>{k.label}</option>{/each}
        </select>
      </div>
      <div>
        <label class="label" for="contract-reference">Reference (optional)</label>
        <input id="contract-reference" class="input font-mono" maxlength="255" placeholder="such as an agreement number" bind:value={form.reference} />
      </div>
      <div>
        <label class="label" for="contract-start">Starts (optional)</label>
        <input id="contract-start" class="input" type="date" bind:value={form.start_date} />
      </div>
      <div>
        <label class="label" for="contract-end">Ends (optional)</label>
        <input id="contract-end" class="input" type="date" bind:value={form.end_date} />
      </div>
    </div>
    <fieldset class="space-y-2">
      <legend class="label">Renewal</legend>
      <label class="flex items-center gap-2 text-sm"><input type="checkbox" bind:checked={form.auto_renews} /> Renews automatically unless notice is given</label>
      {#if form.auto_renews}
        <div class="max-w-48">
          <label class="label" for="contract-notice">Notice period, in days</label>
          <input id="contract-notice" class="input" type="number" min="0" max="3650" bind:value={form.notice_days} />
        </div>
      {/if}
    </fieldset>
    {#if contract?.costs_hidden}
      <p class="text-sm text-zinc-600 dark:text-zinc-400">Its cost is hidden from you, and stays as it is.</p>
    {:else}
      <div class="grid gap-4 sm:grid-cols-3">
        <div>
          <label class="label" for="contract-cost">Cost (optional)</label>
          <input id="contract-cost" class="input" inputmode="decimal" placeholder="120.00" bind:value={form.cost} />
        </div>
        <div>
          <label class="label" for="contract-currency">Currency</label>
          <input id="contract-currency" class="input uppercase" maxlength="3" list="contract-currencies" placeholder="GBP" bind:value={form.currency} />
          <datalist id="contract-currencies">{#each currencies as c (c)}<option value={c}></option>{/each}</datalist>
        </div>
        <div>
          <label class="label" for="contract-billing">Paid</label>
          <select id="contract-billing" class="input" bind:value={form.billing}>
            {#each billings as b (b.key)}<option value={b.key}>{b.label}</option>{/each}
          </select>
        </div>
      </div>
    {/if}
    <div>
      <label class="label" for="contract-notes">Notes (optional)</label>
      <textarea id="contract-notes" class="input" rows="5" aria-describedby="contract-notes-help" bind:value={form.notes}></textarea>
      <p id="contract-notes-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">Markdown: **bold**, *italics*, lists, and [links](https://example.org).</p>
    </div>
    {#if isNew}
      <p class="text-xs text-zinc-600 dark:text-zinc-400">Once it is made, its Covers tab chooses the assets and categories it covers, and its Attachments tab holds the contract itself.</p>
    {/if}
    <div class="flex flex-wrap gap-2">
      <button type="submit" class="btn-primary" disabled={busy}>{isNew ? 'Create contract' : 'Save'}</button>
      {#if !isNew}<button type="button" class="btn-secondary" onclick={() => (editing = false)}>Cancel</button>{/if}
    </div>
  </form>
{/snippet}

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading…</p>
{:else if isNew}
  {#if session.can('vendors.manage')}{@render detailForm()}{/if}
{:else if contract}
  <TabLinks label="Parts of this contract" {tabs} />

  <div class="mt-6 max-w-2xl space-y-6" id="tab-details" hidden={tab !== 'details'}>
    {#if editing && canManage}
      {@render detailForm()}
    {:else}
      <dl class="grid gap-x-6 gap-y-3 text-sm sm:grid-cols-[max-content_1fr]">
        <dt class="font-semibold">Kind</dt>
        <dd>{kindLabel(contract.kind)}</dd>
        <dt class="font-semibold">Vendor</dt>
        <dd><a class="inline-flex min-h-11 items-center underline" href="/vendors/{encodeURIComponent(contract.vendor_id)}">{contract.vendor_name}</a></dd>
        <dt class="font-semibold">Reference</dt>
        <dd>{#if contract.reference}<span class="font-mono">{contract.reference}</span>{:else}<span class="text-zinc-600 dark:text-zinc-400">None</span>{/if}</dd>
        <dt class="font-semibold">Starts</dt>
        <dd>{contract.start_date || 'Not said'}</dd>
        <dt class="font-semibold">Ends</dt>
        <dd>{contract.end_date || 'No end'}</dd>
        <dt class="font-semibold">Renewal</dt>
        <dd>
          {#if contract.auto_renews}
            Renews automatically{contract.notice_days ? `, unless notice is given ${contract.notice_days} day${contract.notice_days === 1 ? '' : 's'} before it ends, by ${contract.notice_deadline}` : ''}
          {:else}
            Does not renew by itself
          {/if}
        </dd>
        <dt class="font-semibold">Cost</dt>
        <dd>
          {#if contract.costs_hidden}<span class="text-zinc-600 dark:text-zinc-400">Hidden</span>{:else if contract.cost}{costText(contract)}{:else}<span class="text-zinc-600 dark:text-zinc-400">None</span>{/if}
        </dd>
      </dl>

      {#if contract.notes.trim()}
        <section aria-labelledby="notes-heading">
          <h2 id="notes-heading" class="mb-1 text-lg font-semibold">Notes</h2>
          <!-- Written by people, so rendered with everything escaped, and links only to the app, https or email (lib/markdown.ts). -->
          <div class="notes">
            <!-- eslint-disable-next-line svelte/no-at-html-tags -->
            {@html renderMarkdown(contract.notes)}
          </div>
        </section>
      {/if}

      {#if canManage}
        <div class="flex flex-wrap gap-2">
          <button type="button" class="btn-primary" onclick={edit}>Edit</button>
          <ConfirmButton label="Delete" question="Delete this contract? The assets it covers stay." onconfirm={remove} />
        </div>
      {/if}
    {/if}
  </div>

  <section class="mt-6 max-w-2xl space-y-4" id="tab-covers" aria-labelledby="covers-heading" hidden={tab !== 'covers'}>
    <h2 id="covers-heading" class="section-title">What it covers</h2>
    <p class="text-sm text-zinc-600 dark:text-zinc-400">The assets it names, and every asset in the categories it names or below them.</p>
    {#if canManage}
      <form class="space-y-4" aria-label="What it covers" onsubmit={saveCovers}>
        <CategoryPicker id="contract-categories" label="Categories" categories={categoryChoices} bind:value={categoryIds} />
        <div>
          <h3 class="label">Assets</h3>
          {#if assets.length === 0}
            <p class="text-sm text-zinc-600 dark:text-zinc-400">None named.</p>
          {:else}
            <ul class="divide-y divide-zinc-300 text-sm dark:divide-zinc-700" aria-label="Assets it names">
              {#each assets as a (a.id)}
                <li class="flex items-center justify-between gap-2">
                  <a class="inline-flex min-h-11 items-center underline" href="/assets/{encodeURIComponent(a.id)}">{a.name}</a>
                  <button type="button" class="btn-secondary" aria-label="Remove {a.name}" onclick={() => (assets = assets.filter((x) => x.id !== a.id))}>Remove</button>
                </li>
              {/each}
            </ul>
          {/if}
        </div>
        <button type="submit" class="btn-primary" disabled={busy}>Save what it covers</button>
      </form>
      <form class="flex flex-wrap items-end gap-2" role="search" aria-label="Find assets to cover" onsubmit={findAssets}>
        <div>
          <label class="label" for="contract-asset-search">Find an asset</label>
          <input id="contract-asset-search" class="input" type="search" placeholder="Name or code" bind:value={assetSearch} />
        </div>
        <button type="submit" class="btn-secondary">Find</button>
      </form>
      {#if found}
        {#if found.length === 0}
          <p class="text-sm text-zinc-600 dark:text-zinc-400">No other asset matches.</p>
        {:else}
          <ul class="divide-y divide-zinc-300 text-sm dark:divide-zinc-700" aria-label="Assets found">
            {#each found as a (a.id)}
              <li class="flex items-center justify-between gap-2">
                <span>{a.name}</span>
                <button type="button" class="btn-secondary" aria-label="Cover {a.name}" onclick={() => addAsset(a)}>Add</button>
              </li>
            {/each}
          </ul>
        {/if}
      {/if}
    {:else}
      <h3 class="label">Categories</h3>
      {#if contract.category_ids.length === 0}
        <p class="text-sm text-zinc-600 dark:text-zinc-400">None named.</p>
      {:else}
        <ul class="text-sm">
          {#each contract.category_ids as cid (cid)}<li><a class="inline-flex min-h-11 items-center underline" href="/categories/{encodeURIComponent(cid)}">{categoryName(cid)}</a></li>{/each}
        </ul>
      {/if}
      <h3 class="label">Assets</h3>
      {#if contract.assets.length === 0}
        <p class="text-sm text-zinc-600 dark:text-zinc-400">None named that you can see.</p>
      {:else}
        <ul class="text-sm">
          {#each contract.assets as a (a.id)}<li><a class="inline-flex min-h-11 items-center underline" href="/assets/{encodeURIComponent(a.id)}">{a.name}</a></li>{/each}
        </ul>
      {/if}
    {/if}
  </section>

  <div class="mt-6 max-w-2xl" id="tab-attachments" hidden={tab !== 'attachments'}>
    <AttachmentsPanel owner="contract" ownerId={id} ownerName={contract.name} canEdit={canManage} onchange={attachmentsChanged} />
  </div>

  <section class="mt-6 max-w-2xl" id="tab-history" aria-labelledby="history-heading" hidden={tab !== 'history'}>
    <h2 id="history-heading" class="section-title mb-3">History</h2>
    {#if history.length === 0}
      <p class="text-sm text-zinc-600 dark:text-zinc-400">No history yet.</p>
    {:else}
      <ul class="space-y-2 text-sm">
        {#each history as e (e.id)}
          <li>
            <span class="font-medium">{describeContractEvent(e)}</span>
            {#if e.payload.changes?.length}
              <ul class="list-disc pl-5">
                {#each e.payload.changes as c, i (i)}<li>{describeContractChange(c)}</li>{/each}
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

<style>
  .notes :global(h2),
  .notes :global(h3),
  .notes :global(h4) {
    margin: 1rem 0 0.5rem;
    font-weight: 600;
  }
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

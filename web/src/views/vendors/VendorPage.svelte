<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import { renderMarkdown } from '../../lib/markdown';
  import type { AssetWithDetails, Category, Scope, Vendor, VendorContact } from '../../lib/models';
  import { priceText } from '../../lib/purchases';
  import { actorName } from '../../lib/people';
  import { DEFAULT_SCOPE } from '../../lib/permissions';
  import { navigate, router } from '../../lib/router.svelte';
  import { session } from '../../lib/session.svelte';
  import { currentTab } from '../../lib/tabs';
  import { describeVendorChange, describeVendorEvent, telHref, websiteHref, type VendorEvent } from '../../lib/vendors';
  import AttachmentsPanel from '../../components/AttachmentsPanel.svelte';
  import CategoryBadge from '../../components/CategoryBadge.svelte';
  import ConfirmButton from '../../components/ConfirmButton.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';
  import TabLinks from '../../components/TabLinks.svelte';

  // /vendors/new and /vendors/:id (docs/vendors.md)
  let { params }: { params: Record<string, string> } = $props();

  // The app remounts the page when the path changes, so these never change.
  const id = untrack(() => params.id ?? '');
  const isNew = !id;

  let vendor = $state<Vendor | null>(null);
  // Whether the requester may change this vendor: vendors.manage in its scopes.
  let canManage = $state(false);
  // The categories that name it among the vendors they are bought from.
  let usedBy = $state<Category[]>([]);
  // The assets bought from it that the requester may read (#218).
  let bought = $state<AssetWithDetails[]>([]);
  // Scopes (docs/access.md): those who manage users, categories or vendors
  // see them; changing them takes vendors.manage in the scopes it ends up in.
  const canSeeScopes = $derived(session.canAny('users.manage', 'categories.manage', 'vendors.manage'));
  let scopes = $state<Scope[]>([]);
  let scopeIds = $state<string[]>([]);
  let history = $state<VendorEvent[]>([]);
  let olderHistory = $state(false);
  let loading = $state(true);
  let error = $state('');
  let saved = $state('');
  let busy = $state(false);
  // Whether its details show as a form, to change them.
  let editing = $state(isNew);

  // The form. Each contact has a key of its own while on the page, since
  // those not saved yet have no ID.
  type DraftContact = VendorContact & { key: string };
  let name = $state('');
  let website = $state('');
  let accountNumber = $state('');
  let address = $state('');
  let notes = $state('');
  let contacts = $state<DraftContact[]>([]);
  let nextKey = 0;
  const draft = (c: VendorContact): DraftContact => ({ ...c, key: c.id ?? `new-${nextKey++}` });

  onMount(() => {
    void load();
    window.addEventListener('beforeunload', warnUnsaved);
    return () => window.removeEventListener('beforeunload', warnUnsaved);
  });

  // The page saves in two places; what each held when loaded or last saved,
  // to say which have changes not saved, and warn before leaving.
  const sectionNames: Record<string, string> = { details: 'its details', scopes: 'who can see it' };
  const formState = () => JSON.stringify([name, website, accountNumber, address, notes, contacts.map(({ key: _key, ...c }) => c)]);
  let savedAs = $state<Record<string, string>>({});
  const sectionState = $derived<Record<string, string>>({
    // Its details count only while they show as a form.
    details: editing ? formState() : (savedAs.details ?? ''),
    scopes: JSON.stringify([...scopeIds].sort()),
  });
  const unsaved = $derived(isNew ? [] : Object.keys(savedAs).filter((k) => savedAs[k] !== sectionState[k]));

  // The page in tabs (#161), kept in the address. All stay on the page,
  // hidden but for one, so changes not saved in one survive looking at
  // another. Its catalog (#219) and its contracts (#221) are to join these
  // as tabs of their own.
  const tabOf: Record<string, string> = { details: 'details', scopes: 'scopes' };
  const tabs = $derived.by(() => {
    const all = [
      { key: 'details', label: 'Details', on: true },
      { key: 'assets', label: 'Assets', on: !isNew },
      { key: 'attachments', label: 'Attachments', on: true },
      { key: 'history', label: 'History', on: true },
      { key: 'scopes', label: 'Who can see it', on: canSeeScopes && scopes.length > 0 },
    ];
    const changed = new Set(unsaved.map((k) => tabOf[k]));
    return all.filter((t) => t.on).map((t) => ({ key: t.key, label: changed.has(t.key) ? `${t.label} (not saved)` : t.label }));
  });
  const tab = $derived(currentTab(router.search, tabs.map((t) => t.key)));

  function warnUnsaved(event: BeforeUnloadEvent) {
    if (unsaved.length) event.preventDefault();
  }

  // Fills the form from the vendor as saved.
  function fill(v: Vendor) {
    name = v.name;
    website = v.website;
    accountNumber = v.account_number;
    address = v.address;
    notes = v.notes;
    contacts = (v.contacts ?? []).map(draft);
    scopeIds = [...(v.scope_ids ?? [])];
  }

  function markSaved(...keys: string[]) {
    for (const k of keys) savedAs[k] = sectionState[k]!;
  }

  async function load() {
    error = '';
    try {
      if (canSeeScopes) {
        // Public goes on vendors; default cannot.
        scopes = await request<{ scopes: Scope[] }>('GET', '/scopes')
          .then((r) => (r.scopes ?? []).filter((s) => s.name !== DEFAULT_SCOPE))
          .catch(() => []);
      }
      if (isNew) return;
      const r = await request<{ vendor: Vendor; can_manage: boolean }>('GET', `/vendors/${encodeURIComponent(id)}`);
      vendor = r.vendor;
      canManage = r.can_manage;
      fill(r.vendor);
      usedBy = await request<{ categories: Category[] }>('GET', '/categories')
        .then((c) => (c.categories ?? []).filter((x) => x.vendor_ids?.includes(id)).sort((a, b) => a.name.localeCompare(b.name)))
        .catch(() => []);
      bought = await request<{ assets: AssetWithDetails[] }>('GET', `/assets?vendor_id=${encodeURIComponent(id)}`)
        .then((a) => (a.assets ?? []).sort((x, y) => x.asset.name.localeCompare(y.asset.name)))
        .catch(() => []);
      await loadHistory();
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
      // As loaded, each part is as saved.
      if (!isNew) {
        savedAs.details = formState();
        markSaved('scopes');
      }
    }
  }

  // Loads the newest page of history, or with before, the page older than it.
  async function loadHistory(before?: string) {
    const query = before ? `?before=${encodeURIComponent(before)}` : '';
    const page = await request<{ events: VendorEvent[]; more: boolean }>('GET', `/vendors/${encodeURIComponent(id)}/history${query}`);
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

  // The vendor as the API takes it: everything, since saving replaces it.
  function body(from: { details: boolean; scopes: boolean }) {
    const v = vendor;
    const details = from.details || !v
      ? { name: name.trim(), website: website.trim(), account_number: accountNumber.trim(), address, notes, contacts: contacts.map(({ key: _key, ...c }) => c) }
      : { name: v.name, website: v.website, account_number: v.account_number, address: v.address, notes: v.notes, contacts: v.contacts };
    // Those who do not see scopes leave them as they are.
    return canSeeScopes ? { ...details, scope_ids: from.scopes || !v ? scopeIds : v.scope_ids } : details;
  }

  async function saveDetails(event: SubmitEvent) {
    event.preventDefault();
    error = saved = '';
    busy = true;
    try {
      if (isNew) {
        const res = await request<{ vendor: Vendor }>('POST', '/vendors', body({ details: true, scopes: true }));
        navigate(`/vendors/${encodeURIComponent(res.vendor.id)}`, true);
        return;
      }
      const res = await request<{ vendor: Vendor }>('PUT', `/vendors/${encodeURIComponent(id)}`, body({ details: true, scopes: false }));
      vendor = res.vendor;
      const keptScopes = scopeIds;
      fill(res.vendor);
      // Scopes ticked but not saved stay ticked.
      scopeIds = keptScopes;
      editing = false;
      savedAs.details = formState();
      saved = 'Saved.';
      await loadHistory().catch(() => {});
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }

  async function saveScopes(event: SubmitEvent) {
    event.preventDefault();
    error = saved = '';
    try {
      vendor = (await request<{ vendor: Vendor }>('PUT', `/vendors/${encodeURIComponent(id)}`, body({ details: false, scopes: true }))).vendor;
      scopeIds = [...(vendor.scope_ids ?? [])];
      markSaved('scopes');
      saved = 'Scopes saved.';
      await loadHistory().catch(() => {});
    } catch (err) {
      error = messageOf(err);
    }
  }

  function edit() {
    if (vendor) {
      const keptScopes = scopeIds;
      fill(vendor);
      scopeIds = keptScopes;
    }
    editing = true;
    savedAs.details = formState();
  }

  function cancel() {
    editing = false;
  }

  function addContact() {
    contacts = [...contacts, draft({ name: '', role: '', email: '', phone: '' })];
  }

  function moveContact(i: number, by: number) {
    const next = [...contacts];
    [next[i], next[i + by]] = [next[i + by]!, next[i]!];
    contacts = next;
  }

  // After its attachments change: what was done, and its history as it is now.
  function attachmentsChanged(done: string) {
    saved = done;
    void loadHistory().catch(() => {});
  }

  async function remove() {
    try {
      await request('DELETE', `/vendors/${encodeURIComponent(id)}`);
      navigate('/vendors');
    } catch (err) {
      error = messageOf(err);
    }
  }

  const when = (iso: string) => new Date(iso).toLocaleString();
  const contactName = (c: VendorContact, i: number) => c.name.trim() || `contact ${i + 1}`;
</script>

<nav aria-label="Breadcrumb" class="mb-2 text-sm">
  <a class="underline" href="/vendors">Vendors</a>
</nav>

<div class="mb-4">
  <h1 class="text-3xl font-bold">{isNew ? 'New vendor' : (vendor?.name ?? 'Vendor')}</h1>
  {#if vendor && usedBy.length}
    <p class="mt-1 text-sm text-zinc-600 dark:text-zinc-400">Preferred for {usedBy.length} categor{usedBy.length === 1 ? 'y' : 'ies'}.</p>
  {/if}
</div>
<ErrorMessage {error} />
{#if saved}<p class="mb-2 text-sm text-green-700 dark:text-green-400" role="status">{saved}</p>{/if}
{#if unsaved.length}
  <p class="mb-2 text-sm text-amber-900 dark:text-amber-200">
    <span aria-hidden="true">⚠ </span>Not saved yet: {unsaved.map((k) => sectionNames[k]).join(', ')}. Each part of this page saves with its own button.
  </p>
{/if}

{#snippet scopeBoxes(disabled: boolean)}
  {#each scopes as s (s.id)}
    <label class="flex items-center gap-2 text-sm">
      <input type="checkbox" value={s.id} bind:group={scopeIds} {disabled} /> <span class="font-mono">{s.name}</span>{#if s.name === 'public'}<span class="text-zinc-600 dark:text-zinc-400">: everyone signed in</span>{/if}
    </label>
  {/each}
{/snippet}

{#snippet detailForm()}
  <form class="card max-w-2xl space-y-4" aria-label="Its details" onsubmit={saveDetails}>
    <div class="grid gap-4 sm:grid-cols-2">
      <div>
        <label class="label" for="vendor-name">Name</label>
        <input id="vendor-name" class="input" required maxlength="255" bind:value={name} />
      </div>
      <div>
        <label class="label" for="vendor-website">Website (optional)</label>
        <input id="vendor-website" class="input" type="url" maxlength="2048" placeholder="https://" bind:value={website} />
      </div>
      <div>
        <label class="label" for="vendor-account">Account number (optional)</label>
        <input id="vendor-account" class="input font-mono" maxlength="255" bind:value={accountNumber} />
      </div>
      <div>
        <label class="label" for="vendor-address">Address (optional)</label>
        <textarea id="vendor-address" class="input" rows="3" bind:value={address}></textarea>
      </div>
    </div>
    <div>
      <label class="label" for="vendor-notes">Notes (optional)</label>
      <textarea id="vendor-notes" class="input" rows="5" aria-describedby="notes-help" bind:value={notes}></textarea>
      <p id="notes-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">Markdown: **bold**, *italics*, lists, and [links](https://example.org).</p>
    </div>
    <fieldset class="space-y-3">
      <legend class="label">Contacts</legend>
      {#if contacts.length === 0}
        <p class="text-sm text-zinc-600 dark:text-zinc-400">None yet.</p>
      {/if}
      <ol class="space-y-3">
        {#each contacts as c, i (c.key)}
          <li class="rounded border border-zinc-300 p-3 dark:border-zinc-700">
            <div class="grid gap-3 sm:grid-cols-2">
              <div>
                <label class="label" for="contact-{c.key}-name">Name</label>
                <input id="contact-{c.key}-name" class="input" maxlength="255" bind:value={c.name} />
              </div>
              <div>
                <label class="label" for="contact-{c.key}-role">Role (optional)</label>
                <input id="contact-{c.key}-role" class="input" maxlength="255" placeholder="such as sales or support" bind:value={c.role} />
              </div>
              <div>
                <label class="label" for="contact-{c.key}-email">Email (optional)</label>
                <input id="contact-{c.key}-email" class="input" type="email" maxlength="255" bind:value={c.email} />
              </div>
              <div>
                <label class="label" for="contact-{c.key}-phone">Phone (optional)</label>
                <input id="contact-{c.key}-phone" class="input" type="tel" maxlength="64" bind:value={c.phone} />
              </div>
            </div>
            <div class="mt-2 flex flex-wrap gap-2">
              <button type="button" class="btn-secondary" aria-label="Move {contactName(c, i)} up" disabled={i === 0} onclick={() => moveContact(i, -1)}>↑</button>
              <button type="button" class="btn-secondary" aria-label="Move {contactName(c, i)} down" disabled={i === contacts.length - 1} onclick={() => moveContact(i, 1)}>↓</button>
              <button type="button" class="btn-secondary" aria-label="Remove {contactName(c, i)}" onclick={() => (contacts = contacts.filter((x) => x.key !== c.key))}>Remove</button>
            </div>
          </li>
        {/each}
      </ol>
      <button type="button" class="btn-secondary" onclick={addContact}>Add a contact</button>
      <p class="text-xs text-zinc-600 dark:text-zinc-400">The first is the one the list of vendors shows.</p>
    </fieldset>
    {#if isNew && canSeeScopes && scopes.length}
      <fieldset class="space-y-2">
        <legend class="label">Who can see it</legend>
        <p class="text-xs text-zinc-600 dark:text-zinc-400">
          Those who read assets in a scope ticked. In none, everyone who reads assets sees it, and only those who manage vendors
          everywhere may make it so.
        </p>
        {@render scopeBoxes(false)}
      </fieldset>
    {/if}
    <div class="flex flex-wrap gap-2">
      <button type="submit" class="btn-primary" disabled={busy}>{isNew ? 'Create vendor' : 'Save'}</button>
      {#if !isNew}<button type="button" class="btn-secondary" onclick={cancel}>Cancel</button>{/if}
    </div>
  </form>
{/snippet}

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading…</p>
{:else if isNew}
  {#if session.can('vendors.manage')}{@render detailForm()}{/if}
{:else if vendor}
  <TabLinks label="Parts of this vendor" {tabs} />

  <div class="mt-6 max-w-2xl space-y-6" id="tab-details" hidden={tab !== 'details'}>
    {#if editing && canManage}
      {@render detailForm()}
    {:else}
      {@const site = websiteHref(vendor.website)}
      <dl class="grid gap-x-6 gap-y-3 text-sm sm:grid-cols-[max-content_1fr]">
        <dt class="font-semibold">Website</dt>
        <dd>{#if site}<a class="inline-flex min-h-11 items-center underline" href={site} rel="noopener noreferrer" target="_blank">{vendor.website}</a>{:else}<span class="text-zinc-600 dark:text-zinc-400">None</span>{/if}</dd>
        <dt class="font-semibold">Account number</dt>
        <dd>{#if vendor.account_number}<span class="font-mono">{vendor.account_number}</span>{:else}<span class="text-zinc-600 dark:text-zinc-400">None</span>{/if}</dd>
        <dt class="font-semibold">Address</dt>
        <dd class="whitespace-pre-line">{#if vendor.address}{vendor.address}{:else}<span class="text-zinc-600 dark:text-zinc-400">None</span>{/if}</dd>
      </dl>

      {#if vendor.notes.trim()}
        <section aria-labelledby="notes-heading">
          <h2 id="notes-heading" class="mb-1 text-lg font-semibold">Notes</h2>
          <!-- Written by people, so rendered with everything escaped, and links only to the app, https or email (lib/markdown.ts). -->
          <div class="notes">
            <!-- eslint-disable-next-line svelte/no-at-html-tags -->
            {@html renderMarkdown(vendor.notes)}
          </div>
        </section>
      {/if}

      <section aria-labelledby="contacts-heading">
        <h2 id="contacts-heading" class="mb-1 text-lg font-semibold">Contacts</h2>
        {#if vendor.contacts.length === 0}
          <p class="text-sm text-zinc-600 dark:text-zinc-400">No contacts yet.</p>
        {:else}
          <ul class="divide-y divide-zinc-300 text-sm dark:divide-zinc-700">
            {#each vendor.contacts as c, i (c.id ?? i)}
              <li class="flex flex-wrap items-center gap-x-4 py-2">
                <span><span class="font-medium">{c.name || 'Someone'}</span>{#if c.role}<span class="text-zinc-600 dark:text-zinc-400"> · {c.role}</span>{/if}</span>
                {#if c.email}<a class="inline-flex min-h-11 items-center underline" href="mailto:{c.email}">{c.email}</a>{/if}
                {#if c.phone}<a class="inline-flex min-h-11 items-center underline" href={telHref(c.phone)}>{c.phone}</a>{/if}
              </li>
            {/each}
          </ul>
        {/if}
      </section>

      {#if canManage}
        <div class="flex flex-wrap gap-2">
          <button type="button" class="btn-primary" onclick={edit}>Edit</button>
          <ConfirmButton label="Delete" question="Delete this vendor? Categories stop naming it." onconfirm={remove} />
        </div>
      {/if}
    {/if}

    <section aria-labelledby="used-by-heading">
      <h2 id="used-by-heading" class="mb-1 text-lg font-semibold">Preferred for</h2>
      {#if usedBy.length}
        <ul class="flex flex-wrap gap-2">
          {#each usedBy as c (c.id)}<li><CategoryBadge category={c} href="/categories/{encodeURIComponent(c.id)}" /></li>{/each}
        </ul>
      {:else}
        <p class="text-sm text-zinc-600 dark:text-zinc-400">No category names it yet. A category’s Vendors tab chooses those it is bought from.</p>
      {/if}
    </section>
  </div>

  <section class="mt-6 max-w-3xl" id="tab-assets" aria-labelledby="bought-heading" hidden={tab !== 'assets'}>
    <h2 id="bought-heading" class="section-title mb-3">Bought from it</h2>
    {#if bought.length === 0}
      <p class="text-sm text-zinc-600 dark:text-zinc-400">No asset you can see was bought from it. An asset's Purchase, in its form, names its vendor.</p>
    {:else}
      <table class="w-full text-left text-sm">
        <thead>
          <tr class="border-b border-zinc-300 dark:border-zinc-700">
            <th scope="col" class="py-2 pr-4 font-semibold">Asset</th>
            <th scope="col" class="py-2 pr-4 font-semibold">Ordered</th>
            <th scope="col" class="py-2 pr-4 font-semibold">Price</th>
            <th scope="col" class="py-2 font-semibold">Warranty ends</th>
          </tr>
        </thead>
        <tbody>
          {#each bought as a (a.asset.id)}
            <tr class="border-b border-zinc-200 dark:border-zinc-800">
              <td class="pr-4"><a class="inline-flex min-h-11 items-center underline" href="/assets/{encodeURIComponent(a.asset.id)}">{a.asset.name}</a></td>
              <td class="pr-4">{a.purchase?.order_date || a.purchase?.received_date || ''}</td>
              <td class="pr-4">{priceText(a.purchase)}</td>
              <td>{a.purchase?.warranty_end ?? ''}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </section>

  <div class="mt-6 max-w-2xl" id="tab-attachments" hidden={tab !== 'attachments'}>
    <AttachmentsPanel owner="vendor" ownerId={id} ownerName={vendor.name} canEdit={canManage} onchange={attachmentsChanged} />
  </div>

  <section class="mt-6 max-w-2xl" id="tab-history" aria-labelledby="history-heading" hidden={tab !== 'history'}>
    <h2 id="history-heading" class="section-title mb-3">History</h2>
    {#if history.length === 0}
      <p class="text-sm text-zinc-600 dark:text-zinc-400">No history yet.</p>
    {:else}
      <ul class="space-y-2 text-sm">
        {#each history as e (e.id)}
          <li>
            <span class="font-medium">{describeVendorEvent(e)}</span>
            {#if e.payload.changes?.length}
              <ul class="list-disc pl-5">
                {#each e.payload.changes as c, i (i)}<li>{describeVendorChange(c)}</li>{/each}
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

  {#if canSeeScopes && scopes.length}
    <section class="mt-6 max-w-2xl" id="tab-scopes" aria-labelledby="scopes-heading" hidden={tab !== 'scopes'}>
      <h2 id="scopes-heading" class="mb-1 text-lg font-semibold">Who can see it</h2>
      <p class="mb-2 text-sm text-zinc-600 dark:text-zinc-400">
        Those who read assets in a scope ticked see it, and those who manage vendors there change it. In no scope, everyone who
        reads assets sees it, and only those who manage vendors in every scope may change it. Ticking <strong>public</strong> shows
        it to everyone signed in.
      </p>
      <form class="space-y-2" onsubmit={saveScopes}>
        {@render scopeBoxes(!canManage)}
        {#if canManage}<button type="submit" class="btn-secondary">Save scopes</button>{/if}
      </form>
    </section>
  {/if}
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
  .notes :global(code) {
    font-family: ui-monospace, monospace;
    font-size: 0.8125rem;
  }
</style>

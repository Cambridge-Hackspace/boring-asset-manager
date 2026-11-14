<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../lib/api';
  import { messageOf } from '../lib/errors';
  import { dueDate } from '../lib/assets';
  import type { AssetWithDetails, CatalogEntry, Category, Favorite, FavoriteKind, Loan, PurchaseOrder, ReviewSummary, Vendor } from '../lib/models';
  import { entryHref, entryPrice, preferredByThing } from '../lib/catalog';
  import { onOrderByThing, onOrderText } from '../lib/orders';
  import { favorites } from '../lib/favorites.svelte';
  import { navSections, type NavLink } from '../lib/nav';
  import { navigate } from '../lib/router.svelte';
  import { scanTarget } from '../lib/scan';
  import { focusOnOpen, isSearchKey, searchHref } from '../lib/search';
  import { displayName } from '../lib/people';
  import { session } from '../lib/session.svelte';
  import { routes } from '../routes';
  import { reorderHref } from '../lib/vendors';
  import CategoryBadge from '../components/CategoryBadge.svelte';
  import ConditionBadge from '../components/ConditionBadge.svelte';
  import ErrorMessage from '../components/ErrorMessage.svelte';
  import SetupChecklist from '../components/SetupChecklist.svelte';
  import InstallApp from '../components/InstallApp.svelte';

  let { params: _params }: { params: Record<string, string> } = $props();

  const sections = $derived(navSections(routes, (...p) => session.canAny(...p)));

  let low = $state<Category[] | null>(null);
  let lowAssets = $state<AssetWithDetails[]>([]);
  // The vendors running-low categories are bought from (docs/vendors.md), by ID.
  let vendors = $state<Map<string, Vendor>>(new Map());
  const vendorsOf = (c: Category) => (c.vendor_ids ?? []).map((id) => vendors.get(id)).filter((v): v is Vendor => !!v);
  // The preferred catalog entry of each running-low category and asset
  // (docs/vendors.md#catalogs), by its ID.
  let preferred = $state<Map<string, CatalogEntry>>(new Map());
  // The open purchase order each running-low category and asset is on
  // (docs/purchase-orders.md), by its ID, so nothing is ordered twice.
  let onOrder = $state<Map<string, PurchaseOrder>>(new Map());
  let attention = $state<AssetWithDetails[]>([]);
  // What waits for the person to review (docs/review.md), where any
  // category uses review states.
  let toReview = $state<ReviewSummary | null>(null);
  const reviewLines = $derived(
    toReview
      ? [
          { n: toReview.overdue, label: 'reviews overdue', href: '/assets?review=overdue' },
          { n: toReview.due, label: 'reviews due', href: '/assets?review=due' },
          { n: toReview.proposals, label: 'with assistant changes to accept or reject', href: '/assets?review=proposed' },
          { n: toReview.drafts, label: 'drafts to confirm', href: '/assets?review=draft' },
          { n: toReview.to_verify, label: 'confirmed, to verify', href: '/assets?review=confirmed' },
        ].filter((l) => l.n > 0)
      : [],
  );
  // Out on loan, and due back within the week or overdue.
  let due = $state<Loan[]>([]);
  let error = $state('');
  let code = $state('');

  // The person's favorites (docs/favorites.md), by kind, each by name;
  // archived assets are left out.
  const favoriteKinds: { kind: FavoriteKind; label: string }[] = [
    { kind: 'asset', label: 'Assets' },
    { kind: 'category', label: 'Categories' },
    { kind: 'report', label: 'Reports' },
    { kind: 'flow', label: 'Flows' },
  ];
  let favoritesRead = $state(false);
  const starred = $derived(
    favoriteKinds
      .map(({ kind, label }) => ({
        kind,
        label,
        list: favorites.list.filter((f) => f.kind === kind && !f.archived).sort((a, b) => a.name.localeCompare(b.name)),
      }))
      .filter((g) => g.list.length),
  );

  function favoriteHref(f: Favorite): string {
    switch (f.kind) {
      case 'asset':
        return `/assets/${encodeURIComponent(f.id)}`;
      case 'category':
        return `/categories/${encodeURIComponent(f.id)}`;
      case 'report':
        return `/reports/${encodeURIComponent(f.id)}`;
      case 'flow':
        return `/f/${encodeURIComponent(f.slug ?? '')}`;
    }
  }

  function lookUp(event: SubmitEvent) {
    event.preventDefault();
    const to = scanTarget(code);
    if (to) navigate(to);
  }

  // Search (docs/data-model.md#search), handed to the asset list.
  let words = $state('');
  let searchField = $state<HTMLInputElement>();
  function search(event: SubmitEvent) {
    event.preventDefault();
    navigate(searchHref(words));
  }
  function searchKey(e: KeyboardEvent) {
    if (!searchField || !isSearchKey(e)) return;
    e.preventDefault();
    searchField.focus();
  }

  onMount(() => {
    // After the page is in place, so that moving focus to the page on
    // arrival doesn't take it straight back.
    if (searchField && focusOnOpen()) setTimeout(() => searchField?.focus());
  });

  onMount(async () => {
    favorites
      .load(true)
      .then(() => (favoritesRead = true))
      .catch((err) => (error = messageOf(err)));
    request<{ loans: Loan[] }>('GET', '/loans?state=open')
      .then((r) => {
        const week = Date.now() + 7 * 24 * 3600 * 1000;
        due = (r.loans ?? []).filter((l) => l.overdue || (l.due_at && Date.parse(l.due_at) <= week)).sort((a, b) => (a.due_at ?? '').localeCompare(b.due_at ?? ''));
      })
      .catch(() => (due = []));
    if (!session.can('assets.read')) return;
    try {
      const [all, assets] = await Promise.all([
        request<{ categories: Category[] }>('GET', '/categories').then((r) => r.categories ?? []),
        request<{ assets: AssetWithDetails[] }>('GET', '/assets').then((r) => r.assets ?? []),
      ]);
      lowAssets = assets.filter((a) => a.asset.stock?.is_running_low).sort((a, b) => a.asset.name.localeCompare(b.asset.name));
      attention = assets.filter((a) => a.asset.condition !== 'ok').sort((a, b) => a.asset.name.localeCompare(b.asset.name));
      low = all.filter((c) => c.is_consumable && c.is_running_low).sort((a, b) => a.name.localeCompare(b.name));
      if (all.some((c) => c.effective_review?.enabled) && session.canAny('assets.confirm', 'assets.verify', 'assets.write')) {
        toReview = await request<ReviewSummary>('GET', '/review').catch(() => null);
      }
      // Read only when something is low; the list stands without them.
      if (low.length || lowAssets.length) {
        preferred = preferredByThing(
          await request<{ entries: CatalogEntry[] }>('GET', '/catalog?preferred=true')
            .then((r) => r.entries ?? [])
            .catch(() => []),
        );
        onOrder = onOrderByThing(
          await request<{ orders: PurchaseOrder[] }>('GET', '/purchase-orders?state=open')
            .then((r) => r.orders ?? [])
            .catch(() => []),
        );
      }
      if (low.some((c) => c.vendor_ids?.length) || [...preferred.values()].some((e) => !e.url)) {
        const found = await request<{ vendors: Vendor[] }>('GET', '/vendors')
          .then((r) => r.vendors ?? [])
          .catch(() => []);
        vendors = new Map(found.map((v) => [v.id, v]));
      }
    } catch (err) {
      error = messageOf(err);
    }
  });
</script>

<svelte:window onkeydown={searchKey} />

<div class="space-y-8">
  <div>
    <h1 class="text-3xl font-bold tracking-tight">Welcome</h1>
    <p class="mt-1 text-zinc-600 dark:text-zinc-400">Signed in as {session.user ? displayName(session.user) : ''}.</p>
  </div>

  <ErrorMessage {error} />
  {#if session.canAny('users.manage', 'settings.manage')}<SetupChecklist />{/if}
  {#if session.can('assets.read')}
    <form class="flex max-w-2xl flex-wrap items-end gap-3" role="search" aria-label="Search assets" onsubmit={search}>
      <div class="min-w-60 grow">
        <label class="label" for="home-search">Search</label>
        <input id="home-search" class="input text-base" type="search" autocomplete="off" placeholder="Name, code, or where it is" aria-keyshortcuts="/" bind:value={words} bind:this={searchField} />
      </div>
      <button type="submit" class="btn-primary">Search</button>
    </form>
  {/if}
  {#if session.can('assets.scan')}
    <form class="flex max-w-2xl flex-wrap items-end gap-3" role="search" onsubmit={lookUp}>
      <div class="min-w-60 grow">
        <label class="label" for="home-code">Look up a code</label>
        <input id="home-code" class="input font-mono text-base" autocomplete="off" placeholder="HS-100" bind:value={code} />
      </div>
      <button type="submit" class="btn-primary" disabled={!code.trim()}>Look up</button>
      <a class="btn-secondary" href="/scan">Scan with the camera</a>
    </form>
  {/if}

  <InstallApp />
  {#if favoritesRead || low || attention.length || due.length || reviewLines.length}
    <div class="grid gap-6 md:grid-cols-2 xl:grid-cols-3">
      {#if favoritesRead}
        <section class="card" aria-labelledby="favorites-heading">
          <h2 id="favorites-heading" class="mb-2 text-lg font-bold"><span class="shadow-[inset_0_-0.38em_0_var(--color-highlight)] dark:shadow-none">Favorites</span></h2>
          {#if starred.length === 0}
            <p class="text-sm text-zinc-600 dark:text-zinc-400">Star an asset, category, report or flow on its page to find it here.</p>
          {:else}
            <div class="space-y-3">
              {#each starred as group (group.kind)}
                <div>
                  <h3 class="text-sm font-semibold text-zinc-600 dark:text-zinc-400">{group.label}</h3>
                  <ul class="divide-y divide-zinc-300 text-sm dark:divide-zinc-700">
                    {#each group.list as f (f.id)}
                      <li><a class="flex min-h-11 items-center underline" href={favoriteHref(f)}>{f.name}</a></li>
                    {/each}
                  </ul>
                </div>
              {/each}
            </div>
          {/if}
        </section>
      {/if}
      {#if low}
        <section class="card" aria-labelledby="low-heading">
          <h2 id="low-heading" class="mb-2 text-lg font-bold"><span class="shadow-[inset_0_-0.38em_0_var(--color-highlight)] dark:shadow-none">Running low</span></h2>
          {#if (low.length || lowAssets.length) && session.can('vendors.manage')}
            <p class="mb-2"><a class="inline-flex min-h-11 items-center font-medium underline" href="/orders/new?from=running-low">Start orders…</a></p>
          {/if}
          {#if low.length === 0 && lowAssets.length === 0}
            <p class="text-sm text-zinc-600 dark:text-zinc-400">Nothing is running low.</p>
          {:else}
            <ul class="divide-y divide-zinc-300 text-sm dark:divide-zinc-700">
              <!-- The phone's shopping list (#214) does not exist yet; when it does, it shows each preferred catalog entry, what is on order, and each category's vendors and reorder links, as this does. -->
              {#snippet entry(e: CatalogEntry, what: string)}
                {@const r = entryHref(e, vendors.get(e.vendor_id))}
                <p class="flex w-full flex-wrap items-center gap-x-2 text-zinc-600 dark:text-zinc-400" data-preferred-entry>
                  <span>Preferred: <a class="inline-flex min-h-11 items-center underline" href="/vendors/{encodeURIComponent(e.vendor_id)}">{e.vendor_name}</a></span>
                  {#if e.part_number}<span class="font-mono">{e.part_number}</span>{/if}
                  {#if entryPrice(e)}<span>{entryPrice(e)}</span>{/if}
                  {#if r.external}
                    <a class="inline-flex min-h-11 items-center font-medium underline" href={r.href} rel="noopener noreferrer" target="_blank" aria-label="Reorder part {e.part_number || e.description} of {what} from {e.vendor_name}">Reorder</a>
                  {:else}
                    <a class="inline-flex min-h-11 items-center font-medium underline" href={r.href} aria-label="Reorder part {e.part_number || e.description} of {what} from {e.vendor_name}">Reorder</a>
                  {/if}
                </p>
              {/snippet}
              {#snippet ordered(thing: string)}
                {@const o = onOrder.get(thing)}
                {#if o}<a class="inline-flex min-h-11 items-center underline" href="/orders/{encodeURIComponent(o.id)}" data-on-order>{onOrderText(o)}</a>{/if}
              {/snippet}
              {#each low as c (c.id)}
                {@const from = vendorsOf(c)}
                {@const best = preferred.get(c.id)}
                <li class="flex min-h-11 flex-wrap items-center gap-x-3">
                  <CategoryBadge category={c} href="/categories/{encodeURIComponent(c.id)}" />
                  {@render ordered(c.id)}
                  {#if best}{@render entry(best, c.name)}{/if}
                  {#if from.length}
                    <ul class="w-full pb-2 text-zinc-600 dark:text-zinc-400" aria-label="Vendors of {c.name}">
                      {#each from as v (v.id)}
                        {@const r = reorderHref(v)}
                        <li class="flex flex-wrap items-center gap-x-2">
                          <a class="inline-flex min-h-11 items-center underline" href="/vendors/{encodeURIComponent(v.id)}">{v.name}</a>
                          {#if r.external}
                            <a class="inline-flex min-h-11 items-center font-medium underline" href={r.href} rel="noopener noreferrer" target="_blank" aria-label="Reorder {c.name} from {v.name}">Reorder</a>
                          {:else}
                            <a class="inline-flex min-h-11 items-center font-medium underline" href={r.href} aria-label="Reorder {c.name} from {v.name}">Reorder</a>
                          {/if}
                        </li>
                      {/each}
                    </ul>
                  {/if}
                </li>
              {/each}
              {#each lowAssets as a (a.asset.id)}
                {@const best = preferred.get(a.asset.id)}
                <li class="flex min-h-11 flex-wrap items-center gap-x-3">
                  <a class="inline-flex min-h-11 items-center underline" href="/assets/{encodeURIComponent(a.asset.id)}">{a.asset.name}</a>
                  {@render ordered(a.asset.id)}
                  {#if best}{@render entry(best, a.asset.name)}{/if}
                </li>
              {/each}
            </ul>
          {/if}
        </section>
      {/if}
      {#if reviewLines.length}
        <section class="card" aria-labelledby="review-heading">
          <h2 id="review-heading" class="mb-2 text-lg font-bold"><span class="shadow-[inset_0_-0.38em_0_var(--color-highlight)] dark:shadow-none">To review</span></h2>
          <ul class="divide-y divide-zinc-300 text-sm dark:divide-zinc-700">
            {#each reviewLines as l (l.href)}
              <li><a class="flex min-h-11 items-center gap-2 underline" href={l.href}><span class="font-mono font-semibold">{l.n}</span> {l.n === 1 ? l.label.replace(/^reviews/, 'review') : l.label}</a></li>
            {/each}
          </ul>
        </section>
      {/if}
      {#if attention.length}
        <section class="card" aria-labelledby="attention-heading">
          <h2 id="attention-heading" class="mb-2 text-lg font-bold"><span class="shadow-[inset_0_-0.38em_0_var(--color-highlight)] dark:shadow-none">Needs attention</span></h2>
          <ul class="divide-y divide-zinc-300 text-sm dark:divide-zinc-700">
            {#each attention as a (a.asset.id)}
              <li class="flex flex-wrap items-center gap-x-2 py-1">
                <a class="inline-flex min-h-11 items-center underline" href="/assets/{encodeURIComponent(a.asset.id)}">{a.asset.name}</a>
                <ConditionBadge condition={a.asset.condition} />
                {#if a.asset.condition_note}<span class="w-full pb-2 text-zinc-600 dark:text-zinc-400">{a.asset.condition_note}</span>{/if}
              </li>
            {/each}
          </ul>
        </section>
      {/if}
      {#if due.length}
        <section class="card" aria-labelledby="due-heading">
          <h2 id="due-heading" class="mb-2 text-lg font-bold"><span class="shadow-[inset_0_-0.38em_0_var(--color-highlight)] dark:shadow-none">Due back this week</span></h2>
          <ul class="divide-y divide-zinc-300 text-sm dark:divide-zinc-700">
            {#each due as l (l.id)}
              <li class="flex items-center justify-between gap-3">
                <a class="inline-flex min-h-11 items-center underline" href="/assets/{encodeURIComponent(l.asset_id)}">{l.asset_name}</a>
                <span class="font-mono text-xs {l.overdue ? 'font-semibold text-red-700 dark:text-red-400' : 'text-zinc-600 dark:text-zinc-400'}">{l.overdue ? 'Overdue' : l.due_at ? dueDate(l.due_at) : ''}</span>
              </li>
            {/each}
          </ul>
        </section>
      {/if}
    </div>
  {/if}

  {#snippet links(list: NavLink[])}
    <ul class="flex flex-wrap gap-x-6">
      {#each list as link (link.href)}
        <li><a class="inline-flex min-h-11 min-w-11 items-center font-medium underline underline-offset-4" href={link.href}>{link.label}</a></li>
      {/each}
    </ul>
  {/snippet}

  <div class="grid gap-6 border-t-2 border-zinc-900 pt-6 sm:grid-cols-2 lg:grid-cols-4 dark:border-zinc-600">
    {#each sections as section (section.group ?? '')}
      {#if section.group}
        <section aria-labelledby="nav-{section.group.toLowerCase()}-heading">
          <h2 id="nav-{section.group.toLowerCase()}-heading" class="mb-1 text-lg font-bold">{section.group}</h2>
          {@render links(section.links)}
        </section>
      {:else}
        <div>
          <h2 class="mb-1 text-lg font-bold">Everyday</h2>
          {@render links(section.links)}
        </div>
      {/if}
    {/each}
  </div>
</div>

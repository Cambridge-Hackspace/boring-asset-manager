<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../lib/api';
  import { messageOf } from '../lib/errors';
  import { dueDate } from '../lib/assets';
  import type { AssetWithDetails, Category, Loan } from '../lib/models';
  import { navSections, type NavLink } from '../lib/nav';
  import { navigate } from '../lib/router.svelte';
  import { scanTarget } from '../lib/scan';
  import { displayName } from '../lib/people';
  import { session } from '../lib/session.svelte';
  import { routes } from '../routes';
  import CategoryBadge from '../components/CategoryBadge.svelte';
  import ConditionBadge from '../components/ConditionBadge.svelte';
  import ErrorMessage from '../components/ErrorMessage.svelte';
  import SetupChecklist from '../components/SetupChecklist.svelte';

  let { params: _params }: { params: Record<string, string> } = $props();

  const sections = $derived(navSections(routes, (...p) => session.canAny(...p)));

  let low = $state<Category[] | null>(null);
  let lowAssets = $state<AssetWithDetails[]>([]);
  let attention = $state<AssetWithDetails[]>([]);
  // Out on loan, and due back within the week or overdue.
  let due = $state<Loan[]>([]);
  let error = $state('');
  let code = $state('');

  function lookUp(event: SubmitEvent) {
    event.preventDefault();
    const to = scanTarget(code);
    if (to) navigate(to);
  }

  onMount(async () => {
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
    } catch (err) {
      error = messageOf(err);
    }
  });
</script>

<div class="space-y-8">
  <div>
    <h1 class="text-3xl font-bold tracking-tight">Welcome</h1>
    <p class="mt-1 text-zinc-600 dark:text-zinc-400">Signed in as {session.user ? displayName(session.user) : ''}.</p>
  </div>

  <ErrorMessage {error} />
  {#if session.canAny('users.manage', 'settings.manage')}<SetupChecklist />{/if}
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

  {#if low || attention.length || due.length}
    <div class="grid gap-6 md:grid-cols-2 xl:grid-cols-3">
      {#if low}
        <section class="card" aria-labelledby="low-heading">
          <h2 id="low-heading" class="mb-2 text-lg font-bold"><span class="shadow-[inset_0_-0.38em_0_var(--color-highlight)] dark:shadow-none">Running low</span></h2>
          {#if low.length === 0 && lowAssets.length === 0}
            <p class="text-sm text-zinc-600 dark:text-zinc-400">Nothing is running low.</p>
          {:else}
            <ul class="divide-y divide-zinc-300 text-sm dark:divide-zinc-700">
              {#each low as c (c.id)}
                <li class="flex min-h-11 items-center"><CategoryBadge category={c} href="/categories/{encodeURIComponent(c.id)}" /></li>
              {/each}
              {#each lowAssets as a (a.asset.id)}
                <li><a class="flex min-h-11 items-center underline" href="/assets/{encodeURIComponent(a.asset.id)}">{a.asset.name}</a></li>
              {/each}
            </ul>
          {/if}
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

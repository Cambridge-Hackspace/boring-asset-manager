<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../lib/api';
  import { messageOf } from '../lib/errors';
  import type { AssetWithDetails, Category } from '../lib/models';
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
  let error = $state('');
  let code = $state('');

  function lookUp(event: SubmitEvent) {
    event.preventDefault();
    const to = scanTarget(code);
    if (to) navigate(to);
  }

  onMount(async () => {
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

<h1 class="text-2xl font-bold">Welcome</h1>
<p class="mt-1 text-zinc-600 dark:text-zinc-400">Signed in as {session.user ? displayName(session.user) : ''}.</p>

<ErrorMessage {error} />
{#if session.canAny('users.manage', 'settings.manage')}<SetupChecklist />{/if}
{#if session.can('assets.scan')}
  <form class="mt-6 flex max-w-md flex-wrap items-end gap-2" role="search" onsubmit={lookUp}>
    <div class="grow">
      <label class="label" for="home-code">Look up a code</label>
      <input id="home-code" class="input" autocomplete="off" placeholder="HS-100" bind:value={code} />
    </div>
    <button type="submit" class="btn-secondary" disabled={!code.trim()}>Look up</button>
    <a class="btn-secondary" href="/scan">Scan with the camera</a>
  </form>
{/if}
{#if low}
  <section class="mt-6" aria-labelledby="low-heading">
    <h2 id="low-heading" class="mb-2 text-lg font-semibold">Running low</h2>
    {#if low.length === 0 && lowAssets.length === 0}
      <p class="text-sm text-zinc-600 dark:text-zinc-400">Nothing is running low.</p>
    {:else}
      <ul class="flex flex-wrap gap-2">
        {#each low as c (c.id)}
          <li><CategoryBadge category={c} href="/categories/{encodeURIComponent(c.id)}" /></li>
        {/each}
        {#each lowAssets as a (a.asset.id)}
          <li><a class="underline" href="/assets/{encodeURIComponent(a.asset.id)}">{a.asset.name}</a></li>
        {/each}
      </ul>
    {/if}
  </section>
{/if}

{#if attention.length}
  <section class="mt-6" aria-labelledby="attention-heading">
    <h2 id="attention-heading" class="mb-2 text-lg font-semibold">Needs attention</h2>
    <ul class="space-y-1 text-sm">
      {#each attention as a (a.asset.id)}
        <li>
          <a class="underline" href="/assets/{encodeURIComponent(a.asset.id)}">{a.asset.name}</a>
          <ConditionBadge condition={a.asset.condition} />
          {#if a.asset.condition_note}<span class="text-zinc-600 dark:text-zinc-400">{a.asset.condition_note}</span>{/if}
        </li>
      {/each}
    </ul>
  </section>
{/if}

{#snippet cards(links: NavLink[])}
  <ul class="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
    {#each links as link (link.href)}
      <li><a class="card block font-medium hover:border-zinc-400" href={link.href}>{link.label}</a></li>
    {/each}
  </ul>
{/snippet}

{#each sections as section (section.group ?? '')}
  {#if section.group}
    <section class="mt-6" aria-labelledby="nav-{section.group.toLowerCase()}-heading">
      <h2 id="nav-{section.group.toLowerCase()}-heading" class="mb-2 text-lg font-semibold">{section.group}</h2>
      {@render cards(section.links)}
    </section>
  {:else}
    <div class="mt-6">{@render cards(section.links)}</div>
  {/if}
{/each}

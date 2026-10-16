<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../lib/api';
  import { messageOf } from '../lib/errors';
  import type { AssetWithDetails, Category } from '../lib/models';
  import { displayName } from '../lib/people';
  import { session } from '../lib/session.svelte';
  import { routes } from '../routes';
  import CategoryBadge from '../components/CategoryBadge.svelte';
  import ConditionBadge from '../components/ConditionBadge.svelte';
  import ErrorMessage from '../components/ErrorMessage.svelte';

  let { params: _params }: { params: Record<string, string> } = $props();

  const sections = $derived(routes.filter((r) => r.nav && (!r.permissions || session.canAny(...r.permissions))));

  let low = $state<Category[] | null>(null);
  let lowAssets = $state<AssetWithDetails[]>([]);
  let attention = $state<AssetWithDetails[]>([]);
  let error = $state('');

  onMount(async () => {
    if (!session.can('assets:read')) return;
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

{#if sections.length}
  <ul class="mt-6 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
    {#each sections as section (section.pattern)}
      <li><a class="card block font-medium hover:border-zinc-400" href={section.pattern}>{section.nav}</a></li>
    {/each}
  </ul>
{/if}

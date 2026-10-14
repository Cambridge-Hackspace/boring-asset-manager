<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../lib/api';
  import { messageOf } from '../lib/errors';
  import type { Category } from '../lib/models';
  import { session } from '../lib/session.svelte';
  import { routes } from '../routes';
  import CategoryBadge from '../components/CategoryBadge.svelte';
  import ErrorMessage from '../components/ErrorMessage.svelte';

  let { params: _params }: { params: Record<string, string> } = $props();

  const sections = $derived(routes.filter((r) => r.nav && (!r.permissions || session.canAny(...r.permissions))));

  let low = $state<Category[] | null>(null);
  let error = $state('');

  onMount(async () => {
    if (!session.can('assets:read')) return;
    try {
      const all = (await request<{ categories: Category[] }>('GET', '/categories')).categories ?? [];
      low = all.filter((c) => c.is_consumable && c.is_running_low).sort((a, b) => a.name.localeCompare(b.name));
    } catch (err) {
      error = messageOf(err);
    }
  });
</script>

<h1 class="text-2xl font-bold">Welcome</h1>
<p class="mt-1 text-zinc-600 dark:text-zinc-400">Signed in as {session.user?.email}.</p>

<ErrorMessage {error} />
{#if low}
  <section class="mt-6" aria-labelledby="low-heading">
    <h2 id="low-heading" class="mb-2 text-lg font-semibold">Running low</h2>
    {#if low.length === 0}
      <p class="text-sm text-zinc-600 dark:text-zinc-400">Nothing is running low.</p>
    {:else}
      <ul class="flex flex-wrap gap-2">
        {#each low as c (c.id)}
          <li><CategoryBadge category={c} href="/categories/{encodeURIComponent(c.id)}" /></li>
        {/each}
      </ul>
    {/if}
  </section>
{/if}

{#if sections.length}
  <ul class="mt-6 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
    {#each sections as section (section.pattern)}
      <li><a class="card block font-medium hover:border-zinc-400" href={section.pattern}>{section.nav}</a></li>
    {/each}
  </ul>
{/if}

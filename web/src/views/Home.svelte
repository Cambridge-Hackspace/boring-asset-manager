<script lang="ts">
  import { session } from '../lib/session.svelte';
  import { routes } from '../routes';

  let { params: _params }: { params: Record<string, string> } = $props();

  const sections = $derived(routes.filter((r) => r.nav && (!r.permissions || session.canAny(...r.permissions))));
</script>

<h1 class="text-2xl font-bold">Welcome</h1>
<p class="mt-1 text-zinc-600 dark:text-zinc-400">Signed in as {session.user?.email}.</p>

{#if sections.length}
  <ul class="mt-6 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
    {#each sections as section (section.pattern)}
      <li><a class="card block font-medium hover:border-zinc-400" href={section.pattern}>{section.nav}</a></li>
    {/each}
  </ul>
{/if}

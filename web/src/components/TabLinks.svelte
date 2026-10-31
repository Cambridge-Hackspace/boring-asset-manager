<script lang="ts">
  // Splits a long page into sections (#159), such as a category's: links
  // that keep the section in the address as ?tab=, so it can be linked to
  // and survives a reload. The page shows currentTab()'s section.
  import { router } from '../lib/router.svelte';
  import { currentTab, tabHref } from '../lib/tabs';

  let { label, tabs }: { label: string; tabs: { key: string; label: string }[] } = $props();

  const keys = $derived(tabs.map((t) => t.key));
  const current = $derived(currentTab(router.search, keys));
</script>

<nav aria-label={label} class="flex flex-wrap gap-x-1 border-b-2 border-zinc-900 dark:border-zinc-400">
  {#each tabs as t (t.key)}
    <a
      href={tabHref(router.path, router.search, keys, t.key)}
      aria-current={t.key === current ? 'page' : undefined}
      class="inline-flex min-h-11 items-center rounded-t px-4 text-sm hover:bg-zinc-100 aria-[current]:bg-zinc-900 aria-[current]:font-semibold aria-[current]:text-zinc-50 dark:hover:bg-zinc-800 dark:aria-[current]:bg-zinc-100 dark:aria-[current]:text-zinc-900"
    >
      {t.label}
    </a>
  {/each}
</nav>

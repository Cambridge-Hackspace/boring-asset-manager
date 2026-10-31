<script lang="ts">
  // A page's title (#162), with a line about the page beneath it and the
  // page's actions to its right, such as its one primary button; on a
  // phone, the actions wrap beneath.
  import type { Snippet } from 'svelte';

  let { title, lede, actions }: { title: string; lede?: Snippet | string; actions?: Snippet } = $props();
</script>

<div class="mb-6 flex flex-wrap items-end justify-between gap-x-6 gap-y-3">
  <div class="min-w-0">
    <h1 class="text-3xl font-bold">{title}</h1>
    {#if typeof lede === 'string'}
      <p class="mt-1 max-w-2xl text-sm text-zinc-600 dark:text-zinc-400">{lede}</p>
    {:else if lede}
      <p class="mt-1 max-w-2xl text-sm text-zinc-600 dark:text-zinc-400">{@render lede()}</p>
    {/if}
  </div>
  {#if actions}<div class="flex flex-wrap gap-2">{@render actions()}</div>{/if}
</div>

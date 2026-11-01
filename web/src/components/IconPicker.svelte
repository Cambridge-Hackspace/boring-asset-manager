<!--
  Choosing an icon (docs/icons.md): every icon the build draws, variants
  included, searched by name or description. Escape closes it.
-->
<script lang="ts">
  import { iconset } from '../lib/iconset.svelte';
  import IsoIcon from './IsoIcon.svelte';

  interface Props {
    /** The icon chosen now, if any. */
    value: string;
    onpick: (name: string) => void;
    onclose: () => void;
    /** The color the category token paints in, for the preview. */
    categoryColor?: string;
  }

  let { value, onpick, onclose, categoryColor = '' }: Props = $props();
  let search = $state('');

  $effect(() => {
    void iconset.load();
  });
  function escape(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.stopPropagation();
      onclose();
    }
  }
  const shown = $derived.by(() => {
    const q = search.trim().toLowerCase();
    return iconset.all.filter((i) => !q || i.name.toLowerCase().includes(q) || i.describe.join(' ').toLowerCase().includes(q));
  });
</script>

<div class="mt-2 rounded border border-zinc-300 p-2 dark:border-zinc-700" role="group" aria-label="Choose an icon">
  <div class="mb-2 flex items-center gap-2">
    <!-- svelte-ignore a11y_autofocus -->
    <input type="search" class="input" aria-label="Search icons" placeholder="Search by name or description" bind:value={search} onkeydown={escape} autofocus />
    <button type="button" class="btn-secondary" onclick={onclose}>Close</button>
  </div>
  {#if shown.length}
    <ul class="grid max-h-64 grid-cols-[repeat(auto-fill,minmax(6rem,1fr))] gap-2 overflow-y-auto p-0.5">
      {#each shown as icon (icon.name)}
        <li>
          <button
            type="button"
            class="flex w-full flex-col items-center gap-1 rounded border border-zinc-200 bg-white p-1 text-xs hover:bg-zinc-100 aria-pressed:ring-2 aria-pressed:ring-amber-500 dark:border-zinc-700 dark:bg-zinc-950 dark:hover:bg-zinc-800"
            aria-pressed={icon.name === value}
            title={icon.describe.join(' ') || undefined}
            onclick={() => onpick(icon.name)}
            onkeydown={escape}
          >
            <IsoIcon {icon} height={40} {categoryColor} />
            <span class="w-full truncate font-mono">{icon.name}</span>
          </button>
        </li>
      {/each}
    </ul>
  {:else}
    <p class="text-sm">No icon matches.</p>
  {/if}
</div>

<script lang="ts">
  import { tick } from 'svelte';
  import { findCommands, type Command } from './commands';

  // Krita's action search, opened with Ctrl+Enter: type a few letters of
  // what to do, the arrow keys or the pointer to choose, Enter to do it.
  // Escape, or a click elsewhere, closes it.
  let { commands, onclose }: { commands: Command[]; onclose: () => void } = $props();

  let query = $state('');
  let active = $state(0);
  let input = $state<HTMLInputElement | null>(null);
  const found = $derived(findCommands(commands, query));

  $effect(() => {
    void tick().then(() => input?.focus());
  });
  // A new search starts at the best.
  $effect(() => {
    void query;
    active = 0;
  });

  function run(c: Command | undefined) {
    if (!c) return;
    onclose();
    c.run();
  }

  function keydown(event: KeyboardEvent) {
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      if (found.length) active = (active + (event.key === 'ArrowDown' ? 1 : found.length - 1)) % found.length;
      document.getElementById(`command-${active}`)?.scrollIntoView({ block: 'nearest' });
    } else if (event.key === 'Enter') {
      event.preventDefault();
      run(found[active]);
    } else if (event.key === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      onclose();
    }
  }
</script>

<!-- svelte-ignore a11y_no_static_element_interactions, a11y_click_events_have_key_events -->
<div class="fixed inset-0 z-40" onpointerdown={(e) => e.target === e.currentTarget && onclose()}>
  <div
    class="mx-auto mt-24 w-[min(32rem,calc(100%-2rem))] rounded-md border border-zinc-300 bg-white shadow-xl dark:border-zinc-700 dark:bg-zinc-900"
    role="dialog"
    aria-modal="true"
    aria-label="Search actions"
  >
    <input
      bind:this={input}
      bind:value={query}
      class="w-full border-b border-zinc-200 bg-transparent px-3 py-2 text-sm outline-none dark:border-zinc-800"
      role="combobox"
      aria-label="Search actions"
      aria-expanded="true"
      aria-controls="command-list"
      aria-autocomplete="list"
      aria-activedescendant={found.length ? `command-${active}` : undefined}
      placeholder="Search actions…"
      autocomplete="off"
      spellcheck="false"
      onkeydown={keydown}
      onblur={(e) => {
        if (!(e.relatedTarget instanceof Element && e.relatedTarget.closest('#command-list'))) onclose();
      }}
    />
    <ul id="command-list" role="listbox" aria-label="Actions" class="max-h-80 overflow-y-auto py-1 text-sm">
      {#each found as c, i (c.name)}
        <li
          id="command-{i}"
          role="option"
          aria-selected={i === active}
          class="flex cursor-pointer items-center justify-between gap-4 px-3 py-1 aria-selected:bg-zinc-800 aria-selected:text-white dark:aria-selected:bg-zinc-200 dark:aria-selected:text-zinc-900"
          onpointerenter={() => (active = i)}
          onpointerdown={(e) => {
            e.preventDefault();
            run(c);
          }}
        >
          <span>{c.name}</span>
          {#if c.keys}<kbd class="font-sans text-xs opacity-70">{c.keys}</kbd>{/if}
        </li>
      {:else}
        <li class="px-3 py-1 text-zinc-600 dark:text-zinc-400">No action by that name.</li>
      {/each}
    </ul>
  </div>
</div>

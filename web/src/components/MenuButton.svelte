<script lang="ts" module>
  export interface MenuButtonItem {
    label: string;
    /** One line under its name, saying what it does. */
    description?: string;
    run: () => void;
  }
</script>

<script lang="ts">
  import { tick } from 'svelte';

  // A button that opens a menu of things to do, each with a line saying
  // what it does, as the WAI-ARIA menu button pattern has it: Enter, Space
  // or the down arrow opens it at its first item, the up arrow at its last;
  // the arrow keys, Home and End move through it, as does typing an item's
  // first letter; Enter or Space chooses; Escape closes it and goes back
  // to the button, as does a click elsewhere, without going back.
  let { id, label, items }: { id: string; label: string; items: MenuButtonItem[] } = $props();

  let open = $state(false);
  let root = $state<HTMLDivElement | null>(null);
  let button = $state<HTMLButtonElement | null>(null);

  const entries = () => [...(root?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]') ?? [])];

  async function show(at: 'first' | 'last') {
    open = true;
    await tick();
    const all = entries();
    (at === 'first' ? all[0] : all.at(-1))?.focus();
  }

  function close(refocus: boolean) {
    open = false;
    if (refocus) button?.focus();
  }

  function choose(item: MenuButtonItem) {
    close(true);
    item.run();
  }

  function buttonKey(event: KeyboardEvent) {
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      void show(event.key === 'ArrowDown' ? 'first' : 'last');
    }
  }

  function menuKey(event: KeyboardEvent) {
    const all = entries();
    const at = all.indexOf(document.activeElement as HTMLButtonElement);
    const go = (i: number) => all[(i + all.length) % all.length]?.focus();
    switch (event.key) {
      case 'ArrowDown':
        event.preventDefault();
        return go(at + 1);
      case 'ArrowUp':
        event.preventDefault();
        return go(at - 1);
      case 'Home':
        event.preventDefault();
        return go(0);
      case 'End':
        event.preventDefault();
        return go(all.length - 1);
      case 'Enter':
      case ' ':
        event.preventDefault();
        if (items[at]) choose(items[at]);
        return;
      case 'Escape':
        event.preventDefault();
        event.stopPropagation();
        return close(true);
      case 'Tab':
        return close(false);
    }
    // Typing a letter moves to the next item that starts with it.
    if (event.key.length === 1 && !event.ctrlKey && !event.metaKey && !event.altKey) {
      const k = event.key.toLowerCase();
      for (let n = 1; n <= items.length; n++) {
        const i = (at + n) % items.length;
        if (items[i]!.label.toLowerCase().startsWith(k)) return go(i);
      }
    }
  }
</script>

<svelte:window onpointerdown={(e) => open && !root?.contains(e.target as Node) && close(false)} />

<div class="relative" bind:this={root}>
  <button
    bind:this={button}
    id="{id}-button"
    type="button"
    class="btn-secondary"
    aria-haspopup="menu"
    aria-expanded={open}
    aria-controls="{id}-menu"
    onclick={() => (open ? close(false) : show('first'))}
    onkeydown={buttonKey}
  >
    {label} <span aria-hidden="true">▾</span>
  </button>
  {#if open}
    <div
      id="{id}-menu"
      role="menu"
      aria-labelledby="{id}-button"
      tabindex="-1"
      class="absolute top-full left-0 z-40 mt-1 w-[min(24rem,calc(100vw-2rem))] rounded border-2 border-zinc-900 bg-white py-1 shadow-lg dark:border-zinc-400 dark:bg-zinc-900"
      onkeydown={menuKey}
    >
      {#each items as item, i (i)}
        <button
          type="button"
          role="menuitem"
          tabindex="-1"
          class="block min-h-11 w-full cursor-pointer px-3 py-2 text-left hover:bg-zinc-100 focus:bg-zinc-100 focus:outline-none focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-zinc-900 dark:hover:bg-zinc-800 dark:focus:bg-zinc-800 dark:focus-visible:outline-zinc-100"
          aria-labelledby="{id}-item-{i}"
          aria-describedby={item.description ? `${id}-item-${i}-help` : undefined}
          onclick={() => choose(item)}
        >
          <span id="{id}-item-{i}" class="block text-sm font-semibold">{item.label}</span>
          {#if item.description}<span id="{id}-item-{i}-help" class="block text-xs text-zinc-600 dark:text-zinc-400">{item.description}</span>{/if}
        </button>
      {/each}
    </div>
  {/if}
</div>

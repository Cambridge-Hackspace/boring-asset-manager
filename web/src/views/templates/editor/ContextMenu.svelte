<script lang="ts" module>
  export type MenuItem = { label: string; run: () => void; disabled?: boolean } | 'separator';
</script>

<script lang="ts">
  import { tick } from 'svelte';

  // A menu at the pointer, as a right-click opens in Krita and Inkscape:
  // arrow keys move through it, Enter runs an item, Escape or a click
  // elsewhere closes it, and focus goes back where it was.
  let { x, y, label, items, onclose }: { x: number; y: number; label: string; items: MenuItem[]; onclose: () => void } = $props();

  let menu = $state<HTMLDivElement | null>(null);
  const returnTo = document.activeElement instanceof HTMLElement ? document.activeElement : null;
  // Kept on the screen.
  let left = $state(0);
  let top = $state(0);

  $effect(() => {
    if (!menu) return;
    const r = menu.getBoundingClientRect();
    left = Math.max(4, Math.min(x, window.innerWidth - r.width - 4));
    top = Math.max(4, Math.min(y, window.innerHeight - r.height - 4));
    void tick().then(() => buttons()[0]?.focus());
  });

  const buttons = () => [...(menu?.querySelectorAll<HTMLButtonElement>('button:not(:disabled)') ?? [])];

  function close() {
    onclose();
    returnTo?.focus();
  }

  function keydown(event: KeyboardEvent) {
    const all = buttons();
    const at = all.indexOf(document.activeElement as HTMLButtonElement);
    if (event.key === 'Escape' || event.key === 'Tab') {
      event.preventDefault();
      event.stopPropagation();
      close();
    } else if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      const next = (at + (event.key === 'ArrowDown' ? 1 : all.length - 1)) % all.length;
      all[next]?.focus();
    } else if (event.key === 'Home' || event.key === 'End') {
      event.preventDefault();
      all[event.key === 'Home' ? 0 : all.length - 1]?.focus();
    }
  }
</script>

<svelte:window onpointerdown={(e) => !menu?.contains(e.target as Node) && close()} onblur={close} />

<div
  bind:this={menu}
  role="menu"
  aria-label={label}
  tabindex="-1"
  class="fixed z-50 min-w-48 rounded-md border border-zinc-300 bg-white py-1 text-xs shadow-lg dark:border-zinc-700 dark:bg-zinc-900"
  style="left:{left}px;top:{top}px"
  onkeydown={keydown}
  oncontextmenu={(e) => e.preventDefault()}
>
  {#each items as item, i (i)}
    {#if item === 'separator'}
      <div role="separator" class="my-1 border-t border-zinc-200 dark:border-zinc-700"></div>
    {:else}
      <button
        type="button"
        role="menuitem"
        class="block w-full px-3 py-1 text-left hover:bg-zinc-100 focus:bg-zinc-100 focus:outline-none disabled:opacity-40 dark:hover:bg-zinc-800 dark:focus:bg-zinc-800"
        disabled={item.disabled}
        onclick={() => {
          close();
          item.run();
        }}>{item.label}</button
      >
    {/if}
  {/each}
</div>

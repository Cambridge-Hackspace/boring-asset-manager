<script lang="ts">
  // A group of the main navigation: a button that shows its links below it.
  // It closes on Escape, a click elsewhere, or moving to another page.
  import { isCurrent, type NavLink } from '../lib/nav';
  import { router } from '../lib/router.svelte';

  let { label, links, itemClass }: { label: string; links: NavLink[]; itemClass: string } = $props();

  let open = $state(false);
  let root: HTMLElement | undefined = $state();
  let button: HTMLButtonElement | undefined = $state();
  const id = $derived(`nav-${label.toLowerCase().replace(/[^a-z0-9]+/g, '-')}`);
  const holdsCurrent = $derived(links.some((l) => isCurrent(l.href, router.path)));

  $effect(() => {
    void router.path;
    open = false;
  });

  function onwindowclick(e: MouseEvent): void {
    if (open && root && !root.contains(e.target as Node)) open = false;
  }

  function onkeydown(e: KeyboardEvent): void {
    if (e.key === 'Escape' && open) {
      open = false;
      button?.focus();
    }
  }

  function onfocusout(e: FocusEvent): void {
    if (open && root && !root.contains(e.relatedTarget as Node | null) && e.relatedTarget) open = false;
  }
</script>

<svelte:window onclick={onwindowclick} />

<div class="relative" bind:this={root} {onkeydown} {onfocusout} role="none">
  <button
    type="button"
    bind:this={button}
    aria-expanded={open}
    aria-controls={id}
    data-current={holdsCurrent || undefined}
    class="{itemClass} data-[current]:bg-zinc-900 data-[current]:text-white dark:data-[current]:bg-zinc-100 dark:data-[current]:text-zinc-900"
    onclick={() => (open = !open)}
  >
    {label} <span aria-hidden="true">▾</span>
  </button>
  {#if open}
    <ul
      {id}
      class="absolute left-0 z-20 mt-1 min-w-44 rounded-md border border-zinc-200 bg-white p-1 shadow-lg dark:border-zinc-700 dark:bg-zinc-900"
    >
      {#each links as link (link.href)}
        <li>
          <a
            href={link.href}
            aria-current={isCurrent(link.href, router.path) ? 'page' : undefined}
            class="block {itemClass} aria-[current=page]:bg-zinc-900 aria-[current=page]:text-white dark:aria-[current=page]:bg-zinc-100 dark:aria-[current=page]:text-zinc-900"
          >
            {link.label}
          </a>
        </li>
      {/each}
    </ul>
  {/if}
</div>

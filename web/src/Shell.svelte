<script lang="ts">
  import type { Snippet } from 'svelte';
  import { router } from './lib/router.svelte';
  import { displayName } from './lib/people';
  import { helpFor } from './lib/help';
  import { isCurrent, navSections } from './lib/nav';
  import { session, signInPath } from './lib/session.svelte';
  import { routes } from './routes';
  import NavMenu from './components/NavMenu.svelte';

  let { children }: { children: Snippet } = $props();

  const sections = $derived(
    // Signed out, nothing: the public see single pages, not the catalog.
    session.user ? navSections(routes, (...p) => session.canAny(...p)) : [],
  );

  const itemClass = 'rounded-md px-3 py-1.5 text-sm font-medium hover:bg-zinc-100 dark:hover:bg-zinc-800';

  let main: HTMLElement | undefined = $state();
  // On a phone, the menu is one button opening every section.
  let menuOpen = $state(false);
  let menuButton: HTMLButtonElement | undefined = $state();
  $effect(() => {
    void router.path;
    menuOpen = false;
  });
  function menuKey(e: KeyboardEvent) {
    if (e.key === 'Escape' && menuOpen) {
      menuOpen = false;
      menuButton?.focus();
    }
  }
  // Move focus to the page after navigating, so screen readers announce it.
  let first = true;
  $effect(() => {
    void router.path;
    if (first) {
      first = false;
      return;
    }
    main?.focus();
  });
</script>

<svelte:window onkeydown={menuKey} />

<a href="#main" class="sr-only focus:not-sr-only focus:absolute focus:left-2 focus:top-2 focus:z-50 focus:rounded focus:bg-white focus:px-3 focus:py-2 focus:text-zinc-900">
  Skip to content
</a>
<header class="border-b border-zinc-200 bg-white print:hidden dark:border-zinc-800 dark:bg-zinc-900">
  <div class="mx-auto flex max-w-7xl flex-wrap items-center gap-x-6 gap-y-2 px-4 py-3">
    <a href="/" class="text-lg font-black tracking-tight">BAM!</a>
    <button
      type="button"
      class="btn-secondary ml-auto sm:hidden"
      bind:this={menuButton}
      aria-expanded={menuOpen}
      aria-controls="phone-menu"
      onclick={() => (menuOpen = !menuOpen)}
    >
      Menu <span aria-hidden="true">{menuOpen ? '✕' : '☰'}</span>
    </button>
    <nav aria-label="Main" class="hidden flex-1 flex-wrap gap-1 sm:flex">
      {#each sections as section (section.group ?? '')}
        {#if section.group && section.links.length > 1}
          <NavMenu label={section.group} links={section.links} {itemClass} />
        {:else}
          {#each section.links as link (link.href)}
            <a
              href={link.href}
              aria-current={isCurrent(link.href, router.path) ? 'page' : undefined}
              class="{itemClass} aria-[current=page]:bg-zinc-900 aria-[current=page]:text-white dark:aria-[current=page]:bg-zinc-100 dark:aria-[current=page]:text-zinc-900"
            >
              {link.label}
            </a>
          {/each}
        {/if}
      {/each}
    </nav>
    <div class="hidden items-center gap-3 text-sm sm:flex">
      <a href="/help/{helpFor(router.path)}" class="text-zinc-600 hover:underline dark:text-zinc-400">Help</a>
      {#if session.user}
        <a href="/account" class="text-zinc-600 hover:underline dark:text-zinc-400">{displayName(session.user)}</a>
        <button type="button" class="btn-secondary" onclick={() => session.signOut()}>Sign out</button>
      {:else}
        <a class="btn-secondary" href={signInPath(router.path + router.search)}>Sign in</a>
      {/if}
    </div>
  </div>
  {#if menuOpen}
    <nav id="phone-menu" aria-label="Main" class="space-y-4 border-t border-zinc-200 px-4 py-3 sm:hidden dark:border-zinc-800">
      {#each sections as section (section.group ?? '')}
        <div>
          {#if section.group}<p class="mb-1 text-xs font-semibold tracking-wide text-zinc-600 uppercase dark:text-zinc-400">{section.group}</p>{/if}
          <ul class="grid grid-cols-2 gap-1">
            {#each section.links as link (link.href)}
              <li>
                <a
                  href={link.href}
                  aria-current={isCurrent(link.href, router.path) ? 'page' : undefined}
                  class="block {itemClass} py-2.5 aria-[current=page]:bg-zinc-900 aria-[current=page]:text-white dark:aria-[current=page]:bg-zinc-100 dark:aria-[current=page]:text-zinc-900"
                >
                  {link.label}
                </a>
              </li>
            {/each}
          </ul>
        </div>
      {/each}
      <div class="flex flex-wrap items-center gap-2 border-t border-zinc-200 pt-3 text-sm dark:border-zinc-800">
        <a href="/help/{helpFor(router.path)}" class="btn-secondary">Help</a>
        {#if session.user}
          <a href="/account" class="btn-secondary">Your account</a>
          <button type="button" class="btn-secondary" onclick={() => session.signOut()}>Sign out</button>
        {:else}
          <a class="btn-primary" href={signInPath(router.path + router.search)}>Sign in</a>
        {/if}
      </div>
    </nav>
  {/if}
</header>
<main id="main" tabindex="-1" bind:this={main} class="mx-auto max-w-7xl px-4 py-6 outline-none">
  {@render children()}
</main>

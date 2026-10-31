<script lang="ts">
  import type { Snippet } from 'svelte';
  import { router } from './lib/router.svelte';
  import { displayName } from './lib/people';
  import { helpFor } from './lib/help';
  import { currentGroup, isCurrent, navSections } from './lib/nav';
  import { session, signInPath } from './lib/session.svelte';
  import { routes } from './routes';
  import logo from './assets/bam.png';

  // A workspace, such as the template editor, has the whole window below
  // the header, rather than a column of the page: the header's height
  // counted with its border, and positioned, so that nothing placed
  // absolutely inside it (a screen reader's label, say) lengthens the page.
  let { children, workspace = false }: { children: Snippet; workspace?: boolean } = $props();

  const sections = $derived(
    // Signed out, nothing: the public see single pages, not the catalog.
    session.user ? navSections(routes, (...p) => session.canAny(...p)) : [],
  );

  // Catalog, Labels and Admin each lead to their first page, and the pages of
  // the one you are in sit in a row beneath the header. A group of one page
  // is a plain link.
  const group = $derived(currentGroup(sections, router.path));
  const row = $derived(group && group.links.length > 1 ? group : undefined);

  const itemClass =
    'inline-flex min-h-11 items-center rounded px-3 text-sm font-medium hover:bg-zinc-100 dark:hover:bg-zinc-800 aria-[current]:font-semibold aria-[current]:underline aria-[current]:decoration-action aria-[current]:decoration-3 aria-[current]:underline-offset-8';

  let main: HTMLElement | undefined = $state();
  let headerHeight = $state(0);
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
<header bind:offsetHeight={headerHeight} class="border-b-2 border-zinc-900 bg-zinc-50 print:hidden dark:border-zinc-600 dark:bg-zinc-950">
  <div class="mx-auto flex max-w-7xl flex-wrap items-center gap-x-6 gap-y-2 px-4 py-2">
    <a href="/" class="inline-flex min-h-11 items-center rounded"><img src={logo} alt="BAM!" width="72" height="45" /></a>
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
    <nav aria-label="Main" class="hidden flex-1 flex-wrap gap-x-1 sm:flex">
      {#each sections as section (section.group ?? '')}
        {#if section.group && section.links.length > 1}
          <a href={section.links[0]!.href} aria-current={section === group ? 'true' : undefined} class={itemClass}>{section.group}</a>
        {:else}
          {#each section.links as link (link.href)}
            <a href={link.href} aria-current={isCurrent(link.href, router.path) ? 'page' : undefined} class={itemClass}>{link.label}</a>
          {/each}
        {/if}
      {/each}
    </nav>
    <div class="hidden items-center gap-1 text-sm sm:flex">
      <a href="/help/{helpFor(router.path)}" class="{itemClass} text-zinc-600 dark:text-zinc-400">Help</a>
      {#if session.user}
        <a href="/account" class="{itemClass} text-zinc-600 dark:text-zinc-400">{displayName(session.user)}</a>
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
                  class="flex {itemClass}"
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
  {#if row}
    <nav aria-label={row.group} class="border-t border-zinc-300 bg-zinc-100 dark:border-zinc-700 dark:bg-zinc-900">
      <ul class="mx-auto flex max-w-7xl flex-wrap gap-x-1 px-4">
        {#each row.links as link (link.href)}
          <li><a href={link.href} aria-current={isCurrent(link.href, router.path) ? 'page' : undefined} class={itemClass}>{link.label}</a></li>
        {/each}
      </ul>
    </nav>
  {/if}
</header>
<main id="main" tabindex="-1" bind:this={main} class={workspace ? 'outline-none md:relative md:h-(--workspace-height) md:overflow-hidden' : 'mx-auto max-w-7xl px-4 py-6 outline-none'}
  style:--workspace-height={workspace ? `calc(100dvh - ${headerHeight}px)` : undefined}
>
  {@render children()}
</main>

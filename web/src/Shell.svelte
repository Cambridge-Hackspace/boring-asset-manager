<script lang="ts">
  import type { Snippet } from 'svelte';
  import { router } from './lib/router.svelte';
  import { displayName } from './lib/people';
  import { session } from './lib/session.svelte';
  import { routes } from './routes';

  let { children }: { children: Snippet } = $props();

  const navItems = $derived(
    routes.filter((r) => r.nav && (!r.permissions || session.canAny(...r.permissions))),
  );

  function current(pattern: string): boolean {
    return pattern === '/' ? router.path === '/' : router.path === pattern || router.path.startsWith(pattern + '/');
  }

  let main: HTMLElement | undefined = $state();
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

<a href="#main" class="sr-only focus:not-sr-only focus:absolute focus:left-2 focus:top-2 focus:z-50 focus:rounded focus:bg-white focus:px-3 focus:py-2 focus:text-zinc-900">
  Skip to content
</a>
<header class="border-b border-zinc-200 bg-white dark:border-zinc-800 dark:bg-zinc-900">
  <div class="mx-auto flex max-w-7xl flex-wrap items-center gap-x-6 gap-y-2 px-4 py-3">
    <a href="/" class="text-lg font-black tracking-tight">BAM!</a>
    <nav aria-label="Main" class="flex flex-1 flex-wrap gap-1">
      {#each navItems as item (item.pattern)}
        <a
          href={item.pattern}
          aria-current={current(item.pattern) ? 'page' : undefined}
          class="rounded-md px-3 py-1.5 text-sm font-medium hover:bg-zinc-100 aria-[current=page]:bg-zinc-900 aria-[current=page]:text-white dark:hover:bg-zinc-800 dark:aria-[current=page]:bg-zinc-100 dark:aria-[current=page]:text-zinc-900"
        >
          {item.nav}
        </a>
      {/each}
    </nav>
    <div class="flex items-center gap-3 text-sm">
      <a href="/account" class="text-zinc-600 hover:underline dark:text-zinc-400">{session.user ? displayName(session.user) : ''}</a>
      <button type="button" class="btn-secondary" onclick={() => session.signOut()}>Sign out</button>
    </div>
  </div>
</header>
<main id="main" tabindex="-1" bind:this={main} class="mx-auto max-w-7xl px-4 py-6 outline-none">
  {@render children()}
</main>

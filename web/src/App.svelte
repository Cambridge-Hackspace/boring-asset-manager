<script lang="ts">
  import { onMount } from 'svelte';
  import { interceptLinks, matchRoute, navigate, router } from './lib/router.svelte';
  import { session, signInPath } from './lib/session.svelte';
  import { routes } from './routes';
  import Shell from './Shell.svelte';
  import SignIn from './views/SignIn.svelte';
  import NotFound from './views/NotFound.svelte';
  import Forbidden from './views/Forbidden.svelte';

  let loadError = $state('');

  onMount(() => {
    session.load().catch((err) => (loadError = err instanceof Error ? err.message : String(err)));
    return interceptLinks(document.body);
  });

  const match = $derived.by(() => {
    for (const route of routes) {
      const params = matchRoute(route.pattern, router.path);
      if (params) return { route, params };
    }
    return null;
  });

  const allowed = $derived(!match?.route.permissions || session.canAny(...match.route.permissions));

  // Signed-out visitors go to sign-in, and come back afterward.
  $effect(() => {
    if (session.loaded && !session.user && router.path !== '/sign-in') {
      navigate(signInPath(router.path + router.search), true);
    }
  });

  $effect(() => {
    const title = router.path === '/sign-in' ? 'Sign in' : match ? match.route.title : 'Not found';
    document.title = `${title} · BAM!`;
  });
</script>

{#if loadError}
  <main class="mx-auto max-w-md p-8">
    <h1 class="text-xl font-semibold">BAM! could not start</h1>
    <p class="error-text mt-2" role="alert">{loadError}</p>
  </main>
{:else if !session.loaded}
  <p class="p-8 text-sm text-zinc-600 dark:text-zinc-400" role="status">Loading…</p>
{:else if router.path === '/sign-in'}
  <SignIn />
{:else if session.user}
  <Shell>
    {#if !match}
      <NotFound />
    {:else if !allowed}
      <Forbidden />
    {:else}
      {#key router.path}
        <match.route.component params={match.params} />
      {/key}
    {/if}
  </Shell>
{/if}

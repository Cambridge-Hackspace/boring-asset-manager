<script lang="ts">
  import { onMount } from 'svelte';
  import { interceptLinks, matchRoute, navigate, router } from './lib/router.svelte';
  import { session, signInPath } from './lib/session.svelte';
  import { movedTo, routes } from './routes';
  import Shell from './Shell.svelte';
  import Footer from './components/Footer.svelte';
  import IconPaletteStyle from './components/IconPaletteStyle.svelte';
  import UpdatePrompt from './components/UpdatePrompt.svelte';
  import { iconset } from './lib/iconset.svelte';
  import { applyThemeColor, installedApp, isOffline } from './lib/install/app.svelte';
  import { offline as offlineCopy } from './lib/offline/offline.svelte';
  import { appearance } from './lib/theme.svelte';
  import AccountLinkPage, { accountLinkPaths } from './views/AccountLinkPage.svelte';
  import FactorSetupPage from './views/FactorSetupPage.svelte';
  import SignIn from './views/SignIn.svelte';
  import NotFound from './views/NotFound.svelte';
  import Forbidden from './views/Forbidden.svelte';
  import OfflinePage from './views/OfflinePage.svelte';
  import NeedsConnection from './views/NeedsConnection.svelte';

  let loadError = $state('');
  // Opened with no connection, as an installed app can be: its shell is
  // kept, but who is signed in comes from the server (docs/installing.md#offline).
  let offline = $state(false);
  // Whether the session was read, or could not be: until then, nobody is
  // sent to sign in, so that an app opened offline keeps its address.
  let started = $state(false);

  function load() {
    loadError = '';
    offline = false;
    started = false;
    session
      .load()
      .then(() => sync())
      .catch(async (err) => {
        offline = isOffline(err);
        // With a copy kept here, the app opens from it (docs/installing.md#offline).
        const me = offline ? await offlineCopy.goOffline() : null;
        if (me) session.adoptOffline(me);
        else loadError = err instanceof Error ? err.message : String(err);
      })
      .finally(() => (started = true));
  }

  // Online and signed in: the waiting changes go, and the copy is refreshed
  // when it is due, or cleared when it is no longer the person's.
  function sync() {
    offlineCopy.standalone = installedApp.standalone;
    // Someone who reads no assets has no copy kept for them, as nobody
    // signed in has none.
    if (session.offline || !navigator.onLine) return;
    void offlineCopy.sync(session.can('assets.read') ? session.me : { user: null, permissions: [] });
  }

  onMount(() => {
    load();
    // Back online, it opens without being asked, and from the copy, goes
    // back to the server.
    const online = () => (offline || session.offline ? load() : sync());
    window.addEventListener('online', online);
    const shown = () => document.visibilityState === 'visible' && sync();
    document.addEventListener('visibilitychange', shown);
    // Each minute, whether the copy is due a refresh.
    const timer = window.setInterval(() => session.user && sync(), 60_000);
    const stop = interceptLinks(document.body);
    return () => {
      window.removeEventListener('online', online);
      document.removeEventListener('visibilitychange', shown);
      window.clearInterval(timer);
      stop();
    };
  });

  // The bar above the page in the instance's colors, and the person's
  // light or dark.
  $effect(() => {
    if (installedApp.settings) applyThemeColor(installedApp.settings, appearance.theme);
  });

  const match = $derived.by(() => {
    for (const route of routes) {
      const params = matchRoute(route.pattern, router.path);
      if (params) return { route, params };
    }
    return null;
  });

  const allowed = $derived(!match?.route.permissions || session.canAny(...match.route.permissions));
  const workspace = $derived(allowed && !!match?.route.workspace);
  // Signed out, a page is open when the public role allows it (docs/access.md).
  const openToPublic = $derived(!!match?.route.public && allowed);

  // A page that moved is opened where it is now, in place of the old
  // address in history.
  $effect(() => {
    const to = movedTo(router.path);
    if (to) navigate(to + router.search + window.location.hash, true);
  });

  // Other signed-out visitors go to sign-in, and come back afterward.
  $effect(() => {
    if (started && !loadError && !session.user && router.path !== '/sign-in' && router.path !== '/signin/local' && !accountLinkPaths.includes(router.path) && !openToPublic) {
      navigate(signInPath(router.path + router.search), true);
    }
  });

  $effect(() => {
    const title = router.path === '/sign-in' || router.path === '/signin/local' ? 'Sign in' : match ? match.route.title : 'Not found';
    document.title = `${title} · BAM!`;
  });
</script>

{#if loadError && offline}
  <!-- nothing kept here to open from -->
  <OfflinePage onretry={load} />
{:else if loadError}
  <main class="mx-auto max-w-md p-8">
    <h1 class="text-xl font-semibold">BAM! could not start</h1>
    <p class="error-text mt-2" role="alert">{loadError}</p>
  </main>
{:else if !session.loaded || !started}
  <p class="p-8 text-sm text-zinc-600 dark:text-zinc-400" role="status">Loading…</p>
{:else if router.path === '/sign-in' || router.path === '/signin/local'}
  <SignIn local={router.path === '/signin/local'} />
{:else if accountLinkPaths.includes(router.path)}
  <AccountLinkPage />
{:else if session.user && session.factorSetup}
  <FactorSetupPage />
{:else if session.user || openToPublic}
  <Shell {workspace}>
    {#if !match}
      <NotFound />
    {:else if !allowed}
      <Forbidden route={match.route} />
    {:else if session.offline && !match.route.offline}
      <NeedsConnection />
    {:else}
      {#key router.path}
        <match.route.component params={match.params} />
      {/key}
    {/if}
  </Shell>
{/if}

{#if !workspace}<Footer />{/if}
<!-- the colors icons paint with (docs/icons.md), once there are icons -->
{#if iconset.palette.length}<IconPaletteStyle palette={iconset.palette} />{/if}
<!-- a new release, waiting for Reload to update (docs/installing.md#updates) -->
<UpdatePrompt />

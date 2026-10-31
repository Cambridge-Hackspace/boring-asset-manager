<script lang="ts">
  import { untrack } from 'svelte';
  import { ApiError } from '../lib/api';
  import { navigate, router } from '../lib/router.svelte';
  import { afterSignIn, session } from '../lib/session.svelte';

  let email = $state('');
  let password = $state('');
  let error = $state('');
  let busy = $state(false);

  // Once signed in (or already, say after the back button), carry on. Only
  // the user is tracked: navigating changes the query, and that must not
  // send the user on a second time.
  $effect(() => {
    if (!session.user) return;
    untrack(() => {
      if (router.path === '/sign-in') navigate(afterSignIn(router.query.get('next')), true);
    });
  });

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    error = '';
    busy = true;
    try {
      await session.signIn(email, password);
    } catch (err) {
      error = err instanceof ApiError ? err.message : 'Could not reach BAM!. Check your connection and try again.';
    } finally {
      busy = false;
    }
  }
</script>

<main class="mx-auto mt-16 max-w-sm px-4">
  <h1 class="mb-6 text-center text-3xl font-black tracking-tight">BAM!</h1>
  <form class="card space-y-4" onsubmit={submit} aria-describedby={error ? 'sign-in-error' : undefined}>
    <h2 class="text-lg font-semibold">Sign in</h2>
    <div>
      <label class="label" for="email">Email</label>
      <input id="email" class="input" type="email" autocomplete="username" required bind:value={email} />
    </div>
    <div>
      <label class="label" for="password">Password</label>
      <input id="password" class="input" type="password" autocomplete="current-password" required bind:value={password} />
    </div>
    {#if error}
      <p id="sign-in-error" class="error-text" role="alert">{error}</p>
    {/if}
    <button type="submit" class="btn-primary w-full" disabled={busy}>{busy ? 'Signing in…' : 'Sign in'}</button>
    <p class="text-center text-xs text-zinc-600 dark:text-zinc-400">Forgot your password? Ask whoever manages users here to set a new one.</p>
  </form>
  <p class="mt-4 text-center text-sm"><a class="inline-flex min-h-11 min-w-11 items-center justify-center underline" href="/help">Help</a></p>
</main>

<script lang="ts">
  import { tick, untrack } from 'svelte';
  import { ApiError } from '../lib/api';
  import { navigate, router } from '../lib/router.svelte';
  import { afterSignIn, session } from '../lib/session.svelte';

  let email = $state('');
  let password = $state('');
  let error = $state('');
  let busy = $state(false);
  /** After a password accepted for someone with a second factor: its code. */
  let secondStep = $state(false);
  let code = $state('');
  let codeInput = $state<HTMLInputElement>();

  // Once signed in (or already, say after the back button), carry on. Only
  // the user is tracked: navigating changes the query, and that must not
  // send the user on a second time.
  $effect(() => {
    if (!session.user) return;
    untrack(() => {
      if (router.path === '/sign-in') navigate(afterSignIn(router.query.get('next')), true);
    });
  });

  function explain(err: unknown): string {
    return err instanceof ApiError ? err.message : 'Could not reach BAM!. Check your connection and try again.';
  }

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    error = '';
    busy = true;
    try {
      if (await session.signIn(email, password)) {
        secondStep = true;
        password = '';
        await tick();
        codeInput?.focus();
      }
    } catch (err) {
      error = explain(err);
    } finally {
      busy = false;
    }
  }

  async function submitCode(event: SubmitEvent) {
    event.preventDefault();
    error = '';
    busy = true;
    try {
      await session.finishSignIn(code);
    } catch (err) {
      error = explain(err);
      code = '';
      // An expired second step starts again from the password.
      if (err instanceof ApiError && err.status === 401 && err.message.includes('expired')) startOver(error);
    } finally {
      busy = false;
    }
  }

  function startOver(message = '') {
    secondStep = false;
    code = '';
    error = message;
  }
</script>

<main class="mx-auto mt-16 max-w-sm px-4">
  <h1 class="mb-6 text-center text-3xl font-black tracking-tight">BAM!</h1>
  {#if secondStep}
    <form class="card space-y-4" onsubmit={submitCode} aria-describedby={error ? 'sign-in-error' : 'code-help'}>
      <h2 class="text-lg font-semibold">Enter your code</h2>
      <div>
        <label class="label" for="code">Code from your authenticator app</label>
        <input
          id="code"
          class="input font-mono tracking-widest"
          inputmode="numeric"
          autocomplete="one-time-code"
          pattern="[0-9 ]*"
          maxlength="7"
          required
          aria-describedby="code-help"
          bind:this={codeInput}
          bind:value={code}
        />
        <p id="code-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">The six digits your app shows for {email} now.</p>
      </div>
      {#if error}
        <p id="sign-in-error" class="error-text" role="alert">{error}</p>
      {/if}
      <button type="submit" class="btn-primary w-full" disabled={busy}>{busy ? 'Checking…' : 'Sign in'}</button>
      <button type="button" class="btn-secondary w-full" onclick={() => startOver()}>Back</button>
      <p class="text-center text-xs text-zinc-600 dark:text-zinc-400">Lost your phone? Ask whoever manages users here to remove your second factor.</p>
    </form>
  {:else}
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
  {/if}
  <p class="mt-4 text-center text-sm"><a class="inline-flex min-h-11 min-w-11 items-center justify-center underline" href="/help">Help</a></p>
</main>

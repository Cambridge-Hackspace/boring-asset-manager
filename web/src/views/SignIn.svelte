<script lang="ts">
  import { onMount, tick, untrack } from 'svelte';
  import { ApiError, request } from '../lib/api';
  import type { PublicProviders } from '../lib/models';
  import { navigate, router } from '../lib/router.svelte';
  import { afterSignIn, session } from '../lib/session.svelte';

  /** The break-glass page, /signin/local, for signing in with a password while passwords are off. */
  let { local = false }: { local?: boolean } = $props();

  let email = $state('');
  let password = $state('');
  // A sign-in through a provider that failed comes back here, saying why.
  let error = $state(router.query.get('error') ?? '');
  let busy = $state(false);
  /** After a password, or a provider, accepted for someone with a second factor: its code. */
  let secondStep = $state(router.query.get('factor') === 'required');
  let code = $state('');
  let codeInput = $state<HTMLInputElement>();
  /** A recovery code in place of the app's (docs/access.md#recovery-codes). */
  let recovery = $state(false);
  /** The identity providers' buttons, and whether passwords are on (docs/access.md). */
  let providers = $state<PublicProviders>({ providers: [], local_passwords: true });

  const next = $derived(router.query.get('next') ?? '');
  const startHref = (key: string) =>
    `/api/v1/auth/providers/${encodeURIComponent(key)}/start` + (next ? `?next=${encodeURIComponent(afterSignIn(next))}` : '');

  onMount(() => {
    request<PublicProviders>('GET', '/auth/providers/public').then(
      (p) => (providers = p),
      () => {},
    );
    if (secondStep) void tick().then(() => codeInput?.focus());
  });

  // Once signed in (or already, say after the back button), carry on. Only
  // the user is tracked: navigating changes the query, and that must not
  // send the user on a second time.
  $effect(() => {
    if (!session.user) return;
    untrack(() => {
      if (router.path === '/sign-in' || router.path === '/signin/local') navigate(afterSignIn(router.query.get('next')), true);
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
      if (await session.signIn(email, password, local)) {
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
    recovery = false;
    code = '';
    error = message;
  }

  async function useRecovery(on: boolean) {
    recovery = on;
    code = '';
    error = '';
    await tick();
    codeInput?.focus();
  }
</script>

<main class="mx-auto mt-16 max-w-sm px-4">
  <h1 class="mb-6 text-center text-3xl font-black tracking-tight">BAM!</h1>
  {#if secondStep}
    <form class="card space-y-4" onsubmit={submitCode} aria-describedby={error ? 'sign-in-error' : 'code-help'}>
      <h2 class="text-lg font-semibold">Enter your code</h2>
      <div>
        {#if recovery}
          <label class="label" for="code">Recovery code</label>
          <input
            id="code"
            class="input font-mono tracking-widest"
            autocomplete="off"
            autocapitalize="off"
            spellcheck="false"
            maxlength="16"
            required
            aria-describedby="code-help"
            bind:this={codeInput}
            bind:value={code}
          />
          <p id="code-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">One of the codes you saved when you set up two-factor sign-in, such as 4f8kq-29xmd. Each works once.</p>
        {:else}
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
          <p id="code-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">The six digits your app shows for {email || 'your account'} now.</p>
        {/if}
      </div>
      {#if error}
        <p id="sign-in-error" class="error-text" role="alert">{error}</p>
      {/if}
      <button type="submit" class="btn-primary w-full" disabled={busy}>{busy ? 'Checking…' : 'Sign in'}</button>
      <button type="button" class="btn-secondary w-full" onclick={() => startOver()}>Back</button>
      {#if recovery}
        <button type="button" class="w-full text-sm underline" onclick={() => useRecovery(false)}>Use a code from your app</button>
      {:else}
        <button type="button" class="w-full text-sm underline" onclick={() => useRecovery(true)}>Use a recovery code</button>
      {/if}
      <p class="text-center text-xs text-zinc-600 dark:text-zinc-400">Lost your phone and your recovery codes? Ask whoever manages users here to remove your second factor.</p>
    </form>
  {:else}
    {#if !local && providers.providers.length}
      <div class="card mb-4 space-y-2" aria-label="Sign in with">
        {#each providers.providers as p (p.key)}
          <a class="btn-secondary flex w-full items-center justify-center gap-2" href={startHref(p.key)}>
            {#if p.icon}<img src={p.icon} alt="" class="h-5 w-5" />{/if}{p.label}
          </a>
        {/each}
        {#if !providers.local_passwords && error}
          <p id="sign-in-error" class="error-text" role="alert">{error}</p>
        {/if}
      </div>
    {/if}
    {#if local || providers.local_passwords}
    <form class="card space-y-4" onsubmit={submit} aria-describedby={error ? 'sign-in-error' : undefined}>
      <h2 class="text-lg font-semibold">{local ? 'Sign in with a password' : 'Sign in'}</h2>
      {#if local && !providers.local_passwords}
        <p class="text-sm">Signing in with a password is turned off here. This page is for those who manage users, when the sign-in buttons do not work; each use is recorded.</p>
      {/if}
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
  {/if}
  <p class="mt-4 text-center text-sm"><a class="inline-flex min-h-11 min-w-11 items-center justify-center underline" href="/help">Help</a></p>
</main>

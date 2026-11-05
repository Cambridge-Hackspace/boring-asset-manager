<!--
  The pages links emailed about someone's own account lead to
  (docs/access.md#account-emails): accepting an invitation, setting a new
  password, and removing a lost second factor. They open signed out.
-->
<script module lang="ts">
  /** The paths this page answers. */
  export const accountLinkPaths = ['/invite', '/reset-password', '/recover-factor'];
</script>

<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../lib/api';
  import { messageOf } from '../lib/errors';
  import { navigate, router } from '../lib/router.svelte';
  import { session } from '../lib/session.svelte';

  const path = router.path;
  const token = router.query.get('token') ?? '';

  let loaded = $state(false);
  let error = $state('');
  let done = $state('');
  let busy = $state(false);
  let who = $state<{ email: string; name?: string; local_passwords?: boolean; providers?: { key: string; label: string; icon?: string }[] } | null>(null);
  let email = $state('');
  let password = $state('');

  const heading = $derived(
    path === '/invite' ? 'Accept your invitation' : path === '/reset-password' ? 'Set a new password' : 'Remove your second factor',
  );

  onMount(async () => {
    // Asking for a reset needs no link.
    if (path === '/reset-password' && !token) {
      loaded = true;
      return;
    }
    const route = path === '/invite' ? '/auth/invite' : path === '/reset-password' ? '/auth/password-reset' : '/auth/factor-recovery';
    try {
      who = await request('GET', `${route}?token=${encodeURIComponent(token)}`);
    } catch (err) {
      error = messageOf(err);
    } finally {
      loaded = true;
    }
  });

  async function run(action: () => Promise<void>) {
    error = '';
    busy = true;
    try {
      await action();
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }

  function askForReset(event: SubmitEvent) {
    event.preventDefault();
    void run(async () => {
      done = (await request<{ message: string }>('POST', '/auth/password-reset', { email })).message;
    });
  }

  function setPassword(event: SubmitEvent) {
    event.preventDefault();
    void run(async () => {
      if (path === '/invite') {
        await request('POST', '/auth/invite', { token, password });
        await session.load();
        navigate('/', true);
        return;
      }
      done = (await request<{ message: string }>('POST', '/auth/password-reset/confirm', { token, password })).message;
    });
  }

  function removeFactor() {
    void run(async () => {
      done = (await request<{ message: string }>('POST', '/auth/factor-recovery', { token })).message;
    });
  }
</script>

<main id="main" class="mx-auto mt-16 max-w-sm space-y-4 px-4">
  <h1 class="text-center text-3xl font-black tracking-tight">BAM!</h1>
  <section class="card space-y-4" aria-labelledby="link-heading">
    <h2 id="link-heading" class="text-lg font-semibold">{heading}</h2>
    {#if error}<p class="error-text" role="alert">{error}</p>{/if}
    {#if done}
      <p role="status" class="text-sm">{done}</p>
      <a class="btn-primary block w-full text-center" href="/sign-in">Sign in</a>
    {:else if !loaded}
      <p class="text-sm text-zinc-600 dark:text-zinc-400">Loading…</p>
    {:else if path === '/reset-password' && !token}
      <form class="space-y-4" onsubmit={askForReset}>
        <p class="text-sm">Give the email of your account, and a link to set a new password is emailed to it.</p>
        <div>
          <label class="label" for="reset-email">Email</label>
          <input id="reset-email" class="input" type="email" autocomplete="username" required bind:value={email} />
        </div>
        <button type="submit" class="btn-primary w-full" disabled={busy}>Email me a link</button>
      </form>
    {:else if who && path === '/recover-factor'}
      <p class="text-sm">
        This removes the second factor of <strong>{who.email}</strong>, so that the password alone signs in until you set up
        another, and signs you out everywhere.
      </p>
      <p class="rounded border border-amber-400 p-2 text-sm dark:border-amber-600">
        Anyone who can read your email and knows your password can do this, so email is a way around two-factor sign-in. Set up a
        new second factor as soon as you have signed in.
      </p>
      <button type="button" class="btn-danger w-full" disabled={busy} onclick={removeFactor}>Remove my second factor</button>
    {:else if who}
      {#if path === '/invite'}
        <p class="text-sm">Welcome{who.name ? `, ${who.name}` : ''}. Your account is <strong>{who.email}</strong>.</p>
        {#if who.providers?.length}
          <div class="space-y-2" aria-label="Sign in with">
            {#each who.providers as p (p.key)}
              <a class="btn-secondary flex w-full items-center justify-center gap-2" href="/api/v1/auth/providers/{encodeURIComponent(p.key)}/start?invite={encodeURIComponent(token)}">
                {#if p.icon}<img src={p.icon} alt="" class="h-5 w-5" />{/if}{p.label}
              </a>
            {/each}
          </div>
        {/if}
      {/if}
      {#if path !== '/invite' || who.local_passwords}
        <form class="space-y-4" onsubmit={setPassword}>
          {#if path === '/invite' && who.providers?.length}<p class="text-sm">Or set a password:</p>{/if}
          <div>
            <label class="label" for="new-password">New password</label>
            <input id="new-password" class="input" type="password" autocomplete="new-password" required minlength="8" maxlength="72" bind:value={password} />
            <p class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">8-72 characters.</p>
          </div>
          <button type="submit" class="btn-primary w-full" disabled={busy}>{path === '/invite' ? 'Set it and sign in' : 'Set my password'}</button>
        </form>
      {/if}
    {/if}
  </section>
</main>

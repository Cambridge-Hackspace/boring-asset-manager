<!--
  Two-factor sign-in on the account page (docs/access.md): set up an
  authenticator app with a QR code, confirmed with a code from it, and turn
  it off with the password and a code.
-->
<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { request } from '../lib/api';
  import { messageOf } from '../lib/errors';
  import type { Factor } from '../lib/models';
  import ErrorMessage from '../components/ErrorMessage.svelte';

  let factors = $state<Factor[]>([]);
  let available = $state(true);
  let loaded = $state(false);
  let error = $state('');
  let status = $state('');
  let busy = $state(false);

  /** An app being set up: its QR code and secret, until confirmed. */
  let setup = $state<{ qr_svg: string; secret: string } | null>(null);
  let code = $state('');
  let codeInput = $state<HTMLInputElement>();

  /** The factor being turned off, with the password and a code. */
  let removing = $state<string | null>(null);
  let password = $state('');
  let removeCode = $state('');

  onMount(() => {
    void load();
  });

  async function load() {
    try {
      const res = await request<{ factors: Factor[]; available: boolean }>('GET', '/auth/me/factors');
      factors = res.factors ?? [];
      available = res.available;
    } catch (err) {
      error = messageOf(err);
    } finally {
      loaded = true;
    }
  }

  async function start() {
    error = status = '';
    busy = true;
    try {
      setup = await request<{ qr_svg: string; secret: string }>('POST', '/auth/me/factors/totp');
      code = '';
      await tick();
      codeInput?.focus();
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }

  async function confirmSetup(event: SubmitEvent) {
    event.preventDefault();
    error = '';
    busy = true;
    try {
      await request('POST', '/auth/me/factors/totp/confirm', { code });
      setup = null;
      status = 'Two-factor sign-in is on. Signing in now asks for a code from your app.';
      await load();
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }

  async function remove(event: SubmitEvent, id: string) {
    event.preventDefault();
    error = '';
    busy = true;
    try {
      await request('DELETE', `/auth/me/factors/${encodeURIComponent(id)}`, { password, code: removeCode });
      removing = null;
      password = removeCode = '';
      status = 'Two-factor sign-in is off. Signing in asks for your password alone.';
      await load();
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }

  const when = (iso?: string) => (iso ? new Date(iso).toLocaleDateString() : '');
</script>

<section class="card space-y-3" aria-labelledby="two-factor-heading">
  <h2 id="two-factor-heading" class="text-lg font-semibold">Two-factor sign-in</h2>
  {#if !loaded}
    <p class="text-sm text-zinc-600 dark:text-zinc-400">Loading…</p>
  {:else if factors.length}
    <ul class="space-y-2 text-sm">
      {#each factors as f (f.id)}
        <li>
          <strong>Authenticator app</strong>, set up {when(f.confirmed_at)}{#if f.last_used_at}; last used {when(f.last_used_at)}{/if}.
          {#if removing === f.id}
            <form class="mt-2 space-y-2" onsubmit={(event) => remove(event, f.id)}>
              <div>
                <label class="label" for="remove-password">Your password</label>
                <input id="remove-password" class="input" type="password" required autocomplete="current-password" bind:value={password} />
              </div>
              <div>
                <label class="label" for="remove-code">A code from the app</label>
                <input id="remove-code" class="input font-mono" inputmode="numeric" autocomplete="one-time-code" maxlength="7" required bind:value={removeCode} />
              </div>
              <div class="flex flex-wrap gap-2">
                <button type="submit" class="btn-danger" disabled={busy}>Turn off</button>
                <button type="button" class="btn-secondary" onclick={() => (removing = null)}>Cancel</button>
              </div>
            </form>
          {:else}
            <button type="button" class="btn-secondary mt-1" onclick={() => { removing = f.id; password = removeCode = ''; status = ''; }}>Turn off…</button>
          {/if}
        </li>
      {/each}
    </ul>
  {:else if setup}
    <p class="text-sm">Scan this with an authenticator app, such as Aegis, Google Authenticator or 1Password, then enter the code it shows.</p>
    <img class="mx-auto h-48 w-48 bg-white p-1" src={'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(setup.qr_svg)} alt="QR code to scan into an authenticator app" />
    <p class="text-sm">Or type this key into the app: <code class="font-mono break-all">{setup.secret}</code></p>
    <form class="space-y-2" onsubmit={confirmSetup}>
      <div>
        <label class="label" for="setup-code">Code from the app</label>
        <input id="setup-code" class="input font-mono tracking-widest" inputmode="numeric" autocomplete="one-time-code" maxlength="7" required bind:this={codeInput} bind:value={code} />
      </div>
      <div class="flex flex-wrap gap-2">
        <button type="submit" class="btn-primary" disabled={busy}>Turn on</button>
        <button type="button" class="btn-secondary" onclick={() => (setup = null)}>Cancel</button>
      </div>
    </form>
  {:else if available}
    <p class="text-sm">Ask for a code from an app on your phone as well as your password when you sign in, so that your password alone is not enough.</p>
    <button type="button" class="btn-secondary" disabled={busy} onclick={start}>Set up an authenticator app</button>
  {:else}
    <p class="text-sm">Not available here yet: whoever runs BAM! needs to add a secret key to its configuration.</p>
  {/if}
  <ErrorMessage {error} />
  {#if status}<p class="text-sm text-green-700 dark:text-green-400" role="status">{status}</p>{/if}
</section>

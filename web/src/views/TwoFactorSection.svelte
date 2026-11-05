<!--
  Two-factor sign-in on the account page (docs/access.md): set up an
  authenticator app with a QR code, confirmed with a code from it, and add
  passkeys, each named. Removing one takes the password and a current
  factor. Recovery codes come with the first, shown once, and new ones take
  a current factor.
-->
<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { ApiError, request } from '../lib/api';
  import { messageOf } from '../lib/errors';
  import type { Factor } from '../lib/models';
  import {
    createPasskey,
    getPasskey,
    passkeyMessage,
    passkeysSupported,
    type CreationOptions,
    type PasskeyAssertion,
    type RequestOptions,
  } from '../lib/passkeys';
  import ErrorMessage from '../components/ErrorMessage.svelte';

  // ondone, if given, is called once a factor is set up and its recovery
  // codes put away.
  let { ondone }: { ondone?: () => void } = $props();

  let factors = $state<Factor[]>([]);
  let available = $state(true);
  let passkeysHere = $state(false);
  let codesLeft = $state(0);
  let loaded = $state(false);
  let error = $state('');
  let status = $state('');
  let busy = $state(false);

  /** An app being set up: its QR code and secret, until confirmed. */
  let setup = $state<{ qr_svg: string; secret: string } | null>(null);
  let code = $state('');
  let codeInput = $state<HTMLInputElement>();

  /** A passkey being added: its name. */
  let addingPasskey = $state(false);
  let passkeyName = $state('');
  let passkeyNameInput = $state<HTMLInputElement>();

  /** The factor being removed, with the password and a current factor. */
  let removing = $state<string | null>(null);
  let password = $state('');
  /** A code from the app, confirming it is them. */
  let proofCode = $state('');

  /** New recovery codes, shown until they are put away; never again. */
  let shown = $state<string[] | null>(null);
  let shownHeading = $state<HTMLElement>();
  /** Asking for new recovery codes, with a current factor. */
  let renewing = $state(false);

  const hasApp = $derived(factors.some((f) => f.kind === 'totp'));
  const usable = (f: Factor) => f.kind !== 'passkey' || !f.rp_id || f.rp_id === location.hostname;
  const hasPasskey = $derived(passkeysSupported() && factors.some((f) => f.kind === 'passkey' && usable(f)));
  const canAddPasskey = $derived(passkeysHere && passkeysSupported());

  const codesText = $derived(
    shown ? `BAM! recovery codes for signing in without your second factor.\nEach works once.\n\n${shown.join('\n')}\n` : '',
  );

  onMount(() => {
    void load();
  });

  async function load() {
    try {
      const res = await request<{ factors: Factor[]; available: boolean; passkeys?: boolean; recovery_codes_left: number }>('GET', '/auth/me/factors');
      factors = res.factors ?? [];
      available = res.available;
      passkeysHere = !!res.passkeys;
      codesLeft = res.recovery_codes_left ?? 0;
    } catch (err) {
      error = messageOf(err);
    } finally {
      loaded = true;
    }
  }

  async function show(codes: string[]) {
    shown = codes;
    await tick();
    shownHeading?.focus();
  }

  /** Says what a passkey or the server answered. */
  function explain(err: unknown): string {
    return err instanceof ApiError ? err.message : err instanceof DOMException ? passkeyMessage(err) : messageOf(err);
  }

  /** A current factor: a code from the app, or one of the person's passkeys. */
  async function proof(withPasskey: boolean): Promise<{ code: string } | { passkey: PasskeyAssertion }> {
    if (!withPasskey) return { code: proofCode };
    const { public_key } = await request<{ public_key: RequestOptions }>('POST', '/auth/me/factors/passkey/challenge');
    return { passkey: await getPasskey(public_key) };
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
      const res = await request<{ recovery_codes?: string[] }>('POST', '/auth/me/factors/totp/confirm', { code });
      setup = null;
      status = 'Two-factor sign-in is on. Signing in now asks for a code from your app.';
      await load();
      if (res.recovery_codes?.length) await show(res.recovery_codes);
      else ondone?.();
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }

  async function startPasskey() {
    error = status = '';
    addingPasskey = true;
    passkeyName = '';
    await tick();
    passkeyNameInput?.focus();
  }

  async function addPasskey(event: SubmitEvent) {
    event.preventDefault();
    error = '';
    busy = true;
    try {
      const { public_key } = await request<{ public_key: CreationOptions }>('POST', '/auth/me/factors/passkey');
      const made = await createPasskey(public_key);
      const res = await request<{ recovery_codes?: string[] }>('POST', '/auth/me/factors/passkey/confirm', { name: passkeyName, ...made });
      addingPasskey = false;
      status = `Passkey “${passkeyName.trim()}” added. Signing in can now ask for it.`;
      await load();
      if (res.recovery_codes?.length) await show(res.recovery_codes);
      else ondone?.();
    } catch (err) {
      error = explain(err);
    } finally {
      busy = false;
    }
  }

  async function renew(withPasskey: boolean) {
    error = status = '';
    busy = true;
    try {
      const res = await request<{ recovery_codes: string[] }>('POST', '/auth/me/factors/recovery', await proof(withPasskey));
      renewing = false;
      proofCode = '';
      await load();
      await show(res.recovery_codes);
    } catch (err) {
      error = explain(err);
    } finally {
      busy = false;
    }
  }

  async function remove(id: string, withPasskey: boolean) {
    error = '';
    if (!password) {
      error = 'Enter your password.';
      return;
    }
    busy = true;
    try {
      await request('DELETE', `/auth/me/factors/${encodeURIComponent(id)}`, { password, ...(await proof(withPasskey)) });
      removing = null;
      password = proofCode = '';
      await load();
      status = factors.length ? 'Removed.' : 'Two-factor sign-in is off. Signing in asks for your password alone.';
    } catch (err) {
      error = explain(err);
    } finally {
      busy = false;
    }
  }

  /** Prints the codes alone, from a page of their own. */
  function printCodes() {
    const w = window.open('', '_blank');
    if (!w) {
      error = 'The browser blocked the window to print from. Download the codes instead.';
      return;
    }
    const doc = w.document;
    doc.title = 'BAM! recovery codes';
    const pre = doc.createElement('pre');
    pre.style.font = '16px/1.6 monospace';
    pre.textContent = codesText;
    doc.body.append(pre);
    w.print();
  }

  const when = (iso?: string) => (iso ? new Date(iso).toLocaleDateString() : '');
</script>

<!-- A current factor, confirming it is them: a code from the app, or a passkey. -->
{#snippet proofFields(field: string, action: string, danger: boolean, run: (withPasskey: boolean) => void, cancel: () => void)}
  {#if hasApp}
    <div>
      <label class="label" for={field}>A code from the app</label>
      <input id={field} class="input font-mono" inputmode="numeric" autocomplete="one-time-code" maxlength="7" required={!hasPasskey} bind:value={proofCode} />
    </div>
  {/if}
  <div class="flex flex-wrap gap-2">
    {#if hasApp}
      <button type="submit" class={danger ? 'btn-danger' : 'btn-primary'} disabled={busy}>{action}</button>
    {/if}
    {#if hasPasskey}
      <button type="button" class={hasApp ? 'btn-secondary' : danger ? 'btn-danger' : 'btn-primary'} disabled={busy} onclick={() => run(true)}>
        {action} with a passkey
      </button>
    {/if}
    <button type="button" class="btn-secondary" onclick={cancel}>Cancel</button>
  </div>
{/snippet}

<section class="card space-y-3" aria-labelledby="two-factor-heading">
  <h2 id="two-factor-heading" class="text-lg font-semibold">Two-factor sign-in</h2>
  {#if !loaded}
    <p class="text-sm text-zinc-600 dark:text-zinc-400">Loading…</p>
  {:else}
    {#if factors.length}
      <ul class="space-y-2 text-sm" aria-label="Second factors">
        {#each factors as f (f.id)}
          <li>
            {#if f.kind === 'passkey'}
              <strong>Passkey “{f.name}”</strong>, added {when(f.confirmed_at)}{#if f.last_used_at}; last used {when(f.last_used_at)}{/if}.
              {#if !usable(f)}
                <span class="text-amber-700 dark:text-amber-400">It was added at {f.rp_id}, so it cannot be used here.</span>
              {/if}
            {:else}
              <strong>Authenticator app</strong>, set up {when(f.confirmed_at)}{#if f.last_used_at}; last used {when(f.last_used_at)}{/if}.
            {/if}
            {#if removing === f.id}
              <form
                class="mt-2 space-y-2"
                onsubmit={(event) => {
                  event.preventDefault();
                  void remove(f.id, false);
                }}
              >
                <div>
                  <label class="label" for="remove-password">Your password</label>
                  <input id="remove-password" class="input" type="password" required autocomplete="current-password" bind:value={password} />
                </div>
                {@render proofFields('remove-code', 'Remove', true, (withPasskey) => void remove(f.id, withPasskey), () => (removing = null))}
              </form>
            {:else}
              <button
                type="button"
                class="btn-secondary mt-1"
                aria-label={f.kind === 'passkey' ? `Remove passkey ${f.name}` : 'Remove the authenticator app'}
                onclick={() => {
                  removing = f.id;
                  password = proofCode = '';
                  status = '';
                }}>Remove…</button
              >
            {/if}
          </li>
        {/each}
      </ul>
    {/if}

    {#if shown}
      <div class="space-y-2 rounded border border-amber-400 p-3 dark:border-amber-600">
        <h3 class="font-semibold" tabindex="-1" bind:this={shownHeading}>Your recovery codes</h3>
        <p class="text-sm">
          Each signs in once in place of your second factor, if you lose it. Keep them somewhere safe, apart from your phone
          and keys. They are shown only now.
        </p>
        <ul class="grid grid-cols-2 gap-x-6 gap-y-1 font-mono text-sm" aria-label="Recovery codes">
          {#each shown as c (c)}<li>{c}</li>{/each}
        </ul>
        <div class="flex flex-wrap gap-2">
          <a class="btn-secondary" href={'data:text/plain;charset=utf-8,' + encodeURIComponent(codesText)} download="bam-recovery-codes.txt">Download</a>
          <button type="button" class="btn-secondary" onclick={printCodes}>Print</button>
          <button
            type="button"
            class="btn-primary"
            onclick={() => {
              shown = null;
              ondone?.();
            }}>I have saved them</button
          >
        </div>
      </div>
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
    {:else if addingPasskey}
      <form class="space-y-2" onsubmit={addPasskey}>
        <div>
          <label class="label" for="passkey-name">Name this passkey</label>
          <input id="passkey-name" class="input" required maxlength="100" placeholder="YubiKey, Phone…" bind:this={passkeyNameInput} bind:value={passkeyName} />
          <p class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">Your browser then asks you to touch your key, or to unlock your phone or computer.</p>
        </div>
        <div class="flex flex-wrap gap-2">
          <button type="submit" class="btn-primary" disabled={busy}>Add the passkey</button>
          <button type="button" class="btn-secondary" onclick={() => (addingPasskey = false)}>Cancel</button>
        </div>
      </form>
    {:else}
      {#if factors.length}
        <div class="space-y-2 text-sm">
          <p>
            <strong>Recovery codes:</strong>
            {codesLeft === 1 ? '1 left' : `${codesLeft} left`}, each signing in once without your second factor.
          </p>
          {#if codesLeft < 3}
            <p class="text-amber-700 dark:text-amber-400" role="status">
              {codesLeft === 0 ? 'None are left' : 'Few are left'}: make new ones, so that losing your second factor does not lock you out.
            </p>
          {/if}
          {#if renewing}
            <form
              class="space-y-2"
              onsubmit={(event) => {
                event.preventDefault();
                void renew(false);
              }}
            >
              <p class="text-xs text-zinc-600 dark:text-zinc-400">New codes replace all the old ones.</p>
              {@render proofFields('renew-code', 'Make new codes', false, (withPasskey) => void renew(withPasskey), () => (renewing = false))}
            </form>
          {:else}
            <button
              type="button"
              class="btn-secondary"
              onclick={() => {
                renewing = true;
                proofCode = '';
                status = '';
              }}>Make new recovery codes…</button
            >
          {/if}
        </div>
      {:else}
        <p class="text-sm">
          Ask for a second factor as well as your password when you sign in, so that your password alone is not enough: a
          passkey, such as a security key or your phone's screen lock, or a code from an app on your phone.
        </p>
      {/if}
      {#if canAddPasskey || (available && !hasApp)}
        <div class="flex flex-wrap gap-2">
          {#if canAddPasskey}
            <button type="button" class="btn-secondary" disabled={busy} onclick={startPasskey}>Add a passkey</button>
          {/if}
          {#if available && !hasApp}
            <button type="button" class="btn-secondary" disabled={busy} onclick={start}>Set up an authenticator app</button>
          {/if}
        </div>
      {:else if !factors.length}
        <p class="text-sm">Not available here yet: whoever runs BAM! needs to add a secret key to its configuration, or set its address.</p>
      {/if}
    {/if}
  {/if}
  <ErrorMessage {error} />
  {#if status}<p class="text-sm text-green-700 dark:text-green-400" role="status">{status}</p>{/if}
</section>

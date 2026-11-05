<!--
  How people sign in (docs/access.md#signing-in-with-an-identity-provider):
  the identity providers, each a button on the sign-in page, and whether a
  password signs in too.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { AccountEmailSettings, Protocol, Provider } from '../../lib/models';
  import ConfirmButton from '../../components/ConfirmButton.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';
  import PageHeader from '../../components/PageHeader.svelte';
  import ProviderForm from './ProviderForm.svelte';

  let { params: _params }: { params: Record<string, string> } = $props();

  type Named = { id: string; name: string };
  let providers = $state<Provider[]>([]);
  let protocols = $state<Protocol[]>([]);
  let roles = $state<Named[]>([]);
  let scopes = $state<Named[]>([]);
  let passwords = $state(true);
  let secretKey = $state(true);
  let mails = $state<AccountEmailSettings>({ password_reset: false, factor_recovery_email: false, factor_recovery_email_required: false, can_email_links: false });
  /** The host passkeys are bound to, base_url's, and how many were added at another. */
  let passkeyHost = $state('');
  let passkeysElsewhere = $state(0);
  let loading = $state(true);
  let error = $state('');
  let notice = $state('');
  // The provider being edited, "new" for a new one, or null.
  let editing = $state<string | null>(null);

  const protocolLabel = (name: string) => protocols.find((p) => p.name === name)?.label ?? name;
  const roleName = (id: string) => roles.find((r) => r.id === id)?.name ?? id;

  async function load() {
    try {
      const res = await request<{
        providers: Provider[];
        protocols: Protocol[];
        roles: Named[];
        scopes: Named[];
        local_passwords: boolean;
        secret_key: boolean;
        account_emails: AccountEmailSettings;
        passkey_host?: string;
        passkeys_elsewhere?: number;
      }>(
        'GET',
        '/auth/providers',
      );
      providers = res.providers ?? [];
      protocols = res.protocols ?? [];
      roles = res.roles ?? [];
      scopes = res.scopes ?? [];
      passwords = res.local_passwords;
      secretKey = res.secret_key;
      if (res.account_emails) mails = res.account_emails;
      passkeyHost = res.passkey_host ?? '';
      passkeysElsewhere = res.passkeys_elsewhere ?? 0;
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
  }
  onMount(load);

  async function saved() {
    editing = null;
    notice = 'Provider saved.';
    await load();
  }

  async function setPasswords(on: boolean) {
    error = notice = '';
    try {
      await request('PUT', '/auth/settings', { local_passwords: on });
      passwords = on;
      notice = on ? 'Signing in with a password is on.' : 'Signing in with a password is off.';
    } catch (err) {
      error = messageOf(err);
    }
  }

  // A setting of account emails, saved alone (docs/access.md#account-emails).
  async function setMail(key: 'password_reset' | 'factor_recovery_email' | 'factor_recovery_email_required', on: boolean, said: string) {
    error = notice = '';
    try {
      const res = await request<{ account_emails: AccountEmailSettings }>('PUT', '/auth/settings', { [key]: on });
      mails = res.account_emails;
      notice = said;
    } catch (err) {
      error = messageOf(err);
      await load();
    }
  }

  async function remove(p: Provider) {
    error = notice = '';
    try {
      await request('DELETE', `/auth/providers/${encodeURIComponent(p.id)}`);
      notice = `Removed ${p.name}.`;
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }

  // Swaps a provider's place among the buttons with its neighbor's.
  async function move(i: number, by: number) {
    const a = providers[i]!;
    const b = providers[i + by]!;
    error = notice = '';
    const body = (p: Provider, position: number) => ({ ...p, position, secrets: {} });
    try {
      await request('PUT', `/auth/providers/${encodeURIComponent(a.id)}`, body(a, i + by));
      await request('PUT', `/auth/providers/${encodeURIComponent(b.id)}`, body(b, i));
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }
</script>

<PageHeader title="Sign-in" lede="How people sign in: with a password, or through identity providers your space already runs, each a button on the sign-in page.">
  {#snippet actions()}
    {#if editing === null && protocols.length}
      <button type="button" class="btn-primary" onclick={() => (editing = 'new')}>New provider</button>
    {/if}
  {/snippet}
</PageHeader>
<ErrorMessage {error} />
{#if notice}<p class="mb-2 text-sm text-green-700 dark:text-green-400" role="status">{notice}</p>{/if}

{#if !loading}
  <section class="card mb-6 max-w-2xl space-y-2" aria-labelledby="passwords-heading">
    <h2 id="passwords-heading" class="font-semibold">Passwords</h2>
    <label class="flex items-center gap-2 text-sm">
      <input type="checkbox" checked={passwords} onchange={(e) => setPasswords(e.currentTarget.checked)} /> People can sign in with a password
    </label>
    <p class="text-xs text-zinc-600 dark:text-zinc-400">
      Off, the sign-in page shows only the providers' buttons, and passwords cannot be set. Those who manage users can still sign in with a
      password at <a class="underline" href="/signin/local">/signin/local</a>, if a provider is down; each time is recorded. On the command
      line, <code>bam admin password</code> always works.
    </p>
  </section>

  <section class="card mb-6 max-w-2xl space-y-2" aria-labelledby="account-emails-heading">
    <h2 id="account-emails-heading" class="font-semibold">Account emails</h2>
    {#if !mails.can_email_links}
      <p class="text-sm">
        Links can be emailed once email is set up and the server's <code>base_url</code> is set (see the documentation's Notifications, Email).
        Until then, invitations cannot be sent, and these stay off.
      </p>
    {/if}
    <label class="flex items-center gap-2 text-sm">
      <input type="checkbox" checked={mails.password_reset} disabled={!mails.can_email_links}
        onchange={(e) => setMail('password_reset', e.currentTarget.checked, e.currentTarget.checked ? 'Forgot password is offered.' : 'Forgot password is not offered.')} />
      Offer <em>Forgot password</em> on the sign-in page
    </label>
    <p class="text-xs text-zinc-600 dark:text-zinc-400">
      A link to set a new password is emailed, working once for 30 minutes; using it signs them out everywhere. Never while passwords are off.
    </p>
    <label class="flex items-center gap-2 text-sm">
      <input type="checkbox" checked={mails.factor_recovery_email} disabled={!mails.can_email_links}
        onchange={(e) => setMail('factor_recovery_email', e.currentTarget.checked, e.currentTarget.checked ? 'A lost second factor can be removed by email.' : 'A lost second factor cannot be removed by email.')} />
      Let someone who has lost their second factor remove it by email
    </label>
    <p class="text-xs text-amber-900 dark:text-amber-200">
      After their password, a link is emailed that removes their second factor. This makes their email a way around two-factor sign-in:
      whoever can read it and knows the password can sign in.
    </p>
    {#if mails.factor_recovery_email}
      <label class="flex items-center gap-2 pl-6 text-sm">
        <input type="checkbox" checked={mails.factor_recovery_email_required}
          onchange={(e) => setMail('factor_recovery_email_required', e.currentTarget.checked, 'Saved.')} />
        Also for those whose roles require two-factor sign-in
      </label>
    {/if}
  </section>

  <section class="card mb-6 max-w-3xl space-y-2 text-sm" aria-labelledby="passkeys-heading">
    <h2 id="passkeys-heading" class="text-lg font-semibold">Passkeys</h2>
    {#if passkeyHost}
      <p>
        Passkeys are bound to <strong>{passkeyHost}</strong>, the host of <code>[server] base_url</code>. Changing that host makes
        every passkey added before unusable: those who use one sign in with their recovery codes, or their app, and add it again.
      </p>
    {:else}
      <p>Passkeys need <code>[server] base_url</code>, with the address people use. Ask whoever runs BAM! to set it.</p>
    {/if}
    {#if passkeysElsewhere}
      <p class="text-amber-900 dark:text-amber-200" role="status">
        {passkeysElsewhere === 1 ? '1 passkey was' : `${passkeysElsewhere} passkeys were`} added at another host, and cannot be used here.
      </p>
    {/if}
  </section>

  {#if editing === 'new'}
    <div class="mb-6">
      <ProviderForm {protocols} {roles} {scopes} position={providers.length} onsaved={saved} oncancel={() => (editing = null)} />
    </div>
  {/if}
  {#if !secretKey}
    <p class="mb-4 text-sm text-amber-900 dark:text-amber-200">
      The configuration has no secret key, so a provider's secrets cannot be kept. Ask whoever runs BAM! to add one.
    </p>
  {/if}
  {#if !protocols.length}
    <p class="mb-4 text-sm text-zinc-600 dark:text-zinc-400">This BAM! has no sign-in protocols yet.</p>
  {/if}

  {#if providers.length === 0}
    <p class="text-sm text-zinc-600 dark:text-zinc-400">No identity providers yet.</p>
  {:else}
    <ul class="space-y-3">
      {#each providers as p, i (p.id)}
        <li class="card">
          {#if editing === p.id}
            <ProviderForm {protocols} {roles} {scopes} provider={p} onsaved={saved} oncancel={() => (editing = null)} />
          {:else}
            <div class="flex flex-wrap items-start justify-between gap-2">
              <div>
                <h2 class="font-semibold">{p.name}</h2>
                <p class="text-sm text-zinc-600 dark:text-zinc-400">
                  {protocolLabel(p.protocol)}, {p.enabled ? 'enabled' : 'disabled'}.
                  {p.provision ? 'Makes accounts' + (p.default_role_ids.length ? `, with ${p.default_role_ids.map(roleName).join(', ')}` : '') + '.' : 'Makes no accounts.'}
                  {p.rules.length === 1 ? '1 rule.' : `${p.rules.length} rules.`}
                </p>
                {#if p.from_config}<p class="text-sm">Set in the configuration file: change it there.</p>{/if}
                {#if p.secrets_unreadable}
                  <p class="text-sm text-amber-900 dark:text-amber-200">Its secrets are sealed with another key: set them again.</p>
                {/if}
              </div>
              {#if !p.from_config}
                <div class="flex flex-wrap gap-2">
                  <button type="button" class="btn-secondary" aria-label="Move {p.name} up" disabled={i === 0} onclick={() => move(i, -1)}>Up</button>
                  <button type="button" class="btn-secondary" aria-label="Move {p.name} down" disabled={i === providers.length - 1} onclick={() => move(i, 1)}>Down</button>
                  <button type="button" class="btn-secondary" aria-label="Edit {p.name}" onclick={() => (editing = p.id)}>Edit</button>
                  <ConfirmButton
                    label="Delete"
                    question="Delete {p.name}? Everyone's identities at it go too: someone with no password and no other way in can no longer sign in."
                    onconfirm={() => remove(p)}
                  />
                </div>
              {/if}
            </div>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
{/if}

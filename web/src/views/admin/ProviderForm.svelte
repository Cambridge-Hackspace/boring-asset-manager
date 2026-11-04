<!--
  An identity provider's form (docs/access.md#signing-in-with-an-identity-provider):
  its protocol's settings and secrets, its button, who gets an account, and
  the rules that give roles from what it says.
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import { DEFAULT_MFA_AMR, type Protocol, type Provider, type ProviderRule } from '../../lib/models';
  import DerivedKey from '../../components/DerivedKey.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  type Named = { id: string; name: string };
  let {
    protocols,
    roles,
    scopes,
    provider = null,
    position = 0,
    onsaved,
    oncancel,
  }: {
    protocols: Protocol[];
    roles: Named[];
    scopes: Named[];
    /** The provider to edit; null adds one. */
    provider?: Provider | null;
    /** Where a new one goes among the buttons. */
    position?: number;
    onsaved: () => void;
    oncancel: () => void;
  } = $props();

  // The form starts from the provider it was opened for and keeps its own state.
  const start = untrack(() => provider);
  const prefix = start ? `idp-${start.id}` : 'idp-new';
  let name = $state(start?.name ?? '');
  let key = $state(start?.key ?? '');
  let protocol = $state(start?.protocol ?? untrack(() => protocols[0]?.name ?? ''));
  let settings = $state<Record<string, string>>({ ...(start?.settings ?? {}) });
  let secrets = $state<Record<string, string>>({});
  let buttonLabel = $state(start?.button_label ?? '');
  let icon = $state(start?.icon ?? '');
  let enabled = $state(start?.enabled ?? true);
  let provision = $state(start?.provision ?? false);
  let defaultRoles = $state<string[]>([...(start?.default_role_ids ?? [])]);
  let domains = $state((start?.allowed_domains ?? []).join(', '));
  let groups = $state((start?.allowed_groups ?? []).join(', '));
  let linkByEmail = $state(start?.link_by_email ?? false);
  let trustEmail = $state(start?.trust_email ?? false);
  let mfaAMR = $state((start?.mfa_amr ?? DEFAULT_MFA_AMR).join(', '));
  let mfaACR = $state((start?.mfa_acr ?? []).join(', '));
  let mfaFallback = $state(start?.mfa_fallback ?? false);
  let mfaAlwaysLocal = $state(start?.mfa_always_local ?? false);
  let rules = $state<ProviderRule[]>((start?.rules ?? []).map((r) => ({ ...r, scope_ids: [...r.scope_ids] })));
  let busy = $state(false);
  let error = $state('');

  const info = $derived(protocols.find((p) => p.name === protocol));
  // Secrets already stored for this protocol can be left blank to keep them.
  const keeps = (field: string) => !!start && start.protocol === protocol && !!start.secrets_set[field];
  const list = (s: string) => s.split(',').map((v) => v.trim()).filter(Boolean);

  function addRule() {
    rules = [...rules, { claim: 'groups', value: '', role_id: roles[0]?.id ?? '', scope_ids: [] }];
  }

  async function save(event: SubmitEvent) {
    event.preventDefault();
    error = '';
    busy = true;
    const sentSecrets: Record<string, string> = {};
    for (const f of info?.fields ?? []) {
      if (f.secret && secrets[f.name]?.trim()) sentSecrets[f.name] = secrets[f.name]!.trim();
    }
    const body = {
      key, name, protocol, settings, secrets: sentSecrets, button_label: buttonLabel, icon, enabled,
      position: start?.position ?? position, provision, default_role_ids: defaultRoles,
      allowed_domains: list(domains), allowed_groups: list(groups), link_by_email: linkByEmail, trust_email: trustEmail,
      mfa_amr: list(mfaAMR), mfa_acr: list(mfaACR), mfa_fallback: mfaFallback, mfa_always_local: mfaAlwaysLocal,
      rules: rules.map((r) => ({ claim: r.claim, value: r.value, role_id: r.role_id, scope_ids: r.scope_ids })),
    };
    try {
      if (start) await request('PUT', `/auth/providers/${encodeURIComponent(start.id)}`, body);
      else await request('POST', '/auth/providers', body);
      onsaved();
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }
</script>

<form class="card max-w-2xl space-y-4" onsubmit={save} aria-label={start ? `Edit ${start.name}` : 'New identity provider'}>
  <ErrorMessage {error} />
  <div class="grid gap-4 sm:grid-cols-2">
    <div>
      <label class="label" for="{prefix}-name">Name</label>
      <input id="{prefix}-name" class="input" required maxlength="255" bind:value={name} />
      {#key start?.id}
        <DerivedKey
          id="{prefix}-key"
          bind:value={key}
          from={name}
          follows={!start}
          what="key in its addresses"
          lead="Its addresses use"
          warning={start ? 'The provider is set up with an address that holds this key: change it there too.' : ''}
          pattern="[a-z][a-z0-9_\-]*"
        />
      {/key}
    </div>
    <div>
      <label class="label" for="{prefix}-protocol">Protocol</label>
      <select id="{prefix}-protocol" class="input" bind:value={protocol}>
        {#each protocols as p (p.name)}<option value={p.name}>{p.label}</option>{/each}
      </select>
    </div>
  </div>

  {#if start}
    <p class="text-sm">
      Give the provider this address to send people back to:
      <code class="font-mono break-all">{start.callback_url}</code>
    </p>
  {/if}

  {#each info?.fields ?? [] as f (f.name)}
    <div>
      <label class="label" for="{prefix}-f-{f.name}">{f.label}{f.required ? '' : ' (optional)'}</label>
      {#if f.secret}
        <input
          id="{prefix}-f-{f.name}"
          class="input"
          type="password"
          autocomplete="off"
          required={f.required && !keeps(f.name)}
          placeholder={keeps(f.name) ? 'Set; leave blank to keep it' : ''}
          aria-describedby="{prefix}-f-{f.name}-help"
          bind:value={secrets[f.name]}
        />
      {:else}
        <input id="{prefix}-f-{f.name}" class="input" autocomplete="off" required={f.required} aria-describedby="{prefix}-f-{f.name}-help" bind:value={settings[f.name]} />
      {/if}
      <p id="{prefix}-f-{f.name}-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">
        {f.help ?? ''}{f.secret ? ' It is never shown again once saved.' : ''}
      </p>
    </div>
  {/each}

  <div class="grid gap-4 sm:grid-cols-2">
    <div>
      <label class="label" for="{prefix}-label">Button label (optional)</label>
      <input id="{prefix}-label" class="input" maxlength="255" placeholder={name ? `Sign in with ${name}` : ''} bind:value={buttonLabel} />
    </div>
    <div>
      <label class="label" for="{prefix}-icon">Button icon (optional)</label>
      <input id="{prefix}-icon" class="input" maxlength="255" placeholder="https://… or /…" bind:value={icon} />
    </div>
  </div>

  <fieldset class="space-y-2">
    <legend class="label">Accounts</legend>
    <label class="flex items-center gap-2 text-sm">
      <input type="checkbox" bind:checked={provision} /> Make an account for someone it vouches for and BAM! has never seen
    </label>
    {#if provision}
      <div class="grid gap-4 pl-6 sm:grid-cols-2">
        <div>
          <label class="label" for="{prefix}-domains">Only for emails at (optional)</label>
          <input id="{prefix}-domains" class="input" placeholder="hackspace.org, example.org" bind:value={domains} />
        </div>
        <div>
          <label class="label" for="{prefix}-groups">Only for members of (optional)</label>
          <input id="{prefix}-groups" class="input" placeholder="members" bind:value={groups} />
        </div>
      </div>
      <fieldset class="space-y-1 pl-6">
        <legend class="text-sm">Roles a new account starts with</legend>
        {#each roles as r (r.id)}
          <label class="flex items-center gap-2 text-sm"><input type="checkbox" value={r.id} bind:group={defaultRoles} /> {r.name}</label>
        {/each}
        <p class="text-xs text-zinc-600 dark:text-zinc-400">With none, the account sees nothing until someone gives it a role.</p>
      </fieldset>
    {/if}
    <label class="flex items-center gap-2 text-sm">
      <input type="checkbox" bind:checked={linkByEmail} /> Link to an account with the same verified email
    </label>
    <label class="flex items-center gap-2 text-sm">
      <input type="checkbox" bind:checked={trustEmail} aria-describedby="{prefix}-trust-help" /> Count its emails as verified
    </label>
    <p id="{prefix}-trust-help" class="text-xs text-zinc-600 dark:text-zinc-400">
      Only for a provider that checks emails itself but does not say so. Otherwise an email counts only when the provider says it is verified.
    </p>
  </fieldset>

  <fieldset class="space-y-2">
    <legend class="label">Two-factor sign-in</legend>
    <p class="text-xs text-zinc-600 dark:text-zinc-400">
      For those whose roles require two-factor sign-in. A sign-in through the provider counts as two-factor when it says one of
      these was used.
    </p>
    <div class="grid gap-4 sm:grid-cols-2">
      <div>
        <label class="label" for="{prefix}-amr">How they signed in (amr)</label>
        <input id="{prefix}-amr" class="input" placeholder="mfa, otp, hwk" bind:value={mfaAMR} />
      </div>
      <div>
        <label class="label" for="{prefix}-acr">Or the level they signed in at (acr, optional)</label>
        <input id="{prefix}-acr" class="input" bind:value={mfaACR} />
      </div>
    </div>
    <label class="flex items-center gap-2 text-sm">
      <input type="checkbox" bind:checked={mfaFallback} disabled={mfaAlwaysLocal} /> When it does not say so, ask for BAM!'s own second factor
    </label>
    <p class="text-xs text-zinc-600 dark:text-zinc-400">
      Otherwise someone whose roles require two-factor sign-in is refused, and told to have the provider require it.
    </p>
    <label class="flex items-center gap-2 text-sm">
      <input type="checkbox" bind:checked={mfaAlwaysLocal} /> Always ask for BAM!'s own second factor, whatever it says
    </label>
  </fieldset>

  <fieldset class="space-y-2">
    <legend class="label">Roles from what it says</legend>
    <p class="text-xs text-zinc-600 dark:text-zinc-400">
      Checked at every sign-in: a role a rule gave is taken away once its claim goes. Roles given by hand are left alone.
    </p>
    {#each rules as r, i (i)}
      <div class="grid items-end gap-2 sm:grid-cols-[1fr_1fr_1fr_auto]" role="group" aria-label="Rule {i + 1}">
        <div>
          <label class="label" for="{prefix}-r{i}-claim">Claim</label>
          <input id="{prefix}-r{i}-claim" class="input" required placeholder="groups" bind:value={r.claim} />
        </div>
        <div>
          <label class="label" for="{prefix}-r{i}-value">Holds</label>
          <input id="{prefix}-r{i}-value" class="input" required placeholder="makerspace-admins" bind:value={r.value} />
        </div>
        <div>
          <label class="label" for="{prefix}-r{i}-role">Gives the role</label>
          <select id="{prefix}-r{i}-role" class="input" bind:value={r.role_id}>
            {#each roles as role (role.id)}<option value={role.id}>{role.name}</option>{/each}
          </select>
        </div>
        <button type="button" class="btn-secondary" aria-label="Remove rule {i + 1}" onclick={() => (rules = rules.filter((_, j) => j !== i))}>Remove</button>
        {#if scopes.length}
          <fieldset class="sm:col-span-4">
            <legend class="text-xs">Narrowed to (none for everywhere)</legend>
            <div class="flex flex-wrap gap-x-4">
              {#each scopes as sc (sc.id)}
                <label class="flex items-center gap-1 text-sm"><input type="checkbox" value={sc.id} bind:group={r.scope_ids} /> {sc.name}</label>
              {/each}
            </div>
          </fieldset>
        {/if}
      </div>
    {/each}
    <button type="button" class="btn-secondary" onclick={addRule} disabled={!roles.length}>Add a rule</button>
  </fieldset>

  <label class="flex items-center gap-2 text-sm"><input type="checkbox" bind:checked={enabled} /> Enabled: its button is on the sign-in page</label>

  <div class="flex flex-wrap gap-2">
    <button type="submit" class="btn-primary" disabled={busy}>{start ? 'Save provider' : 'Add provider'}</button>
    <button type="button" class="btn-secondary" onclick={oncancel}>Cancel</button>
  </div>
</form>

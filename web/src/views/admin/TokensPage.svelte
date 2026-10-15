<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { APIToken, Role } from '../../lib/models';
  import ConfirmButton from '../../components/ConfirmButton.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  let { params: _params }: { params: Record<string, string> } = $props();

  let tokens = $state<APIToken[]>([]);
  let roles = $state<Role[]>([]);
  let loading = $state(true);
  let error = $state('');
  let status = $state('');

  // Making a token, and the secret of the one just made, shown once.
  let creating = $state(false);
  let name = $state('');
  let roleIds = $state<string[]>([]);
  let lifetime = $state('90');
  let made = $state<{ name: string; secret: string } | null>(null);
  let copied = $state(false);

  const lifetimes: [string, string][] = [
    ['30', '30 days'],
    ['90', '90 days'],
    ['365', '1 year'],
    ['', 'Never'],
  ];

  const roleName = (id: string) => roles.find((r) => r.id === id)?.name ?? id;
  const when = (iso: string | null) => (iso ? new Date(iso).toLocaleDateString() : '');

  async function load() {
    try {
      [tokens, roles] = await Promise.all([
        request<{ tokens: APIToken[] }>('GET', '/tokens').then((r) => r.tokens ?? []),
        request<{ roles: Role[] }>('GET', '/roles').then((r) => r.roles ?? []),
      ]);
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
  }
  onMount(load);

  function standing(t: APIToken): string {
    if (t.revoked_at) return `Revoked ${when(t.revoked_at)}`;
    if (t.expires_at && new Date(t.expires_at) <= new Date()) return `Expired ${when(t.expires_at)}`;
    return t.expires_at ? `Expires ${when(t.expires_at)}` : 'Never expires';
  }

  function startCreate() {
    creating = true;
    made = null;
  }

  async function create(event: SubmitEvent) {
    event.preventDefault();
    error = status = '';
    const days = Number(lifetime);
    const expires_at = lifetime ? new Date(Date.now() + days * 86_400_000).toISOString() : null;
    try {
      const res = await request<{ token: APIToken; secret: string }>('POST', '/tokens', { name, role_ids: roleIds, expires_at });
      made = { name: res.token.name, secret: res.secret };
      copied = false;
      creating = false;
      name = '';
      roleIds = [];
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function copy() {
    if (!made) return;
    try {
      await navigator.clipboard.writeText(made.secret);
      copied = true;
    } catch {
      error = 'Could not copy; select the token and copy it by hand.';
    }
  }

  async function revoke(t: APIToken) {
    error = status = '';
    try {
      await request('DELETE', `/tokens/${encodeURIComponent(t.id)}`);
      status = `Revoked ${t.name}.`;
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }
</script>

<div class="mb-4 flex flex-wrap items-center justify-between gap-2">
  <h1 class="text-2xl font-bold">API tokens</h1>
  {#if !creating}<button type="button" class="btn-primary" onclick={startCreate}>New token</button>{/if}
</div>
<p class="mb-4 max-w-2xl text-sm text-zinc-600 dark:text-zinc-400">
  Tokens let scripts and dashboards use the API, sent as <code>Authorization: Bearer &lt;token&gt;</code>. A token may do what its
  roles allow, and its changes are recorded under its name. Tokens cannot manage tokens or change anyone's account.
</p>
<ErrorMessage {error} />
{#if status}<p class="mb-2 text-sm text-green-700 dark:text-green-400" role="status">{status}</p>{/if}

{#if made}
  <section class="card mb-6 max-w-2xl space-y-2 border-amber-400" aria-labelledby="made-heading">
    <h2 id="made-heading" class="text-lg font-semibold">Token for {made.name}</h2>
    <p class="text-sm">Copy it now: it will not be shown again. Anyone holding it can do what its roles allow.</p>
    <label class="sr-only" for="made-secret">The new token</label>
    <input id="made-secret" class="input font-mono" readonly value={made.secret} onfocus={(e) => e.currentTarget.select()} />
    <div class="flex gap-2">
      <button type="button" class="btn-secondary" onclick={copy}>{copied ? 'Copied' : 'Copy'}</button>
      <button type="button" class="btn-secondary" onclick={() => (made = null)}>Done</button>
    </div>
  </section>
{/if}

{#if creating}
  <form class="card mb-6 max-w-2xl space-y-4" onsubmit={create}>
    <h2 class="text-lg font-semibold">New token</h2>
    <div class="grid gap-4 sm:grid-cols-2">
      <div>
        <label class="label" for="token-name">Name</label>
        <input id="token-name" class="input" required maxlength="255" placeholder="Stock dashboard" bind:value={name} />
      </div>
      <div>
        <label class="label" for="token-lifetime">Expires after</label>
        <select id="token-lifetime" class="input" bind:value={lifetime}>
          {#each lifetimes as [value, label] (value)}<option {value}>{label}</option>{/each}
        </select>
      </div>
    </div>
    <fieldset>
      <legend class="label">Roles</legend>
      {#each roles as r (r.id)}
        <label class="mr-4 inline-flex items-center gap-2 text-sm"><input type="checkbox" value={r.id} bind:group={roleIds} /> {r.name}</label>
      {/each}
    </fieldset>
    <div class="flex gap-2">
      <button type="submit" class="btn-primary">Create token</button>
      <button type="button" class="btn-secondary" onclick={() => (creating = false)}>Cancel</button>
    </div>
  </form>
{/if}

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading tokens…</p>
{:else if tokens.length === 0}
  <p class="text-sm text-zinc-600 dark:text-zinc-400">No tokens yet.</p>
{:else}
  <div class="overflow-x-auto">
    <table class="w-full text-left text-sm">
      <thead class="border-b border-zinc-300 dark:border-zinc-700">
        <tr>
          <th scope="col" class="py-2 pr-4">Name</th>
          <th scope="col" class="py-2 pr-4">Roles</th>
          <th scope="col" class="py-2 pr-4">Created</th>
          <th scope="col" class="py-2 pr-4">Last used</th>
          <th scope="col" class="py-2 pr-4">Status</th>
          <th scope="col" class="py-2"><span class="sr-only">Actions</span></th>
        </tr>
      </thead>
      <tbody>
        {#each tokens as t (t.id)}
          <tr class="border-b border-zinc-200 dark:border-zinc-800">
            <td class="py-2 pr-4 font-medium">{t.name}</td>
            <td class="py-2 pr-4">{t.role_ids.map(roleName).join(', ') || 'None'}</td>
            <td class="py-2 pr-4 whitespace-nowrap">{when(t.created_at)}</td>
            <td class="py-2 pr-4 whitespace-nowrap">{when(t.last_used_at) || 'Never'}</td>
            <td class="py-2 pr-4 whitespace-nowrap">{standing(t)}</td>
            <td class="py-2 text-right">
              {#if !t.revoked_at}
                <ConfirmButton label="Revoke" question="Revoke {t.name}? Scripts using it stop working at once." onconfirm={() => revoke(t)} />
              {/if}
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}

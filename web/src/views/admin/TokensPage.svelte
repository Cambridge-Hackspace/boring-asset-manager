<script lang="ts">
  import { stack } from '../../lib/stack';
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { APIToken, LiveConnection, Role, RoleAssignment, Scope } from '../../lib/models';
  import { describeAssignment } from '../../lib/permissions';
  import ConfirmButton from '../../components/ConfirmButton.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';
  import PageHeader from '../../components/PageHeader.svelte';
  import RolePicker from '../../components/RolePicker.svelte';

  let { params: _params }: { params: Record<string, string> } = $props();

  let tokens = $state<APIToken[]>([]);
  let roles = $state<Role[]>([]);
  let scopes = $state<Scope[]>([]);
  let loading = $state(true);
  let error = $state('');
  let status = $state('');

  // Making a token, and the secret of the one just made, shown once.
  let creating = $state(false);
  let name = $state('');
  let assigned = $state<RoleAssignment[]>([]);
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
  const scopeName = (id: string) => scopes.find((s) => s.id === id)?.name ?? id;
  const described = (a: RoleAssignment) => describeAssignment(roleName(a.role_id), a.scope_ids.map(scopeName));
  const when = (iso: string | null) => (iso ? new Date(iso).toLocaleDateString() : '');

  // Live connections each token holds open now, on any server (docs/hooks.md).
  let connections = $state<LiveConnection[]>([]);
  const liveFor = (id: string) => connections.filter((c) => c.token_id === id);

  async function load() {
    try {
      [tokens, roles, scopes, connections] = await Promise.all([
        request<{ tokens: APIToken[] }>('GET', '/tokens').then((r) => r.tokens ?? []),
        request<{ roles: Role[] }>('GET', '/roles').then((r) => (r.roles ?? []).filter((role) => !role.public)), // everyone holds the public role
        // Extra: without scopes, roles are given in every scope.
        request<{ scopes: Scope[] }>('GET', '/scopes').then((r) => r.scopes ?? [], () => []),
        // Extra: the tokens show without it.
        request<{ connections: LiveConnection[] }>('GET', '/tokens/connections').then((r) => r.connections ?? [], () => []),
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
      const res = await request<{ token: APIToken; secret: string }>('POST', '/tokens', { name, roles: assigned, expires_at });
      made = { name: res.token.name, secret: res.secret };
      copied = false;
      creating = false;
      name = '';
      assigned = [];
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

<PageHeader title="API tokens">
  {#snippet lede()}
    Tokens let scripts and dashboards use the API, sent as <code>Authorization: Bearer &lt;token&gt;</code>. A token may do what its
    roles allow, and its changes are recorded under its name. A token cannot make tokens or change anyone's account; one with users.manage can list and revoke them. Personal tokens,
    which people make for their assistants on their account page, act as them instead (<a class="underline" href="/help/assistants">Assistants</a>).
  {/snippet}
  {#snippet actions()}
    {#if !creating}<button type="button" class="btn-primary" onclick={startCreate}>New token</button>{/if}
  {/snippet}
</PageHeader>
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
    <RolePicker id="token-roles" {roles} {scopes} bind:value={assigned} />
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
    <table class="w-full text-left text-sm" use:stack>
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
            <td class="py-2 pr-4">{t.user_id ? `Personal: acts as ${t.user_name || 'Unnamed user'}` : t.roles.map(described).join(', ') || 'None'}</td>
            <td class="py-2 pr-4 whitespace-nowrap">{when(t.created_at)}</td>
            <td class="py-2 pr-4 whitespace-nowrap">{when(t.last_used_at) || 'Never'}</td>
            <td class="py-2 pr-4 whitespace-nowrap">
              {standing(t)}
              {#if liveFor(t.id).length}
                <span class="block text-xs text-zinc-600 dark:text-zinc-400">
                  {liveFor(t.id).length === 1 ? 'Connected live' : `${liveFor(t.id).length} live connections`}, since {new Date(liveFor(t.id).at(-1)!.opened_at).toLocaleString()}
                </span>
              {/if}
            </td>
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

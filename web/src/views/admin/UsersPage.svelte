<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { Role, UserWithRoles } from '../../lib/models';
  import { session } from '../../lib/session.svelte';
  import ConfirmButton from '../../components/ConfirmButton.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  let { params: _params }: { params: Record<string, string> } = $props();

  let users = $state<UserWithRoles[]>([]);
  let roles = $state<Role[]>([]);
  let loading = $state(true);
  let error = $state('');
  let status = $state('');
  let editingRoles = $state<string | null>(null);
  let roleDraft = $state<string[]>([]);

  // Creating a user.
  let creating = $state(false);
  let newEmail = $state('');
  let newPassword = $state('');
  let newRoles = $state<string[]>([]);

  const roleName = (id: string) => roles.find((r) => r.id === id)?.name ?? id;

  async function load() {
    try {
      [users, roles] = await Promise.all([
        request<{ users: UserWithRoles[] }>('GET', '/users').then((r) => r.users ?? []),
        request<{ roles: Role[] }>('GET', '/roles').then((r) => r.roles ?? []),
      ]);
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
  }
  onMount(load);

  // The server refuses changes that would leave no one able to manage users;
  // say so plainly.
  function explain(err: unknown): string {
    const msg = messageOf(err);
    return msg.includes('anti-lockout')
      ? 'Refused: at least one active user must keep the users:manage permission, and this change would remove the last one.'
      : msg;
  }

  async function act(action: () => Promise<unknown>, done: string) {
    error = status = '';
    try {
      await action();
      status = done;
      await load();
    } catch (err) {
      error = explain(err);
    }
  }

  async function create(event: SubmitEvent) {
    event.preventDefault();
    await act(async () => {
      const res = await request<{ user: { id: string } }>('POST', '/users', { email: newEmail, password: newPassword });
      if (newRoles.length) await request('PUT', `/users/${encodeURIComponent(res.user.id)}/roles`, { role_ids: newRoles });
      creating = false;
      newEmail = newPassword = '';
      newRoles = [];
    }, 'User created.');
  }

  function startRoles(u: UserWithRoles) {
    editingRoles = u.id;
    roleDraft = [...u.role_ids];
  }
</script>

<div class="mb-4 flex flex-wrap items-center justify-between gap-2">
  <h1 class="text-2xl font-bold">Users</h1>
  {#if !creating}<button type="button" class="btn-primary" onclick={() => (creating = true)}>New user</button>{/if}
</div>
<ErrorMessage {error} />
{#if status}<p class="mb-2 text-sm text-green-700 dark:text-green-400" role="status">{status}</p>{/if}

{#if creating}
  <form class="card mb-4 max-w-2xl space-y-4" onsubmit={create}>
    <h2 class="text-lg font-semibold">New user</h2>
    <div class="grid gap-4 sm:grid-cols-2">
      <div>
        <label class="label" for="new-email">Email</label>
        <input id="new-email" class="input" type="email" required autocomplete="off" bind:value={newEmail} />
      </div>
      <div>
        <label class="label" for="new-password">Initial password</label>
        <input id="new-password" class="input" type="password" required minlength="8" maxlength="72" autocomplete="new-password" aria-describedby="new-password-help" bind:value={newPassword} />
        <p id="new-password-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">8-72 characters. They can change it after signing in.</p>
      </div>
    </div>
    <fieldset>
      <legend class="label">Roles</legend>
      {#each roles as r (r.id)}
        <label class="mr-4 inline-flex items-center gap-2 text-sm"><input type="checkbox" value={r.id} bind:group={newRoles} /> {r.name}</label>
      {/each}
    </fieldset>
    <div class="flex gap-2">
      <button type="submit" class="btn-primary">Create user</button>
      <button type="button" class="btn-secondary" onclick={() => (creating = false)}>Cancel</button>
    </div>
  </form>
{/if}

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading users…</p>
{:else}
  <div class="overflow-x-auto">
    <table class="w-full text-left text-sm">
      <thead class="border-b border-zinc-300 dark:border-zinc-700">
        <tr>
          <th scope="col" class="py-2 pr-4">Email</th>
          <th scope="col" class="py-2 pr-4">Status</th>
          <th scope="col" class="py-2 pr-4">Roles</th>
          <th scope="col" class="py-2"><span class="sr-only">Actions</span></th>
        </tr>
      </thead>
      <tbody>
        {#each users as u (u.id)}
          <tr class="border-b border-zinc-200 align-top dark:border-zinc-800">
            <td class="py-2 pr-4 font-medium">{u.email}{#if u.id === session.user?.id}{' '}<span class="text-xs text-zinc-600 dark:text-zinc-400">(you)</span>{/if}</td>
            <td class="py-2 pr-4">{u.is_active ? 'Active' : 'Disabled'}</td>
            <td class="py-2 pr-4">
              {#if editingRoles === u.id}
                <fieldset class="space-y-1">
                  <legend class="sr-only">Roles for {u.email}</legend>
                  {#each roles as r (r.id)}
                    <label class="flex items-center gap-2"><input type="checkbox" value={r.id} bind:group={roleDraft} /> {r.name}</label>
                  {/each}
                  <div class="flex gap-2 pt-1">
                    <button type="button" class="btn-primary" onclick={() => act(async () => { await request('PUT', `/users/${encodeURIComponent(u.id)}/roles`, { role_ids: roleDraft }); if (editingRoles === u.id) editingRoles = null; }, `Roles updated for ${u.email}.`)}>Save roles</button>
                    <button type="button" class="btn-secondary" onclick={() => (editingRoles = null)}>Cancel</button>
                  </div>
                </fieldset>
              {:else}
                {u.role_ids.map(roleName).join(', ') || 'None'}
              {/if}
            </td>
            <td class="py-2 text-right">
              <span class="inline-flex flex-wrap justify-end gap-2">
                {#if editingRoles !== u.id}
                  <button type="button" class="btn-secondary" aria-label="Roles for {u.email}" onclick={() => startRoles(u)}>Roles</button>
                {/if}
                <button
                  type="button"
                  class="btn-secondary"
                  aria-label="{u.is_active ? 'Disable' : 'Enable'} {u.email}"
                  onclick={() => act(() => request('PATCH', `/users/${encodeURIComponent(u.id)}/status`, { is_active: !u.is_active }), `${u.email} ${u.is_active ? 'disabled' : 'enabled'}.`)}
                >
                  {u.is_active ? 'Disable' : 'Enable'}
                </button>
                <ConfirmButton
                  label="Archive"
                  question="Archive {u.email}? They will be signed out and cannot sign in again."
                  onconfirm={() => act(() => request('DELETE', `/users/${encodeURIComponent(u.id)}`), `${u.email} archived.`)}
                />
              </span>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>

  <section class="mt-8" aria-labelledby="roles-heading">
    <h2 id="roles-heading" class="mb-2 text-lg font-semibold">Roles</h2>
    <dl class="grid gap-2 text-sm sm:grid-cols-[auto_1fr]">
      {#each roles as r (r.id)}
        <dt class="font-medium">{r.name}</dt>
        <dd class="text-zinc-600 dark:text-zinc-400">{r.permissions.join(', ') || 'No permissions'}</dd>
      {/each}
    </dl>
  </section>
{/if}

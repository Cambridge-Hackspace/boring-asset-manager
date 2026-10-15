<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { Permission, Role } from '../../lib/models';
  import { permissionLabel, permissions } from '../../lib/permissions';
  import ConfirmButton from '../../components/ConfirmButton.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  let { params: _params }: { params: Record<string, string> } = $props();

  let roles = $state<Role[]>([]);
  let loading = $state(true);
  let error = $state('');
  let status = $state('');

  // The role being edited: null for none, '' for a new one.
  let editing = $state<string | null>(null);
  let draft = $state<{ name: string; description: string; permissions: Permission[] }>({ name: '', description: '', permissions: [] });

  async function load() {
    try {
      roles = (await request<{ roles: Role[] }>('GET', '/roles')).roles ?? [];
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

  function start(role?: Role) {
    error = status = '';
    editing = role?.id ?? '';
    draft = role
      ? { name: role.name, description: role.description, permissions: [...role.permissions] }
      : { name: '', description: '', permissions: ['assets:read'] };
  }

  async function save(event: SubmitEvent) {
    event.preventDefault();
    error = status = '';
    try {
      if (editing) await request('PUT', `/roles/${encodeURIComponent(editing)}`, draft);
      else await request('POST', '/roles', draft);
      status = editing ? `Saved ${draft.name.trim()}.` : `Created ${draft.name.trim()}.`;
      editing = null;
      await load();
    } catch (err) {
      error = explain(err);
    }
  }

  async function remove(role: Role) {
    error = status = '';
    try {
      await request('DELETE', `/roles/${encodeURIComponent(role.id)}`);
      status = `Deleted ${role.name}.`;
      if (editing === role.id) editing = null;
      await load();
    } catch (err) {
      error = explain(err);
    }
  }

  const holders = (n: number) => (n === 1 ? '1 user' : `${n} users`);
</script>

<div class="mb-4 flex flex-wrap items-center justify-between gap-2">
  <h1 class="text-2xl font-bold">Roles</h1>
  {#if editing === null}<button type="button" class="btn-primary" onclick={() => start()}>New role</button>{/if}
</div>
<p class="mb-4 max-w-2xl text-sm text-zinc-600 dark:text-zinc-400">
  A role is a set of permissions. People hold any number of roles and may do everything any of them allows.
</p>
<ErrorMessage {error} />
{#if status}<p class="mb-2 text-sm text-green-700 dark:text-green-400" role="status">{status}</p>{/if}

{#if editing !== null}
  <form class="card mb-6 max-w-2xl space-y-4" onsubmit={save}>
    <h2 class="text-lg font-semibold">{editing ? 'Edit role' : 'New role'}</h2>
    <div class="grid gap-4 sm:grid-cols-2">
      <div>
        <label class="label" for="role-name">Name</label>
        <input id="role-name" class="input" required maxlength="64" bind:value={draft.name} />
      </div>
      <div>
        <label class="label" for="role-description">Description (optional)</label>
        <input id="role-description" class="input" maxlength="255" bind:value={draft.description} />
      </div>
    </div>
    <fieldset class="space-y-2">
      <legend class="label">Permissions</legend>
      {#each permissions as p (p.key)}
        <label class="flex items-start gap-2 text-sm">
          <input class="mt-1" type="checkbox" value={p.key} bind:group={draft.permissions} />
          <span><span class="font-medium">{p.label}</span> <span class="text-zinc-600 dark:text-zinc-400">— {p.description}</span></span>
        </label>
      {/each}
    </fieldset>
    <div class="flex gap-2">
      <button type="submit" class="btn-primary">{editing ? 'Save role' : 'Create role'}</button>
      <button type="button" class="btn-secondary" onclick={() => (editing = null)}>Cancel</button>
    </div>
  </form>
{/if}

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading roles…</p>
{:else}
  <ul class="space-y-3">
    {#each roles as r (r.id)}
      <li class="card">
        <div class="flex flex-wrap items-start justify-between gap-2">
          <div>
            <h2 class="font-semibold">{r.name}</h2>
            {#if r.description}<p class="text-sm text-zinc-600 dark:text-zinc-400">{r.description}</p>{/if}
            <p class="text-sm">Held by {holders(r.user_count)}.</p>
          </div>
          {#if r.protected}
            <p class="text-sm text-zinc-600 dark:text-zinc-400">Built in: always has every permission.</p>
          {:else}
            <span class="inline-flex flex-wrap gap-2">
              <button type="button" class="btn-secondary" aria-label="Edit {r.name}" onclick={() => start(r)}>Edit</button>
              <ConfirmButton
                label="Delete"
                question={r.user_count
                  ? `Delete ${r.name}? ${holders(r.user_count)} will lose what it allows.`
                  : `Delete ${r.name}?`}
                onconfirm={() => remove(r)}
              />
            </span>
          {/if}
        </div>
        <p class="mt-2 text-sm">
          {r.protected ? 'Every permission' : r.permissions.map(permissionLabel).join(', ') || 'No permissions'}
        </p>
      </li>
    {/each}
  </ul>
{/if}

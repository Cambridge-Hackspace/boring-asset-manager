<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { Permission, PermissionKey, Role, Scope } from '../../lib/models';
  import { describeKey, heldIn, permissions, publicAllowed, scopable, setHeld } from '../../lib/permissions';
  import ConfirmButton from '../../components/ConfirmButton.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  let { params: _params }: { params: Record<string, string> } = $props();

  let roles = $state<Role[]>([]);
  let scopes = $state<Scope[]>([]);
  let loading = $state(true);
  let error = $state('');
  let status = $state('');

  // The role being edited: null for none, '' for a new one.
  let editing = $state<string | null>(null);
  let draft = $state<{ name: string; description: string; permissions: PermissionKey[] }>({ name: '', description: '', permissions: [] });

  async function load() {
    try {
      const [r, s] = await Promise.all([
        request<{ roles: Role[] }>('GET', '/roles'),
        request<{ scopes: Scope[] }>('GET', '/scopes'),
      ]);
      roles = r.roles ?? [];
      scopes = s.scopes ?? [];
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
      ? 'Refused: at least one active user must keep the Manage users permission, and this change would remove the last one.'
      : msg;
  }

  // The public role, being edited: it keeps its name and may only read.
  let editingPublic = $state(false);
  const offered = $derived(editingPublic ? permissions.filter((p) => publicAllowed.includes(p.key)) : permissions);

  function start(role?: Role) {
    error = status = '';
    editing = role?.id ?? '';
    editingPublic = !!role?.public;
    onlySome = [];
    draft = role
      ? { name: role.name, description: role.description, permissions: [...role.permissions] }
      : { name: '', description: '', permissions: ['assets.read'] };
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

  // Scopes matter once there is one besides the default.
  const scoped = $derived(scopes.length > 1);

  // Permissions set to "Only in…", which stay chosen while no scope is ticked yet.
  let onlySome = $state<Permission[]>([]);
  const chosen = (p: Permission, held: 'all' | string[]) => held === 'all' || held.length > 0 || onlySome.includes(p);

  function toggle(p: Permission, on: boolean) {
    onlySome = onlySome.filter((x) => x !== p);
    draft.permissions = setHeld(draft.permissions, p, on ? 'all' : []);
  }

  function choose(p: Permission, where: string) {
    onlySome = where === 'all' ? onlySome.filter((x) => x !== p) : [...onlySome, p];
    draft.permissions = setHeld(draft.permissions, p, where === 'all' ? 'all' : []);
  }

  function toggleScope(p: Permission, scope: string, on: boolean) {
    const held = heldIn(draft.permissions, p);
    const now = held === 'all' ? [] : held;
    draft.permissions = setHeld(draft.permissions, p, on ? [...now, scope] : now.filter((s) => s !== scope));
  }

  // Scopes themselves: the one being renamed, and a new one's name.
  let renaming = $state<string | null>(null);
  let scopeName = $state('');
  let newScope = $state('');

  async function saveScope(event: SubmitEvent, id?: string) {
    event.preventDefault();
    error = status = '';
    const name = (id ? scopeName : newScope).trim();
    try {
      if (id) await request('PUT', `/scopes/${encodeURIComponent(id)}`, { name });
      else await request('POST', '/scopes', { name });
      status = id ? `Renamed the scope to ${name}.` : `Created the scope ${name}.`;
      renaming = null;
      newScope = '';
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function removeScope(scope: Scope) {
    error = status = '';
    try {
      await request('DELETE', `/scopes/${encodeURIComponent(scope.id)}`);
      status = `Deleted the scope ${scope.name}.`;
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }

  const count = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;
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
        <input id="role-name" class="input" required maxlength="64" readonly={editingPublic} bind:value={draft.name} />
      </div>
      <div>
        <label class="label" for="role-description">Description (optional)</label>
        <input id="role-description" class="input" maxlength="255" bind:value={draft.description} />
      </div>
    </div>
    <fieldset class="space-y-2">
      <legend class="label">Permissions</legend>
      {#if editingPublic}
        <p class="text-sm text-zinc-600 dark:text-zinc-400">Everyone holds this role, signed in or not, so it may only look.</p>
      {/if}
      {#each offered as p (p.key)}
        {@const held = heldIn(draft.permissions, p.key)}
        <div>
          <label class="flex items-start gap-2 text-sm">
            <input
              class="mt-1"
              type="checkbox"
              checked={chosen(p.key, held)}
              onchange={(e) => toggle(p.key, e.currentTarget.checked)}
            />
            <span><span class="font-medium">{p.label}</span> <span class="text-zinc-600 dark:text-zinc-400">— {p.description}</span></span>
          </label>
          {#if scoped && scopable.includes(p.key) && chosen(p.key, held)}
            <div class="mt-1 ml-6 flex flex-wrap items-center gap-x-4 gap-y-1 text-sm">
              <select
                class="input w-auto"
                aria-label="Where: {p.label}"
                value={held === 'all' ? 'all' : 'some'}
                onchange={(e) => choose(p.key, e.currentTarget.value)}
              >
                <option value="all">In every scope</option>
                <option value="some">Only in…</option>
              </select>
              {#if held !== 'all'}
                {#each scopes as s (s.id)}
                  <label class="flex items-center gap-1">
                    <input
                      type="checkbox"
                      aria-label="{p.label} in {s.name}"
                      checked={held.includes(s.name)}
                      onchange={(e) => toggleScope(p.key, s.name, e.currentTarget.checked)}
                    />
                    {s.name}
                  </label>
                {/each}
              {/if}
            </div>
          {/if}
        </div>
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
            <p class="text-sm">{r.public ? 'Held by everyone, signed in or not.' : `Held by ${holders(r.user_count)}.`}</p>
          </div>
          {#if r.protected}
            <p class="text-sm text-zinc-600 dark:text-zinc-400">Built in: always has every permission.</p>
          {:else}
            <span class="inline-flex flex-wrap gap-2">
              <button type="button" class="btn-secondary" aria-label="Edit {r.name}" onclick={() => start(r)}>Edit</button>
              {#if !r.public}
                <ConfirmButton
                  label="Delete"
                  question={r.user_count
                    ? `Delete ${r.name}? ${holders(r.user_count)} will lose what it allows.`
                    : `Delete ${r.name}?`}
                  onconfirm={() => remove(r)}
                />
              {/if}
            </span>
          {/if}
        </div>
        <p class="mt-2 text-sm">
          {r.protected ? 'Every permission' : r.permissions.map(describeKey).join(', ') || 'No permissions'}
        </p>
      </li>
    {/each}
  </ul>
{/if}

<section class="mt-8 max-w-2xl" aria-labelledby="scopes-heading">
  <h2 id="scopes-heading" class="mb-2 text-xl font-semibold">Scopes</h2>
  <p class="mb-4 text-sm text-zinc-600 dark:text-zinc-400">
    A scope is a label on categories, passed down to their subcategories. A role can hold asset permissions in some
    scopes only, and then acts on the assets in those. Assets in no other scope are in <span class="font-mono">default</span>.
  </p>
  <form class="mb-4 flex flex-wrap items-end gap-2" onsubmit={(e) => saveScope(e)}>
    <div>
      <label class="label" for="new-scope">New scope</label>
      <input id="new-scope" class="input" required maxlength="64" placeholder="such as workshop" bind:value={newScope} />
    </div>
    <button type="submit" class="btn-secondary">Add scope</button>
  </form>
  <ul class="space-y-2">
    {#each scopes as s (s.id)}
      <li class="card flex flex-wrap items-center justify-between gap-2">
        {#if renaming === s.id}
          <form class="flex flex-wrap items-end gap-2" onsubmit={(e) => saveScope(e, s.id)}>
            <div>
              <label class="label" for="scope-{s.id}">Name</label>
              <input id="scope-{s.id}" class="input" required maxlength="64" bind:value={scopeName} />
            </div>
            <button type="submit" class="btn-primary">Rename</button>
            <button type="button" class="btn-secondary" onclick={() => (renaming = null)}>Cancel</button>
          </form>
        {:else}
          <div>
            <h3 class="font-mono font-semibold">{s.name}</h3>
            <p class="text-sm text-zinc-600 dark:text-zinc-400">
              {s.name === 'public'
                ? `Built in: what everyone, signed in or not, may see. Put it on categories to make their assets public. On ${count(s.category_count, 'category', 'categories')}.`
                : s.reserved
                ? 'Built in: assets in no other scope.'
                : `On ${count(s.category_count, 'category', 'categories')}; used by ${count(s.role_count, 'role', 'roles')}.`}
            </p>
          </div>
          {#if !s.reserved}
            <span class="inline-flex flex-wrap gap-2">
              <button type="button" class="btn-secondary" aria-label="Rename {s.name}" onclick={() => ((renaming = s.id), (scopeName = s.name))}>
                Rename
              </button>
              <ConfirmButton label="Delete scope" question="Delete the scope {s.name}?" onconfirm={() => removeScope(s)} />
            </span>
          {/if}
        {/if}
      </li>
    {/each}
  </ul>
</section>

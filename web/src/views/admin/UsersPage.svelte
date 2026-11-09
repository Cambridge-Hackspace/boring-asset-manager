<script lang="ts">
  import { stack } from '../../lib/stack';
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { PublicProviders, Role, RoleAssignment, Scope, UserWithRoles } from '../../lib/models';
  import { describeAssignment } from '../../lib/permissions';
  import { session } from '../../lib/session.svelte';
  import ConfirmButton from '../../components/ConfirmButton.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';
  import PageHeader from '../../components/PageHeader.svelte';
  import RolePicker from '../../components/RolePicker.svelte';
  import UserIdentities from './UserIdentities.svelte';

  let { params: _params }: { params: Record<string, string> } = $props();

  let users = $state<UserWithRoles[]>([]);
  // Finding someone among many, by name or email.
  let find = $state('');
  const shown = $derived.by(() => {
    const q = find.trim().toLowerCase();
    return q ? users.filter((u) => u.email.toLowerCase().includes(q) || (u.name ?? '').toLowerCase().includes(q)) : users;
  });
  let roles = $state<Role[]>([]);
  let scopes = $state<Scope[]>([]);
  let loading = $state(true);
  let error = $state('');
  let status = $state('');
  let editingRoles = $state<string | null>(null);
  let renaming = $state<string | null>(null);
  let nameDraft = $state('');
  let roleDraft = $state<RoleAssignment[]>([]);
  // Setting someone's password, for one who has forgotten theirs.
  let settingPassword = $state<string | null>(null);
  // Whose identities at providers are shown.
  let showingIdentities = $state<string | null>(null);
  // While passwords are off, none is set here (docs/access.md#turning-passwords-off).
  let passwordsOn = $state(true);
  request<PublicProviders>('GET', '/auth/providers/public').then(
    (p) => (passwordsOn = p.local_passwords),
    () => {},
  );
  let passwordDraft = $state('');

  // Creating a user.
  let creating = $state(false);
  let newEmail = $state('');
  let newName = $state('');
  let newPassword = $state('');
  let newRoles = $state<RoleAssignment[]>([]);

  // Someone as this page names them: by email, or by name for a person
  // without an account who has none.
  const who = (u: UserWithRoles) => u.email || u.name || 'Unnamed user';
  // Adding a person without an account (docs/data-model.md#person-fields),
  // and merging one into another person.
  let addingPerson = $state(false);
  let personName = $state('');
  let personEmail = $state('');
  let merging = $state<string | null>(null);
  let mergeInto = $state('');

  async function addPerson(event: SubmitEvent) {
    event.preventDefault();
    const name = personName.trim();
    await act(async () => {
      await request('POST', '/people', { name, email: personEmail });
      addingPerson = false;
      personName = personEmail = '';
    }, `${name} added. They cannot sign in until they join through a provider or are given a password.`);
  }

  function merge(event: SubmitEvent, u: UserWithRoles) {
    event.preventDefault();
    const into = users.find((x) => x.id === mergeInto);
    if (!into) return;
    let changed = 0;
    void act(async () => {
      changed = (await request<{ assets: number }>('POST', `/users/${encodeURIComponent(u.id)}/merge`, { into: into.id })).assets;
      merging = null;
      mergeInto = '';
    }, () => `${who(u)} merged into ${who(into)}: ${changed} asset${changed === 1 ? '' : 's'} changed.`);
  }

  const roleName = (id: string) => roles.find((r) => r.id === id)?.name ?? id;
  const scopeName = (id: string) => scopes.find((s) => s.id === id)?.name ?? id;
  const described = (a: RoleAssignment) => describeAssignment(roleName(a.role_id), a.scope_ids.map(scopeName));

  async function load() {
    try {
      [users, roles, scopes] = await Promise.all([
        request<{ users: UserWithRoles[] }>('GET', '/users').then((r) => r.users ?? []),
        request<{ roles: Role[] }>('GET', '/roles').then((r) => (r.roles ?? []).filter((role) => !role.public)), // everyone holds the public role
        // Extra: without scopes, roles are given in every scope.
        request<{ scopes: Scope[] }>('GET', '/scopes').then((r) => r.scopes ?? [], () => []),
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
      ? 'Refused: at least one active user must keep the Manage users permission, and this change would remove the last one.'
      : msg;
  }

  async function act(action: () => Promise<unknown>, done: string | (() => string)) {
    error = status = '';
    try {
      await action();
      status = typeof done === 'function' ? done() : done;
      await load();
    } catch (err) {
      error = explain(err);
    }
  }

  /** Email an invitation to set a password, or sign in through a provider, instead (docs/access.md#invitations). */
  let newInvite = $state(false);

  async function create(event: SubmitEvent) {
    event.preventDefault();
    await act(async () => {
      const body = newInvite ? { email: newEmail, name: newName, invite: true } : { email: newEmail, name: newName, password: newPassword };
      const res = await request<{ user: { id: string } }>('POST', '/users', body);
      if (newRoles.length) await request('PUT', `/users/${encodeURIComponent(res.user.id)}/roles`, { roles: newRoles });
      creating = false;
      newEmail = newName = newPassword = '';
      newRoles = [];
    }, newInvite ? `User created, and invited by email.` : 'User created.');
  }

  function startRename(u: UserWithRoles) {
    renaming = u.id;
    nameDraft = u.name;
  }

  function saveName(event: SubmitEvent, u: UserWithRoles) {
    event.preventDefault();
    void act(async () => {
      await request('PUT', `/users/${encodeURIComponent(u.id)}/name`, { name: nameDraft });
      if (renaming === u.id) renaming = null;
    }, nameDraft.trim() ? `${who(u)} is now shown as ${nameDraft.trim()}.` : `${who(u)} is shown by email.`);
  }

  function savePassword(event: SubmitEvent, u: UserWithRoles) {
    event.preventDefault();
    void act(async () => {
      await request('PUT', `/users/${encodeURIComponent(u.id)}/password`, { password: passwordDraft });
      settingPassword = null;
      passwordDraft = '';
    }, `Password set for ${who(u)}. They are signed out everywhere; give them the new one.`);
  }

  function startRoles(u: UserWithRoles) {
    editingRoles = u.id;
    roleDraft = u.roles.map((a) => ({ ...a, scope_ids: [...a.scope_ids] }));
  }
</script>

<PageHeader title="Users" lede="The people who can sign in, those without accounts whom fields name, and the roles that decide what each may do.">
  {#snippet actions()}
    {#if !addingPerson}<button type="button" class="btn-secondary" onclick={() => (addingPerson = true)}>Add person without account</button>{/if}
    {#if !creating}<button type="button" class="btn-primary" onclick={() => (creating = true)}>New user</button>{/if}
  {/snippet}
</PageHeader>
<ErrorMessage {error} />
{#if status}<p class="mb-2 text-sm text-green-700 dark:text-green-400" role="status">{status}</p>{/if}

{#if addingPerson}
  <form class="card mb-4 max-w-2xl space-y-4" onsubmit={addPerson}>
    <h2 class="text-lg font-semibold">Add a person without an account</h2>
    <p class="text-sm text-zinc-600 dark:text-zinc-400">
      A name to choose in person fields. They cannot sign in: when they first sign in through a provider that links by verified email, they join
      this person, and their fields are already theirs.
    </p>
    <div class="grid gap-4 sm:grid-cols-2">
      <div>
        <label class="label" for="person-name">Name</label>
        <input id="person-name" class="input" required maxlength="255" autocomplete="off" bind:value={personName} />
      </div>
      <div>
        <label class="label" for="person-email">Email (optional)</label>
        <input id="person-email" class="input" type="email" autocomplete="off" bind:value={personEmail} />
      </div>
    </div>
    <div class="flex gap-2">
      <button type="submit" class="btn-primary">Add person</button>
      <button type="button" class="btn-secondary" onclick={() => (addingPerson = false)}>Cancel</button>
    </div>
  </form>
{/if}

{#if creating}
  <form class="card mb-4 max-w-2xl space-y-4" onsubmit={create}>
    <h2 class="text-lg font-semibold">New user</h2>
    <div class="grid gap-4 sm:grid-cols-2">
      <div>
        <label class="label" for="new-email">Email</label>
        <input id="new-email" class="input" type="email" required autocomplete="off" bind:value={newEmail} />
      </div>
      <div>
        <label class="label" for="new-name">Name</label>
        <input id="new-name" class="input" required maxlength="255" autocomplete="off" aria-describedby="new-name-help" bind:value={newName} />
        <p id="new-name-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">How asset pages show them; they never show emails. Sign-in uses the email.</p>
      </div>
      {#if !newInvite}
        <div>
          <label class="label" for="new-password">Initial password</label>
          <input id="new-password" class="input" type="password" required minlength="8" maxlength="72" autocomplete="new-password" aria-describedby="new-password-help" bind:value={newPassword} />
          <p id="new-password-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">8-72 characters. They can change it after signing in.</p>
        </div>
      {/if}
    </div>
    <label class="flex items-start gap-2 text-sm">
      <input type="checkbox" class="mt-1" bind:checked={newInvite} />
      <span>
        Email them an invitation instead of setting a password
        <span class="block text-xs text-zinc-600 dark:text-zinc-400">They set their own, or sign in through a provider. The link works once, for seven days.</span>
      </span>
    </label>
    <RolePicker id="new-roles" {roles} {scopes} bind:value={newRoles} />
    <div class="flex gap-2">
      <button type="submit" class="btn-primary">Create user</button>
      <button type="button" class="btn-secondary" onclick={() => (creating = false)}>Cancel</button>
    </div>
  </form>
{/if}

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading users…</p>
{:else}
  <div class="mb-4 max-w-sm">
    <label class="label" for="users-find">Find someone</label>
    <input id="users-find" class="input" type="search" placeholder="Name or email" autocomplete="off" bind:value={find} />
  </div>
  <div class="overflow-x-auto">
    <table class="w-full text-left text-sm" use:stack>
      <thead class="border-b border-zinc-300 dark:border-zinc-700">
        <tr>
          <th scope="col" class="py-2 pr-4">Name</th>
          <th scope="col" class="py-2 pr-4">Email</th>
          <th scope="col" class="py-2 pr-4">Status</th>
          <th scope="col" class="py-2 pr-4">Roles</th>
          <th scope="col" class="py-2"><span class="sr-only">Actions</span></th>
        </tr>
      </thead>
      <tbody>
        {#each shown as u (u.id)}
          <tr class="border-b border-zinc-200 align-top dark:border-zinc-800">
            <td class="py-2 pr-4">
              {#if renaming === u.id}
                <form class="flex flex-wrap gap-2" onsubmit={(event) => saveName(event, u)}>
                  <label class="sr-only" for="name-{u.id}">Name for {who(u)}</label>
                  <input id="name-{u.id}" class="input w-48" required maxlength="255" bind:value={nameDraft} />
                  <button type="submit" class="btn-primary">Save name</button>
                  <button type="button" class="btn-secondary" onclick={() => (renaming = null)}>Cancel</button>
                </form>
              {:else}
                {u.name || '—'}
              {/if}
            </td>
            <td class="py-2 pr-4 font-medium">{u.email || '—'}{#if u.id === session.user?.id}{' '}<span class="text-xs text-zinc-600 dark:text-zinc-400">(you)</span>{/if}</td>
            <td class="py-2 pr-4">
              {u.is_active ? 'Active' : 'Disabled'}
              {#if u.can_sign_in === false}
                <span class="block text-xs text-zinc-600 dark:text-zinc-400">No account: cannot sign in</span>
              {/if}
              {#if u.sign_in_delayed_until}
                <span class="block text-xs text-amber-900 dark:text-amber-200">Wrong passwords: signing in waits until {new Date(u.sign_in_delayed_until).toLocaleTimeString()}</span>
              {/if}
              {#if u.two_factor}
                <span class="block text-xs text-zinc-600 dark:text-zinc-400">Two-factor sign-in</span>
              {/if}
            </td>
            <td class="py-2 pr-4">
              {#if editingRoles === u.id}
                <RolePicker id="roles-{u.id}" legend="Roles for {who(u)}" {roles} {scopes} bind:value={roleDraft} />
                <div class="flex gap-2 pt-1">
                  <button type="button" class="btn-secondary" onclick={() => act(async () => { await request('PUT', `/users/${encodeURIComponent(u.id)}/roles`, { roles: roleDraft }); if (editingRoles === u.id) editingRoles = null; }, `Roles updated for ${who(u)}.`)}>Save roles</button>
                  <button type="button" class="btn-secondary" onclick={() => (editingRoles = null)}>Cancel</button>
                </div>
              {:else}
                {u.roles.map(described).join(', ') || 'None'}
              {/if}
            </td>
            <td class="py-2 text-right">
              <span class="inline-flex flex-wrap justify-end gap-2">
                {#if renaming !== u.id}
                  <button type="button" class="btn-secondary" aria-label="Rename {who(u)}" onclick={() => startRename(u)}>Rename</button>
                {/if}
                {#if editingRoles !== u.id && u.can_sign_in !== false}
                  <button type="button" class="btn-secondary" aria-label="Change roles for {who(u)}" onclick={() => startRoles(u)}>Change roles</button>
                {/if}
                {#if u.can_sign_in === false && merging !== u.id}
                  <button type="button" class="btn-secondary" aria-label="Merge {who(u)} into someone" onclick={() => { merging = u.id; mergeInto = ''; }}>Merge into…</button>
                {/if}
                {#if settingPassword !== u.id && passwordsOn}
                  <button type="button" class="btn-secondary" aria-label="Set password for {who(u)}" onclick={() => { settingPassword = u.id; passwordDraft = ''; }}>Set password</button>
                {/if}
                {#if showingIdentities !== u.id}
                  <button type="button" class="btn-secondary" aria-label="Identities of {who(u)}" onclick={() => (showingIdentities = u.id)}>Identities</button>
                {/if}
                {#if u.is_active && u.email}
                  <button
                    type="button"
                    class="btn-secondary"
                    aria-label="Send {who(u)} an invitation"
                    onclick={() => act(() => request('POST', `/users/${encodeURIComponent(u.id)}/invite`), `Invitation sent to ${who(u)}.`)}>Send invitation</button>
                {/if}
                {#if u.sign_in_delayed_until}
                  <button
                    type="button"
                    class="btn-secondary"
                    aria-label="Clear sign-in delay for {who(u)}"
                    onclick={() => act(() => request('DELETE', `/users/${encodeURIComponent(u.id)}/sign-in-delay`), `${who(u)} can sign in again at once.`)}
                  >Clear sign-in delay</button>
                {/if}
                {#if u.two_factor}
                  <ConfirmButton
                    label="Remove second factors"
                    question="Remove the second factors of {who(u)}? They will be signed out everywhere, and sign in with their password alone until they set one up again. Do this only for someone who has lost theirs, and is who they say."
                    onconfirm={() => act(() => request('DELETE', `/users/${encodeURIComponent(u.id)}/factors`), `${who(u)} signs in with their password alone now.`)}
                  />
                {/if}
                <button
                  type="button"
                  class="btn-secondary"
                  aria-label="{u.is_active ? 'Disable' : 'Enable'} {who(u)}"
                  onclick={() => act(() => request('PATCH', `/users/${encodeURIComponent(u.id)}/status`, { is_active: !u.is_active }), `${who(u)} ${u.is_active ? 'disabled' : 'enabled'}.`)}
                >
                  {u.is_active ? 'Disable' : 'Enable'}
                </button>
                <ConfirmButton
                  label="Archive"
                  question="Archive {who(u)}? They will be signed out and cannot sign in again."
                  onconfirm={() => act(() => request('DELETE', `/users/${encodeURIComponent(u.id)}`), `${who(u)} archived.`)}
                />
              </span>
              {#if showingIdentities === u.id}
                <UserIdentities userId={u.id} email={who(u)} onclose={() => (showingIdentities = null)} />
              {/if}
              {#if merging === u.id}
                <form class="mt-2 flex flex-wrap justify-end gap-2" onsubmit={(event) => merge(event, u)}>
                  <label class="sr-only" for="merge-{u.id}">Merge {who(u)} into</label>
                  <select id="merge-{u.id}" class="input w-56" required bind:value={mergeInto}>
                    <option value="">Merge into…</option>
                    {#each users.filter((x) => x.id !== u.id) as x (x.id)}<option value={x.id}>{x.name || x.email}{x.email && x.name ? ` (${x.email})` : ''}</option>{/each}
                  </select>
                  <button type="submit" class="btn-primary">Merge</button>
                  <button type="button" class="btn-secondary" onclick={() => (merging = null)}>Cancel</button>
                </form>
              {/if}
              {#if settingPassword === u.id}
                <form class="mt-2 flex flex-wrap justify-end gap-2" onsubmit={(event) => savePassword(event, u)}>
                  <label class="sr-only" for="password-{u.id}">New password for {who(u)}</label>
                  <input id="password-{u.id}" class="input w-48" type="password" required minlength="8" maxlength="72" autocomplete="new-password" placeholder="8-72 characters" bind:value={passwordDraft} />
                  <button type="submit" class="btn-primary">Save password</button>
                  <button type="button" class="btn-secondary" onclick={() => (settingPassword = null)}>Cancel</button>
                </form>
              {/if}
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>

  <p class="mt-6 text-sm">
    What each role allows is set on the <a class="underline" href="/roles">Roles</a> page. A role given in some scopes only acts on those
    scopes' assets, and allows nothing else.
  </p>
  <p class="mt-2 text-sm text-zinc-600 dark:text-zinc-400">
    <strong>Disable</strong> stops someone signing in until they are enabled again. <strong>Archive</strong> removes them for good; what they did stays in history.
  </p>
{/if}

<script lang="ts">
  import { request } from '../lib/api';
  import { messageOf } from '../lib/errors';
  import type { User } from '../lib/models';
  import { describeAssignment, describeHeld } from '../lib/permissions';
  import { session } from '../lib/session.svelte';
  import ErrorMessage from '../components/ErrorMessage.svelte';
  import AssistantsSection from './AssistantsSection.svelte';

  let { params: _params }: { params: Record<string, string> } = $props();

  let name = $state(session.user?.name ?? '');
  let nameError = $state('');
  let nameStatus = $state('');

  async function rename(event: SubmitEvent) {
    event.preventDefault();
    nameError = nameStatus = '';
    try {
      session.user = (await request<{ user: User }>('PUT', '/auth/name', { name })).user;
      name = session.user.name;
      nameStatus = 'Name saved.';
    } catch (err) {
      nameError = messageOf(err);
    }
  }

  let current = $state('');
  let next = $state('');
  let confirm = $state('');
  let error = $state('');
  let status = $state('');
  let busy = $state(false);

  const mismatch = $derived(confirm !== '' && next !== confirm);

  async function change(event: SubmitEvent) {
    event.preventDefault();
    error = status = '';
    if (next !== confirm) {
      error = 'The new passwords do not match.';
      return;
    }
    busy = true;
    try {
      await request('PUT', '/auth/password', { current_password: current, new_password: next });
      status = 'Password changed. You are still signed in here; other sessions were signed out.';
      current = next = confirm = '';
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }
</script>

<h1 class="mb-4 text-2xl font-bold">Your account</h1>
<p class="mb-4 text-sm">Signed in as <strong>{session.user?.email}</strong>.</p>

<section class="card mb-6 max-w-2xl space-y-3" aria-labelledby="allowed-heading">
  <h2 id="allowed-heading" class="text-lg font-semibold">What you may do</h2>
  <p class="text-sm">
    {#if session.roles.length}Your roles: <strong>{session.roles.map((r) => describeAssignment(r, session.roleScopes[r] ?? [])).join(', ')}</strong>.{:else}You hold no roles yet.{/if}
    Whoever manages users here can change them.
  </p>
  {#if describeHeld(session.permissions).length}
    <ul class="space-y-1 text-sm">
      {#each describeHeld(session.permissions) as p (p.label)}
        <li>
          <strong>{p.label}</strong>{#if p.where.length}{' '}<span class="text-zinc-600 dark:text-zinc-400">(in {p.where.map((w) => (w === 'public' ? 'what is public' : w)).join(', ')})</span>{/if}:
          {p.description}
        </li>
      {/each}
    </ul>
  {/if}
</section>

<AssistantsSection />

<form class="card mb-6 max-w-md space-y-4" onsubmit={rename}>
  <h2 class="text-lg font-semibold">Name</h2>
  <div>
    <label class="label" for="own-name">Your name</label>
    <input id="own-name" class="input" required maxlength="255" autocomplete="name" aria-describedby="own-name-help" bind:value={name} />
    <p id="own-name-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">How others see you, such as in asset history; they never see your email. You still sign in with your email.</p>
  </div>
  <ErrorMessage error={nameError} />
  {#if nameStatus}<p class="text-sm text-green-700 dark:text-green-400" role="status">{nameStatus}</p>{/if}
  <button type="submit" class="btn-primary">Save name</button>
</form>

<form class="card max-w-md space-y-4" onsubmit={change}>
  <h2 class="text-lg font-semibold">Change password</h2>
  <div>
    <label class="label" for="current-password">Current password</label>
    <input id="current-password" class="input" type="password" required autocomplete="current-password" bind:value={current} />
  </div>
  <div>
    <label class="label" for="new-password">New password</label>
    <input id="new-password" class="input" type="password" required minlength="8" maxlength="72" autocomplete="new-password" aria-describedby="new-password-help" bind:value={next} />
    <p id="new-password-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">8-72 characters.</p>
  </div>
  <div>
    <label class="label" for="confirm-password">Repeat new password</label>
    <input id="confirm-password" class="input" type="password" required autocomplete="new-password" aria-invalid={mismatch} aria-describedby={mismatch ? 'mismatch' : undefined} bind:value={confirm} />
    {#if mismatch}<p id="mismatch" class="error-text mt-1">Does not match.</p>{/if}
  </div>
  <ErrorMessage {error} />
  {#if status}<p class="text-sm text-green-700 dark:text-green-400" role="status">{status}</p>{/if}
  <button type="submit" class="btn-primary" disabled={busy}>Change password</button>
</form>

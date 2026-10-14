<script lang="ts">
  import { request } from '../lib/api';
  import { messageOf } from '../lib/errors';
  import { session } from '../lib/session.svelte';
  import ErrorMessage from '../components/ErrorMessage.svelte';

  let { params: _params }: { params: Record<string, string> } = $props();

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
<p class="mb-6 text-sm">Signed in as <strong>{session.user?.email}</strong>. Permissions: {session.permissions.join(', ') || 'none'}.</p>

<form class="card max-w-md space-y-4" onsubmit={change}>
  <h2 class="text-lg font-semibold">Change password</h2>
  <div>
    <label class="label" for="current-password">Current password</label>
    <input id="current-password" class="input" type="password" required autocomplete="current-password" bind:value={current} />
  </div>
  <div>
    <label class="label" for="new-password">New password</label>
    <input id="new-password" class="input" type="password" required minlength="8" maxlength="72" autocomplete="new-password" aria-describedby="new-password-help" bind:value={next} />
    <p id="new-password-help" class="mt-1 text-xs text-zinc-500">8-72 characters.</p>
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

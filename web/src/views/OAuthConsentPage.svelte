<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../lib/api';
  import { messageOf } from '../lib/errors';
  import { router } from '../lib/router.svelte';
  import { session } from '../lib/session.svelte';
  import ErrorMessage from '../components/ErrorMessage.svelte';

  // An assistant asks to act as the person signed in (docs/mcp.md): they
  // approve or deny it here, and are sent back to it.

  let { params: _params }: { params: Record<string, string> } = $props();

  const q = router.query;
  const ask = {
    client_id: q.get('client_id') ?? '',
    redirect_uri: q.get('redirect_uri') ?? '',
    code_challenge: q.get('code_challenge') ?? '',
    code_challenge_method: q.get('code_challenge_method') ?? '',
    state: q.get('state') ?? '',
    resource: q.get('resource') ?? '',
  };

  let client = $state<{ id: string; name: string } | null>(null);
  let returnTo = $state('');
  let error = $state('');
  let busy = $state(false);

  onMount(async () => {
    try {
      const res = await request<{ client: { id: string; name: string }; redirect_host: string }>(
        'GET',
        `/oauth/clients/${encodeURIComponent(ask.client_id)}?redirect_uri=${encodeURIComponent(ask.redirect_uri)}`,
      );
      client = res.client;
      returnTo = res.redirect_host;
    } catch (err) {
      error = messageOf(err);
    }
  });

  async function answer(approve: boolean) {
    busy = true;
    error = '';
    try {
      const res = await request<{ redirect: string }>('POST', '/oauth/authorize', { ...ask, approve });
      window.location.assign(res.redirect);
    } catch (err) {
      error = messageOf(err);
      busy = false;
    }
  }
</script>

<h1 class="mb-4 text-2xl font-bold">Connect an assistant</h1>
<ErrorMessage {error} />
{#if client}
  <section class="card max-w-xl space-y-4" aria-labelledby="consent-heading">
    <h2 id="consent-heading" class="text-lg font-semibold"><strong>{client.name}</strong> wants to work with BAM! as you</h2>
    <p class="text-sm">
      It will be able to do anything you may do here, {session.user?.name || session.user?.email}, now and as your roles change: find,
      add and change things, lend them, print labels. What it changes is recorded as yours, through it. It cannot change your password
      or make tokens.
    </p>
    <p class="text-sm">
      You can disconnect it whenever you like, under <a class="underline" href="/account">Your account</a> › Assistants. After you
      choose, you go back to <strong>{returnTo}</strong>.
    </p>
    <div class="flex gap-2">
      <button type="button" class="btn-primary" disabled={busy} onclick={() => answer(true)}>Allow</button>
      <button type="button" class="btn-secondary" disabled={busy} onclick={() => answer(false)}>Deny</button>
    </div>
  </section>
{/if}

<!--
  Someone's identities at identity providers, on the Users page
  (docs/access.md#linking-accounts): unlinking one, and linking one by hand,
  by the subject the provider names them by.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { Identity, PublicProviders } from '../../lib/models';
  import ConfirmButton from '../../components/ConfirmButton.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  let { userId, email, onclose }: { userId: string; email: string; onclose: () => void } = $props();

  let identities = $state<Identity[]>([]);
  let providers = $state<PublicProviders['providers']>([]);
  let provider = $state('');
  let subject = $state('');
  let error = $state('');
  let status = $state('');

  const path = $derived(`/users/${encodeURIComponent(userId)}/identities`);

  async function load() {
    try {
      const [mine, pub] = await Promise.all([
        request<{ identities: Identity[] }>('GET', path),
        request<PublicProviders>('GET', '/auth/providers/public'),
      ]);
      identities = mine.identities ?? [];
      providers = pub.providers ?? [];
      provider ||= providers[0]?.key ?? '';
    } catch (err) {
      error = messageOf(err);
    }
  }
  onMount(load);

  async function link(event: SubmitEvent) {
    event.preventDefault();
    error = status = '';
    try {
      await request('POST', path, { provider_key: provider, subject });
      status = `Linked ${subject}.`;
      subject = '';
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function unlink(i: Identity) {
    error = status = '';
    try {
      await request('DELETE', `${path}/${encodeURIComponent(i.id)}`);
      status = `Unlinked ${i.provider_name}.`;
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }
</script>

<div class="mt-2 space-y-2 rounded border border-zinc-300 p-3 text-left text-sm dark:border-zinc-700" role="group" aria-label="Identities of {email}">
  {#if identities.length}
    <ul class="space-y-1">
      {#each identities as i (i.id)}
        <li class="flex flex-wrap items-center justify-between gap-2">
          <span><strong>{i.provider_name}</strong>: <code class="font-mono break-all">{i.subject}</code>{i.email ? `, ${i.email}` : ''}</span>
          <ConfirmButton label="Unlink" question="Unlink the {i.provider_name} identity of {email}?" onconfirm={() => unlink(i)} />
        </li>
      {/each}
    </ul>
  {:else}
    <p>No identities at providers.</p>
  {/if}
  {#if providers.length}
    <form class="flex flex-wrap items-end gap-2" onsubmit={link}>
      <div>
        <label class="label" for="idp-{userId}">Provider</label>
        <select id="idp-{userId}" class="input w-auto" bind:value={provider}>
          {#each providers as p (p.key)}<option value={p.key}>{p.label}</option>{/each}
        </select>
      </div>
      <div>
        <label class="label" for="subject-{userId}">Subject</label>
        <input id="subject-{userId}" class="input w-56" required aria-describedby="subject-{userId}-help" bind:value={subject} />
      </div>
      <button type="submit" class="btn-secondary">Link</button>
      <p id="subject-{userId}-help" class="w-full text-xs text-zinc-600 dark:text-zinc-400">
        The ID the provider names them by, such as OpenID Connect's sub: whoever signs in as it signs in as {email}.
      </p>
    </form>
  {/if}
  <ErrorMessage {error} />
  {#if status}<p class="text-green-700 dark:text-green-400" role="status">{status}</p>{/if}
  <button type="button" class="btn-secondary" onclick={onclose}>Close</button>
</div>

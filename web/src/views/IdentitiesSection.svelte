<!--
  Your identities at identity providers, on the account page
  (docs/access.md#linking-accounts): which providers you sign in through,
  linking another, and unlinking one while another way in remains.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../lib/api';
  import { messageOf } from '../lib/errors';
  import type { Identity, PublicProviders } from '../lib/models';
  import { router } from '../lib/router.svelte';
  import ConfirmButton from '../components/ConfirmButton.svelte';
  import ErrorMessage from '../components/ErrorMessage.svelte';

  let identities = $state<Identity[]>([]);
  let providers = $state<PublicProviders['providers']>([]);
  let loaded = $state(false);
  // A link through a provider comes back here, saying how it went.
  let error = $state(router.query.get('identity_error') ?? '');
  let status = $state(router.query.get('linked') ? `Linked your ${router.query.get('linked')} account.` : '');

  const linked = (key: string) => identities.some((i) => i.provider_key === key);
  const when = (iso?: string) => (iso ? new Date(iso).toLocaleDateString() : '');

  async function load() {
    try {
      const [mine, pub] = await Promise.all([
        request<{ identities: Identity[] }>('GET', '/auth/me/identities'),
        request<PublicProviders>('GET', '/auth/providers/public'),
      ]);
      identities = mine.identities ?? [];
      providers = pub.providers ?? [];
    } catch (err) {
      error = messageOf(err);
    } finally {
      loaded = true;
    }
  }
  onMount(load);

  async function unlink(i: Identity) {
    error = status = '';
    try {
      await request('DELETE', `/auth/me/identities/${encodeURIComponent(i.id)}`);
      status = `Unlinked your ${i.provider_name} account.`;
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }
</script>

{#if loaded && (identities.length || providers.length)}
  <section class="card space-y-3" aria-labelledby="identities-heading">
    <h2 id="identities-heading" class="text-lg font-semibold">Signing in elsewhere</h2>
    {#if identities.length}
      <ul class="space-y-2 text-sm">
        {#each identities as i (i.id)}
          <li class="flex flex-wrap items-center justify-between gap-2">
            <span>
              <strong>{i.provider_name}</strong>{i.email ? `, as ${i.email}` : ''}{#if i.last_used_at}; last used {when(i.last_used_at)}{/if}.
            </span>
            <ConfirmButton label="Unlink" question="Unlink your {i.provider_name} account? You can no longer sign in through it." onconfirm={() => unlink(i)} />
          </li>
        {/each}
      </ul>
    {:else}
      <p class="text-sm">No accounts elsewhere are linked to yours.</p>
    {/if}
    {#each providers.filter((p) => !linked(p.key)) as p (p.key)}
      <a class="btn-secondary" href="/api/v1/auth/providers/{encodeURIComponent(p.key)}/start?link=1">Link {p.label.replace(/^Sign in with /, '')}</a>
    {/each}
    <ErrorMessage {error} />
    {#if status}<p class="text-sm text-green-700 dark:text-green-400" role="status">{status}</p>{/if}
  </section>
{/if}

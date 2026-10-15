<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { Channel, Delivery, EventKind, NotifierDriver } from '../../lib/models';
  import { outcome } from '../../lib/notifications';
  import ConfirmButton from '../../components/ConfirmButton.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';
  import ChannelForm from './ChannelForm.svelte';

  let { params: _params }: { params: Record<string, string> } = $props();

  let drivers = $state<NotifierDriver[]>([]);
  let events = $state<EventKind[]>([]);
  let channels = $state<Channel[]>([]);
  let deliveries = $state<Delivery[]>([]);
  let loading = $state(true);
  let error = $state('');
  let notice = $state('');
  // The channel being edited, "new" for a new one, or null.
  let editing = $state<string | null>(null);

  const driverLabel = (d: string) => drivers.find((x) => x.driver === d)?.label ?? d;
  const eventLabel = (k: string) => events.find((e) => e.kind === k)?.label ?? k;
  const when = (s: string) => new Date(s).toLocaleString();

  async function load() {
    try {
      const [d, c, l] = await Promise.all([
        request<{ drivers: NotifierDriver[]; events: EventKind[] }>('GET', '/notifiers/drivers'),
        request<{ channels: Channel[] }>('GET', '/notifiers'),
        request<{ deliveries: Delivery[] }>('GET', '/notifiers/deliveries?limit=50'),
      ]);
      drivers = d.drivers ?? [];
      events = d.events ?? [];
      channels = c.channels ?? [];
      deliveries = l.deliveries ?? [];
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
  }
  onMount(load);

  async function saved() {
    editing = null;
    notice = 'Channel saved.';
    await load();
  }

  async function test(c: Channel) {
    error = '';
    notice = '';
    try {
      await request('POST', `/notifiers/${encodeURIComponent(c.id)}/test`);
      notice = `Test message sent to ${c.name}.`;
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function remove(c: Channel) {
    error = '';
    notice = '';
    try {
      await request('DELETE', `/notifiers/${encodeURIComponent(c.id)}`);
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }
</script>

<div class="mb-4 flex flex-wrap items-center justify-between gap-2">
  <div>
    <h1 class="text-2xl font-bold">Notifications</h1>
    <p class="text-sm text-zinc-600 dark:text-zinc-400">Where BAM! announces consumables running low and restocked.</p>
  </div>
  {#if editing === null && drivers.length}
    <button type="button" class="btn-primary" onclick={() => (editing = 'new')}>Add channel</button>
  {/if}
</div>
<ErrorMessage {error} />
{#if notice}<p class="mb-2 text-sm text-green-700 dark:text-green-400" role="status">{notice}</p>{/if}

{#if editing === 'new'}
  <div class="mb-6">
    <ChannelForm {drivers} {events} onsaved={saved} oncancel={() => (editing = null)} />
  </div>
{/if}

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading channels…</p>
{:else if channels.length === 0}
  <p class="mb-6 text-sm text-zinc-600 dark:text-zinc-400">No channels yet.</p>
{:else}
  <ul class="mb-6 space-y-3">
    {#each channels as c (c.id)}
      <li class="card">
        {#if editing === c.id}
          <ChannelForm {drivers} {events} channel={c} onsaved={saved} oncancel={() => (editing = null)} />
        {:else}
          <div class="flex flex-wrap items-start justify-between gap-2">
            <div>
              <h2 class="font-semibold">{c.name}</h2>
              <p class="text-sm text-zinc-600 dark:text-zinc-400">
                {driverLabel(c.driver)}, {c.enabled ? 'enabled' : 'disabled'}.
                {c.events.length ? c.events.map(eventLabel).join('; ') + '.' : 'Subscribed to nothing.'}
              </p>
            </div>
            <div class="flex flex-wrap gap-2">
              <button type="button" class="btn-secondary" onclick={() => test(c)} aria-label="Send a test message to {c.name}">Test</button>
              <button type="button" class="btn-secondary" onclick={() => (editing = c.id)} aria-label="Edit {c.name}">Edit</button>
              <ConfirmButton label="Delete" question="Delete the “{c.name}” channel and its delivery log?" onconfirm={() => remove(c)} />
            </div>
          </div>
        {/if}
      </li>
    {/each}
  </ul>
{/if}

<section aria-labelledby="log-heading">
  <div class="mb-2 flex items-center justify-between gap-2">
    <h2 id="log-heading" class="text-lg font-semibold">Recent deliveries</h2>
    <button type="button" class="btn-secondary" onclick={load}>Refresh</button>
  </div>
  {#if deliveries.length === 0}
    <p class="text-sm text-zinc-600 dark:text-zinc-400">Nothing sent yet.</p>
  {:else}
    <div class="overflow-x-auto">
      <table class="w-full text-left text-sm">
        <thead class="border-b border-zinc-300 dark:border-zinc-700">
          <tr>
            <th scope="col" class="py-2 pr-4">When</th>
            <th scope="col" class="py-2 pr-4">Channel</th>
            <th scope="col" class="py-2 pr-4">Event</th>
            <th scope="col" class="py-2">Outcome</th>
          </tr>
        </thead>
        <tbody>
          {#each deliveries as d (d.id)}
            <tr class="border-b border-zinc-200 align-top dark:border-zinc-800">
              <td class="py-2 pr-4 whitespace-nowrap">{when(d.created_at)}</td>
              <td class="py-2 pr-4">{d.channel_name}</td>
              <td class="py-2 pr-4">{eventLabel(d.event_kind)}{d.category_name || d.asset_name ? `: ${d.category_name || d.asset_name}` : ''}</td>
              <td class="py-2">
                {outcome(d)}
                {#if d.last_error}<span class="block text-xs text-zinc-600 dark:text-zinc-400">{d.last_error}</span>{/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</section>

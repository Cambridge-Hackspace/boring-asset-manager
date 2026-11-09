<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { Channel, Delivery, EventKind, NotifierDriver } from '../../lib/models';
  import { outcome } from '../../lib/notifications';
  import ConfirmButton from '../../components/ConfirmButton.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';
  import PageHeader from '../../components/PageHeader.svelte';
  import ChannelForm from './ChannelForm.svelte';
  import LoanReminderSettings from './LoanReminderSettings.svelte';
  import ReviewSettings from './ReviewSettings.svelte';

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
  // The channel whose deliveries the log shows, or "" for every channel's.
  let logChannel = $state('');

  const driverLabel = (d: string) => drivers.find((x) => x.driver === d)?.label ?? d;
  const eventLabel = (k: string) => events.find((e) => e.kind === k)?.label ?? k;
  const when = (s: string) => new Date(s).toLocaleString();

  async function load() {
    try {
      const [d, c, l] = await Promise.all([
        request<{ drivers: NotifierDriver[]; events: EventKind[] }>('GET', '/notifiers/drivers'),
        request<{ channels: Channel[] }>('GET', '/notifiers'),
        request<{ deliveries: Delivery[] }>('GET', deliveriesPath()),
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

  function deliveriesPath(): string {
    return '/notifiers/deliveries?limit=50' + (logChannel ? `&channel_id=${encodeURIComponent(logChannel)}` : '');
  }

  async function loadLog() {
    try {
      deliveries = (await request<{ deliveries: Delivery[] }>('GET', deliveriesPath())).deliveries ?? [];
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function showLog(c: Channel) {
    logChannel = c.id;
    await loadLog();
    document.getElementById('log-heading')?.scrollIntoView?.({ block: 'start' });
  }

  async function retry(d: Delivery) {
    error = '';
    notice = '';
    try {
      await request('POST', `/notifiers/deliveries/${encodeURIComponent(d.id)}/retry`);
      notice = `Sending again to ${d.channel_name}.`;
      await loadLog();
    } catch (err) {
      error = messageOf(err);
    }
  }

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
      notice = `Deleted ${c.name}.`;
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }
</script>

<PageHeader title="Notifications" lede="Where BAM! sends news, such as consumables running low, overdue loans and problems reported, each channel the events it chooses.">
  {#snippet actions()}
    {#if editing === null && drivers.length}
      <button type="button" class="btn-primary" onclick={() => (editing = 'new')}>New channel</button>
    {/if}
  {/snippet}
</PageHeader>
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
              {#if !c.enabled && c.disabled_reason}
                <p class="mt-1 text-sm text-amber-900 dark:text-amber-200">{c.disabled_reason} Edit it to turn it on again once it is fixed.</p>
              {:else if c.enabled && c.failures}
                <p class="mt-1 text-sm text-amber-900 dark:text-amber-200">Failing: {c.failures === 1 ? '1 attempt' : `${c.failures} attempts`} in a row{c.failing_since ? `, since ${when(c.failing_since)}` : ''}.</p>
              {/if}
            </div>
            <div class="flex flex-wrap gap-2">
              <button type="button" class="btn-secondary" onclick={() => showLog(c)} aria-label="Show the deliveries of {c.name}">Deliveries</button>
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

<LoanReminderSettings />
<ReviewSettings />

<section aria-labelledby="log-heading">
  <div class="mb-2 flex items-center justify-between gap-2">
    <h2 id="log-heading" class="text-lg font-semibold">Recent deliveries</h2>
    <div class="flex flex-wrap items-center gap-2">
      <label class="sr-only" for="log-channel">Whose deliveries</label>
      <select id="log-channel" class="input w-auto" bind:value={logChannel} onchange={loadLog}>
        <option value="">Every channel</option>
        {#each channels as c (c.id)}<option value={c.id}>{c.name}</option>{/each}
      </select>
      <button type="button" class="btn-secondary" onclick={load}>Refresh</button>
    </div>
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
            <th scope="col" class="py-2 pr-4">Outcome</th>
            <th scope="col" class="py-2"><span class="sr-only">Actions</span></th>
          </tr>
        </thead>
        <tbody>
          {#each deliveries as d (d.id)}
            <tr class="border-b border-zinc-200 align-top dark:border-zinc-800">
              <td class="py-2 pr-4 whitespace-nowrap">{when(d.created_at)}</td>
              <td class="py-2 pr-4">{d.channel_name}</td>
              <td class="py-2 pr-4">{eventLabel(d.event_kind)}{d.category_name || d.asset_name ? `: ${d.category_name || d.asset_name}` : ''}</td>
              <td class="py-2 pr-4">
                {outcome(d)}{#if d.status === 'pending' && d.attempts > 0}, next at {when(d.next_attempt_at)}{/if}
                {#if d.last_error}<span class="block text-xs text-zinc-600 dark:text-zinc-400">{d.last_error}</span>{/if}
                {#if d.response_status}
                  <details class="text-xs">
                    <summary>Answered HTTP {d.response_status}</summary>
                    {#if d.response_body}<pre class="mt-1 max-w-md overflow-x-auto whitespace-pre-wrap break-all">{d.response_body}</pre>{:else}<p>With no body.</p>{/if}
                  </details>
                {/if}
              </td>
              <td class="py-2 text-right">
                {#if d.status === 'failed'}
                  <button type="button" class="btn-secondary" onclick={() => retry(d)} aria-label="Send the delivery to {d.channel_name} again">Send again</button>
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</section>

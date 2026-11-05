<script lang="ts">
  import { stack } from '../../lib/stack';
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { Relay, RelayCode, RelayList, RelayPrinter } from '../../lib/models';
  import ConfirmButton from '../../components/ConfirmButton.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';
  import PageHeader from '../../components/PageHeader.svelte';

  let { params: _params }: { params: Record<string, string> } = $props();

  let list = $state<RelayList | null>(null);
  let loading = $state(true);
  let error = $state('');
  let status = $state('');

  // Adding a relay, and the code of the one just added or given a new code, shown once.
  let adding = $state(false);
  let name = $state('');
  let made = $state<RelayCode | null>(null);
  let copied = $state(false);

  const day = (iso: string | null | undefined) => (iso ? new Date(iso).toLocaleDateString() : '');
  const moment = (iso: string | null | undefined) => (iso ? new Date(iso).toLocaleString() : '');
  const transportNames: Record<string, string> = { bonemesh: 'BoneMesh', websocket: 'WebSocket' };

  async function load() {
    try {
      list = await request<RelayList>('GET', '/relays');
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
  }
  onMount(load);

  const jobStates: Record<string, string> = { done: 'printed', failed: 'failed', sending: 'sending', waiting: 'waiting', canceled: 'canceled' };

  /** A printer through a relay, and how its last job went. */
  function printerLine(p: RelayPrinter): string {
    if (!p.last_job) return `${p.name}: no jobs yet`;
    const state = jobStates[p.last_job.state] ?? p.last_job.state;
    return `${p.name}: ${state} ${moment(p.last_job.at)}${p.last_job.error ? ` (${p.last_job.error})` : ''}`;
  }

  /** Where a relay stands: waiting to pair, online or offline and since when, or revoked. */
  function standing(r: Relay): string {
    if (r.revoked_at) return `Revoked ${day(r.revoked_at)}; its name is free after ${day(r.cert_expires_at)}`;
    if (!r.paired) {
      if (r.code_expires_at && new Date(r.code_expires_at) > new Date()) return `Waiting to pair, with a code that works until ${moment(r.code_expires_at)}`;
      return 'Waiting to pair; its code has expired';
    }
    if (r.online) {
      const over = r.transports.map((t) => transportNames[t] ?? t).join(' and ');
      return `Online since ${moment(r.status_at)}${over ? `, over ${over}` : ''}`;
    }
    return r.status_at ? `Offline since ${moment(r.status_at)}` : 'Not online yet';
  }

  async function add(event: SubmitEvent) {
    event.preventDefault();
    error = status = '';
    try {
      made = await request<RelayCode>('POST', '/relays', { name: name.trim() });
      copied = false;
      adding = false;
      name = '';
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function newCode(r: Relay) {
    error = status = '';
    try {
      made = await request<RelayCode>('POST', `/relays/${encodeURIComponent(r.id)}/code`);
      copied = false;
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function revoke(r: Relay) {
    error = status = '';
    try {
      await request('DELETE', `/relays/${encodeURIComponent(r.id)}`);
      status = r.paired ? `Revoked ${r.name}.` : `Removed ${r.name}.`;
      if (made?.relay.id === r.id) made = null;
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function rotate() {
    error = status = '';
    try {
      await request('POST', '/relays/mesh/rotate');
      status = 'Rotated the mesh root. Give each relay a new code to pair it again.';
      made = null;
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function copy() {
    if (!made) return;
    try {
      await navigator.clipboard.writeText(made.command);
      copied = true;
    } catch {
      error = 'Could not copy; select the command and copy it by hand.';
    }
  }
</script>

<PageHeader title="Relays">
  {#snippet lede()}
    A relay is BAM! run on a machine on a local network, such as a Raspberry Pi beside a label printer, so that this BAM! can reach printers and
    devices there. The relay dials out to BAM!, so nothing on that network is opened to the internet (<a class="underline" href="/help/relays">Relays</a>).
  {/snippet}
  {#snippet actions()}
    {#if !adding && list?.secret_key}<button type="button" class="btn-primary" onclick={() => ((adding = true), (made = null))}>Add relay</button>{/if}
  {/snippet}
</PageHeader>
<ErrorMessage {error} />
{#if status}<p class="mb-2 text-sm text-green-700 dark:text-green-400" role="status">{status}</p>{/if}

{#if list && !list.secret_key}
  <p class="card mb-4 max-w-2xl text-sm" role="note">
    Relays need <code>[auth] secret_key</code> in BAM!'s configuration, which keeps the keys that pair them safe. Add one, as <code>bam init</code> writes, and
    start BAM! again.
  </p>
{:else if list && !list.bonemesh && !list.websocket}
  <p class="card mb-4 max-w-2xl text-sm" role="note">
    Relays are off on this server, so none can pair or connect. Turn on <code>websocket</code> or <code>bonemesh</code> under <code>[relay]</code> in the
    configuration.
  </p>
{/if}

{#if made}
  <section class="card mb-6 max-w-2xl space-y-2 border-amber-400" aria-labelledby="code-heading">
    <h2 id="code-heading" class="text-lg font-semibold">Pair {made.relay.name}</h2>
    <p class="text-sm">
      On the relay's machine, run this command before {moment(made.expires_at)}. The code works once, and will not be shown again.
    </p>
    <label class="sr-only" for="relay-command">The command that pairs the relay</label>
    <input id="relay-command" class="input font-mono" readonly value={made.command} onfocus={(e) => e.currentTarget.select()} />
    <div class="flex gap-2">
      <button type="button" class="btn-secondary" onclick={copy}>{copied ? 'Copied' : 'Copy'}</button>
      <button type="button" class="btn-secondary" onclick={() => (made = null)}>Done</button>
    </div>
  </section>
{/if}

{#if adding}
  <form class="card mb-6 max-w-2xl space-y-4" onsubmit={add}>
    <h2 class="text-lg font-semibold">New relay</h2>
    <div>
      <label class="label" for="relay-name">Name</label>
      <input
        id="relay-name"
        class="input"
        required
        maxlength="63"
        placeholder="front-desk"
        aria-describedby="relay-name-hint"
        bind:value={name}
      />
      <p id="relay-name-hint" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">Lowercase letters, digits and hyphens. Printers name their relay by it.</p>
    </div>
    <div class="flex gap-2">
      <button type="submit" class="btn-primary">Add and make a code</button>
      <button type="button" class="btn-secondary" onclick={() => (adding = false)}>Cancel</button>
    </div>
  </form>
{/if}

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading relays…</p>
{:else if list && list.relays.length === 0}
  <p class="text-sm text-zinc-600 dark:text-zinc-400">No relays yet.</p>
{:else if list}
  <div class="overflow-x-auto">
    <table class="w-full text-left text-sm" use:stack>
      <thead class="border-b border-zinc-300 dark:border-zinc-700">
        <tr>
          <th scope="col" class="py-2 pr-4">Name</th>
          <th scope="col" class="py-2 pr-4">Status</th>
          <th scope="col" class="py-2 pr-4">Version</th>
          <th scope="col" class="py-2 pr-4">Address</th>
          <th scope="col" class="py-2 pr-4">May reach</th>
          <th scope="col" class="py-2 pr-4">Printers</th>
          <th scope="col" class="py-2 pr-4">Certificate</th>
          <th scope="col" class="py-2"><span class="sr-only">Actions</span></th>
        </tr>
      </thead>
      <tbody>
        {#each list.relays as r (r.id)}
          <tr class="border-b border-zinc-200 dark:border-zinc-800">
            <td class="py-2 pr-4 font-medium">{r.name}</td>
            <td class="py-2 pr-4">{standing(r)}</td>
            <td class="py-2 pr-4">{r.version}</td>
            <td class="py-2 pr-4">{r.address}</td>
            <td class="py-2 pr-4">{r.allowlist.join(', ')}</td>
            <td class="py-2 pr-4">
              {#each r.printers ?? [] as p (p.id)}<span class="block">{printerLine(p)}</span>{/each}
            </td>
            <td class="py-2 pr-4 whitespace-nowrap">{r.paired && !r.revoked_at ? `Until ${day(r.cert_expires_at)}` : ''}</td>
            <td class="py-2 text-right whitespace-nowrap">
              {#if !r.revoked_at}
                <div class="flex justify-end gap-2">
                  <button type="button" class="btn-secondary" onclick={() => newCode(r)}>New code</button>
                  {#if r.paired}
                    <ConfirmButton label="Revoke" question="Revoke {r.name}? Its connections close at once, and it can no longer renew." onconfirm={() => revoke(r)} />
                  {:else}
                    <ConfirmButton label="Remove" question="Remove {r.name}? Its code stops working." onconfirm={() => revoke(r)} />
                  {/if}
                </div>
              {/if}
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}

{#if list?.mesh}
  <section class="mt-8 max-w-2xl space-y-2" aria-labelledby="mesh-heading">
    <h2 id="mesh-heading" class="text-lg font-semibold">Mesh root</h2>
    <p class="text-sm">
      The mesh root, {list.mesh.name}, issues each relay's certificate. It was made {day(list.mesh.created_at)}, and is kept sealed with the configuration's key.
      Rotating it unpairs every relay, each to pair again with a new code.
    </p>
    <ConfirmButton
      label="Rotate the mesh root"
      confirmLabel="Rotate"
      question="Rotate the mesh root? Every relay is unpaired at once, and pairs again only with a new code."
      onconfirm={rotate}
    />
  </section>
{/if}

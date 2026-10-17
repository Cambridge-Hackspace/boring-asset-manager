<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { Printer, PrinterPool } from '../../lib/models';
  import { session } from '../../lib/session.svelte';
  import ConfirmButton from '../../components/ConfirmButton.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  // Printer pools (README §6.2): printers loaded alike that share print
  // jobs from the spool. Managed with the printers.
  let { printers }: { printers: Printer[] } = $props();

  let pools = $state<PrinterPool[]>([]);
  let error = $state('');
  let status = $state('');
  // The pool being edited: its ID, 'new', or none.
  let editing = $state<string | null>(null);
  let name = $state('');
  let batchSize = $state(10);
  let fixedShares = $state(false);
  // Each printer's share, for those in the pool.
  let shares = $state<Record<string, number>>({});

  const canManage = $derived(session.can('settings.manage'));
  const printerName = (id: string) => printers.find((p) => p.id === id)?.name ?? 'A deleted printer';
  // Behind a spooler, a printer accepts a whole job at once, so it takes a
  // fixed share rather than the next batch when free.
  const spooled = (p: Printer) => p.connection.type === 'ipp' || p.connection.type === 'windows';

  async function load() {
    try {
      pools = (await request<{ pools: PrinterPool[] }>('GET', '/printer-pools')).pools ?? [];
    } catch (err) {
      error = messageOf(err);
    }
  }
  onMount(load);

  function edit(p?: PrinterPool) {
    error = status = '';
    editing = p?.id ?? 'new';
    name = p?.name ?? '';
    batchSize = p?.batch_size ?? 10;
    fixedShares = p?.fixed_shares ?? false;
    shares = Object.fromEntries((p?.members ?? []).map((m) => [m.printer_id, m.share]));
  }

  function toggle(id: string, on: boolean) {
    const next = { ...shares };
    if (on) next[id] = next[id] ?? 1;
    else delete next[id];
    shares = next;
  }

  async function save(event: SubmitEvent) {
    event.preventDefault();
    error = '';
    const body = { name, batch_size: batchSize, fixed_shares: fixedShares, members: Object.entries(shares).map(([printer_id, share]) => ({ printer_id, share })) };
    try {
      if (editing === 'new') await request('POST', '/printer-pools', body);
      else await request('PUT', `/printer-pools/${encodeURIComponent(editing!)}`, body);
      status = `Saved ${name.trim()}.`;
      editing = null;
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function remove(p: PrinterPool) {
    error = status = '';
    try {
      await request('DELETE', `/printer-pools/${encodeURIComponent(p.id)}`);
      status = `Deleted ${p.name}.`;
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }
</script>

<section class="mt-8" aria-labelledby="pools-heading">
  <div class="mb-2 flex flex-wrap items-center justify-between gap-2">
    <h2 id="pools-heading" class="text-xl font-semibold">Pools</h2>
    {#if canManage && editing === null}
      <button type="button" class="btn-secondary" onclick={() => edit()} disabled={!printers.length}>Add pool</button>
    {/if}
  </div>
  <p class="mb-3 max-w-prose text-sm text-zinc-600 dark:text-zinc-400">
    Printers loaded alike share the spool's jobs sent to their pool: each takes the next batch of labels when it has sent
    its last, so a faster printer prints more. Printers behind a spooler (IPP or Windows) take a fixed share instead, as
    every printer does when the pool says so.
  </p>
  <ErrorMessage {error} />
  {#if status}<p class="mb-2 text-sm text-green-700 dark:text-green-400" role="status">{status}</p>{/if}

  {#if editing !== null}
    <form class="card mb-4 max-w-2xl space-y-3" onsubmit={save} aria-label={editing === 'new' ? 'New pool' : `Edit ${name}`}>
      <div>
        <label class="label" for="pool-name">Name</label>
        <input id="pool-name" class="input" required maxlength="255" bind:value={name} />
      </div>
      <div>
        <label class="label" for="pool-batch">Labels in a batch</label>
        <input id="pool-batch" class="input w-24" type="number" min="1" max="100" required bind:value={batchSize} />
      </div>
      <label class="flex items-center gap-2 text-sm"><input type="checkbox" bind:checked={fixedShares} /> Every printer takes a fixed share</label>
      <fieldset class="space-y-2">
        <legend class="label">Printers, and their shares</legend>
        {#each printers as p (p.id)}
          <div class="flex flex-wrap items-center gap-3 text-sm">
            <label class="flex min-w-48 items-center gap-2">
              <input type="checkbox" checked={p.id in shares} onchange={(e) => toggle(p.id, e.currentTarget.checked)} /> {p.name}
            </label>
            {#if p.id in shares}
              <label class="flex items-center gap-2">
                Share <input class="input w-20" type="number" min="1" max="100" aria-label="{p.name}'s share" bind:value={shares[p.id]} />
              </label>
              {#if spooled(p) && !fixedShares}<span class="text-xs text-zinc-600 dark:text-zinc-400">Behind a spooler: takes its share</span>{/if}
            {/if}
          </div>
        {/each}
      </fieldset>
      <div class="flex gap-2">
        <button type="submit" class="btn-primary" disabled={!Object.keys(shares).length}>Save pool</button>
        <button type="button" class="btn-secondary" onclick={() => (editing = null)}>Cancel</button>
      </div>
    </form>
  {/if}

  {#if pools.length === 0}
    <p class="text-sm text-zinc-600 dark:text-zinc-400">No pools yet.</p>
  {:else}
    <ul class="space-y-3">
      {#each pools as p (p.id)}
        <li>
          <article class="card" aria-labelledby="pool-{p.id}">
            <div class="flex flex-wrap items-start justify-between gap-2">
              <div>
                <h3 id="pool-{p.id}" class="font-semibold">{p.name}</h3>
                <p class="text-sm text-zinc-600 dark:text-zinc-400">
                  Batches of {p.batch_size}{p.fixed_shares ? ', fixed shares' : ''}: {p.members.map((m) => (p.fixed_shares || m.share > 1 ? `${printerName(m.printer_id)} (share ${m.share})` : printerName(m.printer_id))).join(', ') || 'no printers left'}
                </p>
              </div>
              {#if canManage}
                <div class="flex gap-2">
                  <button type="button" class="btn-secondary" onclick={() => edit(p)}>Edit</button>
                  <ConfirmButton label="Delete" confirmLabel="Delete {p.name}" question="Delete this pool? Labels waiting for it are left with nowhere to go." onconfirm={() => remove(p)} />
                </div>
              {/if}
            </div>
          </article>
        </li>
      {/each}
    </ul>
  {/if}
</section>

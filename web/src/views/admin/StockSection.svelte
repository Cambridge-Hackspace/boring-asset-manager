<script lang="ts">
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { LabelStock } from '../../lib/models';
  import { session } from '../../lib/session.svelte';
  import { describeStock } from '../../lib/stock';
  import ConfirmButton from '../../components/ConfirmButton.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  // Custom label stock (README §4.4): rolls known by their size, for
  // printers that take any size. Managed with the printers, which reload
  // after a change, since their loaded media may be among them.
  let { stock, onchange }: { stock: LabelStock[]; onchange: () => Promise<void> } = $props();

  let error = $state('');
  let status = $state('');
  // The stock being edited: its ID, 'new', or none.
  let editing = $state<string | null>(null);
  let name = $state('');
  let width = $state<number | null>(null);
  let continuous = $state(false);
  let length = $state<number | null>(null);
  let gap = $state<number | null>(null);

  const canManage = $derived(session.can('settings.manage'));

  function edit(s?: LabelStock) {
    error = status = '';
    editing = s?.id ?? 'new';
    name = s?.name ?? '';
    width = s?.width_mm ?? null;
    continuous = !!s && !s.length_mm;
    length = s?.length_mm || null;
    gap = s ? s.gap_mm : 3;
  }

  async function save(event: SubmitEvent) {
    event.preventDefault();
    error = '';
    const body = { name, width_mm: width ?? 0, length_mm: continuous ? 0 : (length ?? 0), gap_mm: continuous ? 0 : (gap ?? 0) };
    try {
      if (editing === 'new') await request('POST', '/label-stock', body);
      else await request('PUT', `/label-stock/${encodeURIComponent(editing!)}`, body);
      status = `Saved ${name.trim()}.`;
      editing = null;
      await onchange();
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function remove(s: LabelStock) {
    error = status = '';
    try {
      await request('DELETE', `/label-stock/${encodeURIComponent(s.id)}`);
      status = `Deleted ${s.name}.`;
      await onchange();
    } catch (err) {
      error = messageOf(err);
    }
  }
</script>

<section class="mt-8" aria-labelledby="stock-heading">
  <div class="mb-2 flex flex-wrap items-center justify-between gap-2">
    <h2 id="stock-heading" class="text-xl font-semibold">Label stock</h2>
    {#if canManage && editing === null}
      <button type="button" class="btn-secondary" onclick={() => edit()}>New label stock</button>
    {/if}
  </div>
  <p class="mb-3 max-w-prose text-sm text-zinc-600 dark:text-zinc-400">
    Rolls of any size, for printers that take them: TSPL printers such as the Rollo, and DYMO LabelWriters. Brother QL
    printers take only Brother's own rolls, which are built in.
  </p>
  <ErrorMessage {error} />
  {#if status}<p class="mb-2 text-sm text-green-700 dark:text-green-400" role="status">{status}</p>{/if}

  {#if editing !== null}
    <form class="card mb-4 max-w-2xl space-y-3" onsubmit={save} aria-label={editing === 'new' ? 'New label stock' : `Edit ${name}`}>
      <div>
        <label class="label" for="stock-name">Name</label>
        <input id="stock-name" class="input" required maxlength="255" bind:value={name} />
      </div>
      <div class="flex flex-wrap gap-4">
        <div>
          <label class="label" for="stock-width">Width (mm)</label>
          <input id="stock-width" class="input w-28" type="number" min="5" max="300" step="0.01" required bind:value={width} />
        </div>
        {#if !continuous}
          <div>
            <label class="label" for="stock-length">Length (mm)</label>
            <input id="stock-length" class="input w-28" type="number" min="5" max="3000" step="0.01" required bind:value={length} />
          </div>
          <div>
            <label class="label" for="stock-gap">Gap (mm)</label>
            <input id="stock-gap" class="input w-28" type="number" min="0" max="25" step="0.01" required bind:value={gap} />
          </div>
        {/if}
      </div>
      <label class="flex items-center gap-2 text-sm"><input type="checkbox" bind:checked={continuous} /> Continuous roll</label>
      <div class="flex gap-2">
        <button type="submit" class="btn-primary">Save stock</button>
        <button type="button" class="btn-secondary" onclick={() => (editing = null)}>Cancel</button>
      </div>
    </form>
  {/if}

  {#if stock.length === 0}
    <p class="text-sm text-zinc-600 dark:text-zinc-400">No custom stock yet.</p>
  {:else}
    <ul class="space-y-3">
      {#each stock as s (s.id)}
        <li>
          <article class="card" aria-labelledby="stock-{s.id}">
            <div class="flex flex-wrap items-start justify-between gap-2">
              <div>
                <h3 id="stock-{s.id}" class="font-semibold">{s.name}</h3>
                <p class="text-sm text-zinc-600 dark:text-zinc-400">
                  {describeStock(s)}{s.printers.length ? `. Loaded in ${s.printers.join(', ')}` : ''}
                </p>
              </div>
              {#if canManage}
                <div class="flex gap-2">
                  <button type="button" class="btn-secondary" aria-label="Edit {s.name}" onclick={() => edit(s)}>Edit</button>
                  <ConfirmButton label="Delete" confirmLabel="Delete {s.name}" question="Delete this stock?" onconfirm={() => remove(s)} />
                </div>
              {/if}
            </div>
          </article>
        </li>
      {/each}
    </ul>
  {/if}
</section>

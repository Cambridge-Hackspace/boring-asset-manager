<script lang="ts">
  import { onMount } from 'svelte';
  import { request, requestDownload } from '../lib/api';
  import { dueDate } from '../lib/assets';
  import { messageOf } from '../lib/errors';
  import type { Spool, SpoolAddResult, SpoolDestination, SpoolEntry } from '../lib/models';
  import { actorName } from '../lib/people';
  import { addedMessage, destinationName, groupEntries, printersFor, shared, templateName, templatesFor } from '../lib/spool';
  import ConfirmButton from '../components/ConfirmButton.svelte';
  import ErrorMessage from '../components/ErrorMessage.svelte';

  // The spool (README §6.2): labels waiting to print, sorted onto templates
  // and printers in bulk; and the Printed list, to print again from.
  let { params: _params }: { params: Record<string, string> } = $props();

  let spool = $state<Spool>({ entries: [], templates: [], printers: [], fits: {} });
  let printed = $state(false);
  let older = $state(false);
  let mine = $state(false);
  let selected = $state<string[]>([]);
  let loading = $state(true);
  let error = $state('');
  let status = $state('');

  // What the selection is set to, chosen in the bulk bar.
  let setTemplate = $state('');
  let setDestination = $state('');
  let setCopies = $state(1);

  const groups = $derived(printed ? [] : groupEntries(spool));
  const chosen = $derived(spool.entries.filter((e) => selected.includes(e.id)));
  // Choosing a template offers only the printers it suits, and the reverse.
  const destinationChoices = $derived(printersFor(spool, shared(chosen, (e) => e.template_id)));
  const templateChoices = $derived(templatesFor(spool, shared(chosen, (e) => e.destination)));
  const allSelected = $derived(spool.entries.length > 0 && spool.entries.every((e) => selected.includes(e.id)));

  // Each load is numbered, so a slow answer to an older one cannot replace
  // what a newer one asked for.
  let loads = 0;

  async function load() {
    const n = ++loads;
    const q = new URLSearchParams();
    if (printed) q.set('printed', 'true');
    if (printed && older) q.set('older', 'true');
    if (mine) q.set('mine', 'true');
    try {
      const got = await request<Spool>('GET', `/spool?${q}`);
      if (n !== loads) return;
      spool = got;
      selected = selected.filter((id) => spool.entries.some((e) => e.id === id));
    } catch (err) {
      if (n === loads) error = messageOf(err);
    } finally {
      if (n === loads) loading = false;
    }
  }

  onMount(load);

  function show(isPrinted: boolean) {
    if (printed === isPrinted) return;
    printed = isPrinted;
    selected = [];
    error = status = '';
    void load();
  }

  function toggleGroup(entries: SpoolEntry[]) {
    const ids = entries.map((e) => e.id);
    const all = ids.every((id) => selected.includes(id));
    selected = all ? selected.filter((id) => !ids.includes(id)) : [...new Set([...selected, ...ids])];
  }

  function toggleAll() {
    selected = allSelected ? [] : spool.entries.map((e) => e.id);
  }

  async function act(done: string, work: () => Promise<unknown>) {
    error = status = '';
    try {
      await work();
      status = done;
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }

  const count = (n: number) => (n === 1 ? '1 label' : `${n} labels`);

  function update(body: Record<string, unknown>, done: string) {
    return act(done, () => request('PATCH', '/spool', { entry_ids: selected, ...body }));
  }

  function remove() {
    const n = selected.length;
    return act(`Removed ${count(n)} from the spool.`, async () => {
      await request('POST', '/spool/remove', { entry_ids: selected });
      selected = [];
    });
  }

  async function printAgain() {
    error = status = '';
    try {
      const r = await request<SpoolAddResult>('POST', '/spool/requeue', { entry_ids: selected });
      status = addedMessage(r);
      selected = [];
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }

  function saveBlob(blob: Blob, name: string) {
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = name;
    document.body.append(a);
    a.click();
    a.remove();
    setTimeout(() => URL.revokeObjectURL(url), 10_000);
  }

  // Downloading counts as printing: what downloads leaves for the Printed
  // list, and what cannot be produced stays with the reason.
  async function download(entries: SpoolEntry[], format: 'pdf' | 'zip') {
    error = status = '';
    try {
      const { blob, filename } = await requestDownload('POST', '/spool/download', { entry_ids: entries.map((e) => e.id), format }, `spool.${format}`);
      saveBlob(blob, filename);
      await load();
      const left = spool.entries.filter((e) => entries.some((d) => d.id === e.id)).length;
      status = `Downloaded ${count(entries.length - left)}.` + (left ? ` ${count(left)} could not be produced and stayed in the spool.` : '');
    } catch (err) {
      error = messageOf(err);
    }
  }

  const when = (iso: string) => dueDate(iso);
</script>

<div class="mb-4 flex flex-wrap items-center justify-between gap-2">
  <h1 class="text-2xl font-bold">Spool</h1>
  <div class="flex gap-1" role="group" aria-label="Show">
    <button type="button" class={printed ? 'btn-secondary' : 'btn-primary'} aria-pressed={!printed} onclick={() => show(false)}>Waiting</button>
    <button type="button" class={printed ? 'btn-primary' : 'btn-secondary'} aria-pressed={printed} onclick={() => show(true)}>Printed</button>
  </div>
</div>

<div class="mb-4 flex flex-wrap gap-4 text-sm">
  <label class="flex items-center gap-2"><input type="checkbox" bind:checked={mine} onchange={load} /> Only labels I added</label>
  {#if printed}
    <label class="flex items-center gap-2"><input type="checkbox" bind:checked={older} onchange={load} /> Include those printed over 30 days ago</label>
  {/if}
</div>

<ErrorMessage {error} />
{#if status}<p class="mb-2 text-sm text-green-700 dark:text-green-400" role="status">{status}</p>{/if}

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading the spool…</p>
{:else if spool.entries.length === 0}
  <p class="text-sm text-zinc-600 dark:text-zinc-400">
    {#if printed}Nothing printed{older ? '' : ' in the last 30 days'}.{:else}The spool is empty. Add labels from the asset list, a category, or an asset's page.{/if}
  </p>
{:else}
  <p class="sr-only" role="status">{count(spool.entries.length)}, {selected.length} selected.</p>
  <p class="mb-2 text-sm"><label class="flex items-center gap-2"><input type="checkbox" checked={allSelected} onchange={toggleAll} /> Select all {count(spool.entries.length)}</label></p>

  {#if selected.length}
    <section class="sticky top-0 z-10 mb-4 rounded border border-zinc-300 bg-white p-3 dark:border-zinc-700 dark:bg-zinc-900" aria-label="Change the selected labels">
      <p class="mb-2 text-sm font-medium">{count(selected.length)} selected</p>
      {#if printed}
        <button type="button" class="btn-primary" onclick={printAgain}>Print again</button>
      {:else}
        <div class="flex flex-wrap items-end gap-3">
          <form class="flex items-end gap-2" onsubmit={(e) => (e.preventDefault(), update({ template_id: setTemplate }, `Template set for ${count(selected.length)}.`))}>
            <div>
              <label class="label" for="spool-template">Template</label>
              <select id="spool-template" class="input" bind:value={setTemplate}>
                <option value="">None</option>
                {#each templateChoices as t (t.id)}<option value={t.id}>{t.name}</option>{/each}
              </select>
            </div>
            <button type="submit" class="btn-secondary">Set template</button>
          </form>
          <form class="flex items-end gap-2" onsubmit={(e) => (e.preventDefault(), update({ destination: setDestination as SpoolDestination }, `Printer set for ${count(selected.length)}.`))}>
            <div>
              <label class="label" for="spool-destination">Print on</label>
              <select id="spool-destination" class="input" bind:value={setDestination}>
                <option value="">Not chosen</option>
                {#each destinationChoices as p (p.id)}<option value="printer:{p.id}">{p.name}</option>{/each}
                <option value="download">Download (PDF or ZIP)</option>
              </select>
            </div>
            <button type="submit" class="btn-secondary">Set printer</button>
          </form>
          <form class="flex items-end gap-2" onsubmit={(e) => (e.preventDefault(), update({ copies: setCopies }, `Copies set for ${count(selected.length)}.`))}>
            <div>
              <label class="label" for="spool-copies">Copies</label>
              <input id="spool-copies" class="input w-20" type="number" min="1" max="20" bind:value={setCopies} />
            </div>
            <button type="submit" class="btn-secondary">Set copies</button>
          </form>
          <ConfirmButton label="Remove" confirmLabel="Remove {count(selected.length)}" question="Take these labels out of the spool?" onconfirm={remove} />
        </div>
      {/if}
    </section>
  {/if}

  {#if printed}
    <div class="overflow-x-auto">
      <table class="w-full text-left text-sm">
        <thead class="border-b border-zinc-300 dark:border-zinc-700">
          <tr>
            <th scope="col" class="w-8 py-2 pr-2"><span class="sr-only">Select</span></th>
            <th scope="col" class="py-2 pr-4">Asset</th>
            <th scope="col" class="py-2 pr-4">Template</th>
            <th scope="col" class="py-2 pr-4">Printed on</th>
            <th scope="col" class="py-2 pr-4">Copies</th>
            <th scope="col" class="py-2">By, when</th>
          </tr>
        </thead>
        <tbody>
          {#each spool.entries as e (e.id)}
            <tr class="border-b border-zinc-200 dark:border-zinc-800">
              <td class="py-2 pr-2"><input type="checkbox" aria-label="Select {e.asset_name}" value={e.id} bind:group={selected} /></td>
              <td class="py-2 pr-4 font-medium"><a class="hover:underline" href="/assets/{encodeURIComponent(e.asset_id)}">{e.asset_name}</a></td>
              <td class="py-2 pr-4">{e.printed_template}</td>
              <td class="py-2 pr-4">{e.printed_on}</td>
              <td class="py-2 pr-4">{e.copies}</td>
              <td class="py-2">{e.printed_by ? actorName(e.printed_by) : ''}, {e.printed_at ? when(e.printed_at) : ''}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {:else}
    {#each groups as g, gi (g.key)}
      {@const ready = g.entries.filter((e) => e.problems.length === 0)}
      <section class="mb-6" aria-labelledby="spool-group-{gi}">
        <div class="mb-2 flex flex-wrap items-center justify-between gap-2">
          <h2 id="spool-group-{gi}" class="text-lg font-semibold">
            {destinationName(spool, g.destination)} <span class="font-normal text-zinc-600 dark:text-zinc-400">· {templateName(spool, g.templateId)} · {count(g.entries.length)}</span>
          </h2>
          {#if g.destination === 'download' && g.templateId}
            <div class="flex gap-2">
              <button type="button" class="btn-secondary" disabled={!ready.length} onclick={() => download(ready, 'pdf')}>Download PDF</button>
              <button type="button" class="btn-secondary" disabled={!ready.length} onclick={() => download(ready, 'zip')}>Download ZIP</button>
            </div>
          {/if}
        </div>
        <div class="overflow-x-auto">
          <table class="w-full text-left text-sm">
            <thead class="border-b border-zinc-300 dark:border-zinc-700">
              <tr>
                <th scope="col" class="w-8 py-2 pr-2">
                  <input type="checkbox" aria-label="Select every label for {destinationName(spool, g.destination)}, {templateName(spool, g.templateId)}" checked={g.entries.every((e) => selected.includes(e.id))} onchange={() => toggleGroup(g.entries)} />
                </th>
                <th scope="col" class="py-2 pr-4">Asset</th>
                <th scope="col" class="py-2 pr-4">Copies</th>
                <th scope="col" class="py-2 pr-4">Added</th>
                <th scope="col" class="py-2">Problems</th>
              </tr>
            </thead>
            <tbody>
              {#each g.entries as e (e.id)}
                <tr class="border-b border-zinc-200 dark:border-zinc-800">
                  <td class="py-2 pr-2"><input type="checkbox" aria-label="Select {e.asset_name}" value={e.id} bind:group={selected} /></td>
                  <td class="py-2 pr-4 font-medium"><a class="hover:underline" href="/assets/{encodeURIComponent(e.asset_id)}">{e.asset_name}</a></td>
                  <td class="py-2 pr-4">{e.copies}</td>
                  <td class="py-2 pr-4">{actorName(e.added_by)}, {when(e.added_at)}</td>
                  <td class="py-2">
                    {#if e.problems.length}
                      <ul class="text-amber-800 dark:text-amber-300">
                        {#each e.problems as p, i (i)}<li><span aria-hidden="true">⚠</span> {p}</li>{/each}
                      </ul>
                    {:else}
                      <span class="text-zinc-600 dark:text-zinc-400">Ready</span>
                    {/if}
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      </section>
    {/each}
  {/if}
{/if}

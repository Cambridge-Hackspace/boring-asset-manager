<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { request, requestDownload } from '../lib/api';
  import { dueDate } from '../lib/assets';
  import { messageOf } from '../lib/errors';
  import type { Spool, SpoolAddResult, SpoolDestination, SpoolEntry, SpoolJob, SpoolPrintResult } from '../lib/models';
  import { actorName } from '../lib/people';
  import { session } from '../lib/session.svelte';
  import { addedMessage, destinationName, entryWarnings, groupEntries, jobActive, jobStateName, poolsFor, printedMessage, printersFor, printing, shared, templateName, templatesFor } from '../lib/spool';
  import ConfirmButton from '../components/ConfirmButton.svelte';
  import ErrorMessage from '../components/ErrorMessage.svelte';
  import FitWarnings from '../components/FitWarnings.svelte';

  // The spool (README §6.2): labels waiting to print, sorted onto templates
  // and printers in bulk and printed in the background; the jobs printing
  // them; and the Printed list, to print again from.
  let { params: _params }: { params: Record<string, string> } = $props();

  type View = 'waiting' | 'jobs' | 'printed';
  let view = $state<View>('waiting');
  let spool = $state<Spool>({ entries: [], templates: [], printers: [], pools: [], fits: {}, pool_fits: {} });
  let jobs = $state<SpoolJob[]>([]);
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
  // Where each past job is sent again: '' for where it went.
  let resendTo = $state<Record<string, string>>({});

  const groups = $derived(view === 'waiting' ? groupEntries(spool) : []);
  // Entries a job is printing cannot be selected or changed.
  const selectable = $derived(spool.entries.filter((e) => !e.job));
  const chosen = $derived(spool.entries.filter((e) => selected.includes(e.id)));
  // Choosing a template offers only the printers it suits, and the reverse.
  const destinationChoices = $derived(printersFor(spool, shared(chosen, (e) => e.template_id)));
  const poolChoices = $derived(poolsFor(spool, shared(chosen, (e) => e.template_id)));
  const templateChoices = $derived(templatesFor(spool, shared(chosen, (e) => e.destination)));
  const allSelected = $derived(selectable.length > 0 && selectable.every((e) => selected.includes(e.id)));
  const readyToPrint = $derived(spool.entries.filter((e) => e.ready && printing(e.destination)));
  // While anything is printing, the page checks on it.
  const busy = $derived(spool.entries.some((e) => e.job) || jobs.some((j) => jobActive(j.state)));

  // Each load is numbered, so a slow answer to an older one cannot replace
  // what a newer one asked for.
  let loads = 0;

  async function load() {
    const n = ++loads;
    const q = new URLSearchParams();
    if (view === 'printed') q.set('printed', 'true');
    if (view !== 'waiting' && older) q.set('older', 'true');
    if (mine) q.set('mine', 'true');
    try {
      const [got, gotJobs] = await Promise.all([
        // The jobs view reads the spool too, for the printers to send to.
        request<Spool>('GET', view === 'jobs' ? '/spool' : `/spool?${q}`),
        request<{ jobs: SpoolJob[] }>('GET', `/spool/jobs${view === 'jobs' && older ? '?older=true' : ''}`),
      ]);
      if (n !== loads) return;
      spool = got;
      jobs = gotJobs.jobs ?? [];
      selected = selected.filter((id) => spool.entries.some((e) => e.id === id && !e.job));
    } catch (err) {
      if (n === loads) error = messageOf(err);
    } finally {
      if (n === loads) loading = false;
    }
  }

  let poll: ReturnType<typeof setInterval> | undefined;
  $effect(() => {
    if (!busy) return;
    poll = setInterval(() => void load(), 2000);
    return () => clearInterval(poll);
  });
  onMount(load);
  onDestroy(() => clearInterval(poll));

  function show(next: View) {
    if (view === next) return;
    view = next;
    selected = [];
    error = status = '';
    void load();
  }

  function toggleGroup(entries: SpoolEntry[]) {
    const ids = entries.filter((e) => !e.job).map((e) => e.id);
    const all = ids.every((id) => selected.includes(id));
    selected = all ? selected.filter((id) => !ids.includes(id)) : [...new Set([...selected, ...ids])];
  }

  function toggleAll() {
    selected = allSelected ? [] : selectable.map((e) => e.id);
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

  // Printing makes jobs the server sends in the background, each printer
  // one at a time and printers side by side.
  async function print(entryIds?: string[]) {
    error = status = '';
    try {
      const r = await request<SpoolPrintResult>('POST', '/spool/print', entryIds ? { entry_ids: entryIds } : {});
      status = printedMessage(r);
      selected = [];
      await load();
    } catch (err) {
      error = messageOf(err);
    }
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

  function cancel(j: SpoolJob) {
    return act(`Canceled; its ${count(j.labels)} wait in the spool again.`, () => request('POST', `/spool/jobs/${encodeURIComponent(j.id)}/cancel`));
  }

  async function resend(j: SpoolJob) {
    error = status = '';
    const to = resendTo[j.id] ?? '';
    try {
      const r = await request<SpoolAddResult & { jobs: SpoolJob[] }>('POST', `/spool/jobs/${encodeURIComponent(j.id)}/resend`, to ? { destination: to } : {});
      status = r.jobs.length ? `${addedMessage(r).replace(/\.$/, '')}, and sent to print.` : addedMessage(r);
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
  const jobOf = (e: SpoolEntry) => (e.job?.state === 'sending' ? `Sending to ${e.job.target}` : `Waiting for ${e.job?.target}`);
</script>

<div class="mb-4 flex flex-wrap items-center justify-between gap-2">
  <h1 class="text-2xl font-bold">Spool</h1>
  <div class="flex gap-1" role="group" aria-label="Show">
    {#each [['waiting', 'Waiting'], ['jobs', 'Jobs'], ['printed', 'Printed']] as [key, label] (key)}
      <button type="button" class={view === key ? 'btn-primary' : 'btn-secondary'} aria-pressed={view === key} onclick={() => show(key as View)}>{label}</button>
    {/each}
  </div>
</div>

<div class="mb-4 flex flex-wrap items-center gap-4 text-sm">
  {#if view !== 'jobs'}
    <label class="flex items-center gap-2"><input type="checkbox" bind:checked={mine} onchange={load} /> Only labels I added</label>
  {/if}
  {#if view !== 'waiting'}
    <label class="flex items-center gap-2"><input type="checkbox" bind:checked={older} onchange={load} /> Include those over 30 days old</label>
  {/if}
  {#if view === 'waiting' && readyToPrint.length}
    <button type="button" class="btn-primary" onclick={() => print()}>Print everything ready ({count(readyToPrint.reduce((n, e) => n + e.copies, 0))})</button>
  {/if}
</div>

<ErrorMessage {error} />
{#if status}<p class="mb-2 text-sm text-green-700 dark:text-green-400" role="status">{status}</p>{/if}

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading the spool…</p>
{:else if view === 'jobs'}
  {#if jobs.length === 0}
    <p class="text-sm text-zinc-600 dark:text-zinc-400">No print jobs{older ? '' : ' in the last 30 days'}.</p>
  {:else}
    <div class="overflow-x-auto">
      <table class="w-full text-left text-sm">
        <thead class="border-b border-zinc-300 dark:border-zinc-700">
          <tr>
            <th scope="col" class="py-2 pr-4">When</th>
            <th scope="col" class="py-2 pr-4">Printer</th>
            <th scope="col" class="py-2 pr-4">Template</th>
            <th scope="col" class="py-2 pr-4">Labels</th>
            <th scope="col" class="py-2 pr-4">State</th>
            <th scope="col" class="py-2"><span class="sr-only">Actions</span></th>
          </tr>
        </thead>
        <tbody>
          {#each jobs as j (j.id)}
            <tr class="border-b border-zinc-200 align-top dark:border-zinc-800">
              <td class="py-2 pr-4 whitespace-nowrap">{when(j.created_at)}<span class="block text-xs text-zinc-600 dark:text-zinc-400">{actorName(j.created_by, session.user?.id)}</span></td>
              <td class="py-2 pr-4">
                {j.target}
                {#if j.members?.length}
                  <ul class="text-xs text-zinc-600 dark:text-zinc-400" aria-label="Its printers">
                    {#each j.members as m (m.printer_id)}
                      <li>{m.printer}: {m.dropped ? 'dropped out' : m.sending ? `sending, ${m.printed} printed` : `${m.printed} printed`}</li>
                    {/each}
                  </ul>
                {/if}
              </td>
              <td class="py-2 pr-4">{j.template_name}</td>
              <td class="py-2 pr-4 whitespace-nowrap">{j.state === 'sending' ? `${j.printed} of ${j.labels}` : j.labels}</td>
              <td class="py-2 pr-4">
                <span class={j.state === 'failed' ? 'font-medium text-red-700 dark:text-red-400' : ''}>{jobStateName(j)}</span>
                {#if j.error}<span class="block text-xs">{j.error}</span>{/if}
              </td>
              <td class="py-2">
                {#if j.state === 'waiting'}
                  <button type="button" class="btn-secondary" aria-label="Cancel the job printing {count(j.labels)} on {j.target}" onclick={() => cancel(j)}>Cancel</button>
                {:else if j.printed > 0 && !jobActive(j.state)}
                  <div class="flex flex-wrap items-center gap-2">
                    <label class="sr-only" for="resend-{j.id}">Send the labels of the {when(j.created_at)} job on {j.target} again to</label>
                    <select id="resend-{j.id}" class="input" bind:value={resendTo[j.id]}>
                      <option value="">{j.target}</option>
                      {#each printersFor(spool, j.template_id).filter((p) => p.id !== j.printer_id) as p (p.id)}<option value="printer:{p.id}">{p.name}</option>{/each}
                      {#each poolsFor(spool, j.template_id).filter((p) => p.id !== j.pool_id) as p (p.id)}<option value="pool:{p.id}">{p.name} (pool)</option>{/each}
                      {#if !j.download}<option value="download">Download</option>{/if}
                    </select>
                    <button type="button" class="btn-secondary" onclick={() => resend(j)}>Send again</button>
                  </div>
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
{:else if spool.entries.length === 0}
  <p class="text-sm text-zinc-600 dark:text-zinc-400">
    {#if view === 'printed'}Nothing printed{older ? '' : ' in the last 30 days'}.{:else}The spool is empty. Add labels from the asset list, a category, or an asset's page.{/if}
  </p>
{:else}
  <p class="sr-only" role="status">{count(spool.entries.length)}, {selected.length} selected.</p>
  <p class="mb-2 text-sm"><label class="flex items-center gap-2"><input type="checkbox" checked={allSelected} onchange={toggleAll} disabled={!selectable.length} /> Select all {count(selectable.length)}</label></p>

  {#if selected.length}
    <section class="sticky top-0 z-10 mb-4 rounded border border-zinc-300 bg-white p-3 dark:border-zinc-700 dark:bg-zinc-900" aria-label="Change the selected labels">
      <p class="mb-2 text-sm font-medium">{count(selected.length)} selected</p>
      {#if view === 'printed'}
        <button type="button" class="btn-primary" onclick={printAgain}>Print again</button>
      {:else}
        <div class="flex flex-wrap items-end gap-3">
          <button type="button" class="btn-primary" onclick={() => print(selected)}>Print</button>
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
                {#if destinationChoices.length}
                  <optgroup label="Printers">
                    {#each destinationChoices as p (p.id)}<option value="printer:{p.id}">{p.name}</option>{/each}
                  </optgroup>
                {/if}
                {#if poolChoices.length}
                  <optgroup label="Pools">
                    {#each poolChoices as p (p.id)}<option value="pool:{p.id}">{p.name}</option>{/each}
                  </optgroup>
                {/if}
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

  {#if view === 'printed'}
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
              <td class="py-2">{e.printed_by ? actorName(e.printed_by, session.user?.id) : ''}, {e.printed_at ? when(e.printed_at) : ''}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {:else}
    {#each groups as g, gi (g.key)}
      {@const ready = g.entries.filter((e) => e.ready)}
      {@const readyLabels = ready.reduce((n, e) => n + e.copies, 0)}
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
          {:else if printing(g.destination) && ready.length}
            <button type="button" class="btn-secondary" onclick={() => print(ready.map((e) => e.id))}>Print {count(readyLabels)}</button>
          {/if}
        </div>
        <div class="overflow-x-auto">
          <table class="w-full text-left text-sm">
            <thead class="border-b border-zinc-300 dark:border-zinc-700">
              <tr>
                <th scope="col" class="w-8 py-2 pr-2">
                  <input
                    type="checkbox"
                    aria-label="Select every label for {destinationName(spool, g.destination)}, {templateName(spool, g.templateId)}"
                    checked={g.entries.some((e) => !e.job) && g.entries.every((e) => e.job || selected.includes(e.id))}
                    disabled={g.entries.every((e) => e.job)}
                    onchange={() => toggleGroup(g.entries)}
                  />
                </th>
                <th scope="col" class="py-2 pr-4">Asset</th>
                <th scope="col" class="py-2 pr-4">Copies</th>
                <th scope="col" class="py-2 pr-4">Added</th>
                <th scope="col" class="py-2">State</th>
              </tr>
            </thead>
            <tbody>
              {#each g.entries as e (e.id)}
                <tr class="border-b border-zinc-200 dark:border-zinc-800">
                  <td class="py-2 pr-2"><input type="checkbox" aria-label="Select {e.asset_name}" value={e.id} bind:group={selected} disabled={!!e.job} /></td>
                  <td class="py-2 pr-4 font-medium"><a class="hover:underline" href="/assets/{encodeURIComponent(e.asset_id)}">{e.asset_name}</a></td>
                  <td class="py-2 pr-4">{e.copies}</td>
                  <td class="py-2 pr-4">{actorName(e.added_by, session.user?.id)}, {when(e.added_at)}</td>
                  <td class="py-2">
                    {#if e.job}
                      <span class="text-zinc-700 dark:text-zinc-300">{jobOf(e)}…</span>
                    {:else if e.problems.length}
                      <ul class="text-amber-800 dark:text-amber-300">
                        {#each e.problems as p, i (i)}<li><span aria-hidden="true">⚠</span> {p}</li>{/each}
                      </ul>
                    {:else}
                      <span class="text-zinc-600 dark:text-zinc-400">Ready</span>
                      <FitWarnings warnings={entryWarnings(spool, e)} label="Before printing {e.asset_name}" />
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

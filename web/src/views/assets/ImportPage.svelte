<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import { MAX_FILES, targetOptions } from '../../lib/importing';
  import { ImportEntry } from '../../lib/importset.svelte';
  import type { Field, GeneratorInfo } from '../../lib/models';
  import { session } from '../../lib/session.svelte';
  import { addedMessage, addToSpool, queueNewLabels, rememberQueueNewLabels } from '../../lib/spool';
  import ErrorMessage from '../../components/ErrorMessage.svelte';
  import ImportFile from './ImportFile.svelte';

  let { params: _params }: { params: Record<string, string> } = $props();

  let fields = $state<Field[]>([]);
  let generators = $state<GeneratorInfo[]>([]);
  let entries = $state<ImportEntry[]>([]);
  let running = $state(false);
  let error = $state('');
  // What the last run did, in a sentence, when there were several files.
  let outcome = $state('');
  let dragging = $state(false);
  const userId = $derived(session.user?.id ?? '');
  // New assets' labels join the print queue (docs/printing.md), as each person last chose.
  const canSpool = session.can('labels.print');
  let queueLabels = $state(canSpool && queueNewLabels(session.user?.id ?? ''));
  let spooled = $state('');

  const options = $derived(targetOptions(fields, generators));
  // The files still to import, in order.
  const pending = $derived(entries.filter((e) => !e.result));
  const busy = $derived(running || entries.some((e) => e.busy));
  // The first file must be ready; a later one may wait on assets an earlier one creates.
  const ready = $derived(pending.length > 0 && pending.every((e, i) => e.ready || (i > 0 && e.waiting)));
  const creates = $derived(pending.reduce((n, e) => n + (e.plan?.create ?? 0), 0));
  const label = $derived.by(() => {
    if (pending.length > 1) return `Import ${pending.length} files in order`;
    const p = pending[0]?.plan;
    if (!p) return 'Import';
    return p.create + p.update ? `Import ${p.create + p.update} ${p.create + p.update === 1 ? 'asset' : 'assets'}` : 'Import the links';
  });

  onMount(async () => {
    try {
      const [f, g] = await Promise.all([
        request<{ fields: Field[] }>('GET', '/fields'),
        request<{ generators: GeneratorInfo[] }>('GET', '/generators'),
      ]);
      fields = f.fields ?? [];
      generators = g.generators ?? [];
    } catch (err) {
      error = messageOf(err);
    }
  });

  async function add(files: File[]) {
    error = '';
    outcome = '';
    spooled = '';
    // A finished run's files make way for the next.
    if (entries.length && !pending.length) entries = [];
    const room = MAX_FILES - entries.length;
    if (files.length > room) error = `At most ${MAX_FILES} files are imported together; ${files.length - room === 1 ? 'one was' : `${files.length - room} were`} left out.`;
    const added = files.slice(0, Math.max(room, 0)).map((f) => new ImportEntry(f));
    entries = [...entries, ...added];
    // One at a time, in order, so a file can start from the mapping of the one before.
    for (const e of added) await e.start(userId);
  }

  async function choose(event: Event) {
    const input = event.currentTarget as HTMLInputElement;
    const files = [...(input.files ?? [])];
    input.value = '';
    await add(files);
  }

  async function drop(event: DragEvent) {
    event.preventDefault();
    dragging = false;
    if (busy) return;
    await add([...(event.dataTransfer?.files ?? [])].filter((f) => f.name.toLowerCase().endsWith('.csv') || f.type === 'text/csv'));
  }

  function move(entry: ImportEntry, by: number) {
    const from = entries.indexOf(entry);
    const to = from + by;
    if (to < 0 || to >= entries.length) return;
    const next = [...entries];
    next.splice(from, 1);
    next.splice(to, 0, entry);
    entries = next;
  }

  function remove(entry: ImportEntry) {
    entries = entries.filter((e) => e !== entry);
  }

  const names = (list: ImportEntry[]) => list.map((e) => e.file.name).join(', ');

  async function runImport() {
    if (!ready || busy) return;
    running = true;
    error = '';
    outcome = '';
    spooled = '';
    const queue = [...pending];
    for (const e of queue) e.skipped = false;
    const done: ImportEntry[] = [];
    let failed: ImportEntry | null = null;
    try {
      for (const e of queue) {
        if (!(await e.run(userId))) {
          failed = e;
          break;
        }
        done.push(e);
      }
      const rest = failed ? queue.slice(queue.indexOf(failed) + 1) : [];
      for (const e of rest) e.skipped = true;
      if (queue.length > 1) {
        outcome = failed
          ? `${done.length ? `Imported ${names(done)}. ` : 'Nothing was imported. '}${failed.file.name} was refused${rest.length ? `, so ${names(rest)} ${rest.length === 1 ? 'was' : 'were'} not imported` : ''}.`
          : `Imported all ${done.length} files.`;
      }
      if (canSpool) {
        rememberQueueNewLabels(userId, queueLabels);
        // The import is done either way; a spool that cannot take the labels
        // says so here, and they can be added from the asset list.
        const created = done.flatMap((e) => e.result?.created_ids ?? []);
        if (queueLabels && created.length) {
          spooled = await addToSpool({ asset_ids: created }).then(addedMessage, (err) => `The labels were not added to the print queue: ${messageOf(err)}`);
        }
      }
      // What was imported may resolve what the files after the refused one name.
      if (done.length) for (const e of rest) await e.preview(true, userId);
    } finally {
      running = false;
    }
  }
</script>

<h1 class="mb-2 text-2xl font-bold">Import assets</h1>
<p class="mb-4 max-w-prose text-sm text-zinc-600 dark:text-zinc-400">
  A CSV file with one asset per row. Rows with an ID update that asset, and blank cells leave its values alone; rows
  without one create new assets. Categories are paths such as <span class="font-mono">Tools &gt; Drills</span>,
  separated by semicolons, and must already exist. Exporting from the asset list gives a file in this format. Nothing
  is written until you import, and then either every row of a file is imported or none is.
</p>
<p class="mb-4 max-w-prose text-sm text-zinc-600 dark:text-zinc-400">
  Several files are imported in the order listed, each on its own. A file that links to assets another creates goes after
  it. If one is refused, the files after it are not imported.
</p>

<div
  class="mb-4 rounded border-2 border-dashed p-4 {dragging ? 'border-sky-500 bg-sky-50 dark:bg-sky-950' : 'border-zinc-300 dark:border-zinc-700'}"
  role="group"
  aria-label="Files to import"
  ondragover={(e) => {
    e.preventDefault();
    dragging = true;
  }}
  ondragleave={() => (dragging = false)}
  ondrop={drop}
>
  <label class="label" for="import-file">CSV file</label>
  <input id="import-file" type="file" accept=".csv,text/csv" multiple class="block min-h-11 py-2 text-sm" onchange={choose} disabled={busy} />
  <p class="text-xs text-zinc-600 dark:text-zinc-400">Choose or drop up to {MAX_FILES} files.</p>
</div>

<ErrorMessage {error} />

{#each entries as entry, i (entry.id)}
  <ImportFile
    {entry}
    position={i + 1}
    count={entries.length}
    earlier={entries.slice(0, i).some((e) => !e.result)}
    {options}
    {userId}
    disabled={busy}
    onmove={(by) => move(entry, by)}
    onremove={() => remove(entry)}
  />
{/each}

{#if outcome}<p class="mb-4 font-medium" role="status">{outcome}</p>{/if}

{#if pending.length && pending.every((e) => e.checked)}
  {#if canSpool && creates > 0}
    <label class="mb-3 flex items-center gap-2 text-sm"><input type="checkbox" bind:checked={queueLabels} /> Add the new assets’ labels to the print queue</label>
  {/if}
  <button type="button" class="btn-primary mb-4" onclick={runImport} disabled={!ready || busy}>
    {running ? 'Importing…' : label}
  </button>
{/if}

{#if spooled}<p class="mt-1 text-sm">{spooled} <a class="underline" href="/spool">Open the print queue</a></p>{/if}
{#if entries.length && !pending.length}
  <p class="mt-1 text-sm"><a class="underline" href="/assets">Back to the assets</a></p>
{/if}

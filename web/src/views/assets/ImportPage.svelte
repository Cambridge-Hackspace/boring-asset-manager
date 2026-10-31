<script lang="ts">
  import { onMount } from 'svelte';
  import { ApiError, request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import { IGNORE, describeLinkChange, describeProblem, duplicateTargets, importForm, linkCounts, summarize, targetOptions } from '../../lib/importing';
  import type { Field, GeneratorInfo, ImportPlan, ImportResult } from '../../lib/models';
  import { session } from '../../lib/session.svelte';
  import { addedMessage, addToSpool, queueNewLabels, rememberQueueNewLabels } from '../../lib/spool';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  let { params: _params }: { params: Record<string, string> } = $props();

  let fields = $state<Field[]>([]);
  let generators = $state<GeneratorInfo[]>([]);
  let file = $state<File | null>(null);
  let plan = $state<ImportPlan | null>(null);
  let mapping = $state<string[]>([]);
  // The mapping the plan was checked with.
  let checkedWith = $state('');
  let busy = $state(false);
  let error = $state('');
  let result = $state<ImportResult | null>(null);
  // New assets' labels join the print queue (README §6.2), as each person last chose.
  const canSpool = session.can('labels.print');
  let queueLabels = $state(canSpool && queueNewLabels(session.user?.id ?? ''));
  let spooled = $state('');

  const options = $derived(targetOptions(fields, generators));
  const groups = $derived(['Asset', 'Stock', 'Codes', 'Fields'].map((g) => ({ name: g, options: options.filter((o) => o.group === g) })));
  const duplicates = $derived(duplicateTargets(mapping));
  // Removing links no row states, when the file has a links column.
  let removeLinks = $state(false);
  const hasLinks = $derived(mapping.includes('links'));
  const checkKey = $derived(JSON.stringify([mapping, hasLinks && removeLinks]));
  const checked = $derived(plan !== null && checkKey === checkedWith);
  const linkChanges = $derived(plan ? plan.links_added + plan.links_moved + plan.links_removed : 0);
  const ready = $derived(checked && plan !== null && plan.problem_count === 0 && plan.create + plan.update + linkChanges > 0);

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

  async function preview(withMapping: boolean) {
    if (!file) return;
    busy = true;
    error = '';
    try {
      const next = await request<ImportPlan>('POST', '/assets/import/preview', importForm(file, withMapping ? mapping : undefined, hasLinks && removeLinks));
      plan = next;
      mapping = next.columns.map((c) => c.target);
      checkedWith = checkKey;
    } catch (err) {
      plan = null;
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }

  async function choose(event: Event) {
    const input = event.currentTarget as HTMLInputElement;
    file = input.files?.[0] ?? null;
    plan = null;
    result = null;
    mapping = [];
    await preview(false);
  }

  async function runImport() {
    if (!file || !ready) return;
    busy = true;
    error = '';
    try {
      result = await request<ImportResult>('POST', '/assets/import', importForm(file, mapping, hasLinks && removeLinks));
      plan = null;
      file = null;
      spooled = '';
      if (canSpool) {
        rememberQueueNewLabels(session.user?.id ?? '', queueLabels);
        // The import is done either way; a spool that cannot take the labels
        // says so here, and they can be added from the asset list.
        if (queueLabels && result.created_ids?.length) {
          spooled = await addToSpool({ asset_ids: result.created_ids }).then(addedMessage, (err) => `The labels were not added to the print queue: ${messageOf(err)}`);
        }
      }
    } catch (err) {
      // The server checks again; if something changed, show its findings.
      if (err instanceof ApiError && err.status === 422) {
        const details = (err.body as { details?: ImportPlan } | undefined)?.details;
        if (details) plan = details;
      }
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }
</script>

<h1 class="mb-2 text-2xl font-bold">Import assets</h1>
<p class="mb-4 max-w-prose text-sm text-zinc-600 dark:text-zinc-400">
  A CSV file with one asset per row. Rows with an ID update that asset, and blank cells leave its values alone; rows
  without one create new assets. Categories are paths such as <span class="font-mono">Tools &gt; Drills</span>,
  separated by semicolons, and must already exist. Exporting from the asset list gives a file in this format. Nothing
  is written until you import, and then either every row is imported or none is.
</p>

<div class="mb-4">
  <label class="label" for="import-file">CSV file</label>
  <input id="import-file" type="file" accept=".csv,text/csv" class="block min-h-11 py-2 text-sm" onchange={choose} disabled={busy} />
</div>

<ErrorMessage {error} />

{#if result}
  <div class="card mb-4" role="status">
    <p class="font-medium">
      Imported: {result.created} new, {result.updated} updated, {result.unchanged} unchanged{linkCounts(result.links_added, result.links_moved, result.links_removed) ? `; ${linkCounts(result.links_added, result.links_moved, result.links_removed)}` : ''}.
    </p>
    {#if spooled}<p class="mt-1 text-sm">{spooled} <a class="underline" href="/spool">Open the print queue</a></p>{/if}
    <p class="mt-1 text-sm"><a class="underline" href="/assets">Back to the assets</a></p>
  </div>
{/if}

{#if plan}
  <h2 class="mb-2 text-lg font-semibold">Columns</h2>
  <div class="mb-4 overflow-x-auto">
    <table class="w-full text-left text-sm">
      <thead class="border-b border-zinc-300 dark:border-zinc-700">
        <tr>
          <th scope="col" class="py-2 pr-4">Column</th>
          <th scope="col" class="py-2 pr-4">First value</th>
          <th scope="col" class="py-2">Goes to</th>
        </tr>
      </thead>
      <tbody>
        {#each plan.columns as column, i (i)}
          <tr class="border-b border-zinc-200 dark:border-zinc-800">
            <th scope="row" class="py-2 pr-4 font-medium">{column.header || `Column ${i + 1}`}</th>
            <td class="max-w-48 truncate py-2 pr-4 text-zinc-600 dark:text-zinc-400" title={column.sample}>{column.sample}</td>
            <td class="py-2">
              <select
                class="input"
                aria-label="Where {column.header || `column ${i + 1}`} goes"
                aria-invalid={!mapping[i] || duplicates.has(mapping[i]!)}
                bind:value={mapping[i]}
              >
                <option value="">Choose…</option>
                <option value={IGNORE}>Ignore this column</option>
                {#each groups as group (group.name)}
                  {#if group.options.length}
                    <optgroup label={group.name}>
                      {#each group.options as o (o.value)}<option value={o.value}>{o.label}</option>{/each}
                    </optgroup>
                  {/if}
                {/each}
              </select>
              {#if mapping[i] && duplicates.has(mapping[i]!)}
                <p class="error-text">Another column goes here too.</p>
              {/if}
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>

  {#if hasLinks}
    <div class="mb-4 max-w-prose">
      <label class="flex items-center gap-2 text-sm">
        <input type="checkbox" bind:checked={removeLinks} aria-describedby="remove-links-help" /> Also remove links that no row states
      </label>
      <p id="remove-links-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">
        Without this, an import only adds links and moves things between containers. With it, links of the file's assets that
        no row mentions are removed, so an exported file you edit imports back exactly.
      </p>
    </div>
  {/if}

  <h2 class="mb-2 text-lg font-semibold">Check</h2>
  {#if !checked}
    <p class="mb-2 text-sm">The mapping has changed since the last check.</p>
    <button type="button" class="btn-secondary mb-4" onclick={() => preview(true)} disabled={busy}>
      {busy ? 'Checking…' : 'Check again'}
    </button>
  {:else}
    <p class="mb-2 text-sm" role="status">{summarize(plan)}</p>
    {#if plan.unknown_categories.length}
      <p class="error-text mb-2">
        These categories do not exist yet; create them first, or fix the file: {plan.unknown_categories.join(', ')}.
      </p>
    {/if}
    {#if plan.problem_count}
      <p class="mb-1 text-sm font-medium" id="import-problems">
        {plan.problem_count === 1 ? '1 problem' : `${plan.problem_count} problems`} to fix before importing:
      </p>
      <ul class="mb-4 list-disc space-y-1 pl-5 text-sm" aria-labelledby="import-problems">
        {#each plan.problems as p, i (i)}<li>{describeProblem(p)}</li>{/each}
        {#if plan.problem_count > plan.problems.length}
          <li>and {plan.problem_count - plan.problems.length} more.</li>
        {/if}
      </ul>
    {/if}
    {#if plan.link_changes.length}
      <details class="mb-4 text-sm">
        <summary class="cursor-pointer">Link changes</summary>
        <ul class="mt-1 list-disc space-y-1 pl-5">
          {#each plan.link_changes as c, i (i)}<li>{describeLinkChange(c)}</li>{/each}
          {#if linkChanges > plan.link_changes.length}<li>and {linkChanges - plan.link_changes.length} more.</li>{/if}
        </ul>
      </details>
    {/if}
    {#if canSpool && plan.create > 0}
      <label class="mb-3 flex items-center gap-2 text-sm"><input type="checkbox" bind:checked={queueLabels} /> Add the new assets’ labels to the print queue</label>
    {/if}
    <button type="button" class="btn-primary" onclick={runImport} disabled={!ready || busy}>
      {busy ? 'Importing…' : plan.create + plan.update ? `Import ${plan.create + plan.update} ${plan.create + plan.update === 1 ? 'asset' : 'assets'}` : 'Import the links'}
    </button>
  {/if}
{/if}

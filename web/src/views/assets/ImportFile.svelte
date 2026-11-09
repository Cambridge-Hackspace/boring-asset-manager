<script lang="ts">
  import { IGNORE, describeLinkChange, describeProblem, duplicateTargets, linkCounts, summarize, type TargetOption } from '../../lib/importing';
  import type { ImportEntry } from '../../lib/importset.svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  let {
    entry,
    position,
    count,
    earlier,
    options,
    userId,
    disabled,
    onmove,
    onremove,
  }: {
    entry: ImportEntry;
    /** Its place in the set, from 1. */
    position: number;
    count: number;
    /** Whether a file before it is still to be imported. */
    earlier: boolean;
    options: TargetOption[];
    userId: string;
    disabled: boolean;
    onmove: (by: number) => void;
    onremove: () => void;
  } = $props();

  const groups = $derived(['Asset', 'Stock', 'Codes', 'Fields'].map((g) => ({ name: g, options: options.filter((o) => o.group === g) })));
  const duplicates = $derived(duplicateTargets(entry.mapping));
  const plan = $derived(entry.plan);
  const headingId = $derived(`import-file-${entry.id}`);
  const several = $derived(count > 1);

  // Adding the names matching no one as people without accounts
  // (docs/csv.md), then checking again.
  let adding = $state(false);
  let addError = $state('');
  async function addPeople() {
    if (!plan?.new_people?.length) return;
    adding = true;
    addError = '';
    try {
      for (const name of plan.new_people) await request('POST', '/people', { name });
      await entry.preview(true, userId);
    } catch (err) {
      addError = messageOf(err);
    } finally {
      adding = false;
    }
  }
</script>

<section class={several ? 'card mb-4' : 'mb-4'} aria-labelledby={headingId}>
  <div class="mb-2 flex flex-wrap items-center gap-2">
    <h2 id={headingId} class="mr-auto text-lg font-semibold break-all">
      {#if several}<span class="text-zinc-500">{position}.</span>{/if}
      {entry.name}
    </h2>
    {#if several && !entry.result}
      <button type="button" class="btn-secondary" aria-label="Move {entry.name} up" onclick={() => onmove(-1)} disabled={disabled || position === 1}>Up</button>
      <button type="button" class="btn-secondary" aria-label="Move {entry.name} down" onclick={() => onmove(1)} disabled={disabled || position === count}>Down</button>
    {/if}
    {#if !entry.result}
      <button type="button" class="btn-secondary" aria-label="Remove {entry.name}" onclick={onremove} disabled={disabled}>Remove</button>
    {/if}
  </div>

  <ErrorMessage error={entry.error} />

  {#if entry.result}
    <p class="text-sm" role="status">
      Imported: {entry.result.created} new, {entry.result.updated} updated, {entry.result.unchanged} unchanged{linkCounts(entry.result.links_added, entry.result.links_moved, entry.result.links_removed) ? `; ${linkCounts(entry.result.links_added, entry.result.links_moved, entry.result.links_removed)}` : ''}.
    </p>
  {:else if entry.skipped}
    <p class="mb-2 text-sm" role="status">Not imported, because a file before it failed.</p>
  {/if}

  {#if plan && !entry.result}
    {#if entry.remembered}
      <p class="mb-2 text-sm">
        Started from the mapping you last used for these columns.
        <button type="button" class="underline" onclick={() => entry.useSuggested(userId)} disabled={disabled}>Use the suggested mapping</button>
      </p>
    {/if}
    <h3 class="mb-2 font-semibold">Columns</h3>
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
                  aria-invalid={!entry.mapping[i] || duplicates.has(entry.mapping[i]!)}
                  bind:value={entry.mapping[i]}
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
                {#if entry.mapping[i] && duplicates.has(entry.mapping[i]!)}
                  <p class="error-text">Another column goes here too.</p>
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>

    {#if entry.hasLinks}
      <div class="mb-4 max-w-prose">
        <label class="flex items-center gap-2 text-sm">
          <input type="checkbox" bind:checked={entry.removeLinks} aria-describedby="remove-links-help-{entry.id}" /> Also remove links that no row states
        </label>
        <p id="remove-links-help-{entry.id}" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">
          Without this, an import only adds links and moves things between containers. With it, links of the file's assets that
          no row mentions are removed, so an exported file you edit imports back exactly.
        </p>
      </div>
    {/if}

    <h3 class="mb-2 font-semibold">Check</h3>
    {#if !entry.checked}
      <p class="mb-2 text-sm">The mapping has changed since the last check.</p>
      <button type="button" class="btn-secondary" onclick={() => entry.preview(true, userId)} disabled={disabled || entry.busy}>
        {entry.busy ? 'Checking…' : 'Check again'}
      </button>
    {:else}
      <p class="mb-2 text-sm" role="status">{summarize(plan)}</p>
      {#if plan.new_people?.length}
        <div class="mb-2 text-sm">
          <p>No one is named {plan.new_people.map((n) => `“${n}”`).join(', ')}.</p>
          {#if plan.can_add_people}
            <button type="button" class="btn-secondary mt-1" onclick={addPeople} disabled={disabled || adding}>
              {adding ? 'Adding…' : `Add ${plan.new_people.length === 1 ? 'them' : `these ${plan.new_people.length}`} as people without accounts`}
            </button>
          {:else}
            <p class="text-zinc-600 dark:text-zinc-400">Fix the file, or ask someone who may add people to add them.</p>
          {/if}
          <ErrorMessage error={addError} />
        </div>
      {/if}
            {#if plan.unknown_categories.length}
        <p class="error-text mb-2">
          These categories do not exist yet; create them first, or fix the file: {plan.unknown_categories.join(', ')}.
        </p>
      {/if}
      {#if plan.problem_count}
        <p class="mb-1 text-sm font-medium" id="import-problems-{entry.id}">
          {plan.problem_count === 1 ? '1 problem' : `${plan.problem_count} problems`} to fix before importing:
        </p>
        <ul class="mb-2 list-disc space-y-1 pl-5 text-sm" aria-labelledby="import-problems-{entry.id}">
          {#each plan.problems as p, i (i)}<li>{describeProblem(p)}</li>{/each}
          {#if plan.problem_count > plan.problems.length}
            <li>and {plan.problem_count - plan.problems.length} more.</li>
          {/if}
        </ul>
      {/if}
      {#if plan.unresolved}
        <p class="mb-2 max-w-prose text-sm">
          {#if entry.waiting && earlier}
            {plan.unresolved === 1 ? 'A link names an asset that does not' : `${plan.unresolved} links name assets that do not`} exist yet. If a file
            before this one creates them, this one can be imported after it: each file is checked again as it is imported.
          {:else}
            {plan.unresolved === 1 ? 'A link names an asset that does not' : `${plan.unresolved} links name assets that do not`} exist yet. If another
            file creates them, add it and put it before this one.
          {/if}
        </p>
      {/if}
      {#if plan.link_changes.length}
        <details class="mb-2 text-sm">
          <summary class="cursor-pointer">Link changes</summary>
          <ul class="mt-1 list-disc space-y-1 pl-5">
            {#each plan.link_changes as c, i (i)}<li>{describeLinkChange(c)}</li>{/each}
            {#if entry.linkChanges > plan.link_changes.length}<li>and {entry.linkChanges - plan.link_changes.length} more.</li>{/if}
          </ul>
        </details>
      {/if}
    {/if}
  {/if}
</section>

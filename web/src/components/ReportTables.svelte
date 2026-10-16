<script lang="ts">
  import type { ReportResult } from '../lib/models';

  // A report's sections, as tables. csvHref, if given, links each table to
  // its CSV download.
  let { result, csvHref }: { result: ReportResult; csvHref?: (section: number) => string } = $props();
</script>

<p class="mb-4 text-sm text-zinc-600 dark:text-zinc-400">{result.assets === 1 ? '1 asset' : `${result.assets} assets`} match.</p>

{#each result.sections as section, s (s)}
  <section class="mb-8 break-inside-avoid" aria-label={section.title || `Table ${s + 1}`}>
    <div class="mb-2 flex flex-wrap items-center justify-between gap-2">
      {#if section.title}<h2 class="text-lg font-semibold">{section.title}</h2>{/if}
      {#if csvHref}
        <a class="btn-secondary print:hidden" href={csvHref(s + 1)} download>Download CSV<span class="sr-only"> of {section.title || `table ${s + 1}`}</span></a>
      {/if}
    </div>
    {#each section.groups as group, g (g)}
      {#if group.title}
        <h3 class="mt-4 mb-1 font-medium">
          {group.title}
          {#if group.running_low}<span class="ml-1 rounded bg-amber-100 px-1.5 py-0.5 text-xs font-medium text-amber-900 dark:bg-amber-900 dark:text-amber-100">Running low</span>{/if}
        </h3>
      {/if}
      {#if group.rows.length === 0}
        <p class="text-sm text-zinc-600 dark:text-zinc-400">Nothing to show.</p>
      {:else}
        <div class="overflow-x-auto">
          <table class="w-full text-left text-sm">
            <thead class="border-b border-zinc-300 dark:border-zinc-700">
              <tr>{#each section.columns as heading, c (c)}<th scope="col" class="py-2 pr-4">{heading}</th>{/each}</tr>
            </thead>
            <tbody>
              {#each group.rows as row, r (r)}
                <tr class="border-b border-zinc-200 dark:border-zinc-800">
                  {#each row.cells as cell, c (c)}
                    <td class="py-1.5 pr-4">
                      {#if c === 0 && cell.trim()}<a class="hover:underline" href="/assets/{encodeURIComponent(row.asset_id)}">{cell}</a>{:else}{cell}{/if}
                    </td>
                  {/each}
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    {/each}
  </section>
{/each}

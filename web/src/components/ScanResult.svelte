<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { ReportResult, ScanAction } from '../lib/models';
  import { scanActionLabel } from '../lib/scan';

  // A scan template laid out for one asset (README §6.4): its details, tables
  // of what it holds, and its quick actions. Without an actions snippet the
  // actions are listed, as an editor's preview shows them.
  let { result, actions, linkRows = false }: { result: ReportResult; actions?: Snippet<[ScanAction[]]>; linkRows?: boolean } = $props();

  type Section = ReportResult['sections'][number];

  // Scanned, rather than previewed, a section with nothing to show is left
  // off: details whose every value is empty, and contents with nothing in them.
  function shown(section: Section): boolean {
    if (!actions) return true;
    if (section.type === 'table') return (section.groups?.[0]?.rows ?? []).length > 0;
    if (section.type === 'details') return (section.groups?.[0]?.rows[0]?.cells ?? []).some((c) => c.trim());
    return true;
  }
</script>

<div class="space-y-6">
  {#each result.sections.filter(shown) as section, s (s)}
    <section aria-label={section.title || undefined}>
      {#if section.title}<h2 class="mb-2 text-lg font-semibold">{section.title}</h2>{/if}
      {#if section.type === 'details'}
        <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
          {#each section.columns ?? [] as heading, i (i)}
            {@const value = section.groups?.[0]?.rows[0]?.cells[i] ?? ''}
            {#if value.trim() || !actions}
              <dt class="font-medium">{heading}</dt>
              <dd>{value}</dd>
            {/if}
          {/each}
        </dl>
      {:else if section.type === 'table'}
        {@const rows = section.groups?.[0]?.rows ?? []}
        {#if rows.length === 0}
          {#if !actions}<p class="text-sm text-zinc-600 dark:text-zinc-400">Nothing.</p>{/if}
        {:else}
          <div class="overflow-x-auto">
            <table class="w-full text-left text-sm">
              <thead class="border-b border-zinc-300 dark:border-zinc-700">
                <tr>{#each section.columns ?? [] as heading, i (i)}<th scope="col" class="py-2 pr-4">{heading}</th>{/each}</tr>
              </thead>
              <tbody>
                {#each rows as row (row.asset_id)}
                  <tr class="border-b border-zinc-200 dark:border-zinc-800">
                    {#each row.cells as cell, i (i)}
                      <td class="py-2 pr-4">
                        {#if i === 0 && linkRows}<a class="underline" href="/assets/{encodeURIComponent(row.asset_id)}">{cell}</a>{:else}{cell}{/if}
                      </td>
                    {/each}
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {/if}
      {:else if section.type === 'actions'}
        {#if actions}
          {@render actions(section.actions ?? [])}
        {:else}
          <p class="text-sm">Actions: {(section.actions ?? []).map(scanActionLabel).join(', ')}</p>
        {/if}
      {/if}
    </section>
  {/each}
</div>

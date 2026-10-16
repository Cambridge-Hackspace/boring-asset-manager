<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import { UNCATEGORIZED, type Category, type Field, type GeneratorInfo, type Report, type ReportResult, type ReportSection, type ReportSpec } from '../../lib/models';
  import { navigate } from '../../lib/router.svelte';
  import { placeholders } from '../../lib/template';
  import { flattenTree } from '../../lib/tree';
  import ErrorMessage from '../../components/ErrorMessage.svelte';
  import ReportTables from '../../components/ReportTables.svelte';

  // /reports/new and /reports/:id/edit
  let { params }: { params: Record<string, string> } = $props();
  const id = untrack(() => params.id ?? '');

  let name = $state('');
  let spec = $state<ReportSpec>({ filter: {}, sections: [newTable()] });
  let categoryIds = $state<string[]>([]);
  let categories = $state<Category[]>([]);
  let fields = $state<Field[]>([]);
  let generators = $state<GeneratorInfo[]>([]);
  let preview = $state<ReportResult | null>(null);
  let loading = $state(true);
  let busy = $state(false);
  let error = $state('');

  const categoryChoices = $derived(flattenTree(categories.filter((c) => c.id !== UNCATEGORIZED)));
  const bindingChoices = $derived(placeholders(fields.map((f) => f.key), generators.map((g) => g.type)));

  function newTable(): ReportSection {
    return { type: 'table', title: '', columns: [{ heading: 'Asset', binding: '{{asset.name}}' }], sort_by: 0, descending: false, group_by: '' };
  }

  onMount(async () => {
    try {
      [categories, fields, generators] = await Promise.all([
        request<{ categories: Category[] }>('GET', '/categories').then((r) => r.categories ?? []),
        request<{ fields: Field[] }>('GET', '/fields').then((r) => r.fields ?? []),
        request<{ generators: GeneratorInfo[] }>('GET', '/generators').then((r) => r.generators ?? []),
      ]);
      if (id) {
        const r = (await request<{ report: Report }>('GET', `/reports/${encodeURIComponent(id)}`)).report;
        name = r.name;
        categoryIds = [...(r.spec.filter.category_ids ?? [])];
        spec = {
          filter: { ...r.spec.filter },
          sections: r.spec.sections.map((s) =>
            s.type === 'chart' ? { ...newChart(), ...s, columns: [] } : { ...newTable(), ...s, columns: s.columns.map((c) => ({ ...c })) },
          ),
        };
      }
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
  });

  // The spec as the API takes it: empty filters left out.
  function cleaned(): ReportSpec {
    const f = spec.filter;
    const filter: ReportSpec['filter'] = {};
    if (categoryIds.length) filter.category_ids = [...categoryIds];
    if (f.search?.trim()) filter.search = f.search.trim();
    if (f.archived) filter.archived = true;
    if (f.running_low) filter.running_low = true;
    if (f.loan) filter.loan = f.loan;
    if (f.condition) filter.condition = f.condition;
    const sections = spec.sections.map((s) => {
      if (s.type === 'chart') {
        const chart: ReportSection = { type: 'chart', columns: [], chart: s.chart, measure: s.measure };
        if (s.title?.trim()) chart.title = s.title.trim();
        if (s.chart === 'line') chart.period = s.period;
        else chart.group_by = s.group_by;
        // A chart has no columns; the API takes none.
        const { columns: _none, ...rest } = chart;
        return rest as ReportSection;
      }
      const out: ReportSection = { type: 'table', columns: s.columns.map((c) => ({ heading: c.heading, binding: c.binding })) };
      if (s.title?.trim()) out.title = s.title.trim();
      if (s.sort_by) out.sort_by = s.sort_by;
      if (s.descending) out.descending = true;
      if (s.group_by) out.group_by = s.group_by;
      return out;
    });
    return { filter, sections };
  }

  async function runPreview() {
    error = '';
    busy = true;
    try {
      preview = (await request<{ result: ReportResult }>('POST', '/reports/preview', { spec: cleaned() })).result;
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }

  async function save(event: SubmitEvent) {
    event.preventDefault();
    error = '';
    busy = true;
    try {
      const body = { name, spec: cleaned() };
      const saved = id
        ? await request<{ report: Report }>('PUT', `/reports/${encodeURIComponent(id)}`, body)
        : await request<{ report: Report }>('POST', '/reports', body);
      navigate(`/reports/${encodeURIComponent(saved.report.id)}`);
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }

  function newChart(): ReportSection {
    return { type: 'chart', title: '', columns: [], chart: 'bar', group_by: 'category', measure: 'assets', period: '' };
  }

  // Bars and donuts group and count; lines count history over time.
  function chartKindChanged(s: ReportSection) {
    if (s.chart === 'line') {
      if (!['used', 'restocked', 'loans'].includes(s.measure ?? '')) s.measure = 'used';
      s.period ||= 'month';
    } else {
      if (!['assets', 'quantity', 'loans'].includes(s.measure ?? '')) s.measure = 'assets';
      s.group_by ||= 'category';
    }
  }

  function addColumn(s: ReportSection) {
    s.columns.push({ heading: '', binding: '' });
  }

  function removeColumn(s: ReportSection, i: number) {
    s.columns.splice(i, 1);
    if ((s.sort_by ?? 0) >= s.columns.length) s.sort_by = 0;
  }

  function moveColumn(s: ReportSection, i: number, by: number) {
    const [c] = s.columns.splice(i, 1);
    s.columns.splice(i + by, 0, c!);
  }
</script>

<nav aria-label="Breadcrumb" class="mb-2 text-sm"><a class="underline" href="/reports">Reports</a></nav>
<h1 class="mb-4 text-2xl font-bold">{id ? 'Edit report' : 'New report'}</h1>
<ErrorMessage {error} />

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading…</p>
{:else}
  <datalist id="report-bindings">
    {#each bindingChoices as b (b)}<option value={b}></option>{/each}
  </datalist>
  <form class="space-y-6" onsubmit={save}>
    <div class="max-w-md">
      <label class="label" for="report-name">Name</label>
      <input id="report-name" class="input" required maxlength="255" bind:value={name} />
    </div>

    <fieldset class="card max-w-3xl space-y-3">
      <legend class="px-1 text-lg font-semibold">Which assets</legend>
      <div class="grid gap-4 sm:grid-cols-3">
        <div>
          <label class="label" for="report-search">Search</label>
          <input id="report-search" class="input" type="search" placeholder="Name or code" bind:value={spec.filter.search} />
        </div>
        <div>
          <label class="label" for="report-loan">Loans</label>
          <select id="report-loan" class="input" bind:value={spec.filter.loan}>
            <option value="">Any</option>
            <option value="open">Lent out</option>
            <option value="overdue">Overdue</option>
          </select>
        </div>
        <div>
          <label class="label" for="report-condition">Condition</label>
          <select id="report-condition" class="input" bind:value={spec.filter.condition}>
            <option value="">Any</option>
            <option value="problem">Needs attention</option>
            <option value="needs_maintenance">Needs maintenance</option>
            <option value="out_of_service">Out of service</option>
            <option value="ok">Working</option>
          </select>
        </div>
      </div>
      <label class="flex items-center gap-2 text-sm"><input type="checkbox" bind:checked={spec.filter.running_low} /> Only what is running low, or in a category running low</label>
      <label class="flex items-center gap-2 text-sm"><input type="checkbox" bind:checked={spec.filter.archived} /> Archived assets instead of active ones</label>
      <fieldset>
        <legend class="label">In these categories, or below them (none for all)</legend>
        <div class="flex max-h-48 flex-col flex-wrap gap-x-6 overflow-auto">
          {#each categoryChoices as row (row.category.id)}
            <label class="flex items-center gap-2 text-sm" style="padding-left: {row.depth * 1.25}rem">
              <input type="checkbox" value={row.category.id} bind:group={categoryIds} />
              {row.category.name}
            </label>
          {/each}
        </div>
      </fieldset>
    </fieldset>

    {#each spec.sections as section, s (s)}
      <fieldset class="card max-w-3xl space-y-3">
        <legend class="px-1 text-lg font-semibold">{section.type === 'chart' ? 'Chart' : 'Table'} {s + 1}</legend>
        {#if section.type === 'chart'}
          <div class="grid gap-4 sm:grid-cols-2">
            <div>
              <label class="label" for="section-{s}-title">Title (optional)</label>
              <input id="section-{s}-title" class="input" maxlength="255" bind:value={section.title} />
            </div>
            <div>
              <label class="label" for="section-{s}-chart">Chart</label>
              <select id="section-{s}-chart" class="input" bind:value={section.chart} onchange={() => chartKindChanged(section)}>
                <option value="bar">Bars</option>
                <option value="donut">Donut</option>
                <option value="line">Line over time</option>
              </select>
            </div>
            {#if section.chart === 'line'}
              <div>
                <label class="label" for="section-{s}-measure">Shows</label>
                <select id="section-{s}-measure" class="input" bind:value={section.measure}>
                  <option value="used">Stock used</option>
                  <option value="restocked">Stock restocked</option>
                  <option value="loans">Loans made</option>
                </select>
              </div>
              <div>
                <label class="label" for="section-{s}-period">Per</label>
                <select id="section-{s}-period" class="input" bind:value={section.period}>
                  <option value="week">Week, for 12 weeks</option>
                  <option value="month">Month, for 12 months</option>
                </select>
              </div>
            {:else}
              <div>
                <label class="label" for="section-{s}-groups">Grouped by</label>
                <select id="section-{s}-groups" class="input" bind:value={section.group_by}>
                  <option value="category">Category</option>
                  <option value="condition">Condition</option>
                  <option value="borrower">Who has it</option>
                  <option value="location">Where it is</option>
                  <option value="unit">Unit</option>
                  {#each fields as f (f.id)}<option value="field:{f.key}">{f.label}</option>{/each}
                </select>
              </div>
              <div>
                <label class="label" for="section-{s}-measure">Counting</label>
                <select id="section-{s}-measure" class="input" bind:value={section.measure}>
                  <option value="assets">Assets</option>
                  <option value="quantity">Quantity in stock, by unit</option>
                  <option value="loans">Assets on loan</option>
                </select>
              </div>
            {/if}
          </div>
          <p class="text-xs text-zinc-600 dark:text-zinc-400">Charts over time read the history of the assets the report finds, as it was recorded.</p>
        {:else}
        <div class="grid gap-4 sm:grid-cols-3">
          <div>
            <label class="label" for="section-{s}-title">Title (optional)</label>
            <input id="section-{s}-title" class="input" maxlength="255" bind:value={section.title} />
          </div>
          <div>
            <label class="label" for="section-{s}-sort">Sort by</label>
            <select id="section-{s}-sort" class="input" bind:value={section.sort_by}>
              {#each section.columns as c, i (i)}<option value={i}>{c.heading || `Column ${i + 1}`}</option>{/each}
            </select>
            <label class="mt-1 flex items-center gap-2 text-sm"><input type="checkbox" bind:checked={section.descending} /> Largest or last first</label>
          </div>
          <div>
            <label class="label" for="section-{s}-group">Group</label>
            <select id="section-{s}-group" class="input" bind:value={section.group_by}>
              <option value="">No groups</option>
              <option value="category">By category</option>
            </select>
          </div>
        </div>
        <table class="w-full text-left text-sm">
          <thead>
            <tr>
              <th scope="col" class="pr-2">Heading</th>
              <th scope="col" class="pr-2">Shows</th>
              <th scope="col"><span class="sr-only">Actions</span></th>
            </tr>
          </thead>
          <tbody>
            {#each section.columns as column, i (i)}
              <tr>
                <td class="py-1 pr-2">
                  <label class="sr-only" for="section-{s}-col-{i}-heading">Heading of column {i + 1}</label>
                  <input id="section-{s}-col-{i}-heading" class="input" required maxlength="255" bind:value={column.heading} />
                </td>
                <td class="py-1 pr-2">
                  <label class="sr-only" for="section-{s}-col-{i}-binding">What column {i + 1} shows</label>
                  <input id="section-{s}-col-{i}-binding" class="input font-mono" list="report-bindings" placeholder={'{{asset.name}}'} bind:value={column.binding} />
                </td>
                <td class="py-1 whitespace-nowrap">
                  <button type="button" class="btn-secondary px-2" aria-label="Move column {i + 1} left" disabled={i === 0} onclick={() => moveColumn(section, i, -1)}>←</button>
                  <button type="button" class="btn-secondary px-2" aria-label="Move column {i + 1} right" disabled={i === section.columns.length - 1} onclick={() => moveColumn(section, i, 1)}>→</button>
                  <button type="button" class="btn-secondary px-2" aria-label="Remove column {i + 1}" disabled={section.columns.length === 1} onclick={() => removeColumn(section, i)}>✕</button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
        <p class="text-xs text-zinc-600 dark:text-zinc-400">
          A column shows text with placeholders, as labels do: <code>{'{{asset.name}}'}</code>, <code>{'{{stock.quantity}} {{stock.unit}}'}</code>,
          <code>{'{{field.serial}}'}</code>. Suggestions appear as you type.
        </p>
        {/if}
        <div class="flex flex-wrap gap-2">
          {#if section.type === 'table'}<button type="button" class="btn-secondary" onclick={() => addColumn(section)}>Add column</button>{/if}
          {#if spec.sections.length > 1}
            <button type="button" class="btn-secondary" onclick={() => spec.sections.splice(s, 1)}>Remove {section.type === 'chart' ? 'chart' : 'table'} {s + 1}</button>
          {/if}
        </div>
      </fieldset>
    {/each}

    <div class="flex flex-wrap gap-2">
      <button type="button" class="btn-secondary" onclick={() => spec.sections.push(newTable())}>Add table</button>
      <button type="button" class="btn-secondary" onclick={() => spec.sections.push(newChart())}>Add chart</button>
      <button type="button" class="btn-secondary" disabled={busy} onclick={runPreview}>Preview</button>
      <button type="submit" class="btn-primary" disabled={busy}>Save report</button>
    </div>
  </form>

  {#if preview}
    <section class="mt-8" aria-labelledby="preview-heading">
      <h2 id="preview-heading" class="mb-2 text-xl font-semibold">Preview</h2>
      <ReportTables result={preview} />
    </section>
  {/if}
{/if}

<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import { scanActions, type AssetWithDetails, type Field, type GeneratorInfo, type Report, type ReportResult, type ReportSection, type ReportSpec } from '../../lib/models';
  import { navigate } from '../../lib/router.svelte';
  import { cleanedScanSpec, newScanSection, newScanSpec, scanActionLabel } from '../../lib/scan';
  import { placeholders } from '../../lib/template';
  import ErrorMessage from '../../components/ErrorMessage.svelte';
  import ScanResult from '../../components/ScanResult.svelte';

  // /scan-templates/new and /scan-templates/:id/edit: what scanning an
  // asset's code shows (README §6.4).
  let { params }: { params: Record<string, string> } = $props();
  const id = untrack(() => params.id ?? '');

  let name = $state('');
  let spec = $state<ReportSpec>(newScanSpec());
  let fields = $state<Field[]>([]);
  let generators = $state<GeneratorInfo[]>([]);
  let loading = $state(true);
  let busy = $state(false);
  let error = $state('');

  // Previewing with an asset found by searching.
  let search = $state('');
  let found = $state<AssetWithDetails[]>([]);
  let assetId = $state('');
  let preview = $state<ReportResult | null>(null);

  const bindingChoices = $derived(placeholders(fields.map((f) => f.key), generators.map((g) => g.type)));
  const sectionName = (s: ReportSection) => (s.type === 'details' ? 'Details' : s.type === 'table' ? 'Contents' : 'Actions');

  onMount(async () => {
    try {
      [fields, generators] = await Promise.all([
        request<{ fields: Field[] }>('GET', '/fields').then((r) => r.fields ?? []),
        request<{ generators: GeneratorInfo[] }>('GET', '/generators').then((r) => r.generators ?? []),
      ]);
      if (id) {
        const r = (await request<{ report: Report }>('GET', `/reports/${encodeURIComponent(id)}`)).report;
        name = r.name;
        spec = {
          filter: {},
          sections: r.spec.sections.map((s) => ({ ...newScanSection(s.type as 'details' | 'table' | 'actions'), ...s, columns: (s.columns ?? []).map((c) => ({ ...c })), actions: [...(s.actions ?? [])] })),
        };
      }
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
  });

  async function find() {
    error = '';
    try {
      found = ((await request<{ assets: AssetWithDetails[] }>('GET', `/assets?search=${encodeURIComponent(search.trim())}`)).assets ?? []).slice(0, 20);
      assetId = found[0]?.asset.id ?? '';
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function runPreview() {
    error = '';
    busy = true;
    try {
      preview = (await request<{ result: ReportResult }>('POST', '/reports/preview', { kind: 'scan', asset_id: assetId, spec: cleanedScanSpec(spec) })).result;
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
      const body = { name, kind: 'scan', spec: cleanedScanSpec(spec) };
      if (id) await request<{ report: Report }>('PUT', `/reports/${encodeURIComponent(id)}`, body);
      else await request<{ report: Report }>('POST', '/reports', body);
      navigate('/reports');
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }

  function toggleAction(s: ReportSection, action: (typeof scanActions)[number], on: boolean) {
    const now = s.actions ?? [];
    // Kept in the order the scan page shows them.
    s.actions = scanActions.filter((a) => (a === action ? on : now.includes(a)));
  }

  function removeColumn(s: ReportSection, i: number) {
    s.columns.splice(i, 1);
    if ((s.sort_by ?? 0) >= s.columns.length) s.sort_by = 0;
  }
</script>

<nav aria-label="Breadcrumb" class="mb-2 text-sm"><a class="underline" href="/reports">Reports</a></nav>
<h1 class="mb-1 text-2xl font-bold">{id ? 'Edit scan template' : 'New scan template'}</h1>
<p class="mb-4 max-w-prose text-sm text-zinc-600 dark:text-zinc-400">
  What scanning an asset’s code shows: details of the asset, what it holds, and quick actions. Categories choose the
  template their assets show.
</p>
<ErrorMessage {error} />

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading…</p>
{:else}
  <datalist id="scan-bindings">
    {#each bindingChoices as b (b)}<option value={b}></option>{/each}
  </datalist>
  <form class="space-y-6" onsubmit={save}>
    <div class="max-w-md">
      <label class="label" for="scan-name">Name</label>
      <input id="scan-name" class="input" required maxlength="255" bind:value={name} />
    </div>

    {#each spec.sections as section, s (s)}
      <fieldset class="card max-w-3xl space-y-3">
        <legend class="px-1 text-lg font-semibold">{sectionName(section)} {s + 1}</legend>
        {#if section.type === 'actions'}
          <div class="flex flex-wrap gap-x-6 gap-y-1">
            {#each scanActions as action (action)}
              <label class="flex items-center gap-2 text-sm">
                <input type="checkbox" checked={section.actions?.includes(action)} onchange={(e) => toggleAction(section, action, e.currentTarget.checked)} />
                {scanActionLabel(action)}
              </label>
            {/each}
          </div>
          <p class="text-xs text-zinc-600 dark:text-zinc-400">Each shows only to those allowed to take it, and only where it applies.</p>
        {:else}
          <div class="grid gap-4 sm:grid-cols-2">
            <div>
              <label class="label" for="scan-{s}-title">Title (optional)</label>
              <input id="scan-{s}-title" class="input" maxlength="255" bind:value={section.title} />
            </div>
            {#if section.type === 'table'}
              <div>
                <label class="label" for="scan-{s}-sort">Sort by</label>
                <select id="scan-{s}-sort" class="input" bind:value={section.sort_by}>
                  {#each section.columns as c, i (i)}<option value={i}>{c.heading || `Column ${i + 1}`}</option>{/each}
                </select>
              </div>
            {/if}
          </div>
          {#if section.type === 'table'}<p class="text-sm">Lists what the scanned asset holds.</p>{/if}
          <table class="w-full text-left text-sm">
            <thead>
              <tr>
                <th scope="col" class="pr-2">{section.type === 'details' ? 'Label' : 'Heading'}</th>
                <th scope="col" class="pr-2">Shows</th>
                <th scope="col"><span class="sr-only">Actions</span></th>
              </tr>
            </thead>
            <tbody>
              {#each section.columns as column, i (i)}
                <tr>
                  <td class="py-1 pr-2">
                    <label class="sr-only" for="scan-{s}-col-{i}-heading">Label {i + 1} of {sectionName(section).toLowerCase()} {s + 1}</label>
                    <input id="scan-{s}-col-{i}-heading" class="input" required maxlength="255" bind:value={column.heading} />
                  </td>
                  <td class="py-1 pr-2">
                    <label class="sr-only" for="scan-{s}-col-{i}-binding">What {column.heading || `line ${i + 1}`} shows</label>
                    <input id="scan-{s}-col-{i}-binding" class="input font-mono" list="scan-bindings" placeholder={'{{asset.name}}'} bind:value={column.binding} />
                  </td>
                  <td class="py-1">
                    <button type="button" class="btn-secondary px-2" aria-label="Remove {column.heading || `line ${i + 1}`}" disabled={section.columns.length === 1} onclick={() => removeColumn(section, i)}>✕</button>
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
          <button type="button" class="btn-secondary" onclick={() => section.columns.push({ heading: '', binding: '' })}>Add a line</button>
        {/if}
        {#if spec.sections.length > 1}
          <button type="button" class="btn-secondary" onclick={() => spec.sections.splice(s, 1)}>Remove {sectionName(section).toLowerCase()} {s + 1}</button>
        {/if}
      </fieldset>
    {/each}

    <div class="flex flex-wrap gap-2">
      <button type="button" class="btn-secondary" onclick={() => spec.sections.push(newScanSection('details'))}>Add details</button>
      <button type="button" class="btn-secondary" onclick={() => spec.sections.push(newScanSection('table'))}>Add contents</button>
      <button type="button" class="btn-secondary" onclick={() => spec.sections.push(newScanSection('actions'))}>Add actions</button>
      <button type="submit" class="btn-primary" disabled={busy}>Save scan template</button>
    </div>
  </form>

  <section class="mt-8 max-w-3xl" aria-labelledby="preview-heading">
    <h2 id="preview-heading" class="mb-2 text-xl font-semibold">Preview</h2>
    <form class="mb-4 flex flex-wrap items-end gap-2" onsubmit={(e) => (e.preventDefault(), find())}>
      <div>
        <label class="label" for="scan-preview-search">Preview with</label>
        <input id="scan-preview-search" class="input" type="search" placeholder="Name or code" bind:value={search} />
      </div>
      <button type="submit" class="btn-secondary">Find</button>
      {#if found.length}
        <div>
          <label class="label" for="scan-preview-asset">Asset</label>
          <select id="scan-preview-asset" class="input" bind:value={assetId}>
            {#each found as a (a.asset.id)}<option value={a.asset.id}>{a.asset.name}</option>{/each}
          </select>
        </div>
        <button type="button" class="btn-secondary" disabled={busy || !assetId} onclick={runPreview}>Preview</button>
      {/if}
    </form>
    {#if preview}<div class="card"><ScanResult result={preview} /></div>{/if}
  </section>
{/if}

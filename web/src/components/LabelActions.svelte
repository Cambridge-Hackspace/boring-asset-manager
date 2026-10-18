<script lang="ts">
  import { request, requestDownload } from '../lib/api';
  import { messageOf } from '../lib/errors';
  import { printersFor, sizeText, type LabelOptions } from '../lib/labels';
  import { session } from '../lib/session.svelte';
  import AddToSpool from './AddToSpool.svelte';
  import ErrorMessage from './ErrorMessage.svelte';
  import FitWarnings from './FitWarnings.svelte';

  // Export or print labels for one or more assets. With a single asset it
  // also previews the label and offers direct downloads. Only the printers
  // a template suits are offered, and a template that cannot label an
  // asset says why before anything is tried (README §4.4).
  let {
    assetIds,
    templateId: fixedTemplate,
    spool = false,
  }: { assetIds: string[]; templateId?: string; spool?: boolean } = $props();

  let options = $state<LabelOptions | null>(null);
  let templateId = $state('');
  let printerId = $state('');
  let copies = $state(1);
  let format = $state<'pdf' | 'zip'>('pdf');
  let error = $state('');
  let status = $state('');
  let printedWarnings = $state<string[]>([]);
  let busy = $state(false);

  const canPrint = $derived(session.can('labels.print'));
  const single = $derived(assetIds.length === 1 ? assetIds[0]! : null);
  const labelURL = (fmt: string) =>
    single ? `/api/v1/assets/${encodeURIComponent(single)}/label?template_id=${encodeURIComponent(templateId)}&format=${fmt}` : '';

  const templates = $derived((options?.templates ?? []).filter((t) => !fixedTemplate || t.id === fixedTemplate));
  const template = $derived(templates.find((t) => t.id === templateId));
  const preferred = $derived(templates.filter((t) => t.preferred));
  const others = $derived(templates.filter((t) => !t.preferred));
  const problems = $derived(template?.problems ?? []);
  const choices = $derived(options && templateId ? printersFor(options, templateId) : []);
  const fitting = $derived(choices.filter((c) => c.fit.fits));
  const chosen = $derived(choices.find((c) => c.id === printerId && c.fit.fits));

  $effect(() => {
    const ids = [...assetIds];
    void (async () => {
      try {
        const opts = await request<LabelOptions>('POST', '/labels/options', { asset_ids: ids });
        options = opts;
        templateId = fixedTemplate ?? opts.template_id;
      } catch (err) {
        error = messageOf(err);
      }
    })();
  });

  // Keep a printer the template suits chosen, if any does.
  $effect(() => {
    if (!fitting.some((c) => c.id === printerId)) printerId = fitting[0]?.id ?? '';
  });

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

  async function exportLabels() {
    error = status = '';
    printedWarnings = [];
    busy = true;
    try {
      const { blob, filename } = await requestDownload('POST', '/labels/export', { template_id: templateId, asset_ids: assetIds, format }, `labels.${format}`);
      saveBlob(blob, filename);
      status = `Exported ${assetIds.length} label${assetIds.length === 1 ? '' : 's'}.`;
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }

  async function print() {
    error = status = '';
    printedWarnings = [];
    busy = true;
    try {
      const res = await request<{ labels: number; warnings?: string[] }>('POST', `/printers/${encodeURIComponent(printerId)}/print`, {
        template_id: templateId,
        asset_ids: assetIds,
        copies,
      });
      status = `Sent ${res.labels} label${res.labels === 1 ? '' : 's'} to ${chosen?.name ?? 'the printer'}.`;
      printedWarnings = res.warnings ?? [];
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }
</script>

{#snippet option(t: LabelOptions['templates'][number])}
  <option value={t.id}>{t.name} ({sizeText(t)})</option>
{/snippet}

<section class="card space-y-4" aria-labelledby="labels-heading">
  <h2 id="labels-heading" class="text-lg font-semibold">Labels</h2>
  {#if !options}
    {#if !error}<p class="text-sm text-zinc-600 dark:text-zinc-400" role="status">Loading templates…</p>{/if}
  {:else if templates.length === 0}
    <p class="text-sm text-zinc-600 dark:text-zinc-400">No label templates yet.</p>
  {:else}
    <div class="max-w-sm" hidden={Boolean(fixedTemplate)}>
      <label class="label" for="label-template">Template</label>
      <select id="label-template" class="input" bind:value={templateId}>
        {#if preferred.length && others.length}
          <optgroup label="Its category's">{#each preferred as t (t.id)}{@render option(t)}{/each}</optgroup>
          <optgroup label="Others">{#each others as t (t.id)}{@render option(t)}{/each}</optgroup>
        {:else}
          {#each templates as t (t.id)}{@render option(t)}{/each}
        {/if}
      </select>
    </div>

    {#if problems.length}
      <ul class="space-y-1 rounded border border-amber-300 bg-amber-50 p-3 text-sm text-amber-900 dark:border-amber-700 dark:bg-amber-950 dark:text-amber-200" aria-label="Why it cannot label these">
        {#each problems as p (p.asset_id)}<li>{p.message}</li>{/each}
        <li>Choose another template{assetIds.length > 1 ? ', or leave those assets out' : ''}.</li>
      </ul>
    {:else if single && templateId}
      <figure class="space-y-2">
        <img
          src={labelURL('svg')}
          alt="Preview of the label for this asset"
          class="max-h-64 max-w-full border border-zinc-300 bg-white dark:border-zinc-700"
        />
        <figcaption class="flex flex-wrap gap-2 text-sm">
          <a class="btn-secondary" href={labelURL('png')} download>Download PNG</a>
          <a class="btn-secondary" href={labelURL('pdf')} download>Download PDF</a>
        </figcaption>
      </figure>
    {:else if !single}
      <div class="flex flex-wrap items-end gap-3">
        <div>
          <label class="label" for="label-format">Format</label>
          <select id="label-format" class="input" bind:value={format}>
            <option value="pdf">PDF, one label per page</option>
            <option value="zip">ZIP of PNG images</option>
          </select>
        </div>
        <button type="button" class="btn-secondary" onclick={exportLabels} disabled={busy || !templateId}>
          Export {assetIds.length} label{assetIds.length === 1 ? '' : 's'}
        </button>
      </div>
    {/if}

    {#if canPrint}
      {#if choices.length === 0}
        <p class="text-sm text-zinc-600 dark:text-zinc-400">
          No printers are set up.{#if session.can('settings.manage')} <a class="underline" href="/printers">Add one</a>{/if}
        </p>
      {:else if fitting.length === 0}
        <p class="text-sm text-amber-900 dark:text-amber-200">
          No printer has labels loaded that this template fits.{#if session.can('settings.manage')} <a class="underline" href="/printers">Printers</a>{/if}
        </p>
      {:else}
        <form class="flex flex-wrap items-end gap-3" onsubmit={(e) => { e.preventDefault(); void print(); }}>
          <div>
            <label class="label" for="label-printer">Printer</label>
            <select id="label-printer" class="input" bind:value={printerId}>
              {#each choices as c (c.id)}
                <option value={c.id} disabled={!c.fit.fits}>{c.name}{c.fit.fits ? '' : ' (does not take this label)'}</option>
              {/each}
            </select>
          </div>
          <div class="w-24">
            <label class="label" for="label-copies">Copies</label>
            <input id="label-copies" class="input" type="number" min="1" max="20" bind:value={copies} />
          </div>
          <button type="submit" class="btn-primary" disabled={busy || !templateId || !chosen || problems.length > 0}>Print</button>
        </form>
        {#if chosen && !status}<FitWarnings warnings={chosen.fit.warnings} />{/if}
      {/if}
      {#if choices.length > fitting.length}
        <details class="text-sm text-zinc-600 dark:text-zinc-400">
          <summary>Why not {choices.filter((c) => !c.fit.fits).map((c) => c.name).join(', ')}?</summary>
          <ul class="mt-1 space-y-1">
            {#each choices.filter((c) => !c.fit.fits) as c (c.id)}<li>{c.fit.reason}</li>{/each}
          </ul>
        </details>
      {/if}
      {#if spool}
        <p class="flex flex-wrap items-center gap-2">
          <AddToSpool what={() => ({ asset_ids: assetIds, ...(templateId !== options?.template_id ? { template_id: templateId } : {}) })} label="Add to the queue" />
          <span class="text-xs text-zinc-600 dark:text-zinc-400">to print later with others, from the print queue</span>
        </p>
      {/if}
    {/if}
  {/if}
  <ErrorMessage {error} />
  {#if status}
    <p class="text-sm text-green-700 dark:text-green-400" role="status">{status}</p>
    <FitWarnings warnings={printedWarnings} label="Printed, with warnings" />
  {/if}
</section>

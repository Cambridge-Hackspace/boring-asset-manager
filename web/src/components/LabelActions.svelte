<script lang="ts">
  import { onMount } from 'svelte';
  import { request, requestDownload } from '../lib/api';
  import { messageOf } from '../lib/errors';
  import type { Printer, TemplateSummary } from '../lib/models';
  import { session } from '../lib/session.svelte';
  import ErrorMessage from './ErrorMessage.svelte';

  // Export or print labels for one or more assets. With a single asset it
  // also previews the label and offers direct downloads.
  let { assetIds, templateId: fixedTemplate }: { assetIds: string[]; templateId?: string } = $props();

  let templates = $state<TemplateSummary[]>([]);
  let printers = $state<Printer[]>([]);
  let templateId = $state('');
  let printerId = $state('');
  let copies = $state(1);
  let format = $state<'pdf' | 'zip'>('pdf');
  let error = $state('');
  let status = $state('');
  let busy = $state(false);

  const canPrint = $derived(session.can('labels:print'));
  const single = $derived(assetIds.length === 1 ? assetIds[0]! : null);
  const labelURL = (fmt: string) =>
    single ? `/api/v1/assets/${encodeURIComponent(single)}/label?template_id=${encodeURIComponent(templateId)}&format=${fmt}` : '';

  onMount(async () => {
    try {
      templates = (await request<{ templates: TemplateSummary[] }>('GET', '/templates')).templates ?? [];
      if (fixedTemplate) templates = templates.filter((t) => t.id === fixedTemplate);
      templateId = templates[0]?.id ?? '';
      if (canPrint) {
        printers = (await request<{ printers: Printer[] }>('GET', '/printers')).printers ?? [];
        printerId = printers[0]?.id ?? '';
      }
    } catch (err) {
      error = messageOf(err);
    }
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
    busy = true;
    try {
      const res = await request<{ labels: number }>('POST', `/printers/${encodeURIComponent(printerId)}/print`, {
        template_id: templateId,
        asset_ids: assetIds,
        copies,
      });
      const name = printers.find((p) => p.id === printerId)?.name ?? 'the printer';
      status = `Sent ${res.labels} label${res.labels === 1 ? '' : 's'} to ${name}.`;
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }
</script>

<section class="card space-y-4" aria-labelledby="labels-heading">
  <h2 id="labels-heading" class="text-lg font-semibold">Labels</h2>
  {#if templates.length === 0}
    <p class="text-sm text-zinc-600 dark:text-zinc-400">No label templates yet.</p>
  {:else}
    <div class="max-w-sm" hidden={Boolean(fixedTemplate)}>
      <label class="label" for="label-template">Template</label>
      <select id="label-template" class="input" bind:value={templateId}>
        {#each templates as t (t.id)}
          <option value={t.id}>{t.name} ({t.width} × {t.height} {t.units})</option>
        {/each}
      </select>
    </div>

    {#if single && templateId}
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
      {#if printers.length === 0}
        <p class="text-sm text-zinc-600 dark:text-zinc-400">No printers are set up.</p>
      {:else}
        <form class="flex flex-wrap items-end gap-3" onsubmit={(e) => { e.preventDefault(); void print(); }}>
          <div>
            <label class="label" for="label-printer">Printer</label>
            <select id="label-printer" class="input" bind:value={printerId}>
              {#each printers as p (p.id)}
                <option value={p.id}>{p.name}</option>
              {/each}
            </select>
          </div>
          <div class="w-24">
            <label class="label" for="label-copies">Copies</label>
            <input id="label-copies" class="input" type="number" min="1" max="20" bind:value={copies} />
          </div>
          <button type="submit" class="btn-primary" disabled={busy || !templateId || !printerId}>Print</button>
        </form>
      {/if}
    {/if}
  {/if}
  <ErrorMessage {error} />
  {#if status}<p class="text-sm text-green-700 dark:text-green-400" role="status">{status}</p>{/if}
</section>

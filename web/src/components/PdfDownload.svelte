<!--
  A PDF of a report or of the asset list (docs/reports.md#pdf): its page
  size, the instance's unless another is chosen, its orientation, and
  whether rows have their asset's photo and label; for the asset list, which
  columns, or instead a sheet per asset or a stocktake checklist
  (docs/reports.md#asset-sheets). What it needs from the server is read
  when it is first opened.
-->
<script lang="ts">
  import { request } from '../lib/api';
  import type { Field } from '../lib/models';

  let {
    base,
    query = {},
    assetColumns = false,
  }: {
    /** The PDF's address, without its query. */
    base: string;
    /** What else its query holds, such as the list's filters. */
    query?: Record<string, string>;
    /** Whether the columns of the asset list are chosen, and its sheets and stocktake offered. */
    assetColumns?: boolean;
  } = $props();

  // What the asset list is made into: a table, a sheet per asset, or a
  // stocktake checklist, each at its own address.
  const kinds = [
    { value: 'table', label: 'A table', path: '' },
    { value: 'sheets', label: 'A sheet per asset', path: '/api/v1/assets/sheets.pdf' },
    { value: 'stocktake', label: 'A stocktake checklist', path: '/api/v1/assets/stocktake.pdf' },
  ];
  let kind = $state('table');
  const table = $derived(kind === 'table');

  const fixedColumns = [
    { value: 'name', label: 'Name', checked: true },
    { value: 'categories', label: 'Categories', checked: true },
    { value: 'quantity', label: 'Quantity', checked: true },
    { value: 'unit', label: 'Unit', checked: false },
    { value: 'code.qr', label: 'QR Code', checked: false },
    { value: 'code.micro_qr', label: 'Micro QR Code', checked: false },
    { value: 'code.code128', label: 'Code 128', checked: false },
  ];

  let opened = false;
  let pageSize = $state('letter');
  let orientation = $state('portrait');
  let photos = $state(false);
  let labels = $state(false);
  let columns = $state(fixedColumns);
  let chosen = $state<string[]>(fixedColumns.filter((c) => c.checked).map((c) => c.value));

  async function open(event: Event) {
    if (opened || !(event.currentTarget as HTMLDetailsElement).open) return;
    opened = true;
    try {
      pageSize = (await request<{ page_size: string }>('GET', '/settings/documents')).page_size;
    } catch {
      // Letter stands.
    }
    if (assetColumns) {
      try {
        const { fields } = await request<{ fields: Field[] }>('GET', '/fields');
        columns = [...fixedColumns, ...(fields ?? []).map((f) => ({ value: `field.${f.key}`, label: f.label, checked: false }))];
      } catch {
        // The fixed columns stand.
      }
    }
  }

  const href = $derived.by(() => {
    const q = new URLSearchParams(Object.entries(query).filter(([, v]) => v));
    q.set('page_size', pageSize);
    if (orientation !== 'portrait') q.set('orientation', orientation);
    if (table && photos) q.set('photos', '1');
    if (table && labels) q.set('labels', '1');
    if (table && assetColumns && chosen.length) q.set('columns', columns.map((c) => c.value).filter((v) => chosen.includes(v)).join(','));
    const path = kinds.find((k) => k.value === kind)?.path || base;
    return `${path}?${q.toString()}`;
  });
</script>

<details class="relative" ontoggle={open}>
  <summary class="btn-secondary cursor-pointer list-none">PDF…</summary>
  <div class="card absolute right-0 z-20 mt-1 w-72 space-y-3 text-sm shadow-lg">
    <div class="grid grid-cols-2 gap-2">
      <label class="space-y-1">
        <span class="label">Page size</span>
        <select class="input" bind:value={pageSize}>
          <option value="letter">Letter</option>
          <option value="a4">A4</option>
        </select>
      </label>
      <label class="space-y-1">
        <span class="label">Orientation</span>
        <select class="input" bind:value={orientation}>
          <option value="portrait">Portrait</option>
          <option value="landscape">Landscape</option>
        </select>
      </label>
    </div>
    {#if assetColumns}
      <fieldset class="space-y-1">
        <legend class="label">Make</legend>
        {#each kinds as k (k.value)}
          <label class="flex items-center gap-2"><input type="radio" name="pdf-kind" value={k.value} bind:group={kind} /> {k.label}</label>
        {/each}
      </fieldset>
    {/if}
    {#if table}
      <label class="flex items-center gap-2"><input type="checkbox" bind:checked={photos} /> Each asset's photo</label>
      <label class="flex items-center gap-2"><input type="checkbox" bind:checked={labels} /> Each asset's label</label>
    {/if}
    {#if assetColumns && table}
      <fieldset class="space-y-1">
        <legend class="label">Columns</legend>
        <div class="max-h-40 space-y-1 overflow-y-auto">
          {#each columns as c (c.value)}
            <label class="flex items-center gap-2"><input type="checkbox" value={c.value} bind:group={chosen} /> {c.label}</label>
          {/each}
        </div>
      </fieldset>
    {/if}
    <a class="btn-secondary block w-full text-center" {href} download>Download PDF</a>
  </div>
</details>

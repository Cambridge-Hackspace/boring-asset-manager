<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { DriverInfo, Printer } from '../../lib/models';
  import { convertUnits, fromMM, makeAdaptive, makeFixed, toMM, type Adapt, type TemplateDoc, type Units } from '../../lib/template';
  import FitWarnings from '../../components/FitWarnings.svelte';

  // The template's own settings: name, units, size and DPI, which can
  // be taken from a printer's loaded media; the printer's fit check runs as
  // they change.
  let {
    doc,
    isNew,
    problems,
    onchange,
  }: {
    doc: TemplateDoc;
    isNew: boolean;
    problems: Record<string, string>;
    // record is false for edits that are not undo steps (the name).
    onchange: (doc: TemplateDoc, record?: boolean) => void;
  } = $props();

  let printers = $state<Printer[]>([]);
  let drivers = $state<DriverInfo[]>([]);
  let printerId = $state('');
  let fit = $state<{ fits: boolean; message?: string; warnings?: string[] } | null>(null);
  let fitError = $state('');

  onMount(async () => {
    try {
      printers = (await request<{ printers: Printer[] }>('GET', '/printers')).printers ?? [];
      drivers = (await request<{ drivers: DriverInfo[] }>('GET', '/printers/drivers')).drivers ?? [];
    } catch {
      // Without access to printers, sizes are entered by hand.
    }
  });

  function update(patch: Partial<TemplateDoc>, record = true) {
    onchange({ ...doc, ...patch }, record);
  }

  function setNumber(key: 'width' | 'height' | 'dpi', raw: string) {
    const n = Number(raw);
    if (Number.isFinite(n) && n > 0) update({ [key]: key === 'dpi' ? Math.round(n) : n });
  }

  /** The chosen printer's media and resolution. */
  const printerMedia = $derived.by(() => {
    const p = printers.find((p) => p.id === printerId);
    if (!p) return null;
    const d = drivers.find((d) => d.driver === p.driver);
    const media = p.loaded ?? d?.media.find((m) => m.id === p.media);
    return media ? { media, dpi: d?.model_dpi?.[p.model] } : null;
  });

  // Takes the label size from the printer: the longer side across, so
  // labels read along the tape; continuous rolls keep the current length.
  function sizeFromPrinter() {
    if (!printerMedia) return;
    const { media, dpi } = printerMedia;
    const across = fromMM(media.width_mm, doc.units);
    const along = media.die_cut && media.length_mm ? fromMM(media.length_mm, doc.units) : Math.max(doc.width, across);
    update({ width: Math.max(across, along), height: Math.min(across, along), ...(dpi ? { dpi } : {}) });
  }

  // The fit check, re-run (debounced) as the size changes.
  $effect(() => {
    const id = printerId;
    const mm = (v: number) => toMM(v, doc.units);
    const body = {
      width_mm: mm(doc.width),
      height_mm: mm(doc.height),
      dpi: doc.dpi,
      // Where the template has content, for warnings about parts that will not print.
      content: doc.elements.map((e) => ({ x: mm(e.x), y: mm(e.y), width: mm(e.width), height: mm(e.height) })),
    };
    fit = null;
    fitError = '';
    if (!id) return;
    const timer = setTimeout(async () => {
      try {
        fit = await request<{ fits: boolean; message?: string; warnings?: string[] }>('POST', `/printers/${encodeURIComponent(id)}/fit`, body);
      } catch (err) {
        fitError = messageOf(err);
      }
    }, 300);
    return () => clearTimeout(timer);
  });

  const fieldError = (key: string) => problems[key];

  function setRange(key: 'min_width' | 'max_width' | 'min_height' | 'max_height', raw: string) {
    const n = Number(raw);
    if (doc.adapt && Number.isFinite(n) && n > 0) update({ adapt: { ...doc.adapt, [key]: n } });
  }
</script>

<section class="card space-y-4" aria-labelledby="canvas-heading">
  <h2 id="canvas-heading" class="text-lg font-semibold">Label</h2>
  <div class="grid gap-3 sm:grid-cols-2">
    <div>
      <label class="label" for="tpl-name">Name</label>
      <input id="tpl-name" class="input" required value={doc.name} oninput={(e) => update({ name: e.currentTarget.value }, false)} aria-invalid={Boolean(fieldError('name'))} />
      {#if fieldError('name')}<p class="error-text mt-1">{fieldError('name')}</p>{/if}
    </div>
    {#if !isNew}
      <div>
        <p class="label">ID</p>
        <p class="break-all font-mono text-sm">{doc.id}</p>
        <p class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">Assigned when first saved; the API names the template by it.</p>
      </div>
    {/if}
  </div>

  <div class="grid grid-cols-2 gap-3 sm:grid-cols-4">
    <div>
      <label class="label" for="tpl-units">Units</label>
      <select id="tpl-units" class="input" value={doc.units} onchange={(e) => onchange(convertUnits(doc, e.currentTarget.value as Units))}>
        <option value="mm">Millimeters</option>
        <option value="in">Inches</option>
      </select>
    </div>
    <div>
      <label class="label" for="tpl-width">Width</label>
      <input id="tpl-width" class="input" type="number" min="0" step="any" value={doc.width} onchange={(e) => setNumber('width', e.currentTarget.value)} />
    </div>
    <div>
      <label class="label" for="tpl-height">Height</label>
      <input id="tpl-height" class="input" type="number" min="0" step="any" value={doc.height} onchange={(e) => setNumber('height', e.currentTarget.value)} />
    </div>
    <div>
      <label class="label" for="tpl-dpi">DPI</label>
      <input id="tpl-dpi" class="input" type="number" min="150" max="600" step="1" value={doc.dpi} onchange={(e) => setNumber('dpi', e.currentTarget.value)} />
    </div>
  </div>
  {#each ['width', 'height', 'dpi', 'units'] as key (key)}
    {#if fieldError(key)}<p class="error-text">{fieldError(key)}</p>{/if}
  {/each}

  <fieldset class="space-y-3">
    <legend class="label">Sizing</legend>
    <div class="flex flex-wrap gap-4 text-sm">
      <label class="flex items-center gap-2">
        <input type="radio" name="tpl-sizing" checked={doc.sizing !== 'adaptive'} onchange={() => onchange(makeFixed(doc))} />
        Fixed: prints only at this size and DPI
      </label>
      <label class="flex items-center gap-2">
        <input type="radio" name="tpl-sizing" checked={doc.sizing === 'adaptive'} onchange={() => onchange(makeAdaptive(doc))} />
        Adaptive: laid out again for each label it prints on
      </label>
    </div>
    {#if doc.sizing === 'adaptive' && doc.adapt}
      <div class="grid grid-cols-2 gap-3 sm:grid-cols-4">
        {#each [['min_width', 'Narrowest'], ['max_width', 'Widest'], ['min_height', 'Shortest'], ['max_height', 'Tallest']] as const as [key, label] (key)}
          <div>
            <label class="text-xs" for="tpl-{key}">{label} ({doc.units})</label>
            <input id="tpl-{key}" class="input" type="number" min="0" step="any" value={doc.adapt[key]} onchange={(e) => setRange(key, e.currentTarget.value)} />
          </div>
        {/each}
      </div>
      <div>
        <label class="text-xs" for="tpl-length">On a continuous roll</label>
        <select
          id="tpl-length"
          class="input"
          value={doc.adapt.continuous_length}
          onchange={(e) => update({ adapt: { ...doc.adapt!, continuous_length: e.currentTarget.value as Adapt['continuous_length'] } })}
        >
          <option value="proportional">Its length in proportion to the roll's width</option>
          <option value="fixed">Its length as designed</option>
        </select>
      </div>
      {#each Object.entries(problems).filter(([k]) => k.startsWith('adapt') || k === 'sizing') as [k, message] (k)}
        <p class="error-text">{message}</p>
      {/each}
    {/if}
  </fieldset>

  {#if printers.length}
    <div class="flex flex-wrap items-end gap-3">
      <div>
        <label class="label" for="tpl-printer">Printer</label>
        <select id="tpl-printer" class="input" bind:value={printerId}>
          <option value="">None</option>
          {#each printers as p (p.id)}<option value={p.id}>{p.name}</option>{/each}
        </select>
      </div>
      <button type="button" class="btn-secondary" disabled={!printerMedia} onclick={sizeFromPrinter}>Use its label size</button>
    </div>
    {#if printerMedia}
      <p class="text-sm text-zinc-600 dark:text-zinc-400">Loaded: {printerMedia.media.name}{printerMedia.dpi ? `, ${printerMedia.dpi} DPI` : ''}.</p>
    {/if}
    <div aria-live="polite">
      {#if fit?.fits}
        <p class="text-sm text-green-700 dark:text-green-400">Fits this printer.</p>
        <FitWarnings warnings={fit.warnings} />
      {:else if fit}
        <p class="error-text">{fit.message}</p>
      {:else if fitError}
        <p class="error-text">{fitError}</p>
      {/if}
    </div>
  {/if}
</section>

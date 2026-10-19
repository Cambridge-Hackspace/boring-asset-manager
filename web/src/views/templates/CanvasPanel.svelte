<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { DriverInfo, Printer } from '../../lib/models';
  import { session } from '../../lib/session.svelte';
  import {
    bounds,
    convertUnits,
    describeElement,
    fromMM,
    makeAdaptive,
    makeFixed,
    orientation,
    turnTemplate,
    type Feed,
    resizeLabel,
    toMM,
    type Adapt,
    type PrintArea,
    type TemplateDoc,
    type Units,
  } from '../../lib/template';
  import ErrorMessage from '../../components/ErrorMessage.svelte';
  import FitWarnings from '../../components/FitWarnings.svelte';

  // The template's own settings: name, units, size and DPI, which can
  // be taken from a printer's loaded media; the printer's fit check runs as
  // they change, against the printer last used with this template or the
  // first that takes it, and a test label prints there (README §4.4).
  let {
    doc,
    isNew,
    problems,
    assetId,
    onchange,
    onfit,
  }: {
    doc: TemplateDoc;
    isNew: boolean;
    problems: Record<string, string>;
    /** The asset being previewed, for test prints. */
    assetId?: string;
    // record is false for edits that are not undo steps (the name).
    onchange: (doc: TemplateDoc, record?: boolean) => void;
    /** Where the chosen printer prints, for the canvas; null with none. */
    onfit?: (area: PrintArea | null) => void;
  } = $props();

  interface FitAnswer {
    fits: boolean;
    message?: string;
    warnings?: string[];
    printable?: { x: number; y: number; width: number; height: number };
    edge_mm?: number;
    outside?: number[];
    near_edge?: number[];
    feed?: 'across' | 'along';
  }

  let printers = $state<Printer[]>([]);
  let drivers = $state<DriverInfo[]>([]);
  let printerId = $state('');
  let fit = $state<FitAnswer | null>(null);
  let fitError = $state('');
  let testing = $state(false);
  let testStatus = $state('');
  let testWarnings = $state<string[]>([]);
  let testError = $state('');

  const rememberKey = () => `bam.editor-printer.${doc.id || 'new'}`;

  onMount(async () => {
    try {
      printers = (await request<{ printers: Printer[] }>('GET', '/printers')).printers ?? [];
      drivers = (await request<{ drivers: DriverInfo[] }>('GET', '/printers/drivers')).drivers ?? [];
    } catch {
      // Without access to printers, sizes are entered by hand.
      return;
    }
    const chosen = await choosePrinter();
    // Someone who chose while it was looking keeps their choice.
    if (!choseOwn) printerId = chosen;
  });

  /** The printer last used with this template, or else the first that takes it, or else the first. */
  async function choosePrinter(): Promise<string> {
    try {
      const last = localStorage.getItem(rememberKey());
      if (last !== null && (last === '' || printers.some((p) => p.id === last))) return last;
    } catch {
      // Storage unavailable: choose afresh.
    }
    const answers = await Promise.all(printers.map((p) => check(p.id).catch(() => null)));
    const i = answers.findIndex((a) => a?.fits);
    return printers[i >= 0 ? i : 0]?.id ?? '';
  }

  // Set once a printer is chosen by hand, so the first choice made for
  // them, which can take a moment, does not replace it.
  let choseOwn = false;

  function choose(id: string) {
    choseOwn = true;
    printerId = id;
    testStatus = testError = '';
    testWarnings = [];
    try {
      localStorage.setItem(rememberKey(), id);
    } catch {
      // Storage unavailable: it is chosen afresh next time.
    }
  }

  /** The printer's fit check: a fixed template as it is, an adaptive one as laid out for its roll. */
  async function check(id: string): Promise<FitAnswer> {
    const snapshot = JSON.parse(JSON.stringify(doc)) as TemplateDoc;
    if (snapshot.sizing === 'adaptive') {
      const r = await request<{ fits: boolean; template?: TemplateDoc; notices?: string[]; problem?: { message: string } }>('POST', '/templates/adapt', {
        template: { ...snapshot, id: snapshot.id || 'preview' },
        printer_id: id,
      });
      return r.fits
        ? { fits: true, warnings: [`Laid out at ${r.template!.width} × ${r.template!.height} ${r.template!.units} there.`, ...(r.notices ?? [])] }
        : { fits: false, message: r.problem?.message };
    }
    const mm = (v: number) => toMM(v, snapshot.units);
    return request<FitAnswer>('POST', `/printers/${encodeURIComponent(id)}/fit`, {
      width_mm: mm(snapshot.width),
      height_mm: mm(snapshot.height),
      dpi: snapshot.dpi,
      ...(snapshot.feed ? { feed: snapshot.feed } : {}),
      // Where the template has content, for warnings about parts that will not print.
      content: snapshot.elements.map(bounds).map((b) => ({ x: mm(b.x), y: mm(b.y), width: mm(b.width), height: mm(b.height) })),
    });
  }

  async function testPrint() {
    testing = true;
    testStatus = testError = '';
    testWarnings = [];
    try {
      const template = JSON.parse(JSON.stringify(doc)) as TemplateDoc;
      const res = await request<{ labels: number; warnings?: string[] }>('POST', `/printers/${encodeURIComponent(printerId)}/test-print`, {
        template,
        ...(assetId ? { asset_id: assetId } : {}),
      });
      testStatus = `Sent a test label to ${chosenPrinter?.name ?? 'the printer'}.`;
      testWarnings = res.warnings ?? [];
    } catch (err) {
      testError = messageOf(err);
    } finally {
      testing = false;
    }
  }

  function update(patch: Partial<TemplateDoc>, record = true) {
    onchange({ ...doc, ...patch }, record);
  }

  const chosenPrinter = $derived(printers.find((p) => p.id === printerId));
  const canTest = $derived(session.can('labels.print'));

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
  // labels read along the tape, or upright, the shorter side across;
  // continuous rolls keep the current length. The elements scale with it,
  // so they stay on the label.
  function sizeFromPrinter(upright = false) {
    if (!printerMedia) return;
    const { media, dpi } = printerMedia;
    const across = fromMM(media.width_mm, doc.units);
    const along = media.die_cut && media.length_mm ? fromMM(media.length_mm, doc.units) : Math.max(doc.width, doc.height, across);
    const [long, short] = [Math.max(across, along), Math.min(across, along)];
    onchange({ ...(upright ? resizeLabel(doc, short, long) : resizeLabel(doc, long, short)), ...(dpi ? { dpi } : {}) });
  }

  /** Portrait or landscape: the template turned to it, a quarter turn, with everything on it. */
  function setOrientation(to: 'portrait' | 'landscape') {
    if (orientation(doc) === to) return;
    onchange(turnTemplate(doc, to === 'portrait'));
  }

  // The fit check, re-run (debounced) as the template changes; an answer
  // for an earlier printer or template, arriving late, is dropped.
  let checking = 0;
  $effect(() => {
    const id = printerId;
    JSON.stringify(doc);
    const mine = ++checking;
    fit = null;
    fitError = '';
    if (!id) return;
    const timer = setTimeout(async () => {
      try {
        const answer = await check(id);
        if (mine === checking) fit = answer;
      } catch (err) {
        if (mine === checking) fitError = messageOf(err);
      }
    }, 300);
    return () => clearTimeout(timer);
  });

  // Where it prints, in the template's units, for the canvas.
  $effect(() => {
    const f = fit;
    const units = doc.units;
    if (!f?.fits || !f.printable) {
      onfit?.(null);
      return;
    }
    const u = (v: number) => fromMM(v, units);
    onfit?.({
      printable: { x: u(f.printable.x), y: u(f.printable.y), width: u(f.printable.width), height: u(f.printable.height) },
      edge: u(f.edge_mm ?? 0),
      outside: f.outside ?? [],
      nearEdge: f.near_edge ?? [],
    });
  });

  const named = (indices: number[] | undefined) =>
    (indices ?? []).filter((i) => doc.elements[i]).map((i) => describeElement(doc.elements[i]!, i)).join(', ');

  const fieldError = (key: string) => problems[key];

  /** Which way the label goes through the printer, said after the fit. */
  const fedText = (f: FitAnswer) => (f.feed && doc.sizing !== 'adaptive' ? ` Fed with its width ${f.feed} the roll.` : '');

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
      <label class="label" for="tpl-dpi" title="Dots per inch: the printer's resolution">DPI</label>
      <input id="tpl-dpi" class="input" type="number" min="150" max="600" step="1" value={doc.dpi} onchange={(e) => setNumber('dpi', e.currentTarget.value)} />
    </div>
  </div>
  <p class="-mt-2 text-xs text-zinc-600 dark:text-zinc-400">DPI is the printer's resolution, in dots per inch; <strong>Use its label size</strong>, below, sets it from a printer.</p>
  <div class="flex flex-wrap items-center gap-3">
    <fieldset class="flex flex-wrap items-center gap-3 text-sm">
      <legend class="sr-only">Orientation</legend>
      <span class="label" aria-hidden="true">Orientation</span>
      <label class="flex items-center gap-1">
        <input type="radio" name="tpl-orientation" checked={orientation(doc) === 'landscape'} onchange={() => setOrientation('landscape')} /> Landscape
      </label>
      <label class="flex items-center gap-1">
        <input type="radio" name="tpl-orientation" checked={orientation(doc) === 'portrait'} onchange={() => setOrientation('portrait')} /> Portrait
      </label>
    </fieldset>
    <div class="flex gap-1">
      <button type="button" class="btn-secondary" onclick={() => onchange(turnTemplate(doc, false))}>Turn left</button>
      <button type="button" class="btn-secondary" onclick={() => onchange(turnTemplate(doc, true))}>Turn right</button>
    </div>
  </div>
  <p class="-mt-2 text-xs text-zinc-600 dark:text-zinc-400">
    {orientation(doc) === 'square' ? 'Square. ' : ''}Changing it turns the label a quarter turn, with everything on it, so the design reads the same, turned.
  </p>
  {#each ['width', 'height', 'dpi', 'units'] as key (key)}
    {#if fieldError(key)}<p class="error-text">{fieldError(key)}</p>{/if}
  {/each}

  <fieldset class="space-y-3">
    <legend class="label">Sizing</legend>
    <div class="flex flex-wrap gap-4 text-sm">
      <label class="flex items-center gap-2">
        <input type="radio" name="tpl-sizing" checked={doc.sizing !== 'adaptive'} onchange={() => onchange(makeFixed(doc))} />
        One label size: prints only at this size and DPI
      </label>
      <label class="flex items-center gap-2">
        <input type="radio" name="tpl-sizing" checked={doc.sizing === 'adaptive'} onchange={() => onchange(makeAdaptive(doc))} />
        Fits several label sizes: laid out again for each label it prints on
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
      <label class="flex items-center gap-2 text-sm">
        <input type="checkbox" checked={doc.turnable ?? false} onchange={(e) => update({ turnable: e.currentTarget.checked || undefined })} />
        Also takes labels turned a quarter turn, its content turned to match
      </label>
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
        <select id="tpl-printer" class="input" value={printerId} onchange={(e) => choose(e.currentTarget.value)}>
          <option value="">None</option>
          {#each printers as p (p.id)}<option value={p.id}>{p.name}</option>{/each}
        </select>
      </div>
      <button type="button" class="btn-secondary" disabled={!printerMedia} onclick={() => sizeFromPrinter()}>Use its label size</button>
      <button type="button" class="btn-secondary" disabled={!printerMedia} onclick={() => sizeFromPrinter(true)}>Use it upright</button>
      <div>
        <label class="label" for="tpl-feed">Feed</label>
        <select id="tpl-feed" class="input" value={doc.feed ?? 'auto'} onchange={(e) => update({ feed: e.currentTarget.value === 'auto' ? undefined : (e.currentTarget.value as Feed) })}>
          <option value="auto">As it fits</option>
          <option value="across">Its width across the roll</option>
          <option value="along">Its width along the roll</option>
        </select>
      </div>
      {#if canTest}
        <button type="button" class="btn-secondary" disabled={!printerId || testing || fit?.fits === false} onclick={testPrint}>Test print</button>
      {/if}
    </div>
    {#if printerMedia}
      <p class="text-sm text-zinc-600 dark:text-zinc-400">Loaded: {printerMedia.media.name}{printerMedia.dpi ? `, ${printerMedia.dpi} DPI` : ''}.</p>
    {/if}
    <div aria-live="polite" class="space-y-1">
      {#if fit?.fits && fit.warnings?.length && doc.sizing !== 'adaptive'}
        {@const n = fit.warnings.length}
        <p class="text-sm text-amber-900 dark:text-amber-200">Fits {chosenPrinter?.name}, with {n} warning{n === 1 ? '' : 's'}.{fedText(fit)}</p>
        <FitWarnings warnings={fit.warnings} />
        {#if named(fit.outside)}<p class="text-sm text-amber-900 dark:text-amber-200">In the shaded area, which it does not print: {named(fit.outside)}.</p>{/if}
        {#if named(fit.near_edge)}<p class="text-sm text-amber-900 dark:text-amber-200">Near the edge: {named(fit.near_edge)}.</p>{/if}
      {:else if fit?.fits}
        <p class="text-sm text-green-700 dark:text-green-400">Fits {chosenPrinter?.name}.{fedText(fit)}</p>
        <FitWarnings warnings={fit.warnings} label="Laid out there" />
      {:else if fit}
        <p class="error-text">{fit.message}</p>
      {:else if fitError}
        <p class="error-text">{fitError}</p>
      {/if}
      {#if testStatus}<p class="text-sm text-green-700 dark:text-green-400" role="status">{testStatus}</p>{/if}
      <FitWarnings warnings={testWarnings} label="Printed, with warnings" />
      <ErrorMessage error={testError} />
    </div>
    {#if canTest}
      <p class="text-xs text-zinc-600 dark:text-zinc-400">
        Test print prints one label as it is here, saved or not, {assetId ? 'for the asset being previewed' : 'with placeholder data'}.
      </p>
    {/if}
  {/if}
</section>

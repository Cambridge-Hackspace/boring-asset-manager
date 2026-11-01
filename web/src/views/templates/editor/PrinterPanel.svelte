<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../../../lib/api';
  import { messageOf } from '../../../lib/errors';
  import type { DriverInfo, Printer } from '../../../lib/models';
  import { session } from '../../../lib/session.svelte';
  import { bounds, describeElement, fromMM, resizeLabel, toMM, type Feed, type PrintArea, type TemplateDoc } from '../../../lib/template';
  import ErrorMessage from '../../../components/ErrorMessage.svelte';
  import FitWarnings from '../../../components/FitWarnings.svelte';
  import { useEditor } from './editor.svelte';

  // A printer to design for: the template checked against it as it
  // changes (docs/printing.md), against the printer last used with it or the
  // first that takes it; its label size taken from it; a test label printed
  // there; and which way labels go through it.
  let { assetId, onfit }: { assetId?: string; onfit: (area: PrintArea | null) => void } = $props();

  const editor = useEditor();
  const doc = $derived(editor.doc);

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
  let shade = $state(remembered());

  function remembered(): boolean {
    try {
      return localStorage.getItem('bam.editor-printer-limits') !== 'off';
    } catch {
      return true;
    }
  }

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

  const chosenPrinter = $derived(printers.find((p) => p.id === printerId));
  const canTest = $derived(session.can('labels.print'));

  /** The chosen printer's media and resolution. */
  const printerMedia = $derived.by(() => {
    const p = printers.find((p) => p.id === printerId);
    if (!p) return null;
    const d = drivers.find((d) => d.driver === p.driver);
    const media = p.loaded ?? d?.media.find((m) => m.id === p.media);
    return media ? { media, dpi: d?.model_dpi?.[p.model] } : null;
  });

  // The label size from the printer: the longer side across, so labels
  // read along the tape, or upright; continuous rolls keep the current
  // length. The elements scale with it, so they stay on the label.
  function sizeFromPrinter(upright = false) {
    if (!printerMedia) return;
    const { media, dpi } = printerMedia;
    const across = fromMM(media.width_mm, doc.units);
    const along = media.die_cut && media.length_mm ? fromMM(media.length_mm, doc.units) : Math.max(doc.width, doc.height, across);
    const [long, short] = [Math.max(across, along), Math.min(across, along)];
    editor.apply({ ...(upright ? resizeLabel(doc, short, long) : resizeLabel(doc, long, short)), ...(dpi ? { dpi } : {}) });
  }

  function setFeed(feed: Feed) {
    const next = { ...$state.snapshot(editor.doc), feed: feed === 'auto' ? undefined : feed } as TemplateDoc;
    if (!next.feed) delete next.feed;
    editor.apply(next);
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

  // Where it prints, in the template's units, for the canvas to shade the rest.
  $effect(() => {
    const f = fit;
    const units = doc.units;
    if (!shade || !f?.fits || !f.printable) {
      onfit(null);
      return;
    }
    const u = (v: number) => fromMM(v, units);
    onfit({
      printable: { x: u(f.printable.x), y: u(f.printable.y), width: u(f.printable.width), height: u(f.printable.height) },
      edge: u(f.edge_mm ?? 0),
      outside: f.outside ?? [],
      nearEdge: f.near_edge ?? [],
    });
  });

  const named = (indices: number[] | undefined) =>
    (indices ?? []).filter((i) => doc.elements[i]).map((i) => describeElement(doc.elements[i]!, i)).join(', ');
  const fedText = (f: FitAnswer) => (f.feed && doc.sizing !== 'adaptive' ? ` Fed with its width ${f.feed} the roll.` : '');
</script>

{#if printers.length}
  <div class="space-y-2">
    <label class="flex items-center gap-1 text-xs" for="tpl-printer">
      <span class="text-zinc-600 dark:text-zinc-400">Printer</span>
      <select id="tpl-printer" class="input-sm" value={printerId} onchange={(e) => choose(e.currentTarget.value)}>
        <option value="">None</option>
        {#each printers as p (p.id)}<option value={p.id}>{p.name}</option>{/each}
      </select>
    </label>
    {#if printerMedia}
      <p class="text-xs text-zinc-600 dark:text-zinc-400">Loaded: {printerMedia.media.name}{printerMedia.dpi ? `, ${printerMedia.dpi} DPI` : ''}.</p>
    {/if}
    <div class="flex flex-wrap gap-1">
      <button type="button" class="btn-sm" disabled={!printerMedia} onclick={() => sizeFromPrinter()}>Use its label size</button>
      <button type="button" class="btn-sm" disabled={!printerMedia} onclick={() => sizeFromPrinter(true)}>Use it upright</button>
      {#if canTest}
        <button type="button" class="btn-sm" disabled={!printerId || testing || fit?.fits === false} onclick={testPrint} title="Prints one label as it is here, saved or not, {assetId ? 'for the asset being previewed' : 'with placeholder data'}">Test print</button>
      {/if}
    </div>
    <label class="flex items-center gap-1 text-xs" for="tpl-feed">
      <span class="text-zinc-600 dark:text-zinc-400">Feed</span>
      <select id="tpl-feed" class="input-sm" value={doc.feed ?? 'auto'} onchange={(e) => setFeed(e.currentTarget.value as Feed)}>
        <option value="auto">As it fits</option>
        <option value="across">Its width across the roll</option>
        <option value="along">Its width along the roll</option>
      </select>
    </label>
    <label class="flex min-h-8 items-center gap-1 text-xs">
      <input
        type="checkbox"
        checked={shade}
        onchange={(e) => {
          shade = e.currentTarget.checked;
          try {
            localStorage.setItem('bam.editor-printer-limits', shade ? 'on' : 'off');
          } catch {
            // Storage unavailable: it lasts as long as the page.
          }
        }}
      /> Shade what it does not print
    </label>
    <div aria-live="polite" class="space-y-1 text-xs">
      {#if fit?.fits && fit.warnings?.length && doc.sizing !== 'adaptive'}
        {@const n = fit.warnings.length}
        <p class="text-amber-900 dark:text-amber-200">Fits {chosenPrinter?.name}, with {n} warning{n === 1 ? '' : 's'}.{fedText(fit)}</p>
        <FitWarnings warnings={fit.warnings} />
        {#if named(fit.outside)}<p class="text-amber-900 dark:text-amber-200">In the shaded area, which it does not print: {named(fit.outside)}.</p>{/if}
        {#if named(fit.near_edge)}<p class="text-amber-900 dark:text-amber-200">Near the edge: {named(fit.near_edge)}.</p>{/if}
      {:else if fit?.fits}
        <p class="text-green-700 dark:text-green-400">Fits {chosenPrinter?.name}.{fedText(fit)}</p>
        <FitWarnings warnings={fit.warnings} label="Laid out there" />
      {:else if fit}
        <p class="error-text text-xs">{fit.message}</p>
      {:else if fitError}
        <p class="error-text text-xs">{fitError}</p>
      {/if}
      {#if testStatus}<p class="text-green-700 dark:text-green-400" role="status">{testStatus}</p>{/if}
      <FitWarnings warnings={testWarnings} label="Printed, with warnings" />
      <ErrorMessage error={testError} />
    </div>
  </div>
{:else}
  <p class="text-xs text-zinc-600 dark:text-zinc-400">No printers are set up, or you may not see them. Sizes are entered by hand.</p>
{/if}

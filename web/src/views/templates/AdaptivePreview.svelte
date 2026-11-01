<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { request, requestBlob } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { Printer } from '../../lib/models';
  import { describeElement, type TemplateDoc } from '../../lib/template';

  // An adaptive template laid out as each printer would print it, and at a
  // size typed in, with the sizes it takes in its own shape (docs/labels.md).
  let { doc }: { doc: TemplateDoc } = $props();

  interface Laid {
    fits: boolean;
    template?: TemplateDoc;
    notices?: string[];
    problem?: { path?: string; message: string };
  }
  interface Shown {
    label: string;
    result?: Laid;
    url?: string;
    error?: string;
  }
  interface Span {
    fits: boolean;
    units: string;
    smallest?: { width: number; height: number };
    largest?: { width: number; height: number };
    smaller?: string;
    larger?: string;
    problem?: { path?: string; message: string };
  }

  let printers = $state<Printer[]>([]);
  let shown = $state<Shown[]>([]);
  let span = $state<Span | null>(null);
  let width = $state<number | null>(null);
  let height = $state<number | null>(null);
  let dpi = $state<number | null>(null);
  let custom = $state<Shown | null>(null);
  let urls: string[] = [];

  onMount(async () => {
    try {
      printers = (await request<{ printers: Printer[] }>('GET', '/printers')).printers ?? [];
    } catch {
      // Without access to printers, only sizes typed in are shown.
    }
  });
  onDestroy(() => urls.forEach((u) => URL.revokeObjectURL(u)));

  /** Names the elements a message mentions as the editor does: "Text 2" rather than elements[1]. */
  function named(text: string, d: TemplateDoc): string {
    return text.replace(/elements\[(\d+)\](?: \([^)]*\))?/g, (whole, i: string) => {
      const e = d.elements[Number(i)];
      return e ? describeElement(e, Number(i)) : whole;
    });
  }

  const body = (d: TemplateDoc) => ({ ...JSON.parse(JSON.stringify(d)), id: d.id || 'preview' }) as TemplateDoc;

  /** Lays the template out, as asked, and renders it when it fits. */
  async function lay(label: string, d: TemplateDoc, ask: Record<string, unknown>): Promise<Shown> {
    try {
      const result = await request<Laid>('POST', '/templates/adapt', { template: body(d), ...ask });
      if (!result.fits || !result.template) return { label, result };
      const url = URL.createObjectURL(await requestBlob('POST', '/templates/preview', { template: result.template }));
      urls.push(url);
      return { label, result, url };
    } catch (err) {
      return { label, error: messageOf(err) };
    }
  }

  // Ask again shortly after each change; a later change wins.
  let round = 0;
  $effect(() => {
    const d = $state.snapshot(doc) as TemplateDoc;
    const list = printers;
    const mine = ++round;
    const timer = setTimeout(async () => {
      const [r, laid] = await Promise.all([
        request<Span>('POST', '/templates/range', { template: body(d) }).catch((err) => ({ fits: false, units: d.units, problem: { message: messageOf(err) } })),
        Promise.all(list.map((p) => lay(`${p.name}${p.loaded ? `, ${p.loaded.name}` : ''}`, d, { printer_id: p.id }))),
      ]);
      if (mine !== round) return;
      span = r;
      shown = laid;
    }, 600);
    return () => clearTimeout(timer);
  });

  async function showSize(event: SubmitEvent) {
    event.preventDefault();
    if (!width || !height) return;
    const d = $state.snapshot(doc) as TemplateDoc;
    custom = await lay(`${width} × ${height} ${d.units} at ${dpi || d.dpi} DPI`, d, { width, height, dpi: dpi || d.dpi });
  }

  const below = $derived(
    span?.fits && span.smallest && doc.adapt && (doc.adapt.min_width < span.smallest.width - 0.01 || doc.adapt.min_height < span.smallest.height - 0.01),
  );
</script>

{#snippet result(s: Shown)}
  <li class="space-y-1">
    <p class="text-sm font-medium">{s.label}</p>
    {#if s.error}
      <p class="error-text">{s.error}</p>
    {:else if s.result && !s.result.fits}
      <p class="error-text">Does not fit: {named(s.result.problem?.path ? `${s.result.problem.path}: ${s.result.problem.message}` : (s.result.problem?.message ?? ''), doc)}</p>
    {:else if s.result?.template}
      <p class="text-xs text-zinc-600 dark:text-zinc-400">{s.result.template.width.toFixed(1)} × {s.result.template.height.toFixed(1)} {s.result.template.units} at {s.result.template.dpi} DPI</p>
      {#if s.url}<img src={s.url} alt="Preview at {s.label}" class="max-h-40 border border-zinc-300 bg-white dark:border-zinc-700" />{/if}
      {#if s.result.notices?.length}
        <ul class="space-y-1 text-sm text-amber-900 dark:text-amber-200">
          {#each s.result.notices as n (n)}<li><span aria-hidden="true">⚠ </span>{named(n, doc)}</li>{/each}
        </ul>
      {/if}
    {/if}
  </li>
{/snippet}

<section class="card space-y-3" aria-labelledby="adaptive-heading">
  <h2 id="adaptive-heading" class="text-lg font-semibold">On other labels</h2>
  <div aria-live="polite">
    {#if span?.fits && span.smallest && span.largest}
      <p class="text-sm">
        In its shape it fits labels from {span.smallest.width} × {span.smallest.height} to {span.largest.width} × {span.largest.height}
        {span.units}, with the longest codes assigned now.
      </p>
      {#if span.smaller}<p class="text-sm text-zinc-600 dark:text-zinc-400">Smaller: {named(span.smaller, doc)}</p>{/if}
      {#if span.larger}<p class="text-sm text-zinc-600 dark:text-zinc-400">Larger: {named(span.larger, doc)}</p>{/if}
      {#if below}
        <p class="text-sm text-amber-900 dark:text-amber-200"><span aria-hidden="true">⚠ </span>Its range goes smaller than it fits in its shape.</p>
      {/if}
    {:else if span?.problem}
      <p class="error-text">Its own size does not fit: {named(span.problem.path ? `${span.problem.path}: ${span.problem.message}` : span.problem.message, doc)}</p>
    {/if}
  </div>

  {#if shown.length}
    <ul class="space-y-3" aria-label="On each printer">
      {#each shown as s (s.label)}{@render result(s)}{/each}
    </ul>
  {/if}

  <form class="flex flex-wrap items-end gap-2" onsubmit={showSize}>
    <div>
      <label class="text-xs" for="adapt-width">Width ({doc.units})</label>
      <input id="adapt-width" class="input w-24" type="number" min="0" step="any" required bind:value={width} />
    </div>
    <div>
      <label class="text-xs" for="adapt-height">Height ({doc.units})</label>
      <input id="adapt-height" class="input w-24" type="number" min="0" step="any" required bind:value={height} />
    </div>
    <div>
      <label class="text-xs" for="adapt-dpi">DPI</label>
      <input id="adapt-dpi" class="input w-24" type="number" min="150" max="600" step="1" placeholder={String(doc.dpi)} bind:value={dpi} />
    </div>
    <button type="submit" class="btn-secondary">Show this size</button>
  </form>
  {#if custom}<ul aria-label="At the size typed in">{@render result(custom)}</ul>{/if}
</section>

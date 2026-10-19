<script lang="ts">
  import { onDestroy, onMount, untrack } from 'svelte';
  import { ApiError, request, requestBlob, type ProblemDetail } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import { History } from '../../lib/history';
  import type { AssetWithDetails, Field, GeneratorInfo } from '../../lib/models';
  import { navigate } from '../../lib/router.svelte';
  import {
    blankTemplate,
    elementIndex,
    inListOrder,
    moveLayer,
    newElement,
    newShape,
    placeholders,
    restack,
    rotateElements,
    arrange,
    clip,
    flipElements,
    newGroupName,
    unclip,
    withGroups,
    gridStep,
    type Alignment,
    type Guides,
    type CodeLayout,
    type ElementType,
    type Restack,
    type ShapeKind,
    type PrintArea,
    type TemplateDoc,
    type TemplateElement,
  } from '../../lib/template';
  import ErrorMessage from '../../components/ErrorMessage.svelte';
  import LabelActions from '../../components/LabelActions.svelte';
  import CanvasPanel from './CanvasPanel.svelte';
  import EditorCanvas from './EditorCanvas.svelte';
  import ElementPanel from './ElementPanel.svelte';
  import AdaptivePreview from './AdaptivePreview.svelte';
  import LayersPanel from './LayersPanel.svelte';
  import StackButtons from './StackButtons.svelte';
  import ShortcutsDialog from './ShortcutsDialog.svelte';

  // /templates/new and /templates/:id/edit
  let { params }: { params: Record<string, string> } = $props();
  const routeId = untrack(() => params.id ?? '');
  const isNew = !routeId;
  const draftKey = `bam.template-draft.${routeId || 'new'}`;

  let doc = $state<TemplateDoc>(blankTemplate());
  let saved = $state(''); // the last saved document, as JSON
  let selected = $state<number[]>([]);
  let generators = $state<GeneratorInfo[]>([]);
  let fields = $state<Field[]>([]);
  let loading = $state(true);
  let error = $state('');
  let status = $state('');
  let problems = $state<ProblemDetail[]>([]);
  let saving = $state(false);
  let draft = $state<{ doc: TemplateDoc; at: string } | null>(null);

  // The server preview.
  let previewURL = $state('');
  let previewStale = $state(false);
  let previewProblems = $state<ProblemDetail[]>([]);
  // How the server laid out each code, by element index, for its margins.
  let codeLayouts = $state<Record<number, CodeLayout>>({});

  // Previewing against an asset.
  let assetQuery = $state('');
  let assetResults = $state<AssetWithDetails[]>([]);
  let previewAsset = $state<{ id: string; name: string } | null>(null);

  // Where the printer chosen in the label settings prints.
  let printArea = $state<PrintArea | null>(null);

  const history = new History<TemplateDoc>();
  let canUndo = $state(false);
  let canRedo = $state(false);

  const dirty = $derived(JSON.stringify(doc) !== saved);
  const placeholderList = $derived(placeholders(fields.map((f) => f.key), generators.map((g) => g.type)));
  const allProblems = $derived([...problems, ...previewProblems]);
  const problemElements = $derived(new Set(allProblems.map((p) => elementIndex(p.path)).filter((i): i is number => i !== null)));
  const canvasProblems = $derived(
    Object.fromEntries(allProblems.filter((p) => elementIndex(p.path) === null && p.path).map((p) => [p.path, p.message])),
  );
  const generalProblems = $derived(allProblems.filter((p) => !p.path));

  onMount(async () => {
    try {
      [generators, fields] = await Promise.all([
        request<{ generators: GeneratorInfo[] }>('GET', '/generators').then((r) => r.generators ?? []),
        request<{ fields: Field[] }>('GET', '/fields').then((r) => r.fields ?? []),
      ]);
      if (!isNew) {
        const t = (await request<{ template: TemplateDoc }>('GET', `/templates/${encodeURIComponent(routeId)}`)).template;
        doc = inListOrder({
          id: t.id, name: t.name, units: t.units, width: t.width, height: t.height, dpi: t.dpi,
          ...(t.sizing === 'adaptive' ? { sizing: t.sizing, adapt: t.adapt } : {}),
          ...(t.layering ? { layering: t.layering } : {}),
          ...(t.feed ? { feed: t.feed } : {}),
          ...(t.guides ? { guides: t.guides } : {}),
          ...(t.turnable ? { turnable: true } : {}),
          elements: t.elements ?? [],
        });
      }
      saved = JSON.stringify(doc);
      draft = readDraft();
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
    window.addEventListener('beforeunload', warnUnsaved);
  });

  onDestroy(() => {
    window.removeEventListener('beforeunload', warnUnsaved);
    if (previewURL) URL.revokeObjectURL(previewURL);
  });

  function warnUnsaved(event: BeforeUnloadEvent) {
    if (dirty) event.preventDefault();
  }

  // Unsaved work is kept in this browser, in case the tab closes.
  function readDraft(): { doc: TemplateDoc; at: string } | null {
    try {
      const raw = localStorage.getItem(draftKey);
      if (!raw) return null;
      const d = JSON.parse(raw) as { doc: TemplateDoc; at: string };
      return JSON.stringify(d.doc) === saved ? null : d;
    } catch {
      return null;
    }
  }
  $effect(() => {
    const json = JSON.stringify(doc);
    if (loading || draft) return;
    try {
      if (json === saved) localStorage.removeItem(draftKey);
      else localStorage.setItem(draftKey, JSON.stringify({ doc: JSON.parse(json), at: new Date().toISOString() }));
    } catch {
      // Storage unavailable: nothing to keep.
    }
  });

  function restoreDraft() {
    if (!draft) return;
    history.record($state.snapshot(doc));
    // Kept before layering, it is listed as it was drawn.
    doc = inListOrder(draft.doc);
    draft = null;
    syncHistory();
  }

  function discardDraft() {
    draft = null;
    try {
      localStorage.removeItem(draftKey);
    } catch {
      // ignore
    }
  }

  // The server renders the preview; ask again shortly after each change.
  // A later change wins: an answer for an earlier one, arriving after it,
  // is dropped rather than shown over the newer preview.
  let previewRound = 0;
  $effect(() => {
    const body = { template: JSON.parse(JSON.stringify(doc)) as TemplateDoc, asset_id: previewAsset?.id };
    if (loading) return;
    if (!body.template.id) body.template.id = 'preview';
    const mine = ++previewRound;
    previewStale = true;
    const timer = setTimeout(async () => {
      try {
        const hasCodes = body.template.elements.some((e) => e.type === 'generator');
        const [blob, described] = await Promise.all([
          requestBlob('POST', '/templates/preview', body),
          hasCodes
            ? request<{ elements: { index: number; code?: CodeLayout }[] }>('POST', '/templates/preview', { ...body, format: 'json' })
            : Promise.resolve({ elements: [] }),
        ]);
        if (mine !== previewRound) return;
        const url = URL.createObjectURL(blob);
        if (previewURL) URL.revokeObjectURL(previewURL);
        previewURL = url;
        previewProblems = [];
        codeLayouts = Object.fromEntries((described.elements ?? []).flatMap((d) => (d.code ? [[d.index, d.code]] : [])));
      } catch (err) {
        if (mine !== previewRound) return;
        previewProblems =
          err instanceof ApiError && err.details.length ? err.details : [{ path: '', message: messageOf(err) }];
      } finally {
        if (mine === previewRound) previewStale = false;
      }
    }, 250);
    return () => clearTimeout(timer);
  });

  function syncHistory() {
    canUndo = history.canUndo;
    canRedo = history.canRedo;
  }

  /** Marks the start of one undoable change. */
  function begin() {
    history.record($state.snapshot(doc));
    syncHistory();
    status = '';
  }

  function undo() {
    const prev = history.undo($state.snapshot(doc));
    if (prev) {
      doc = prev;
      selected = selected.filter((i) => i < prev.elements.length);
    }
    syncHistory();
  }

  function redo() {
    const next = history.redo($state.snapshot(doc));
    if (next) {
      doc = next;
      selected = selected.filter((i) => i < next.elements.length);
    }
    syncHistory();
  }

  // The shape being drawn on the canvas, if any.
  let tool = $state<ShapeKind | null>(null);
  const tools: [ShapeKind, string][] = [
    ['rectangle', 'Rectangle'],
    ['ellipse', 'Ellipse'],
    ['line', 'Line'],
    ['polygon', 'Polygon'],
  ];

  /** Chooses a shape to draw; from the keyboard, adds one in the middle of the label instead. */
  function chooseTool(event: MouseEvent, kind: ShapeKind) {
    if (event.detail === 0) {
      tool = null;
      addDrawn(newShape(kind, doc));
      return;
    }
    tool = tool === kind ? null : kind;
  }

  function addDrawn(e: TemplateElement) {
    begin();
    doc.elements.push(e);
    selected = [doc.elements.length - 1];
    tool = null;
  }

  // What the last import left out, or why it failed.
  let imported = $state('');

  /**
   * Imports SVG artwork as shapes, fitted into the middle of the label,
   * selected together.
   */
  async function importSVG(file: File) {
    imported = '';
    if (file.size > 1 << 20) {
      imported = `${file.name} is larger than 1 MB.`;
      return;
    }
    try {
      const svg = await file.text();
      const box = { x: doc.width * 0.1, y: doc.height * 0.1, width: doc.width * 0.8, height: doc.height * 0.8 };
      const res = await request<{ elements: TemplateElement[]; skipped: string[] }>('POST', '/templates/import-svg', { svg, box });
      begin();
      const first = doc.elements.length;
      doc.elements.push(...res.elements);
      selected = res.elements.map((_, k) => first + k);
      const n = res.elements.length;
      imported = `Imported ${n} shape${n === 1 ? '' : 's'} from ${file.name}.` + (res.skipped.length ? ` Left out: ${res.skipped.join('; ')}.` : '');
    } catch (err) {
      imported = messageOf(err);
    }
  }

  function addElement(type: ElementType) {
    begin();
    doc.elements.push(newElement(type, doc, generators[0]?.type ?? 'qr'));
    selected = [doc.elements.length - 1];
  }

  function removeSelected() {
    if (!selected.length) return;
    begin();
    doc.elements = doc.elements.filter((_, i) => !selected.includes(i));
    selected = [];
  }

  /** Moves the selected elements over or under the others. */
  function restackSelected(how: Restack) {
    if (!selected.length) return;
    const next = restack($state.snapshot(doc.elements) as TemplateElement[], selected, how);
    if (JSON.stringify(next.elements) === JSON.stringify(doc.elements)) return;
    begin();
    doc.elements = next.elements;
    selected = next.selected;
  }

  /** Turns the selected elements together, about the middle of them all. */
  function turnSelected(delta: number) {
    begin();
    doc.elements = rotateElements(doc, selected, $state.snapshot(doc.elements) as TemplateElement[], delta);
  }

  function moveSelectedLayer(from: number, to: number) {
    begin();
    const next = moveLayer($state.snapshot(doc.elements) as TemplateElement[], selected, from, to);
    doc.elements = next.elements;
    selected = next.selected;
  }

  let shortcuts = $state(false);

  function keydown(event: KeyboardEvent) {
    if (event.target instanceof Element && event.target.closest('input, textarea, select, dialog')) return;
    const mod = event.ctrlKey || event.metaKey;
    const key = event.key.toLowerCase();
    if (mod && key === 'z') {
      event.preventDefault();
      if (event.shiftKey) redo();
      else undo();
    } else if (mod && key === 'y') {
      event.preventDefault();
      redo();
    } else if (mod && key === 'a') {
      event.preventDefault();
      selected = doc.elements.flatMap((e, i) => (e.locked ? [] : [i]));
    } else if (mod && key === 'd' && selected.length) {
      event.preventDefault();
      paste(clip(doc, selected));
    } else if (mod && key === 'g' && selected.length) {
      event.preventDefault();
      if (event.shiftKey) ungroup();
      else group();
    } else if (event.key === '?') {
      event.preventDefault();
      shortcuts = true;
    }
  }

  // The clipboard: elements, as the editor writes them, or SVG artwork.
  function copying(event: ClipboardEvent, cut: boolean) {
    if ((event.target instanceof Element && event.target.closest('input, textarea, select')) || !selected.length || !event.clipboardData) return;
    event.preventDefault();
    event.clipboardData.setData('text/plain', clip(doc, selected));
    if (cut) removeSelected();
  }

  function pasting(event: ClipboardEvent) {
    if (event.target instanceof Element && event.target.closest('input, textarea, select')) return;
    const text = event.clipboardData?.getData('text/plain') ?? '';
    if (!text) return;
    event.preventDefault();
    if (/<svg[\s>]/.test(text)) {
      void importSVG(new File([text], 'the clipboard', { type: 'image/svg+xml' }));
      return;
    }
    paste(text);
  }

  /** Adds elements from the clipboard's text, a little way from where they were, selected. */
  function paste(text: string) {
    const elements = unclip(doc, text, gridStep(doc.units) * 4);
    if (!elements?.length) return;
    begin();
    const first = doc.elements.length;
    doc.elements.push(...elements);
    selected = elements.map((_, k) => first + k);
  }

  /** The selection made one group, moved, resized and turned as one. */
  function group() {
    if (selected.length < 2) return;
    begin();
    const name = newGroupName(doc.elements);
    doc.elements = doc.elements.map((e, i) => (selected.includes(i) ? { ...e, group: name } : e));
  }

  function ungroup() {
    begin();
    doc.elements = doc.elements.map((e, i) => {
      if (!selected.includes(i) || !e.group) return e;
      const { group: _g, ...rest } = e;
      return rest;
    });
  }

  function arrangeSelected(how: { align?: Alignment; distribute?: 'across' | 'down' }) {
    begin();
    doc.elements = arrange(doc, selected, { ...how, toLabel: toLabel || selected.length === 1 });
  }

  function flipSelected(across: boolean) {
    begin();
    doc.elements = flipElements(doc, selected, across);
  }

  function setGuides(guides: Guides) {
    doc.guides = guides.x?.length || guides.y?.length ? { ...(guides.x?.length ? { x: guides.x } : {}), ...(guides.y?.length ? { y: guides.y } : {}) } : undefined;
    if (!doc.guides) delete doc.guides;
  }

  // Aligning with the label, rather than with each other.
  let toLabel = $state(false);
  const alignments: [Alignment, string][] = [
    ['left', 'Left edges'],
    ['center', 'Centers'],
    ['right', 'Right edges'],
    ['top', 'Tops'],
    ['middle', 'Middles'],
    ['bottom', 'Bottoms'],
  ];
  const grouped = $derived(selected.length > 1 && selected.every((i) => doc.elements[i]?.group && doc.elements[i]?.group === doc.elements[selected[0]!]?.group));

  async function save() {
    error = status = '';
    problems = [];
    saving = true;
    const body = $state.snapshot(doc);
    try {
      // A new template is given its ID by the server.
      let id = routeId;
      if (isNew) {
        id = (await request<{ template: TemplateDoc }>('POST', '/templates', body)).template.id;
      } else {
        await request('PUT', `/templates/${encodeURIComponent(routeId)}`, body);
      }
      saved = JSON.stringify(doc);
      try {
        localStorage.removeItem(draftKey);
      } catch {
        // ignore
      }
      if (isNew) navigate(`/templates/${encodeURIComponent(id)}/edit`, true);
      else status = 'Saved.';
    } catch (err) {
      error = messageOf(err);
      if (err instanceof ApiError) problems = err.details;
      const first = problems.map((p) => elementIndex(p.path)).find((i) => i !== null);
      if (first !== undefined && first !== null) selected = [first];
    } finally {
      saving = false;
    }
  }

  async function searchAssets(event: SubmitEvent) {
    event.preventDefault();
    try {
      const q = new URLSearchParams({ search: assetQuery });
      assetResults = ((await request<{ assets: AssetWithDetails[] }>('GET', `/assets?${q}`)).assets ?? []).slice(0, 8);
    } catch (err) {
      error = messageOf(err);
    }
  }

  const problemsFor = (i: number) => allProblems.filter((p) => elementIndex(p.path) === i).map((p) => p.message);
</script>

<svelte:window onkeydown={keydown} oncopy={(ev) => copying(ev, false)} oncut={(ev) => copying(ev, true)} onpaste={pasting} />

<ShortcutsDialog open={shortcuts} onclose={() => (shortcuts = false)} />

<nav aria-label="Breadcrumb" class="mb-2 text-sm"><a class="underline" href="/templates">Templates</a></nav>

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading…</p>
{:else}
  <div class="mb-4 flex flex-wrap items-center justify-between gap-2">
    <h1 class="text-2xl font-bold">{isNew ? 'New template' : doc.name || routeId}{#if dirty}<span class="ml-2 text-sm font-normal text-zinc-600 dark:text-zinc-400">Unsaved changes</span>{/if}</h1>
    <div class="flex flex-wrap gap-2">
      <button type="button" class="btn-secondary" onclick={() => (shortcuts = true)}>Keyboard shortcuts</button>
      <button type="button" class="btn-secondary" onclick={undo} disabled={!canUndo}>Undo</button>
      <button type="button" class="btn-secondary" onclick={redo} disabled={!canRedo}>Redo</button>
      <button type="button" class="btn-primary" onclick={save} disabled={saving || (!dirty && !isNew)}>{saving ? 'Saving…' : 'Save'}</button>
    </div>
  </div>

  {#if draft}
    <div class="mb-4 flex flex-wrap items-center gap-2 rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-sm dark:border-amber-800 dark:bg-amber-950" role="status">
      Unsaved changes from {new Date(draft.at).toLocaleString()} were kept in this browser.
      <button type="button" class="btn-secondary" onclick={restoreDraft}>Restore them</button>
      <button type="button" class="btn-secondary" onclick={discardDraft}>Discard them</button>
    </div>
  {/if}

  <ErrorMessage {error} />
  {#if status}<p class="mb-2 text-sm text-green-700 dark:text-green-400" role="status">{status}</p>{/if}

  <div class="grid gap-6 xl:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
    <div class="space-y-4">
      <div class="flex flex-wrap gap-2" role="toolbar" aria-label="Add element">
        <button type="button" class="btn-secondary" onclick={() => addElement('text')}>Add text</button>
        <button type="button" class="btn-secondary" onclick={() => addElement('generator')}>Add barcode</button>
        <button type="button" class="btn-secondary" onclick={() => addElement('static_image')}>Add image</button>
        <label class="btn-secondary cursor-pointer focus-within:outline-2 focus-within:outline-blue-600">
          Import SVG
          <input
            type="file"
            accept=".svg,image/svg+xml"
            class="sr-only"
            onchange={(ev) => {
              const file = ev.currentTarget.files?.[0];
              ev.currentTarget.value = '';
              if (file) void importSVG(file);
            }}
          />
        </label>
      </div>
      <div class="flex flex-wrap items-center gap-2" role="toolbar" aria-label="Draw a shape">
        {#each tools as [kind, label] (kind)}
          <button type="button" class="btn-secondary" aria-pressed={tool === kind} onclick={(ev) => chooseTool(ev, kind)}>{label}</button>
        {/each}
      </div>
      {#if imported}<p class="text-sm" role="status">{imported}</p>{/if}
      <p class="text-xs text-zinc-600 dark:text-zinc-400" role="status">
        {#if tool === 'polygon'}
          Click to place each point; double-click, press Enter or click the first point to finish. Escape stops.
        {:else if tool}
          Drag on the label to draw it, with Shift to keep it {tool === 'line' ? 'at 45°' : tool === 'ellipse' ? 'round' : 'square'}, or click to place one. Escape stops.
        {:else}
          Choose a shape, then drag on the label to draw it; from the keyboard, the shape is added in the middle of the label.
        {/if}
      </p>

      <EditorCanvas
        {doc}
        {selected}
        {previewURL}
        {previewStale}
        {problemElements}
        area={printArea}
        onselect={(s) => (selected = s)}
        onchange={(els) => (doc.elements = els)}
        onbegin={begin}
        ondelete={removeSelected}
        onrestack={restackSelected}
        {codeLayouts}
        {tool}
        ondraw={addDrawn}
        onfile={importSVG}
        onguides={setGuides}
        ontoolcancel={() => (tool = null)}
      />

      {#if generalProblems.length || (previewProblems.length && !problemElements.size && !Object.keys(canvasProblems).length)}
        <div role="alert" class="space-y-1">
          {#each generalProblems as p (p.message)}<p class="error-text">{p.message}</p>{/each}
        </div>
      {/if}

      {#if doc.elements.length}
        <LayersPanel
          elements={doc.elements}
          {selected}
          {problemElements}
          onselect={(s) => (selected = s)}
          onrestack={restackSelected}
          onmove={moveSelectedLayer}
          onlock={(i) => {
            begin();
            const e = doc.elements[i]!;
            doc.elements[i] = e.locked ? (({ locked: _l, ...rest }) => rest)(e) : { ...e, locked: true };
          }}
        />
      {/if}
    </div>

    <div class="space-y-4">
      {#if selected.length === 1 && doc.elements[selected[0]!]}
        <ElementPanel
          {doc}
          index={selected[0]!}
          {generators}
          {placeholderList}
          problems={problemsFor(selected[0]!)}
          onupdate={(e: TemplateElement) => {
            begin();
            doc.elements[selected[0]!] = e;
          }}
          onremove={removeSelected}
          onrestack={restackSelected}
          onflip={flipSelected}
          codeLayout={codeLayouts[selected[0]!]}
        />
      {:else if selected.length > 1}
        <section class="card space-y-3" aria-labelledby="several-heading">
          <h2 id="several-heading" class="text-lg font-semibold">{grouped ? `Group ${doc.elements[selected[0]!]!.group}` : `${selected.length} elements`}</h2>
          <p class="text-sm">Drag or use the arrow keys to move them together; the box round them scales and turns them as one.</p>
          <StackButtons elements={doc.elements} {selected} onrestack={restackSelected} />
          <div class="flex flex-wrap gap-1">
            {#if grouped}
              <button type="button" class="btn-secondary" onclick={ungroup}>Ungroup</button>
            {:else}
              <button type="button" class="btn-secondary" onclick={group}>Group</button>
            {/if}
            <button type="button" class="btn-secondary" onclick={() => turnSelected(-90)}>Turn them 90° left</button>
            <button type="button" class="btn-secondary" onclick={() => turnSelected(90)}>Turn them 90° right</button>
            <button type="button" class="btn-secondary" onclick={() => flipSelected(true)}>Flip across</button>
            <button type="button" class="btn-secondary" onclick={() => flipSelected(false)}>Flip down</button>
          </div>
          <div class="space-y-1" role="group" aria-label="Line up">
            <p class="label" aria-hidden="true">Line up</p>
            <div class="flex flex-wrap gap-1">
              {#each alignments as [how, label] (how)}
                <button type="button" class="btn-secondary" onclick={() => arrangeSelected({ align: how })}>{label}</button>
              {/each}
            </div>
            <label class="flex items-center gap-2 text-sm"><input type="checkbox" bind:checked={toLabel} /> With the label, not each other</label>
          </div>
          <div class="flex flex-wrap gap-1" role="group" aria-label="Spread out evenly">
            <button type="button" class="btn-secondary" disabled={selected.length < 3} onclick={() => arrangeSelected({ distribute: 'across' })}>Spread out across</button>
            <button type="button" class="btn-secondary" disabled={selected.length < 3} onclick={() => arrangeSelected({ distribute: 'down' })}>Spread out down</button>
          </div>
          <button type="button" class="btn-secondary text-red-700 dark:text-red-400" onclick={removeSelected}>Remove them</button>
        </section>
      {/if}

      <CanvasPanel
        {doc}
        {isNew}
        problems={canvasProblems}
        assetId={previewAsset?.id}
        onfit={(a) => (printArea = a)}
        onchange={(d, record = true) => {
          if (record) begin();
          doc = d;
        }}
      />

      {#if doc.sizing === 'adaptive'}
        <AdaptivePreview {doc} />
      {/if}

      <section class="card space-y-3" aria-labelledby="preview-heading">
        <h2 id="preview-heading" class="text-lg font-semibold">Preview data</h2>
        {#if previewAsset}
          <p class="text-sm">
            Showing <strong>{previewAsset.name}</strong>.
            <button type="button" class="underline" onclick={() => (previewAsset = null)}>Use placeholders</button>
          </p>
        {:else}
          <p class="text-sm text-zinc-600 dark:text-zinc-400">Showing placeholder data. Pick an asset to see real values.</p>
        {/if}
        <form class="flex gap-2" role="search" onsubmit={searchAssets}>
          <label class="sr-only" for="preview-search">Find an asset</label>
          <input id="preview-search" class="input" type="search" placeholder="Name or code" bind:value={assetQuery} />
          <button type="submit" class="btn-secondary">Find</button>
        </form>
        {#if assetResults.length}
          <ul class="text-sm">
            {#each assetResults as a (a.asset.id)}
              <li>
                <button type="button" class="hover:underline" onclick={() => { previewAsset = { id: a.asset.id, name: a.asset.name }; assetResults = []; }}>
                  {a.asset.name}
                </button>
              </li>
            {/each}
          </ul>
        {/if}
      </section>

      {#if previewAsset && !isNew && !dirty}
        {#key previewAsset.id}
          <LabelActions assetIds={[previewAsset.id]} templateId={doc.id} />
        {/key}
      {:else if previewAsset}
        <p class="text-sm text-zinc-600 dark:text-zinc-400">Save the template to export this asset’s label, or print it for several assets; Test print, above, works as it is.</p>
      {/if}
    </div>
  </div>
{/if}

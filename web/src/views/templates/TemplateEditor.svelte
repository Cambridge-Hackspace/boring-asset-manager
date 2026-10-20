<script lang="ts">
  import { onDestroy, onMount, untrack } from 'svelte';
  import { ApiError, request, requestBlob, type ProblemDetail } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import { startLiveRenderer, type Frame, type LiveRenderer, type LiveState, type PreviewData } from '../../lib/live/client';
  import type { Field, GeneratorInfo } from '../../lib/models';
  import { navigate } from '../../lib/router.svelte';
  import { screenKey } from '../../lib/screen';
  import {
    elementIndex,
    gridStep,
    snap,
    inListOrder,
    moveElements,
    newElement,
    newShape,
    placeholders,
    type CodeLayout,
    type PrintArea,
    type TemplateDoc,
    type TemplateElement,
  } from '../../lib/template';
  import ArrangePanel from './editor/ArrangePanel.svelte';
  import Canvas from './editor/Canvas.svelte';
  import Dock from './editor/Dock.svelte';
  import { Editor, provideEditor, type Tool } from './editor/editor.svelte';
  import Icon from './editor/Icon.svelte';
  import LabelPanel from './editor/LabelPanel.svelte';
  import GridPanel from './editor/GridPanel.svelte';
  import HistoryPanel from './editor/HistoryPanel.svelte';
  import LayersPanel from './editor/LayersPanel.svelte';
  import OptionsBar from './editor/OptionsBar.svelte';
  import OverviewPanel from './editor/OverviewPanel.svelte';
  import PreviewPanel from './editor/PreviewPanel.svelte';
  import PrinterPanel from './editor/PrinterPanel.svelte';
  import PropertiesPanel from './editor/PropertiesPanel.svelte';
  import StatusBar from './editor/StatusBar.svelte';
  import Toolbox from './editor/Toolbox.svelte';

  // The template editor (README §4.1), laid out as Krita is: the toolbox on
  // the left, the tool's options across the top, the label in the middle,
  // panels down the right, and the status bar along the bottom. Tab hides
  // everything but the label.
  // /templates/new and /templates/:id/edit
  let { params }: { params: Record<string, string> } = $props();
  const routeId = untrack(() => params.id ?? '');
  const isNew = !routeId;
  const draftKey = `bam.template-draft.${routeId || 'new'}`;

  const editor = provideEditor(new Editor());
  const doc = $derived(editor.doc);

  let saved = $state(''); // the last saved document, as JSON
  let generators = $state<GeneratorInfo[]>([]);
  let fields = $state<Field[]>([]);
  let loading = $state(true);
  let error = $state('');
  let status = $state('');
  let problems = $state<ProblemDetail[]>([]);
  let saving = $state(false);
  let draft = $state<{ doc: TemplateDoc; at: string } | null>(null);
  let notice = $state('');

  // The server's preview, while the live renderer cannot draw.
  let previewURL = $state('');
  let previewStale = $state(false);
  let previewProblems = $state<ProblemDetail[]>([]);
  // How each code was laid out, by element index, for its margins.
  let codeLayouts = $state<Record<number, CodeLayout>>({});
  let previewAsset = $state<{ id: string; name: string } | null>(null);
  // Where the printer chosen prints.
  let printArea = $state<PrintArea | null>(null);

  const dirty = $derived(JSON.stringify(doc) !== saved);
  const placeholderList = $derived(placeholders(fields.map((f) => f.key), generators.map((g) => g.type)));
  const allProblems = $derived([...problems, ...previewProblems]);
  const problemElements = $derived(new Set(allProblems.map((p) => elementIndex(p.path)).filter((i): i is number => i !== null)));
  const labelProblems = $derived(Object.fromEntries(allProblems.filter((p) => elementIndex(p.path) === null && p.path).map((p) => [p.path, p.message])));
  const generalProblems = $derived(allProblems.filter((p) => !p.path));
  const problemsFor = (i: number) => allProblems.filter((p) => elementIndex(p.path) === i).map((p) => p.message);

  onMount(async () => {
    try {
      [generators, fields] = await Promise.all([
        request<{ generators: GeneratorInfo[] }>('GET', '/generators').then((r) => r.generators ?? []),
        request<{ fields: Field[] }>('GET', '/fields').then((r) => r.fields ?? []),
      ]);
      if (generators.length && !generators.some((g) => g.type === editor.codeType)) editor.codeType = generators[0]!.type;
      if (!isNew) {
        const t = (await request<{ template: TemplateDoc }>('GET', `/templates/${encodeURIComponent(routeId)}`)).template;
        editor.load(
          inListOrder({
            id: t.id, name: t.name, units: t.units, width: t.width, height: t.height, dpi: t.dpi,
            ...(t.sizing === 'adaptive' ? { sizing: t.sizing, adapt: t.adapt } : {}),
            ...(t.layering ? { layering: t.layering } : {}),
            ...(t.feed ? { feed: t.feed } : {}),
            ...(t.guides ? { guides: t.guides } : {}),
            ...(t.turnable ? { turnable: true } : {}),
            elements: t.elements ?? [],
          }),
        );
      }
      saved = JSON.stringify(editor.doc);
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
    // Kept before layering, it is listed as it was drawn.
    editor.apply(inListOrder(draft.doc));
    draft = null;
  }

  function discardDraft() {
    draft = null;
    try {
      localStorage.removeItem(draftKey);
    } catch {
      // ignore
    }
  }

  // The live renderer draws the label in the browser, exactly as it will
  // print, at every change (README §4.1). Until it has loaded, or where it
  // cannot, the server renders the preview instead.
  let live: LiveRenderer | null = null;
  let liveState = $state<LiveState>('loading');
  let liveData = $state<PreviewData | null>(null);
  let frame = $state<Frame | null>(null);
  onMount(() => {
    live = startLiveRenderer(async (id) => (await requestBlob('GET', `/images/${encodeURIComponent(id)}`)).arrayBuffer());
    live.onstate = (s) => (liveState = s);
    liveState = live.state;
  });
  onDestroy(() => live?.close());
  const drawingLive = $derived(liveState === 'ready' && liveData !== null);

  // What it draws with: the sample data, or the asset previewed.
  $effect(() => {
    const id = previewAsset?.id;
    liveData = null;
    request<PreviewData>('POST', '/templates/preview-data', id ? { asset_id: id } : {}).then(
      (d) => {
        if (previewAsset?.id === id) liveData = d;
      },
      () => {
        // The server's preview says why.
      },
    );
  });

  // Hidden elements are left out of the label drawn while editing, and
  // what is said of those drawn is said of them by their own indices.
  function drawn(): { template: TemplateDoc; shown: number[] } {
    const template = JSON.parse(JSON.stringify(doc)) as TemplateDoc;
    // Drawn as designed, the right way round and level, as the live renderer draws it.
    delete template.print_mirror;
    delete template.print_rotation;
    const shown = template.elements.map((_, i) => i).filter((i) => !editor.hidden.includes(i) && i !== editor.editingText);
    if (shown.length < template.elements.length) template.elements = shown.map((i) => template.elements[i]!);
    return { template, shown };
  }
  const own = (shown: number[], problems: ProblemDetail[]) =>
    problems.map((p) => ({ ...p, path: p.path.replace(/^elements\[(\d+)\]/, (_, k: string) => `elements[${shown[Number(k)] ?? k}]`) }));
  const ownLayouts = (shown: number[], described: { index: number; code?: unknown }[]) =>
    Object.fromEntries(described.flatMap((d) => (d.code ? [[shown[d.index] ?? d.index, d.code as CodeLayout]] : [])));
  /** The size each text was drawn at, by its own index; kept for one being typed, left out of the drawing. */
  function noteSizes(shown: number[], described: { index: number; drawn_font_size?: number }[]) {
    const sizes: Record<number, number> = {};
    for (const d of described) if (d.drawn_font_size) sizes[shown[d.index] ?? d.index] = d.drawn_font_size;
    const typing = editor.editingText;
    if (typing !== null && editor.drawnSizes[typing] !== undefined && sizes[typing] === undefined) sizes[typing] = editor.drawnSizes[typing]!;
    editor.drawnSizes = sizes;
  }

  $effect(() => {
    const template = JSON.parse(JSON.stringify(doc)) as TemplateDoc;
    // Hidden elements, and text being typed, are left out of the dots; the
    // renderer still says how they were laid out, as typing needs their size.
    const hidden = [...editor.hidden, ...(editor.editingText !== null ? [editor.editingText] : [])];
    const data = liveData;
    if (loading || liveState !== 'ready' || !data || !live) return;
    const all = template.elements.map((_, i) => i);
    // Frames come in the order they were asked for, so each is shown.
    void live.render(template, data, hidden).then((f) => {
      if (!f || liveState !== 'ready') return;
      previewProblems = f.problems;
      if (!f.problems.length) {
        frame = f;
        codeLayouts = ownLayouts(all, f.description?.elements ?? []);
        noteSizes(all, (f.description?.elements ?? []) as { index: number; drawn_font_size?: number }[]);
      }
    });
  });

  // The server renders the preview while the live renderer cannot; ask
  // again shortly after each change. A later change wins: an answer for an
  // earlier one, arriving after it, is dropped.
  let previewRound = 0;
  $effect(() => {
    const { template, shown } = drawn();
    const body = { template, asset_id: previewAsset?.id };
    if (loading || drawingLive) return;
    if (!body.template.id) body.template.id = 'preview';
    const mine = ++previewRound;
    previewStale = true;
    const timer = setTimeout(async () => {
      try {
        const hasCodes = body.template.elements.some((e) => e.type === 'generator' || e.type === 'text');
        const [blob, described] = await Promise.all([
          requestBlob('POST', '/templates/preview', body),
          hasCodes
            ? request<{ elements: { index: number; code?: CodeLayout; drawn_font_size?: number }[] }>('POST', '/templates/preview', { ...body, format: 'json' })
            : Promise.resolve({ elements: [] }),
        ]);
        if (mine !== previewRound || drawingLive) return;
        const url = URL.createObjectURL(blob);
        if (previewURL) URL.revokeObjectURL(previewURL);
        previewURL = url;
        previewProblems = [];
        codeLayouts = ownLayouts(shown, described.elements ?? []);
        noteSizes(shown, described.elements ?? []);
      } catch (err) {
        if (mine !== previewRound) return;
        previewProblems = own(shown, err instanceof ApiError && err.details.length ? err.details : [{ path: '', message: messageOf(err) }]);
      } finally {
        if (mine === previewRound) previewStale = false;
      }
    }, 250);
    return () => clearTimeout(timer);
  });

  /** Adds what a tool adds, in the middle of the label: the toolbox, from the keyboard. */
  function addCentered(tool: Exclude<Tool, 'select'>) {
    let e: TemplateElement;
    if (tool === 'text' || tool === 'code' || tool === 'image') {
      e = newElement(({ text: 'text', code: 'generator', image: 'static_image' } as const)[tool], doc, editor.codeType);
      // On the grid, as everything placed is.
      const step = gridStep(doc.units);
      e = { ...e, x: Math.max(0, snap((doc.width - e.width) / 2, step, doc.units)), y: Math.max(0, snap((doc.height - e.height) / 2, step, doc.units)) };
    } else if (tool === 'pen' || tool === 'measure') {
      // They need a pointer: nothing to add from the keyboard.
      return;
    } else {
      e = newShape(tool, doc);
    }
    editor.add(e);
  }

  /** Imports SVG artwork as shapes, fitted into the middle of the label, selected together. */
  async function importSVG(file: File) {
    notice = '';
    if (file.size > 1 << 20) {
      notice = `${file.name} is larger than 1 MB.`;
      return;
    }
    try {
      const svg = await file.text();
      const box = { x: doc.width * 0.1, y: doc.height * 0.1, width: doc.width * 0.8, height: doc.height * 0.8 };
      const res = await request<{ elements: TemplateElement[]; skipped: string[] }>('POST', '/templates/import-svg', { svg, box });
      editor.add(...res.elements);
      const n = res.elements.length;
      notice = `Imported ${n} shape${n === 1 ? '' : 's'} from ${file.name}.` + (res.skipped.length ? ` Left out: ${res.skipped.join('; ')}.` : '');
    } catch (err) {
      notice = messageOf(err);
    }
  }

  async function save() {
    error = status = '';
    problems = [];
    saving = true;
    const body = $state.snapshot(editor.doc);
    try {
      // A new template is given its ID by the server.
      let id = routeId;
      if (isNew) {
        id = (await request<{ template: TemplateDoc }>('POST', '/templates', body)).template.id;
      } else {
        await request('PUT', `/templates/${encodeURIComponent(routeId)}`, body);
      }
      saved = JSON.stringify(editor.doc);
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
      if (first !== undefined && first !== null) editor.selected = [first];
    } finally {
      saving = false;
    }
  }

  let canvas = $state<ReturnType<typeof Canvas> | null>(null);

  function typing(target: EventTarget | null): boolean {
    return target instanceof Element && Boolean(target.closest('input, textarea, select, [contenteditable]'));
  }

  // The shortcuts: few, and standard (#54).
  function keydown(event: KeyboardEvent) {
    if (loading || typing(event.target)) return;
    const mod = event.ctrlKey || event.metaKey;
    const key = event.key.toLowerCase();
    if (mod && key === 'z') {
      event.preventDefault();
      if (event.shiftKey) editor.redo();
      else editor.undo();
    } else if (mod && key === 'a') {
      event.preventDefault();
      if (event.shiftKey) editor.deselect();
      else editor.selectAll();
    } else if (event.key === 'Escape') {
      if (canvas?.cancel()) event.preventDefault();
      else editor.deselect();
    } else if (event.key === 'Enter' && canvas?.finish()) {
      event.preventDefault();
    } else if ((event.key === 'Delete' || event.key === 'Backspace') && canvas?.deletePoint()) {
      // Editing points: the chosen point comes off.
      event.preventDefault();
    } else if ((event.key === 'Delete' || event.key === 'Backspace') && !mod && editor.selected.length) {
      // As in Krita, Inkscape and Figma: what is selected comes off.
      event.preventDefault();
      editor.remove();
    } else if (event.key === 'Tab' && !mod && !event.shiftKey && !event.altKey && (document.activeElement === document.body || !document.activeElement)) {
      // Tab with nothing focused: everything but the label, hidden or shown.
      event.preventDefault();
      setCanvasOnly(!editor.canvasOnly);
    } else if (!mod && !event.altKey && zoomKey(event.key)) {
      event.preventDefault();
    } else if (event.key.startsWith('Arrow') && editor.selected.length && !mod) {
      const base = gridStep(doc.units);
      const step = event.altKey ? base / 10 : base;
      const by = event.shiftKey ? base * 10 : step;
      const moves: Record<string, [number, number]> = { ArrowLeft: [-by, 0], ArrowRight: [by, 0], ArrowUp: [0, -by], ArrowDown: [0, by] };
      const [dx, dy] = moves[event.key] ?? [0, 0];
      event.preventDefault();
      editor.begin('Move');
      editor.doc.elements = moveElements(doc, editor.selected, editor.elements(), dx, dy, step);
    }
  }

  // The zoom keys, as in Krita: + and - toward the pointer, 1 for 100%, 2
  // to fit the label, 3 to fit its width, and 0 between fitting and the
  // zoom before.
  function zoomKey(key: string): boolean {
    const at = editor.pointer && canvas ? canvas.viewportPoint(editor.pointer.x, editor.pointer.y) : undefined;
    switch (key) {
      case '+':
      case '=':
        editor.zoomStep(true, ...(at ?? []));
        return true;
      case '-':
        editor.zoomStep(false, ...(at ?? []));
        return true;
      case '1':
        editor.setZoom(100, ...(at ?? []));
        return true;
      case '2':
        editor.fit();
        return true;
      case '3':
        editor.fitWidth();
        return true;
      case '0':
        editor.toggleFit();
        return true;
    }
    return false;
  }

  // Print size follows the screen the window is on: moved to another, or
  // its pixel ratio changed, it takes that screen's calibration.
  onMount(() => {
    const update = () => (editor.screen = screenKey());
    let query: MediaQueryList | null = null;
    const watch = () => {
      query?.removeEventListener('change', onChange);
      query = window.matchMedia?.(`(resolution: ${window.devicePixelRatio || 1}dppx)`) ?? null;
      query?.addEventListener('change', onChange);
    };
    const onChange = () => {
      update();
      watch();
    };
    watch();
    window.addEventListener('resize', update);
    return () => {
      window.removeEventListener('resize', update);
      query?.removeEventListener('change', onChange);
    };
  });

  // The clipboard: elements, as the editor writes them, or SVG artwork.
  function copying(event: ClipboardEvent) {
    if (typing(event.target) || !editor.selected.length || !event.clipboardData) return;
    event.preventDefault();
    event.clipboardData.setData('text/plain', editor.copy());
  }
  function pasting(event: ClipboardEvent) {
    if (typing(event.target)) return;
    const text = event.clipboardData?.getData('text/plain') ?? '';
    if (!text) return;
    event.preventDefault();
    if (/<svg[\s>]/.test(text)) void importSVG(new File([text], 'the clipboard', { type: 'image/svg+xml' }));
    else editor.paste(text);
  }

  // Canvas-only mode, and the message saying how to leave it, which fades.
  let showLeaveHint = $state(false);
  let hintTimer: ReturnType<typeof setTimeout> | undefined;
  function setCanvasOnly(on: boolean) {
    editor.canvasOnly = on;
    clearTimeout(hintTimer);
    showLeaveHint = on;
    if (on) hintTimer = setTimeout(() => (showLeaveHint = false), 2500);
  }
  onDestroy(() => clearTimeout(hintTimer));

  const title = $derived(isNew ? 'New template' : doc.name || routeId);
</script>

<svelte:window onkeydown={keydown} oncopy={copying} onpaste={pasting} />

{#if loading}
  <p role="status" class="p-4 text-sm text-zinc-600 dark:text-zinc-400">Loading…</p>
{:else}
  <!-- On a phone, the parts stack and the page scrolls: tools, label, panels. -->
  <div class="flex flex-col bg-white text-zinc-900 md:h-full dark:bg-zinc-950 dark:text-zinc-100">
    <!-- Canvas only hides the rest, keeping it as it was, not removing it. -->
    <div class="contents" hidden={editor.canvasOnly}>
      <div class="flex h-11 shrink-0 items-center gap-2 border-b border-zinc-200 px-2 dark:border-zinc-800">
        <nav aria-label="Breadcrumb"><a class="text-sm hover:underline" href="/templates">Templates</a></nav>
        <span aria-hidden="true" class="text-zinc-400">/</span>
        <h1 class="min-w-0 truncate text-sm font-semibold">{title}</h1>
        {#if dirty}<span class="shrink-0 text-xs text-zinc-600 dark:text-zinc-400">Unsaved changes</span>{/if}
        <div class="ml-auto flex items-center gap-0.5">
          <button type="button" class="icon-btn" aria-label="Undo" title="Undo (Ctrl+Z)" disabled={!editor.canUndo} onclick={() => editor.undo()}><Icon name="undo" size={16} /></button>
          <button type="button" class="icon-btn" aria-label="Redo" title="Redo (Ctrl+Shift+Z)" disabled={!editor.canRedo} onclick={() => editor.redo()}><Icon name="redo" size={16} /></button>
          <button type="button" class="icon-btn" aria-label="Canvas only" title="Hide everything but the label (Tab)" onclick={() => setCanvasOnly(true)}><Icon name="expand" size={16} /></button>
          <button type="button" class="btn-primary ml-2 px-3 py-1" onclick={save} disabled={saving || (!dirty && !isNew)}>{saving ? 'Saving…' : 'Save'}</button>
        </div>
      </div>

      {#if draft || error || status || notice || generalProblems.length}
        <div class="shrink-0 space-y-1 border-b border-zinc-200 px-3 py-1.5 text-xs dark:border-zinc-800">
          {#if draft}
            <div class="flex flex-wrap items-center gap-2 text-amber-900 dark:text-amber-200" role="status">
              Unsaved changes from {new Date(draft.at).toLocaleString()} were kept in this browser.
              <button type="button" class="btn-sm" onclick={restoreDraft}>Restore them</button>
              <button type="button" class="btn-sm" onclick={discardDraft}>Discard them</button>
            </div>
          {/if}
          {#if error}<p class="error-text text-xs" role="alert">{error}</p>{/if}
          {#if status}<p class="text-green-700 dark:text-green-400" role="status">{status}</p>{/if}
          {#if notice}<p role="status">{notice}</p>{/if}
          {#if generalProblems.length}
            <div role="alert">
              {#each generalProblems as p (p.message)}<p class="error-text text-xs">{p.message}</p>{/each}
            </div>
          {/if}
        </div>
      {/if}

      <OptionsBar {generators} {placeholderList} />
    </div>

    <div class="flex min-h-0 flex-1 flex-col md:flex-row">
      <aside hidden={editor.canvasOnly} class="shrink-0 overflow-y-auto border-b border-zinc-200 bg-zinc-50 md:border-r md:border-b-0 dark:border-zinc-800 dark:bg-zinc-900" aria-label="Toolbox">
          <Toolbox onadd={addCentered} onimport={importSVG} />
      </aside>
      <div class="relative h-[55dvh] min-w-0 md:h-auto md:flex-1">
        <Canvas
          bind:this={canvas}
          frame={drawingLive ? frame : null}
          {previewURL}
          {previewStale}
          {problemElements}
          area={printArea}
          generatorType={editor.codeType}
          onfile={importSVG}
          {codeLayouts}
        />
        {#if editor.canvasOnly}
          <button
            type="button"
            class="absolute top-2 right-2 rounded bg-zinc-900/70 px-2 py-1 text-xs text-white transition-opacity duration-500 hover:opacity-100 focus:opacity-100 {showLeaveHint ? 'opacity-100' : 'opacity-0'}"
            onclick={() => setCanvasOnly(false)}>Canvas only. Press Tab, or here, to show everything.</button
          >
        {/if}
      </div>
      <aside hidden={editor.canvasOnly} class="w-full shrink-0 border-t border-zinc-200 md:w-72 md:overflow-y-auto md:border-t-0 md:border-l bg-white dark:border-zinc-800 dark:bg-zinc-950" aria-label="Panels">
          <Dock
            panels={[
              // Layers above Properties, which grows as something is
              // selected, so the list does not move under the pointer.
              { id: 'overview', title: 'Overview', content: overview },
              { id: 'layers', title: 'Layers', content: layers },
              { id: 'properties', title: 'Properties', content: properties },
              { id: 'arrange', title: 'Arrange', content: arrange },
              { id: 'history', title: 'History', content: history },
              { id: 'grid', title: 'Grid and guides', content: gridAndGuides },
              { id: 'label', title: 'Label', content: label },
              { id: 'printer', title: 'Printer', content: printer },
              { id: 'preview', title: 'Preview data', content: preview },
            ]}
          />
      </aside>
    </div>

    <div class="contents" hidden={editor.canvasOnly}><StatusBar {liveState} /></div>
  </div>
{/if}

{#snippet overview()}
  <OverviewPanel frame={drawingLive ? frame : null} {previewURL} />
{/snippet}
{#snippet properties()}
  <PropertiesPanel {generators} {placeholderList} {problemsFor} {codeLayouts} />
{/snippet}
{#snippet layers()}
  <LayersPanel {problemElements} frame={drawingLive ? frame : null} />
{/snippet}
{#snippet gridAndGuides()}
  <GridPanel />
{/snippet}
{#snippet history()}
  <HistoryPanel />
{/snippet}
{#snippet arrange()}
  <ArrangePanel />
{/snippet}
{#snippet label()}
  <LabelPanel {isNew} problems={labelProblems} frame={drawingLive ? frame : null} {previewURL} />
{/snippet}
{#snippet printer()}
  <PrinterPanel assetId={previewAsset?.id} onfit={(a) => (printArea = a)} />
{/snippet}
{#snippet preview()}
  <PreviewPanel bind:asset={previewAsset} {isNew} {dirty} />
{/snippet}

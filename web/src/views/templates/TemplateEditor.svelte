<script lang="ts">
  import { onDestroy, onMount, untrack } from 'svelte';
  import { ApiError, request, requestBlob, type ProblemDetail } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import { History } from '../../lib/history';
  import type { AssetWithDetails, Field, GeneratorInfo } from '../../lib/models';
  import { navigate } from '../../lib/router.svelte';
  import {
    blankTemplate,
    describeElement,
    elementIndex,
    newElement,
    placeholders,
    type ElementType,
    type TemplateDoc,
    type TemplateElement,
  } from '../../lib/template';
  import ErrorMessage from '../../components/ErrorMessage.svelte';
  import LabelActions from '../../components/LabelActions.svelte';
  import CanvasPanel from './CanvasPanel.svelte';
  import EditorCanvas from './EditorCanvas.svelte';
  import ElementPanel from './ElementPanel.svelte';

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

  // Previewing against an asset.
  let assetQuery = $state('');
  let assetResults = $state<AssetWithDetails[]>([]);
  let previewAsset = $state<{ id: string; name: string } | null>(null);

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
        doc = { id: t.id, name: t.name, units: t.units, width: t.width, height: t.height, dpi: t.dpi, elements: t.elements ?? [] };
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
    doc = draft.doc;
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
  $effect(() => {
    const body = { template: JSON.parse(JSON.stringify(doc)) as TemplateDoc, asset_id: previewAsset?.id };
    if (loading) return;
    if (!body.template.id) body.template.id = 'preview';
    previewStale = true;
    const timer = setTimeout(async () => {
      try {
        const blob = await requestBlob('POST', '/templates/preview', body);
        const url = URL.createObjectURL(blob);
        if (previewURL) URL.revokeObjectURL(previewURL);
        previewURL = url;
        previewProblems = [];
      } catch (err) {
        previewProblems =
          err instanceof ApiError && err.details.length ? err.details : [{ path: '', message: messageOf(err) }];
      } finally {
        previewStale = false;
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

  function reorder(delta: number) {
    const [i] = selected;
    if (i === undefined) return;
    const j = i + delta;
    if (j < 0 || j >= doc.elements.length) return;
    begin();
    const els = [...doc.elements];
    [els[i], els[j]] = [els[j]!, els[i]!];
    doc.elements = els;
    selected = [j];
  }

  function keydown(event: KeyboardEvent) {
    const target = event.target as HTMLElement;
    if (target.closest('input, textarea, select')) return;
    const mod = event.ctrlKey || event.metaKey;
    if (mod && event.key.toLowerCase() === 'z') {
      event.preventDefault();
      if (event.shiftKey) redo();
      else undo();
    } else if (mod && event.key.toLowerCase() === 'y') {
      event.preventDefault();
      redo();
    }
  }

  async function save() {
    error = status = '';
    problems = [];
    saving = true;
    const body = $state.snapshot(doc);
    try {
      if (isNew) {
        await request('POST', '/templates', body);
      } else {
        await request('PUT', `/templates/${encodeURIComponent(routeId)}`, body);
      }
      saved = JSON.stringify(doc);
      try {
        localStorage.removeItem(draftKey);
      } catch {
        // ignore
      }
      if (isNew) navigate(`/templates/${encodeURIComponent(body.id)}/edit`, true);
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

<svelte:window onkeydown={keydown} />

<nav aria-label="Breadcrumb" class="mb-2 text-sm"><a class="underline" href="/templates">Templates</a></nav>

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading…</p>
{:else}
  <div class="mb-4 flex flex-wrap items-center justify-between gap-2">
    <h1 class="text-2xl font-bold">{isNew ? 'New template' : doc.name || routeId}{#if dirty}<span class="ml-2 text-sm font-normal text-zinc-600 dark:text-zinc-400">Unsaved changes</span>{/if}</h1>
    <div class="flex flex-wrap gap-2">
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
      </div>

      <EditorCanvas
        {doc}
        {selected}
        {previewURL}
        {previewStale}
        {problemElements}
        onselect={(s) => (selected = s)}
        onchange={(els) => (doc.elements = els)}
        onbegin={begin}
        ondelete={removeSelected}
      />

      {#if generalProblems.length || (previewProblems.length && !problemElements.size && !Object.keys(canvasProblems).length)}
        <div role="alert" class="space-y-1">
          {#each generalProblems as p (p.message)}<p class="error-text">{p.message}</p>{/each}
        </div>
      {/if}

      {#if doc.elements.length}
        <ul class="text-sm" aria-label="Elements">
          {#each doc.elements as e, i (i)}
            <li>
              <button type="button" class="hover:underline {selected.includes(i) ? 'font-semibold' : ''} {problemElements.has(i) ? 'text-red-700 dark:text-red-400' : ''}" onclick={() => (selected = [i])}>
                {describeElement(e, i)}
              </button>
            </li>
          {/each}
        </ul>
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
          onorder={reorder}
        />
      {:else if selected.length > 1}
        <section class="card">
          <p class="text-sm">{selected.length} elements selected. Drag or use the arrow keys to move them together.</p>
          <button type="button" class="btn-secondary mt-2 text-red-700 dark:text-red-400" onclick={removeSelected}>Remove them</button>
        </section>
      {/if}

      <CanvasPanel
        {doc}
        {isNew}
        problems={canvasProblems}
        onchange={(d, record = true) => {
          if (record) begin();
          doc = d;
        }}
      />

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
        <p class="text-sm text-zinc-600 dark:text-zinc-400">Save the template to export or print this asset’s label.</p>
      {/if}
    </div>
  </div>
{/if}

<script lang="ts">
  // Search terms (docs/data-model.md#related-terms): what things are
  // called, which search expands through. A term has a preferred label
  // and other names, and links to broader, narrower and related terms.
  import { onMount } from 'svelte';
  import { request } from '../lib/api';
  import { messageOf } from '../lib/errors';
  import type { Term, TermExpansion, TermImport, TermLink } from '../lib/models';
  import { neighborsOf, parseImport, sourceOf, splitNames, termMatches } from '../lib/terms';
  import ConfirmButton from '../components/ConfirmButton.svelte';
  import ErrorMessage from '../components/ErrorMessage.svelte';

  let { params: _params }: { params: Record<string, string> } = $props();

  type Relation = 'broader' | 'narrower' | 'related';
  const relations: { key: Relation; label: string; hint: string }[] = [
    { key: 'broader', label: 'Broader terms', hint: 'A search for one of these finds this.' },
    { key: 'narrower', label: 'Narrower terms', hint: 'A search for this finds these.' },
    { key: 'related', label: 'Related terms', hint: 'Suggested beside it, not searched.' },
  ];

  let terms = $state<Term[]>([]);
  let links = $state<TermLink[]>([]);
  let imports = $state<TermImport[]>([]);
  let loading = $state(true);
  let error = $state('');
  let status = $state('');
  let filter = $state('');
  // The term being edited: null for none, '' for a new one.
  let editing = $state<string | null>(null);
  let draft = $state({ label: '', alt_labels: '', note: '' });
  let adding = $state<Record<Relation, string>>({ broader: '', narrower: '', related: '' });

  let trial = $state('');
  let tried = $state<{ search: string; expansion: TermExpansion[]; related: string[] } | null>(null);

  let importName = $state('');
  let importFile = $state<File | null>(null);

  const byId = $derived(new Map(terms.map((t) => [t.id, t])));
  const shown = $derived(terms.filter((t) => termMatches(t, filter)));
  const current = $derived(editing ? byId.get(editing) : undefined);
  const near = $derived(editing ? neighborsOf(editing, links) : { broader: [], narrower: [], related: [] });

  async function load() {
    try {
      const out = await request<{ terms: Term[]; links: TermLink[]; imports: TermImport[] }>('GET', '/terms');
      terms = out.terms ?? [];
      links = out.links ?? [];
      imports = out.imports ?? [];
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
  }
  onMount(load);

  function labelOf(id: string): string {
    return byId.get(id)?.label ?? 'a removed term';
  }

  function start(t?: Term) {
    error = status = '';
    editing = t?.id ?? '';
    draft = t ? { label: t.label, alt_labels: t.alt_labels.join('\n'), note: t.note } : { label: '', alt_labels: '', note: '' };
    adding = { broader: '', narrower: '', related: '' };
  }

  async function save(event: SubmitEvent) {
    event.preventDefault();
    error = status = '';
    const body = { label: draft.label, alt_labels: splitNames(draft.alt_labels), note: draft.note };
    try {
      if (editing) {
        await request('PUT', `/terms/${encodeURIComponent(editing)}`, body);
        status = `Saved ${draft.label.trim()}.`;
        editing = null;
      } else {
        // A new term stays open, so that its links can be added.
        const made = await request<{ term: Term }>('POST', '/terms', body);
        status = `Added ${made.term.label}. Link it to other terms below.`;
        editing = made.term.id;
      }
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function remove(t: Term) {
    error = status = '';
    try {
      await request('DELETE', `/terms/${encodeURIComponent(t.id)}`);
      status = `Removed ${t.label}.`;
      if (editing === t.id) editing = null;
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function link(relation: Relation) {
    const other = adding[relation];
    if (!editing || !other) return;
    error = status = '';
    try {
      await request('PUT', `/terms/${encodeURIComponent(editing)}/links/${relation}/${encodeURIComponent(other)}`);
      adding[relation] = '';
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function unlink(relation: Relation, other: string) {
    if (!editing) return;
    error = status = '';
    try {
      await request('DELETE', `/terms/${encodeURIComponent(editing)}/links/${relation}/${encodeURIComponent(other)}`);
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function tryIt(event: SubmitEvent) {
    event.preventDefault();
    if (!trial.trim()) return;
    error = '';
    try {
      tried = await request('GET', `/terms/expand?q=${encodeURIComponent(trial.trim())}`);
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function importTerms(event: SubmitEvent) {
    event.preventDefault();
    if (!importFile) return;
    error = status = '';
    try {
      const fallback = importName.trim() || importFile.name.replace(/\.json$/i, '');
      const parsed = parseImport(await importFile.text(), fallback);
      if (importName.trim()) parsed.name = importName.trim();
      const made = await request<{ import: TermImport }>('POST', '/terms/imports', parsed);
      status = `Imported ${made.import.name}: ${counted(made.import.terms, 'term')} and ${counted(made.import.links, 'link')}.`;
      importName = '';
      importFile = null;
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function removeImport(imp: TermImport) {
    error = status = '';
    try {
      await request('DELETE', `/terms/imports/${encodeURIComponent(imp.id)}`);
      status = `Removed the import ${imp.name}.`;
      editing = null;
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }

  function counted(n: number, word: string): string {
    return n === 1 ? `1 ${word}` : `${n} ${word}s`;
  }

  function linkable(relation: Relation): Term[] {
    const taken = new Set([editing, ...near[relation]]);
    return terms.filter((t) => !taken.has(t.id));
  }

  function summary(t: Term): string {
    const n = neighborsOf(t.id, links);
    const parts: string[] = [];
    if (n.broader.length) parts.push(`Narrower than ${n.broader.map(labelOf).join(', ')}.`);
    if (n.narrower.length) parts.push(`Finds ${n.narrower.map(labelOf).join(', ')}.`);
    if (n.related.length) parts.push(`Related to ${n.related.map(labelOf).join(', ')}.`);
    return parts.join(' ');
  }
</script>

<div class="mb-4 flex flex-wrap items-center justify-between gap-2">
  <h1 class="text-2xl font-bold">Search terms</h1>
  {#if editing === null}<button type="button" class="btn-primary" onclick={() => start()}>New term</button>{/if}
</div>
<p class="mb-4 max-w-2xl text-sm text-zinc-600 dark:text-zinc-400">
  What things are called, so that people find them by what they are: a search for a term also finds its other names, and everything narrower than it. Your own assistant can fill these in for you, through
  BAM!'s MCP server.
</p>
<ErrorMessage {error} />
{#if status}<p class="mb-2 text-sm text-green-700 dark:text-green-400" role="status">{status}</p>{/if}

{#if editing !== null}
  <section class="card mb-6 max-w-2xl space-y-4" aria-labelledby="term-form-heading">
    <form class="space-y-4" onsubmit={save}>
      <h2 id="term-form-heading" class="text-lg font-semibold">{editing ? `Edit ${current?.label ?? 'term'}` : 'New term'}</h2>
      <div>
        <label class="label" for="term-label">Preferred label</label>
        <input id="term-label" class="input" required maxlength="255" placeholder="TPU" bind:value={draft.label} />
      </div>
      <div>
        <label class="label" for="term-alts">Other names, one per line</label>
        <textarea id="term-alts" class="input" rows="3" placeholder="thermoplastic polyurethane" bind:value={draft.alt_labels}></textarea>
      </div>
      <div>
        <label class="label" for="term-note">What it means</label>
        <input id="term-note" class="input" maxlength="4000" bind:value={draft.note} />
      </div>
      {#if current && sourceOf(current, imports)}<p class="text-xs text-zinc-600 dark:text-zinc-400">{sourceOf(current, imports)}.</p>{/if}
      <div class="flex gap-2">
        <button type="submit" class="btn-primary">{editing ? 'Save term' : 'Add term'}</button>
        <button type="button" class="btn-secondary" onclick={() => (editing = null)}>{editing ? 'Done' : 'Cancel'}</button>
      </div>
    </form>
    {#if editing}
      {#each relations as r (r.key)}
        <div class="space-y-2">
          <h3 class="font-semibold">{r.label}</h3>
          <p class="text-xs text-zinc-600 dark:text-zinc-400">{r.hint}</p>
          {#if near[r.key].length}
            <ul class="flex flex-wrap gap-2">
              {#each near[r.key] as other (other)}
                <li class="inline-flex items-center gap-1 rounded border border-zinc-300 px-2 py-1 text-sm dark:border-zinc-600">
                  {labelOf(other)}
                  <button type="button" class="btn-secondary" aria-label="Unlink {labelOf(other)} from {r.label.toLowerCase()}" onclick={() => unlink(r.key, other)}>Unlink</button>
                </li>
              {/each}
            </ul>
          {/if}
          <div class="flex flex-wrap items-end gap-2">
            <select class="input w-auto" aria-label="Add one of the {r.label.toLowerCase()}" bind:value={adding[r.key]}>
              <option value="">Choose a term</option>
              {#each linkable(r.key) as t (t.id)}
                <option value={t.id}>{t.label}</option>
              {/each}
            </select>
            <button type="button" class="btn-secondary" disabled={!adding[r.key]} onclick={() => link(r.key)}>Link</button>
          </div>
        </div>
      {/each}
    {/if}
  </section>
{/if}

<section class="card mb-6 max-w-2xl space-y-2" aria-labelledby="try-heading">
  <h2 id="try-heading" class="text-lg font-semibold">Try a search</h2>
  <form class="flex flex-wrap items-end gap-2" onsubmit={tryIt}>
    <div class="grow">
      <label class="label" for="term-try">Words</label>
      <input id="term-try" class="input" placeholder="flexible filament" bind:value={trial} />
    </div>
    <button type="submit" class="btn-secondary">See what it finds</button>
  </form>
  {#if tried}
    <div aria-live="polite">
      {#if tried.expansion.length}
        <p class="text-sm">A search for “{tried.search}” also looks for:</p>
        <ul class="list-disc pl-5 text-sm" data-testid="expansion">
          {#each tried.expansion as x (x.label)}<li>{x.why}</li>{/each}
        </ul>
      {:else}
        <p class="text-sm">A search for “{tried.search}” looks only for what is typed.</p>
      {/if}
      {#if tried.related.length}<p class="text-sm">Suggests: {tried.related.join(', ')}.</p>{/if}
    </div>
  {/if}
</section>

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading terms…</p>
{:else}
  <div class="mb-3 max-w-sm">
    <label class="label" for="term-filter">Find a term</label>
    <input id="term-filter" class="input" type="search" bind:value={filter} />
  </div>
  {#if terms.length === 0}
    <p class="text-sm text-zinc-600 dark:text-zinc-400">No terms yet.</p>
  {:else}
    <ul class="mb-6 space-y-3" aria-label="Terms">
      {#each shown as t (t.id)}
        <li class="card flex flex-wrap items-start justify-between gap-2">
          <div>
            <h2 class="font-semibold">{t.label}</h2>
            {#if t.alt_labels.length}<p class="text-sm">Also called {t.alt_labels.join(', ')}.</p>{/if}
            {#if t.note}<p class="text-sm text-zinc-600 dark:text-zinc-400">{t.note}</p>{/if}
            {#if summary(t)}<p class="text-sm">{summary(t)}</p>{/if}
            {#if sourceOf(t, imports)}<p class="text-xs text-zinc-600 dark:text-zinc-400">{sourceOf(t, imports)}.</p>{/if}
          </div>
          <span class="inline-flex flex-wrap gap-2">
            <button type="button" class="btn-secondary" aria-label="Edit {t.label}" onclick={() => start(t)}>Edit</button>
            <ConfirmButton label="Remove" question="Remove {t.label}, and its links?" onconfirm={() => remove(t)} />
          </span>
        </li>
      {/each}
    </ul>
  {/if}

  <section class="card max-w-2xl space-y-3" aria-labelledby="imports-heading">
    <h2 id="imports-heading" class="text-lg font-semibold">Imports</h2>
    <p class="text-sm text-zinc-600 dark:text-zinc-400">
      Many terms added at once, from a file or by an assistant, kept together so that they can be reviewed and removed as one. A file is JSON: a list of terms, each with a label and, by label, the terms
      broader than it and related to it.
    </p>
    {#if imports.length}
      <ul class="space-y-2">
        {#each imports as imp (imp.id)}
          <li class="flex flex-wrap items-center justify-between gap-2">
            <span class="text-sm"><strong>{imp.name}</strong>: {counted(imp.terms, 'term')}, {counted(imp.links, 'link')}. {sourceOf({ ...imp, source: imp.via ? 'agent' : 'person' })}.</span>
            <ConfirmButton label="Remove import" question="Remove {imp.name}, its terms and its links?" onconfirm={() => removeImport(imp)} />
          </li>
        {/each}
      </ul>
    {/if}
    <form class="flex flex-wrap items-end gap-2" onsubmit={importTerms}>
      <div>
        <label class="label" for="import-file">File</label>
        <input id="import-file" class="input" type="file" accept=".json,application/json" onchange={(e) => (importFile = e.currentTarget.files?.[0] ?? null)} />
      </div>
      <div>
        <label class="label" for="import-name">What it is</label>
        <input id="import-name" class="input" placeholder="Filament words" bind:value={importName} />
      </div>
      <button type="submit" class="btn-secondary" disabled={!importFile}>Import</button>
    </form>
  </section>
{/if}

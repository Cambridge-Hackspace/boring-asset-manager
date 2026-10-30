<script lang="ts">
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import { session } from '../../lib/session.svelte';
  import type { Flow } from '../../lib/flows';
  import type { AssetAction, Category, Field, RelationType, Report, Scope, TemplateSummary } from '../../lib/models';
  import {
    anyChosen,
    base64Of,
    categoryTree,
    createExport,
    groups,
    kindNames,
    needed,
    neededBySaid,
    planPackage,
    planSummary,
    toggleSubtree,
    treeState,
    type ItemKind,
    type Plan,
    type Selection,
    type TreeNode,
  } from '../../lib/packages';
  import ErrorMessage from '../../components/ErrorMessage.svelte';
  import ImportSection from './ImportSection.svelte';

  let { params: _params }: { params: Record<string, string> } = $props();

  // Packaging part of the instance to share (README §2.3): what may be
  // chosen is what you may read, and what it needs comes with it.
  const seesFlows = $derived(session.can('flows.manage') || session.can('flows.run'));
  const seesScopes = $derived(session.can('users.manage') || session.can('categories.manage'));

  let loading = $state(true);
  let tree = $state<TreeNode[]>([]);
  let options = $state<Partial<Record<ItemKind, { id: string; name: string }[]>>>({});

  let categories = $state<string[]>([]);
  let chosen = $state<Record<string, string[]>>({});
  let icons = $state(false);
  let assets = $state<'none' | 'chosen' | 'all'>('none');
  let photos = $state(false);
  let links = $state(false);
  let history = $state(false);

  let name = $state('');
  let description = $state('');
  let author = $state('');
  let license = $state('');
  let version = $state('');
  let preview = $state('');
  let previewName = $state('');

  let plan = $state<Plan | null>(null);
  let planning = $state(false);
  let exporting = $state(false);
  let saved = $state('');
  let error = $state('');

  const selection = $derived<Selection>({
    categories,
    ...Object.fromEntries(groups.map((g) => [g.key, chosen[g.kind] ?? []])),
    icons,
    assets: assets === 'none' ? undefined : { all: assets === 'all', photos, links, history },
  });
  const needs = $derived(needed(plan));
  const canExport = $derived(anyChosen(selection) && name.trim() !== '' && !exporting && !planning);
  const shown = $derived(
    groups.filter((g) => (g.kind !== 'flow' || seesFlows) && (g.kind !== 'scope' || seesScopes) && (options[g.kind]?.length ?? 0) > 0),
  );

  async function load() {
    try {
      const [cats, fields, actions, types, templates, reports, flows, scopes] = await Promise.all([
        request<{ categories: Category[] }>('GET', '/categories').then((r) => r.categories ?? []),
        request<{ fields: Field[] }>('GET', '/fields').then((r) => r.fields ?? []),
        request<{ actions: AssetAction[] }>('GET', '/actions').then((r) => r.actions ?? []),
        request<{ relation_types: RelationType[] }>('GET', '/relation-types').then((r) => r.relation_types ?? []),
        request<{ templates: TemplateSummary[] }>('GET', '/templates').then((r) => r.templates ?? []),
        request<{ reports: Report[] }>('GET', '/reports').then((r) => r.reports ?? []),
        seesFlows ? request<{ flows: Flow[] }>('GET', '/flows').then((r) => r.flows ?? []) : Promise.resolve([] as Flow[]),
        seesScopes ? request<{ scopes: Scope[] }>('GET', '/scopes').then((r) => r.scopes ?? []) : Promise.resolve([] as Scope[]),
      ]);
      tree = categoryTree(cats);
      const byName = (list: { id: string; name: string }[]) => [...list].sort((a, b) => a.name.localeCompare(b.name));
      options = {
        field: byName(fields.map((f) => ({ id: f.id, name: f.label }))),
        action: byName(actions.map((a) => ({ id: a.id, name: a.label }))),
        relation_type: byName(types),
        label_template: byName(templates),
        scan_template: byName(reports.filter((r) => r.kind === 'scan')),
        report: byName(reports.filter((r) => r.kind !== 'scan')),
        flow: byName(flows),
        scope: byName(scopes.filter((s) => !s.reserved)),
      };
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
  }
  void load();

  // What the selection holds, worked out again as it changes, the latest
  // answer kept.
  let asked = 0;
  $effect(() => {
    const sel = $state.snapshot(selection) as Selection;
    if (!anyChosen(sel)) {
      plan = null;
      return;
    }
    const n = ++asked;
    planning = true;
    const timer = setTimeout(async () => {
      try {
        const got = await planPackage(sel);
        if (n === asked) {
          plan = got;
          error = '';
        }
      } catch (err) {
        if (n === asked) error = messageOf(err);
      } finally {
        if (n === asked) planning = false;
      }
    }, 250);
    return () => clearTimeout(timer);
  });

  function toggle(kind: ItemKind, id: string, on: boolean) {
    const list = chosen[kind] ?? [];
    chosen[kind] = on ? [...list, id] : list.filter((x) => x !== id);
  }

  function toggleGroup(kind: ItemKind, on: boolean) {
    chosen[kind] = on ? (options[kind] ?? []).map((o) => o.id) : [];
  }

  async function choosePreview(e: Event) {
    const input = e.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    preview = '';
    previewName = '';
    if (!file) return;
    if (file.size > 1 << 20) {
      error = 'A preview image is at most 1 MiB.';
      input.value = '';
      return;
    }
    preview = await base64Of(file);
    previewName = file.name;
  }

  async function exportPackage() {
    error = '';
    saved = '';
    exporting = true;
    try {
      const made = await createExport($state.snapshot(selection) as Selection, { name, description, author, license, version, preview });
      plan = made.plan;
      // The browser saves it as the server writes it, however large.
      const a = document.createElement('a');
      a.href = made.download;
      a.download = '';
      document.body.append(a);
      a.click();
      a.remove();
      saved = `Your browser is saving ${name.trim()}.`;
    } catch (err) {
      error = messageOf(err);
    } finally {
      exporting = false;
    }
  }

  // tri-state checkboxes: the DOM's indeterminate has no attribute
  function indeterminate(node: HTMLInputElement, on: boolean) {
    node.indeterminate = on;
    return { update: (v: boolean) => (node.indeterminate = v) };
  }

  function groupState(kind: ItemKind): 'all' | 'some' | 'none' {
    const n = (chosen[kind] ?? []).length;
    return n === 0 ? 'none' : n === (options[kind]?.length ?? 0) ? 'all' : 'some';
  }
</script>

{#snippet neededNote(kind: ItemKind, id: string)}
  {@const by = needs.get(`${kind}:${id}`)}
  {#if by}
    <span id={`needed-${kind}-${id}`} class="text-xs text-zinc-600 dark:text-zinc-400" data-testid="needed">{neededBySaid(by)}</span>
  {/if}
{/snippet}

{#snippet item(kind: ItemKind, id: string, label: string, on: boolean, ontoggle: (on: boolean) => void)}
  {@const by = needs.get(`${kind}:${id}`)}
  <div class="flex flex-wrap items-baseline gap-x-2 text-sm">
    <label class="flex gap-2">
      <input
        type="checkbox"
        checked={on || !!by}
        disabled={!on && !!by}
        aria-describedby={by ? `needed-${kind}-${id}` : undefined}
        onchange={(e) => ontoggle(e.currentTarget.checked)}
      />
      {label}
    </label>
    {@render neededNote(kind, id)}
  </div>
{/snippet}

{#snippet branch(node: TreeNode)}
  {@const state = treeState(categories, node)}
  {@const by = needs.get(`category:${node.id}`)}
  <li>
    <div class="flex flex-wrap items-baseline gap-x-2 text-sm">
      <label class="flex gap-2">
        <input
          type="checkbox"
          checked={state === 'all' || (state === 'none' && !!by)}
          disabled={state === 'none' && !!by}
          use:indeterminate={state === 'some'}
          aria-describedby={by ? `needed-category-${node.id}` : undefined}
          onchange={(e) => (categories = toggleSubtree(categories, node, e.currentTarget.checked))}
        />
        {node.name}
      </label>
      {@render neededNote('category', node.id)}
    </div>
    {#if node.children.length > 0}
      <ul class="ml-6 space-y-1 border-l border-zinc-200 pl-2 dark:border-zinc-700">
        {#each node.children as child (child.id)}{@render branch(child)}{/each}
      </ul>
    {/if}
  </li>
{/snippet}

<h1 class="mb-4 text-2xl font-bold">Import &amp; export</h1>

<ImportSection {tree} onimported={() => void load()} />

<section class="card mb-6 max-w-3xl space-y-4" aria-labelledby="export-heading">
  <h2 id="export-heading" class="text-lg font-semibold">Export a package</h2>
  <p class="text-sm">
    Choose part of this instance to share with another: categories, the things they use, and their assets if you like. What
    something needs comes with it, ticked and marked, and stays ticked until what needs it is unticked. A package never holds
    people, passwords, sessions, tokens, notification channels’ secrets or printers, and holds only what you can see.
  </p>

  {#if loading}
    <p class="text-sm" role="status">Loading what can be packaged…</p>
  {:else}
    <fieldset class="space-y-1">
      <legend class="text-sm font-medium">Categories</legend>
      <p class="text-xs text-zinc-600 dark:text-zinc-400">
        A category brings its fields, actions, link types, label and scan templates, icon, and the flows that file into it.
      </p>
      {#if tree.length === 0}
        <p class="text-sm">There are no categories.</p>
      {:else}
        <ul class="space-y-1" data-testid="category-tree">
          {#each tree as node (node.id)}{@render branch(node)}{/each}
        </ul>
      {/if}
    </fieldset>

    {#each shown as g (g.kind)}
      {@const state = groupState(g.kind)}
      <fieldset class="space-y-1" data-testid={`group-${g.kind}`}>
        <legend class="sr-only">{g.label}</legend>
        <label class="flex gap-2 text-sm font-medium">
          <input
            type="checkbox"
            checked={state === 'all'}
            use:indeterminate={state === 'some'}
            onchange={(e) => toggleGroup(g.kind, e.currentTarget.checked)}
          />
          {g.label}
        </label>
        <div class="ml-6 space-y-1">
          {#each options[g.kind] ?? [] as o (o.id)}
            {@render item(g.kind, o.id, o.name, (chosen[g.kind] ?? []).includes(o.id), (on) => toggle(g.kind, o.id, on))}
          {/each}
        </div>
      </fieldset>
    {/each}

    <fieldset class="space-y-1">
      <legend class="text-sm font-medium">Icons</legend>
      {@render item('icons', 'icons', 'The icon library, palette and icon settings', icons, (on) => (icons = on))}
      <p class="text-xs text-zinc-600 dark:text-zinc-400">A category’s icon comes with it, with the library files it uses.</p>
    </fieldset>

    <fieldset class="space-y-1">
      <legend class="text-sm font-medium">Assets</legend>
      <label class="flex gap-2 text-sm"><input type="radio" value="none" bind:group={assets} /> None</label>
      <label class="flex gap-2 text-sm"><input type="radio" value="chosen" bind:group={assets} /> Those in the categories chosen</label>
      <label class="flex gap-2 text-sm"><input type="radio" value="all" bind:group={assets} /> Every asset, with its categories</label>
      <div class="ml-6 flex flex-wrap gap-x-4">
        <label class="flex gap-2 text-sm"><input type="checkbox" bind:checked={photos} disabled={assets === 'none'} /> Photos</label>
        <label class="flex gap-2 text-sm"><input type="checkbox" bind:checked={links} disabled={assets === 'none'} /> Links between them</label>
        <label class="flex gap-2 text-sm"><input type="checkbox" bind:checked={history} disabled={assets === 'none'} /> History, without who did what</label>
      </div>
    </fieldset>

    {#if plan}
      <div class="space-y-1 text-sm" role="status" data-testid="plan">
        <p>{planSummary(plan, photos)}</p>
        {#if plan.left_out.length > 0}
          <p class="font-medium">Staying behind:</p>
          <ul class="list-disc pl-5" data-testid="left-out">
            {#each plan.left_out as l (`${l.kind}:${l.id}`)}
              <li>{kindNames[l.kind]} {l.name}, named by {l.needed_by.map((b) => b.name).join(', ')}: {l.reason}.</li>
            {/each}
          </ul>
        {/if}
      </div>
    {/if}

    <fieldset class="grid gap-3 sm:grid-cols-2">
      <legend class="mb-2 text-sm font-medium">About the package</legend>
      <label class="block text-sm sm:col-span-2">Name <input class="input mt-1" bind:value={name} required maxlength="200" /></label>
      <label class="block text-sm sm:col-span-2">
        Description <textarea class="input mt-1" rows="2" bind:value={description} maxlength="4000"></textarea>
      </label>
      <label class="block text-sm">Author <input class="input mt-1" bind:value={author} maxlength="200" /></label>
      <label class="block text-sm">License <input class="input mt-1" bind:value={license} maxlength="200" placeholder="CC-BY-4.0" /></label>
      <label class="block text-sm">Version <input class="input mt-1" bind:value={version} maxlength="64" placeholder="1.0" /></label>
      <label class="block text-sm">
        Preview image <input class="mt-1 block text-sm" type="file" accept="image/png,image/jpeg,image/webp" onchange={choosePreview} />
        {#if previewName}<span class="text-xs">{previewName}</span>{/if}
      </label>
    </fieldset>

    <button type="button" class="btn-primary" disabled={!canExport} onclick={exportPackage} data-testid="export">
      {exporting ? 'Exporting…' : 'Export'}
    </button>
    {#if saved}<p class="text-sm" role="status">{saved}</p>{/if}
  {/if}
  <ErrorMessage {error} />
</section>

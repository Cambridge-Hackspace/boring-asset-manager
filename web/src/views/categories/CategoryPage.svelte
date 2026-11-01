<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { request } from '../../lib/api';
  import { amount } from '../../lib/assets';
  import { messageOf } from '../../lib/errors';
  import { UNCATEGORIZED, type Category, type EffectiveField, type Field, type RelationType, type Report, type Scope, type TemplateSummary, type AssetHook, type CategoryHook } from '../../lib/models';
  import { navigate, router } from '../../lib/router.svelte';
  import { DEFAULT_SCOPE } from '../../lib/permissions';
  import { session } from '../../lib/session.svelte';
  import { ancestry, flattenTree, subtree } from '../../lib/tree';
  import CategoryBadge from '../../components/CategoryBadge.svelte';
  import ConfirmButton from '../../components/ConfirmButton.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';
  import AddToSpool from '../../components/AddToSpool.svelte';
  import TabLinks from '../../components/TabLinks.svelte';
  import { currentTab } from '../../lib/tabs';
  import CategoryIconSection from './CategoryIconSection.svelte';

  // /categories/new and /categories/:id
  let { params }: { params: Record<string, string> } = $props();

  // The app remounts the page when the path changes, so these never change.
  const id = untrack(() => params.id ?? '');
  const isNew = !id;
  const canManage = $derived(session.can('categories.manage'));

  let categories = $state<Category[]>([]);
  let effective = $state<EffectiveField[]>([]);
  let allFields = $state<Field[]>([]);
  let relationTypes = $state<RelationType[]>([]);
  let suggested = $state<string[]>([]);
  // Scopes (docs/access.md): category managers see them; only user managers
  // change them, since they decide who reaches the assets.
  const canSeeScopes = $derived(session.canAny('categories.manage', 'users.manage'));
  const canScope = $derived(session.can('users.manage'));
  let scopes = $state<Scope[]>([]);
  let scopeIds = $state<string[]>([]);
  // Scan templates (docs/scanning.md): the one its assets show, and the ties
  // between categories that choose them.
  let scanTemplates = $state<Report[]>([]);
  let scanTemplate = $state('');
  let savedScanTemplate = $state('');
  let conflicts = $state<Record<string, number>>({});
  let afterSaving = $state<Record<string, number> | null>(null);
  const newTies = $derived(
    afterSaving && scanTemplate !== savedScanTemplate
      ? Object.keys(afterSaving).filter((c) => (afterSaving![c] ?? 0) > (conflicts[c] ?? 0))
      : [],
  );
  // Label templates (docs/printing.md): those its assets join the print queue with, in
  // order of preference, and the ties between categories that choose them.
  let labelTemplates = $state<TemplateSummary[]>([]);
  let labelList = $state<string[]>([]);
  let savedLabelList = $state<string[]>([]);
  let adding = $state('');
  let labelConflicts = $state<Record<string, number>>({});
  let labelAfterSaving = $state<Record<string, number> | null>(null);
  const labelsChanged = $derived(labelList.join(',') !== savedLabelList.join(','));
  const newLabelTies = $derived(
    labelAfterSaving && labelsChanged
      ? Object.keys(labelAfterSaving).filter((c) => (labelAfterSaving![c] ?? 0) > (labelConflicts[c] ?? 0))
      : [],
  );
  const templateName = (tid: string) => labelTemplates.find((t) => t.id === tid)?.name ?? 'A deleted template';
  const canSpool = $derived(session.can('labels.print'));
  // Hooks (docs/hooks.md): which it offers on its assets, and whether its
  // subcategories' too.
  let hooks = $state<AssetHook[]>([]);
  let offered = $state<Record<string, { on: boolean; cascades: boolean }>>({});
  let withSubcategories = $state(true);
  const categoryName = (cid: string) => categories.find((c) => c.id === cid)?.name ?? 'another category';
  let loading = $state(true);
  let error = $state('');
  let saved = $state('');

  // The form.
  let name = $state('');
  let parentId = $state('');
  let color = $state('#2563eb');
  let consumable = $state(false);
  let lendable = $state(false);
  let runningLow = $state(false);
  let threshold = $state<number | null>(null);
  let unit = $state('');
  let busy = $state(false);

  // Stock, for consumable categories.
  let activeCount = $state<number | null>(null);
  // For a category with a unit, the total of its stock in it.
  let total = $state<number | null>(null);
  let flagging = $state(false);

  // Assigning a field.
  let newFieldId = $state('');
  let newRequired = $state(false);

  const category = $derived(categories.find((c) => c.id === id));
  const isSystem = id === UNCATEGORIZED;
  const parentChoices = $derived(
    flattenTree(categories.filter((c) => c.id !== UNCATEGORIZED && !(id && subtree(categories, id).has(c.id)))),
  );
  const byId = $derived(new Map(categories.map((c) => [c.id, c])));
  const direct = $derived(effective.filter((e) => e.source_category_id === id));
  const unassigned = $derived(allFields.filter((f) => !direct.some((d) => d.field.id === f.id)));

  onMount(() => {
    void load();
    window.addEventListener('beforeunload', warnUnsaved);
    return () => window.removeEventListener('beforeunload', warnUnsaved);
  });

  // The page saves in several places; what each held when loaded or last
  // saved, to say which have changes not saved, and warn before leaving.
  const sectionNames: Record<string, string> = {
    details: 'its details',
    stock: 'stock and lending',
    suggestions: 'suggested links',
    scan: 'scan template',
    labels: 'label templates',
    hooks: 'hooks',
    scopes: 'who can see its assets',
    icon: 'its icon',
  };
  let savedAs = $state<Record<string, string>>({});
  const sectionState = $derived<Record<string, string>>({
    details: JSON.stringify([name, parentId, color]),
    stock: JSON.stringify([consumable, lendable, runningLow, threshold, unit]),
    suggestions: JSON.stringify([...suggested].sort()),
    scan: scanTemplate,
    labels: JSON.stringify(labelList),
    hooks: JSON.stringify(offered),
    scopes: JSON.stringify([...scopeIds].sort()),
  });
  // The icon section keeps its own: whether its source differs from what is saved.
  let iconUnsaved = $state(false);
  const unsaved = $derived(
    isNew ? [] : [...Object.keys(savedAs).filter((k) => savedAs[k] !== sectionState[k]), ...(iconUnsaved ? ['icon'] : [])],
  );
  // The page in tabs (#161), each a part of it, kept in the address. All
  // stay on the page, hidden but for one, so changes not saved in one
  // survive looking at another; a tab with any says so.
  const tabOf: Record<string, string> = { details: 'about', stock: 'stock', suggestions: 'links', scan: 'links', hooks: 'links', labels: 'labels', scopes: 'scopes', icon: 'icon' };
  const tabs = $derived.by(() => {
    const all = [
      { key: 'about', label: 'About', on: true },
      { key: 'fields', label: 'Fields', on: true },
      { key: 'stock', label: 'Stock and lending', on: !!category?.is_consumable || (canManage && !isSystem) },
      { key: 'labels', label: 'Labels', on: (canManage && labelTemplates.length > 0) || canSpool || (labelConflicts[id] ?? 0) > 0 },
      { key: 'icon', label: 'Icon', on: !isSystem },
      { key: 'links', label: 'Links and hooks', on: (canManage && (relationTypes.length > 0 || scanTemplates.length > 0 || hooks.length > 0)) || (conflicts[id] ?? 0) > 0 },
      { key: 'scopes', label: 'Who can see it', on: canSeeScopes && scopes.length > 0 },
    ];
    const changed = new Set(unsaved.map((k) => tabOf[k]));
    return all.filter((t) => t.on).map((t) => ({ key: t.key, label: changed.has(t.key) ? `${t.label} (not saved)` : t.label }));
  });
  const tab = $derived(currentTab(router.search, tabs.map((t) => t.key)));

  function markSaved(...keys: string[]) {
    for (const k of keys) savedAs[k] = sectionState[k]!;
  }
  function warnUnsaved(event: BeforeUnloadEvent) {
    if (unsaved.length) event.preventDefault();
  }

  // How many assets are in it, for a way to them.
  let assetCount = $state<number | null>(null);

  async function load() {
    error = '';
    try {
      categories = (await request<{ categories: Category[] }>('GET', '/categories')).categories ?? [];
      if (isNew) {
        parentId = router.query.get('parent') ?? '';
      } else {
        const c = categories.find((c) => c.id === id);
        if (!c) {
          error = 'Category not found.';
          return;
        }
        name = c.name;
        parentId = c.parent_id ?? '';
        color = c.color_hex;
        consumable = c.is_consumable;
        lendable = c.is_lendable;
        runningLow = c.is_running_low;
        threshold = c.low_stock_threshold ?? null;
        unit = c.unit;
        assetCount = await request<{ assets: unknown[] }>('GET', `/assets?category_id=${encodeURIComponent(id)}`)
          .then((r) => (r.assets ?? []).length)
          .catch(() => null);
        const level = await request<{ active_count: number; total?: number }>('GET', `/categories/${encodeURIComponent(id)}`);
        activeCount = level.active_count;
        total = level.total ?? null;
        effective = (await request<{ fields: EffectiveField[] }>('GET', `/categories/${encodeURIComponent(id)}/fields`)).fields ?? [];
        if (canManage) {
          allFields = (await request<{ fields: Field[] }>('GET', '/fields')).fields ?? [];
          relationTypes = (await request<{ relation_types: RelationType[] }>('GET', '/relation-types')).relation_types ?? [];
          suggested = (await request<{ relation_type_ids: string[] }>('GET', `/categories/${encodeURIComponent(id)}/relation-types`)).relation_type_ids ?? [];
        }
        if (canManage) {
          scanTemplates = (await request<{ reports: Report[] }>('GET', '/reports?kind=scan')).reports ?? [];
          scanTemplate = savedScanTemplate = c.scan_template_id ?? '';
          labelTemplates = (await request<{ templates: TemplateSummary[] }>('GET', '/templates')).templates ?? [];
          labelList = [...(c.label_template_ids ?? [])];
          savedLabelList = [...labelList];
          hooks = (await request<{ hooks: AssetHook[] }>('GET', '/hooks')).hooks ?? [];
          const own = (await request<{ hooks: CategoryHook[] }>('GET', `/categories/${encodeURIComponent(id)}/hooks`)).hooks ?? [];
          offered = Object.fromEntries(hooks.map((a) => {
            const o = own.find((x) => x.hook_id === a.id);
            return [a.id, { on: !!o, cascades: o?.cascades ?? true }];
          }));
        }
        if (canManage || canSpool) {
          labelConflicts = await request<{ categories: Record<string, number> }>('GET', '/categories/label-conflicts')
            .then((r) => r.categories ?? {})
            .catch(() => ({}));
        }
        // A warning, not the page: it may fail without stopping the rest.
        conflicts = await request<{ categories: Record<string, number> }>('GET', '/categories/scan-conflicts')
          .then((r) => r.categories ?? {})
          .catch(() => ({}));
        if (canSeeScopes) {
          scopes = ((await request<{ scopes: Scope[] }>('GET', '/scopes')).scopes ?? []).filter((s) => s.name !== DEFAULT_SCOPE); // public goes on categories; default cannot
          scopeIds = (await request<{ scope_ids: string[] }>('GET', `/categories/${encodeURIComponent(id)}/scopes`)).scope_ids ?? [];
        }
      }
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
      // As loaded, each part is as saved.
      if (!isNew) markSaved(...Object.keys(sectionNames));
    }
  }

  async function saveSuggestions(event: SubmitEvent) {
    event.preventDefault();
    error = saved = '';
    try {
      suggested = (await request<{ relation_type_ids: string[] }>('PUT', `/categories/${encodeURIComponent(id)}/relation-types`, { relation_type_ids: suggested })).relation_type_ids ?? [];
      markSaved('suggestions');
      saved = 'Suggestions saved.';
    } catch (err) {
      error = messageOf(err);
    }
  }

  // What saving the chosen template would do, to warn before it ties.
  async function checkScanTemplate() {
    afterSaving = null;
    if (scanTemplate === savedScanTemplate) return;
    try {
      const q = `category_id=${encodeURIComponent(id)}&scan_template_id=${encodeURIComponent(scanTemplate)}`;
      afterSaving = (await request<{ categories: Record<string, number> }>('GET', `/categories/scan-conflicts?${q}`)).categories ?? {};
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function saveScanTemplate(event: SubmitEvent) {
    event.preventDefault();
    error = saved = '';
    try {
      await request('PUT', `/categories/${encodeURIComponent(id)}/scan-template`, { scan_template_id: scanTemplate || null });
      savedScanTemplate = scanTemplate;
      markSaved('scan');
      afterSaving = null;
      conflicts = (await request<{ categories: Record<string, number> }>('GET', '/categories/scan-conflicts')).categories ?? {};
      saved = 'Scan template saved.';
    } catch (err) {
      error = messageOf(err);
    }
  }

  // What saving the label templates as listed would do, to warn before
  // they tie.
  async function checkLabelTemplates() {
    labelAfterSaving = null;
    if (!labelsChanged) return;
    try {
      const q = `category_id=${encodeURIComponent(id)}&label_template_ids=${encodeURIComponent(labelList.join(','))}`;
      labelAfterSaving = (await request<{ categories: Record<string, number> }>('GET', `/categories/label-conflicts?${q}`)).categories ?? {};
    } catch (err) {
      error = messageOf(err);
    }
  }

  function changeLabels(next: string[]) {
    labelList = next;
    void checkLabelTemplates();
  }

  function moveLabel(i: number, by: number) {
    const next = [...labelList];
    [next[i], next[i + by]] = [next[i + by]!, next[i]!];
    changeLabels(next);
  }

  async function saveLabelTemplates(event: SubmitEvent) {
    event.preventDefault();
    error = saved = '';
    try {
      await request('PUT', `/categories/${encodeURIComponent(id)}/label-template`, { label_template_ids: labelList });
      savedLabelList = [...labelList];
      markSaved('labels');
      labelAfterSaving = null;
      labelConflicts = (await request<{ categories: Record<string, number> }>('GET', '/categories/label-conflicts')).categories ?? {};
      saved = 'Label templates saved.';
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function saveHooks(event: SubmitEvent) {
    event.preventDefault();
    error = saved = '';
    const chosen = hooks.filter((a) => offered[a.id]?.on).map((a) => ({ hook_id: a.id, cascades: offered[a.id]!.cascades }));
    try {
      await request('PUT', `/categories/${encodeURIComponent(id)}/hooks`, { hooks: chosen });
      markSaved('hooks');
      saved = 'Hooks saved.';
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function saveScopes(event: SubmitEvent) {
    event.preventDefault();
    error = saved = '';
    try {
      scopeIds = (await request<{ scope_ids: string[] }>('PUT', `/categories/${encodeURIComponent(id)}/scopes`, { scope_ids: scopeIds })).scope_ids ?? [];
      markSaved('scopes');
      saved = 'Scopes saved.';
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function save(event: SubmitEvent) {
    event.preventDefault();
    error = '';
    saved = '';
    busy = true;
    // The API reads a missing parent as "move to the top", so always send it.
    const body = {
      name,
      parent_id: parentId || null,
      color_hex: color,
      is_consumable: consumable,
      is_lendable: lendable,
      is_running_low: consumable && runningLow,
      unit: consumable ? unit.trim() : '',
      low_stock_threshold: consumable && threshold ? threshold : null,
    };
    try {
      if (isNew) {
        const res = await request<{ category: Category }>('POST', '/categories', body);
        navigate(`/categories/${encodeURIComponent(res.category.id)}`, true);
      } else {
        await request('PUT', `/categories/${encodeURIComponent(id)}`, body);
        saved = 'Saved.';
        await load();
      }
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }

  // Flags or clears the category by hand, which needs only assets.write.
  async function setRunningLow(low: boolean) {
    error = '';
    saved = '';
    flagging = true;
    try {
      await request('PUT', `/categories/${encodeURIComponent(id)}/running-low`, { running_low: low });
      saved = low ? 'Marked as running low.' : 'Marked as restocked.';
      await load();
    } catch (err) {
      error = messageOf(err);
    } finally {
      flagging = false;
    }
  }

  // A copy beside it, as saved: settings, fields, suggestions, hooks,
  // scopes and templates, but not its subcategories or assets.
  let duplicating = $state(false);
  async function duplicate() {
    error = '';
    duplicating = true;
    try {
      const res = await request<{ category: Category }>('POST', `/categories/${encodeURIComponent(id)}/clone`);
      navigate(`/categories/${encodeURIComponent(res.category.id)}`);
    } catch (err) {
      error = messageOf(err);
    } finally {
      duplicating = false;
    }
  }

  async function remove() {
    try {
      await request('DELETE', `/categories/${encodeURIComponent(id)}`);
      navigate('/categories');
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function assign(event: SubmitEvent) {
    event.preventDefault();
    if (!newFieldId) return;
    error = '';
    try {
      await request('POST', `/categories/${encodeURIComponent(id)}/fields`, { field_id: newFieldId, is_required: newRequired });
      newFieldId = '';
      newRequired = false;
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function unassign(fieldId: string) {
    error = '';
    try {
      await request('DELETE', `/categories/${encodeURIComponent(id)}/fields/${encodeURIComponent(fieldId)}`);
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }
</script>

<nav aria-label="Breadcrumb" class="mb-2 text-sm">
  <a class="underline" href="/categories">Categories</a>
  {#each category ? ancestry(categories, id).slice(0, -1) : [] as a (a.id)}
    <span aria-hidden="true" class="mx-1 text-zinc-400">/</span><a class="underline" href="/categories/{encodeURIComponent(a.id)}">{a.name}</a>
  {/each}
</nav>

<div class="mb-4 flex flex-wrap items-end justify-between gap-4">
  <div>
    <h1 class="flex items-center gap-3 text-3xl font-bold">
      {#if isNew}New category{:else if category}<span aria-hidden="true" class="inline-block h-6 w-6 shrink-0 rounded-full border-2 border-zinc-900 dark:border-zinc-300" style="background-color: {category.color_hex}"></span>{category.name}{:else}Category{/if}
    </h1>
    {#if category && assetCount !== null}
      <p class="mt-1 text-sm text-zinc-600 dark:text-zinc-400">{assetCount === 0 ? 'No assets in it yet.' : `${assetCount} asset${assetCount === 1 ? '' : 's'} here.`}</p>
    {/if}
  </div>
  {#if category && assetCount !== null}
    <div class="flex flex-wrap gap-2">
      {#if assetCount > 0}<a class="btn-secondary" href="/assets?category={encodeURIComponent(id)}">See its {assetCount} asset{assetCount === 1 ? '' : 's'}</a>{/if}
      {#if session.can('assets.write') && !isSystem}<a class="btn-secondary" href="/assets/new?category={encodeURIComponent(id)}">Add an asset to it</a>{/if}
    </div>
  {/if}
</div>
<ErrorMessage {error} />
{#if saved}<p class="mb-2 text-sm text-green-700 dark:text-green-400" role="status">{saved}</p>{/if}
{#if unsaved.length}
  <p class="mb-2 text-sm text-amber-900 dark:text-amber-200">
    <span aria-hidden="true">⚠ </span>Not saved yet: {unsaved.map((k) => sectionNames[k]).join(', ')}. Each part of this page saves with its own button.
  </p>
{/if}

{#snippet stockFields()}
  <fieldset class="space-y-2">
    <legend class="label">Stock</legend>
    <label class="flex items-center gap-2 text-sm"><input type="checkbox" bind:checked={consumable} /> Consumable</label>
    <label class="flex items-center gap-2 text-sm" class:opacity-50={!consumable}>
      <input type="checkbox" bind:checked={runningLow} disabled={!consumable} aria-describedby="low-help" /> Running low
    </label>
    <p id="low-help" class="text-xs text-zinc-600 dark:text-zinc-400">Only consumable categories can run low.</p>
    <div>
      <label class="label" for="cat-unit" class:opacity-50={!consumable}>Unit (optional)</label>
      <input id="cat-unit" class="input w-32" maxlength="32" placeholder="g" disabled={!consumable} aria-describedby="unit-help" bind:value={unit} />
      <p id="unit-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">
        Weighs instead of counting: totals the stock its assets count in this unit, such as spools in g, and the threshold is in it too. Leave empty to count items.
      </p>
    </div>
    <div>
      <label class="label" for="cat-threshold" class:opacity-50={!consumable}>Low stock threshold (optional)</label>
      <input id="cat-threshold" class="input w-32" type="number" min="1" step="1" disabled={!consumable} aria-describedby="threshold-help" bind:value={threshold} />
      <p id="threshold-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">
        Flag it automatically when fewer active assets than this remain here and in its subcategories, and clear the
        flag when there are enough again.
      </p>
    </div>
  </fieldset>
  <fieldset class="space-y-1">
    <legend class="label">Lending</legend>
    <label class="flex items-center gap-2 text-sm"><input type="checkbox" bind:checked={lendable} aria-describedby="lend-help" /> Lendable</label>
    <p id="lend-help" class="text-xs text-zinc-600 dark:text-zinc-400">Its assets, and those of its subcategories, can be checked out to people.</p>
  </fieldset>
{/snippet}

{#snippet detailFields()}
  <div>
    <label class="label" for="cat-name">Name</label>
    <input id="cat-name" class="input" required bind:value={name} />
  </div>
  <div>
    <label class="label" for="cat-parent">Parent</label>
    <select id="cat-parent" class="input" bind:value={parentId}>
      <option value="">None (top level)</option>
      {#each parentChoices as row (row.category.id)}
        <option value={row.category.id}>{' '.repeat(row.depth * 3)}{row.category.name}</option>
      {/each}
    </select>
  </div>
  <div>
    <label class="label" for="cat-color">Color</label>
    <div class="flex items-center gap-2">
      <input id="cat-color" type="color" class="h-11 w-14 rounded border-2 border-zinc-900 dark:border-zinc-400" bind:value={color} />
      <CategoryBadge category={{ id: 'preview', name: name || 'Preview', color_hex: color }} />
    </div>
  </div>
{/snippet}

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading…</p>
{:else if isNew}
  {#if canManage}
    <form class="card max-w-2xl space-y-4" onsubmit={save}>
      <div class="grid gap-4 sm:grid-cols-2">
        {@render detailFields()}
        {@render stockFields()}
      </div>
      <button type="submit" class="btn-primary" disabled={busy}>Create category</button>
    </form>
  {/if}
{:else if category}
  <TabLinks label="Parts of this category" {tabs} />

  <div class="mt-6" id="tab-about" hidden={tab !== 'about'}>
    {#if canManage && !isSystem}
      <form class="card max-w-2xl space-y-4" aria-label="Its details" onsubmit={save}>
        <div class="grid gap-4 sm:grid-cols-2">{@render detailFields()}</div>
        <div class="flex flex-wrap gap-2">
          <button type="submit" class="btn-primary" disabled={busy}>Save</button>
          <a class="btn-secondary" href="/categories/new?parent={encodeURIComponent(id)}">New subcategory</a>
          <button
            type="button"
            class="btn-secondary"
            disabled={duplicating || unsaved.length > 0}
            title={unsaved.length ? 'Save your changes first: the copy is of the category as saved.' : 'A copy beside it, without its subcategories or assets'}
            onclick={duplicate}>Duplicate</button
          >
          <ConfirmButton label="Delete" question="Delete this category? Its subcategories move to the top level." onconfirm={remove} />
        </div>
      </form>
    {:else if isSystem}
      <p class="text-sm text-zinc-600 dark:text-zinc-400">The system category for assets with no other category. It cannot be renamed, moved or deleted.</p>
    {:else}
      <p class="text-sm text-zinc-600 dark:text-zinc-400">{category.parent_id ? `Under ${categoryName(category.parent_id)}.` : 'At the top level.'}</p>
    {/if}
  </div>

  <section class="mt-6" id="tab-fields" aria-labelledby="fields-heading" hidden={tab !== 'fields'}>
    <h2 id="fields-heading" class="mb-2 text-lg font-semibold">Fields</h2>
    {#if effective.length === 0}
      <p class="text-sm text-zinc-600 dark:text-zinc-400">
        Assets in this category record no fields yet.{#if canManage && !allFields.length}{' '}Make fields, such as a serial number, on the <a class="underline" href="/fields">Fields</a> page first.{/if}
      </p>
    {:else}
      <div class="data-table mb-4 max-w-3xl">
        <table>
          <thead>
            <tr>
              <th scope="col">Field</th>
              <th scope="col">Required</th>
              <th scope="col">From</th>
              <th scope="col"><span class="sr-only">Actions</span></th>
            </tr>
          </thead>
          <tbody>
            {#each effective as e (e.field.id)}
              {@const source = e.source_category_id ? byId.get(e.source_category_id) : undefined}
              <tr>
                <td><span class="font-semibold">{e.field.label}</span> <span class="font-mono text-xs text-zinc-600 dark:text-zinc-400">{e.field.key}</span></td>
                <td>{e.is_required ? 'Required' : 'Optional'}</td>
                <td>
                  {#if e.source_category_id === id}This category{:else if source}Inherited from <CategoryBadge category={source} />{/if}
                </td>
                <td class="text-right">
                  {#if canManage && e.source_category_id === id}
                    <ConfirmButton label="Remove" question="Stop asking for “{e.field.label}” here?" onconfirm={() => unassign(e.field.id)} />
                  {/if}
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}

    {#if canManage && unassigned.length}
      <form class="mt-4 flex flex-wrap items-end gap-3" onsubmit={assign}>
        <div>
          <label class="label" for="assign-field">Add a field</label>
          <select id="assign-field" class="input" bind:value={newFieldId}>
            <option value="">Choose a field…</option>
            {#each unassigned as f (f.id)}
              <option value={f.id}>{f.label}</option>
            {/each}
          </select>
        </div>
        <label class="flex items-center gap-2 text-sm"><input type="checkbox" bind:checked={newRequired} /> Required</label>
        <button type="submit" class="btn-secondary" disabled={!newFieldId}>Add field</button>
      </form>
      <p class="mt-2 text-xs text-zinc-600 dark:text-zinc-400">Adding a field another category already gives this one makes it this category’s own; required anywhere means required.</p>
    {/if}
  </section>

  <div class="mt-6 max-w-2xl space-y-6" id="tab-stock" hidden={tab !== 'stock'}>
    {#if category.is_consumable}
      <section class="card" aria-labelledby="stock-heading">
        <h2 id="stock-heading" class="mb-1 text-lg font-semibold">Stock</h2>
        <p class="text-sm">
          <strong>{category.is_running_low ? 'Running low.' : 'In stock.'}</strong>
          {#if category.unit}
            {amount(total ?? 0, category.unit)} here and in its subcategories, in
            {activeCount === 1 ? '1 active asset' : `${activeCount ?? 0} active assets`}{category.low_stock_threshold
              ? `; flagged automatically below ${amount(category.low_stock_threshold, category.unit)}`
              : ''}.
          {:else}
            {activeCount === 1 ? '1 active asset' : `${activeCount ?? 0} active assets`} here and in its subcategories{category.low_stock_threshold
              ? `; flagged automatically below ${category.low_stock_threshold}`
              : ''}.
          {/if}
        </p>
        {#if session.can('stock.record')}
          <div class="mt-3">
            {#if category.is_running_low}
              <button type="button" class="btn-secondary" disabled={flagging} onclick={() => setRunningLow(false)}>Mark restocked</button>
            {:else}
              <button type="button" class="btn-secondary" disabled={flagging} onclick={() => setRunningLow(true)}>Mark running low</button>
            {/if}
          </div>
        {/if}
      </section>
    {/if}
    {#if canManage && !isSystem}
      <form class="card space-y-4" aria-label="Stock and lending" onsubmit={save}>
        <div class="grid gap-4 sm:grid-cols-2">{@render stockFields()}</div>
        <button type="submit" class="btn-secondary" disabled={busy}>Save stock and lending</button>
      </form>
    {/if}
  </div>

  <div class="mt-6 max-w-2xl space-y-6" id="tab-labels" hidden={tab !== 'labels'}>
    {#if (labelConflicts[id] ?? 0) > 0}
      <p class="rounded border border-amber-400 bg-amber-50 p-3 text-sm text-amber-900 dark:border-amber-600 dark:bg-amber-950 dark:text-amber-100" role="note">
        <span aria-hidden="true">⚠</span>
        {labelConflicts[id] === 1 ? '1 asset' : `${labelConflicts[id]} assets`} here take a label template chosen by a tie with
        another category as close to them. <a class="underline" href="/assets?conflict=label_template">See which</a>, and name a
        template on one of the categories to settle it.
      </p>
    {/if}
    {#if (canManage && labelTemplates.length) || canSpool}
      <section aria-labelledby="labels-heading">
        <h2 id="labels-heading" class="mb-1 text-lg font-semibold">Labels</h2>
        {#if canManage && labelTemplates.length}
          <p class="mb-2 text-sm text-zinc-600 dark:text-zinc-400">
            The templates its assets, and those of its subcategories, join the print queue with, unless they choose their own. Each
            label takes the first that suits the printer it goes to, so the same label can come in several sizes.
          </p>
          <form class="mb-4 space-y-2" aria-label="Label templates" onsubmit={saveLabelTemplates}>
            {#if labelList.length}
              <ol class="space-y-1 text-sm">
                {#each labelList as tid, i (tid)}
                  <li class="flex flex-wrap items-center gap-2">
                    <span class="min-w-48">{i + 1}. {templateName(tid)}</span>
                    <button type="button" class="btn-secondary" aria-label="Move {templateName(tid)} up" disabled={i === 0} onclick={() => moveLabel(i, -1)}>↑</button>
                    <button type="button" class="btn-secondary" aria-label="Move {templateName(tid)} down" disabled={i === labelList.length - 1} onclick={() => moveLabel(i, 1)}>↓</button>
                    <button type="button" class="btn-secondary" aria-label="Remove {templateName(tid)}" onclick={() => changeLabels(labelList.filter((x) => x !== tid))}>Remove</button>
                  </li>
                {/each}
              </ol>
            {:else}
              <p class="text-sm text-zinc-600 dark:text-zinc-400">None: its parent’s, if it names any.</p>
            {/if}
            <div class="flex flex-wrap items-end gap-2">
              <div>
                <label class="label" for="label-template-add">Add a template</label>
                <select id="label-template-add" class="input" bind:value={adding}>
                  <option value="">Choose one</option>
                  {#each labelTemplates.filter((t) => !labelList.includes(t.id)) as t (t.id)}<option value={t.id}>{t.name}</option>{/each}
                </select>
              </div>
              <button type="button" class="btn-secondary" disabled={!adding} onclick={() => { changeLabels([...labelList, adding]); adding = ''; }}>Add</button>
              <button type="submit" class="btn-secondary" disabled={!labelsChanged}>Save label templates</button>
            </div>
          </form>
          {#if newLabelTies.length}
            <p class="mb-4 text-sm text-amber-800 dark:text-amber-300" role="alert">
              <span aria-hidden="true">⚠</span>
              Saving this ties {newLabelTies.filter((c) => c !== id).map(categoryName).join(', ') || 'it'} with this category for
              some assets in both: they would get a template chosen by which category is deeper, smaller or older.
            </p>
          {/if}
        {/if}
        {#if canSpool}
          <div class="flex flex-wrap items-center gap-3">
            <AddToSpool what={() => ({ category_id: id, subcategories: withSubcategories })} label="Add its assets to the print queue" />
            <label class="flex items-center gap-2 text-sm"><input type="checkbox" bind:checked={withSubcategories} /> With its subcategories’</label>
          </div>
        {/if}
      </section>
    {/if}
  </div>

  {#if !isSystem}
    <div class="mt-6" id="tab-icon" hidden={tab !== 'icon'}>
      <CategoryIconSection {category} {categories} bind:unsaved={iconUnsaved} />
    </div>
  {/if}

  <div class="mt-6 max-w-2xl space-y-8" id="tab-links" hidden={tab !== 'links'}>
    {#if canManage && relationTypes.length}
      <section aria-labelledby="relations-heading">
        <h2 id="relations-heading" class="mb-1 text-lg font-semibold">Suggested links</h2>
        <p class="mb-2 text-sm text-zinc-600 dark:text-zinc-400">Offered first when linking its assets, and those of its subcategories, to others.</p>
        <form class="space-y-2" onsubmit={saveSuggestions}>
          {#each relationTypes as t (t.id)}
            <label class="flex items-center gap-2 text-sm">
              <input type="checkbox" value={t.id} bind:group={suggested} /> {t.is_symmetric ? t.name : `${t.name} / ${t.inverse_name}`}
            </label>
          {/each}
          <button type="submit" class="btn-secondary">Save suggestions</button>
        </form>
      </section>
    {/if}
    {#if (conflicts[id] ?? 0) > 0}
      <p class="rounded border border-amber-400 bg-amber-50 p-3 text-sm text-amber-900 dark:border-amber-600 dark:bg-amber-950 dark:text-amber-100" role="note">
        <span aria-hidden="true">⚠</span>
        {conflicts[id] === 1 ? '1 asset' : `${conflicts[id]} assets`} here show a scan template chosen by a tie with another
        category as close to them. <a class="underline" href="/assets?conflict=scan_template">See which</a>, and name a
        template on one of the categories to settle it.
      </p>
    {/if}
    {#if canManage && scanTemplates.length}
      <section aria-labelledby="scan-template-heading">
        <h2 id="scan-template-heading" class="mb-1 text-lg font-semibold">Scan template</h2>
        <p class="mb-2 text-sm text-zinc-600 dark:text-zinc-400">What scanning its assets, and those of its subcategories, shows, unless they choose their own.</p>
        <form class="flex flex-wrap items-end gap-2" onsubmit={saveScanTemplate}>
          <div>
            <label class="label" for="scan-template">Shows</label>
            <select id="scan-template" class="input" bind:value={scanTemplate} onchange={checkScanTemplate}>
              <option value="">Its parent’s, or the default</option>
              {#each scanTemplates as t (t.id)}<option value={t.id}>{t.name}{t.default ? ' (default)' : ''}</option>{/each}
            </select>
          </div>
          <button type="submit" class="btn-secondary" disabled={scanTemplate === savedScanTemplate}>Save scan template</button>
        </form>
        {#if newTies.length}
          <p class="mt-2 text-sm text-amber-800 dark:text-amber-300" role="alert">
            <span aria-hidden="true">⚠</span>
            Saving this ties {newTies.filter((c) => c !== id).map(categoryName).join(', ') || 'it'} with this category for some
            assets in both: they would get a template chosen by which category is deeper, smaller or older.
          </p>
        {/if}
      </section>
    {/if}
    {#if canManage && hooks.length}
      <section aria-labelledby="hooks-heading">
        <h2 id="hooks-heading" class="mb-1 text-lg font-semibold">Hooks</h2>
        <p class="mb-2 text-sm text-zinc-600 dark:text-zinc-400">Buttons offered on its assets, such as Identify. <a class="underline" href="/hooks">Define hooks</a>.</p>
        <form class="space-y-2" onsubmit={saveHooks}>
          {#each hooks as a (a.id)}
            <div class="flex flex-wrap items-center gap-4 text-sm">
              <label class="flex min-w-48 items-center gap-2"><input type="checkbox" bind:checked={offered[a.id]!.on} /> {a.label}</label>
              {#if offered[a.id]?.on}
                <label class="flex items-center gap-2"><input type="checkbox" bind:checked={offered[a.id]!.cascades} /> On its subcategories’ assets too</label>
              {/if}
            </div>
          {/each}
          <button type="submit" class="btn-secondary">Save hooks</button>
        </form>
      </section>
    {/if}
  </div>

  {#if canSeeScopes && scopes.length}
    <section class="mt-6 max-w-2xl" id="tab-scopes" aria-labelledby="scopes-heading" hidden={tab !== 'scopes'}>
      <h2 id="scopes-heading" class="mb-1 text-lg font-semibold">Who can see its assets</h2>
      <p class="mb-2 text-sm text-zinc-600 dark:text-zinc-400">
        Its assets, and those of its subcategories, are in the scopes ticked, and roles holding permissions in them act on
        those assets. Ticking <strong>public</strong> lets anyone see them, even signed out, as by scanning a label. Assets in no
        scope are in <span class="font-mono">default</span>.
      </p>
      <form class="space-y-2" onsubmit={saveScopes}>
        {#each scopes as s (s.id)}
          <label class="flex items-center gap-2 text-sm">
            <input type="checkbox" value={s.id} bind:group={scopeIds} disabled={!canScope} /> <span class="font-mono">{s.name}</span>{#if s.name === 'public'}<span class="text-zinc-600 dark:text-zinc-400">: anyone, even signed out</span>{/if}
          </label>
        {/each}
        {#if canScope}<button type="submit" class="btn-secondary">Save scopes</button>{/if}
      </form>
    </section>
  {/if}
{/if}

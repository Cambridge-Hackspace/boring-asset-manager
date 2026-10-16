<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { request } from '../../lib/api';
  import { amount } from '../../lib/assets';
  import { messageOf } from '../../lib/errors';
  import { UNCATEGORIZED, type Category, type EffectiveField, type Field, type RelationType, type Scope } from '../../lib/models';
  import { navigate, router } from '../../lib/router.svelte';
  import { DEFAULT_SCOPE } from '../../lib/permissions';
  import { session } from '../../lib/session.svelte';
  import { ancestry, flattenTree, subtree } from '../../lib/tree';
  import CategoryBadge from '../../components/CategoryBadge.svelte';
  import ConfirmButton from '../../components/ConfirmButton.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

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
  // Scopes (README §3.3): category managers see them; only user managers
  // change them, since they decide who reaches the assets.
  const canSeeScopes = $derived(session.canAny('categories.manage', 'users.manage'));
  const canScope = $derived(session.can('users.manage'));
  let scopes = $state<Scope[]>([]);
  let scopeIds = $state<string[]>([]);
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

  onMount(load);

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
        const level = await request<{ active_count: number; total?: number }>('GET', `/categories/${encodeURIComponent(id)}`);
        activeCount = level.active_count;
        total = level.total ?? null;
        effective = (await request<{ fields: EffectiveField[] }>('GET', `/categories/${encodeURIComponent(id)}/fields`)).fields ?? [];
        if (canManage) {
          allFields = (await request<{ fields: Field[] }>('GET', '/fields')).fields ?? [];
          relationTypes = (await request<{ relation_types: RelationType[] }>('GET', '/relation-types')).relation_types ?? [];
          suggested = (await request<{ relation_type_ids: string[] }>('GET', `/categories/${encodeURIComponent(id)}/relation-types`)).relation_type_ids ?? [];
        }
        if (canSeeScopes) {
          scopes = ((await request<{ scopes: Scope[] }>('GET', '/scopes')).scopes ?? []).filter((s) => s.name !== DEFAULT_SCOPE); // public goes on categories; default cannot
          scopeIds = (await request<{ scope_ids: string[] }>('GET', `/categories/${encodeURIComponent(id)}/scopes`)).scope_ids ?? [];
        }
      }
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
  }

  async function saveSuggestions(event: SubmitEvent) {
    event.preventDefault();
    error = saved = '';
    try {
      suggested = (await request<{ relation_type_ids: string[] }>('PUT', `/categories/${encodeURIComponent(id)}/relation-types`, { relation_type_ids: suggested })).relation_type_ids ?? [];
      saved = 'Suggestions saved.';
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function saveScopes(event: SubmitEvent) {
    event.preventDefault();
    error = saved = '';
    try {
      scopeIds = (await request<{ scope_ids: string[] }>('PUT', `/categories/${encodeURIComponent(id)}/scopes`, { scope_ids: scopeIds })).scope_ids ?? [];
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

<h1 class="mb-4 flex items-center gap-3 text-2xl font-bold">
  {#if isNew}New category{:else if category}<span aria-hidden="true" class="inline-block h-5 w-5 rounded-full border border-zinc-300 dark:border-zinc-600" style="background-color: {category.color_hex}"></span>{category.name}{:else}Category{/if}
</h1>

<ErrorMessage {error} />
{#if saved}<p class="mb-2 text-sm text-green-700 dark:text-green-400" role="status">{saved}</p>{/if}

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading…</p>
{:else if isNew || category}
  {#if category?.is_consumable}
    <section class="card mb-6 max-w-2xl" aria-labelledby="stock-heading">
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
    <form class="card mb-6 max-w-2xl space-y-4" onsubmit={save}>
      <div class="grid gap-4 sm:grid-cols-2">
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
            <input id="cat-color" type="color" class="h-9 w-14 rounded border border-zinc-300 dark:border-zinc-700" bind:value={color} />
            <CategoryBadge category={{ id: 'preview', name: name || 'Preview', color_hex: color }} />
          </div>
        </div>
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
            <input
              id="cat-threshold"
              class="input w-32"
              type="number"
              min="1"
              step="1"
              disabled={!consumable}
              aria-describedby="threshold-help"
              bind:value={threshold}
            />
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
      </div>
      <div class="flex flex-wrap gap-2">
        <button type="submit" class="btn-primary" disabled={busy}>{isNew ? 'Create category' : 'Save'}</button>
        {#if !isNew}
          <a class="btn-secondary" href="/categories/new?parent={encodeURIComponent(id)}">Add subcategory</a>
          <ConfirmButton label="Delete" question="Delete this category? Its subcategories move to the top level." onconfirm={remove} />
        {/if}
      </div>
    </form>
  {:else if isSystem}
    <p class="mb-6 text-sm text-zinc-600 dark:text-zinc-400">The system category for assets with no other category. It cannot be renamed, moved or deleted.</p>
  {/if}

  {#if !isNew}
    <section aria-labelledby="fields-heading">
      <h2 id="fields-heading" class="mb-2 text-lg font-semibold">Fields</h2>
      {#if effective.length === 0}
        <p class="text-sm text-zinc-600 dark:text-zinc-400">Assets in this category record no fields yet.</p>
      {:else}
        <table class="mb-4 w-full max-w-3xl text-left text-sm">
          <thead class="border-b border-zinc-300 dark:border-zinc-700">
            <tr>
              <th scope="col" class="py-2 pr-4">Field</th>
              <th scope="col" class="py-2 pr-4">Required</th>
              <th scope="col" class="py-2 pr-4">From</th>
              <th scope="col" class="py-2"><span class="sr-only">Actions</span></th>
            </tr>
          </thead>
          <tbody>
            {#each effective as e (e.field.id)}
              {@const source = e.source_category_id ? byId.get(e.source_category_id) : undefined}
              <tr class="border-b border-zinc-200 dark:border-zinc-800">
                <td class="py-2 pr-4">{e.field.label} <span class="font-mono text-zinc-600 dark:text-zinc-400">{e.field.key}</span></td>
                <td class="py-2 pr-4">{e.is_required ? 'Required' : 'Optional'}</td>
                <td class="py-2 pr-4">
                  {#if e.source_category_id === id}This category{:else if source}Inherited from <CategoryBadge category={source} />{/if}
                </td>
                <td class="py-2 text-right">
                  {#if canManage && e.source_category_id === id}
                    <ConfirmButton label="Remove" question="Stop asking for “{e.field.label}” here?" onconfirm={() => unassign(e.field.id)} />
                  {/if}
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}

      {#if canManage && unassigned.length}
        <form class="flex flex-wrap items-end gap-3" onsubmit={assign}>
          <div>
            <label class="label" for="assign-field">Add a field</label>
            <select id="assign-field" class="input" bind:value={newFieldId}>
              <option value="">Choose a field…</option>
              {#each unassigned as f (f.id)}
                <option value={f.id}>{f.label}</option>
              {/each}
            </select>
          </div>
          <label class="flex items-center gap-2 pb-2 text-sm"><input type="checkbox" bind:checked={newRequired} /> Required</label>
          <button type="submit" class="btn-secondary" disabled={!newFieldId}>Add field</button>
        </form>
        <p class="mt-2 text-xs text-zinc-600 dark:text-zinc-400">Adding a field another category already gives this one makes it this category’s own; required anywhere means required.</p>
      {/if}
    </section>
    {#if canManage && relationTypes.length}
      <section class="mt-6 max-w-2xl" aria-labelledby="relations-heading">
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
    {#if canSeeScopes && scopes.length}
      <section class="mt-6 max-w-2xl" aria-labelledby="scopes-heading">
        <h2 id="scopes-heading" class="mb-1 text-lg font-semibold">Scopes</h2>
        <p class="mb-2 text-sm text-zinc-600 dark:text-zinc-400">
          Its assets, and those of its subcategories, are in these scopes, and roles holding permissions in them act on
          those assets. Assets in no scope are in <span class="font-mono">default</span>.
        </p>
        <form class="space-y-2" onsubmit={saveScopes}>
          {#each scopes as s (s.id)}
            <label class="flex items-center gap-2 text-sm">
              <input type="checkbox" value={s.id} bind:group={scopeIds} disabled={!canScope} /> <span class="font-mono">{s.name}</span>
            </label>
          {/each}
          {#if canScope}<button type="submit" class="btn-secondary">Save scopes</button>{/if}
        </form>
      </section>
    {/if}
  {/if}
{/if}

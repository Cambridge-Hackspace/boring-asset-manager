<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { ApiError, request } from '../../lib/api';
  import { assetPayload, errorTarget, inputType, keptValues, stockDraft, type StockDraft } from '../../lib/assets';
  import { messageOf } from '../../lib/errors';
  import { UNCATEGORIZED, type AssetWithDetails, type Category, type EffectiveField, type GeneratorInfo } from '../../lib/models';
  import { navigate } from '../../lib/router.svelte';
  import { session } from '../../lib/session.svelte';
  import { addToSpool, queueNewLabels, rememberQueueNewLabels } from '../../lib/spool';
  import { flattenTree } from '../../lib/tree';
  import CategoryBadge from '../../components/CategoryBadge.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  // /assets/new and /assets/:id/edit
  let { params }: { params: Record<string, string> } = $props();
  const id = untrack(() => params.id ?? '');
  const isNew = !id;

  let categories = $state<Category[]>([]);
  let generators = $state<GeneratorInfo[]>([]);
  let effective = $state<EffectiveField[]>([]);
  let original = $state<AssetWithDetails | null>(null);
  let loading = $state(true);
  let busy = $state(false);
  let error = $state('');
  let errorFor = $state<string | null>(null);

  let name = $state('');
  let categoryIds = $state<string[]>([]);
  let codes = $state<Record<string, string>>({});
  let values = $state<Record<string, string>>({});
  let stock = $state<StockDraft>(stockDraft(null));
  // New assets' labels join the spool (README §6.2), as each person last chose.
  const canSpool = isNew && session.can('labels.print');
  let queueLabel = $state(canSpool && queueNewLabels(session.user?.id ?? ''));

  const tree = $derived(flattenTree(categories.filter((c) => c.id !== UNCATEGORIZED)));
  const kept = $derived(original ? keptValues(original.field_values, effective) : []);

  onMount(async () => {
    try {
      const [cats, gens] = await Promise.all([
        request<{ categories: Category[] }>('GET', '/categories'),
        request<{ generators: GeneratorInfo[] }>('GET', '/generators'),
      ]);
      categories = cats.categories ?? [];
      generators = gens.generators ?? [];
      if (!isNew) {
        const a = (await request<{ asset: AssetWithDetails }>('GET', `/assets/${encodeURIComponent(id)}`)).asset;
        original = a;
        name = a.asset.name;
        categoryIds = a.categories.map((c) => c.id).filter((c) => c !== UNCATEGORIZED);
        codes = Object.fromEntries(a.generator_keys.map((k) => [k.generator_type, k.assigned_code]));
        values = { ...a.field_values };
        stock = stockDraft(a.asset.stock);
      }
      await resolveFields();
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
  });

  // The fields depend on the chosen categories; ask the server, which owns
  // the inheritance rules.
  let resolving = 0;
  async function resolveFields() {
    const ticket = ++resolving;
    try {
      const res = await request<{ fields: EffectiveField[] }>('POST', '/categories/effective-fields', { category_ids: categoryIds });
      if (ticket === resolving) effective = res.fields ?? [];
    } catch (err) {
      if (ticket === resolving) error = messageOf(err);
    }
  }

  function toggleCategory(catId: string, checked: boolean) {
    categoryIds = checked ? [...categoryIds, catId] : categoryIds.filter((c) => c !== catId);
    void resolveFields();
  }

  async function save(event: SubmitEvent) {
    event.preventDefault();
    error = '';
    errorFor = null;
    busy = true;
    const body = assetPayload({ name, categoryIds, codes, values, stock }, effective, original?.asset.stock);
    try {
      if (isNew) {
        const res = await request<{ asset: AssetWithDetails }>('POST', '/assets', body);
        if (canSpool) {
          rememberQueueNewLabels(session.user?.id ?? '', queueLabel);
          // The asset is made either way; one the spool cannot take now can
          // still be added from its page.
          if (queueLabel) await addToSpool({ asset_ids: [res.asset.asset.id] }).catch(() => undefined);
        }
        navigate(`/assets/${encodeURIComponent(res.asset.asset.id)}`, true);
      } else {
        await request('PUT', `/assets/${encodeURIComponent(id)}`, body);
        navigate(`/assets/${encodeURIComponent(id)}`, true);
      }
    } catch (err) {
      error = messageOf(err);
      if (err instanceof ApiError) {
        errorFor = errorTarget(err.message, effective, generators.map((g) => g.type));
        if (errorFor) document.getElementById(errorFor)?.focus();
      }
    } finally {
      busy = false;
    }
  }

  const describedBy = (target: string, help?: string) =>
    [help, errorFor === target ? `${target}-error` : undefined].filter(Boolean).join(' ') || undefined;
</script>

<nav aria-label="Breadcrumb" class="mb-2 text-sm">
  <a class="underline" href="/assets">Assets</a>
  {#if original}<span aria-hidden="true" class="mx-1 text-zinc-400">/</span><a class="underline" href="/assets/{encodeURIComponent(id)}">{original.asset.name}</a>{/if}
</nav>
<h1 class="mb-4 text-2xl font-bold">{isNew ? 'New asset' : 'Edit asset'}</h1>

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading…</p>
{:else}
  <form class="max-w-3xl space-y-6" onsubmit={save} novalidate>
    {#if error && !errorFor}<ErrorMessage {error} />{/if}

    <div class="card space-y-4">
      <div>
        <label class="label" for="asset-name">Name</label>
        <input id="asset-name" class="input" required bind:value={name} />
      </div>
      <fieldset>
        <legend class="label">Categories</legend>
        {#if tree.length === 0}
          <p class="text-sm text-zinc-600 dark:text-zinc-400">No categories yet; the asset will be Uncategorized.</p>
        {:else}
          <ul class="max-h-64 space-y-1 overflow-y-auto">
            {#each tree as row (row.category.id)}
              <li style="padding-left: {row.depth * 1.25}rem">
                <label class="flex items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    checked={categoryIds.includes(row.category.id)}
                    onchange={(e) => toggleCategory(row.category.id, e.currentTarget.checked)}
                  />
                  <CategoryBadge category={row.category} />
                </label>
              </li>
            {/each}
          </ul>
        {/if}
      </fieldset>
    </div>

    <fieldset class="card space-y-4">
      <legend class="text-lg font-semibold">Stock</legend>
      <div>
        <label class="flex items-center gap-2 text-sm">
          <input type="checkbox" bind:checked={stock.counted} aria-describedby="stock-help" /> Count stock by quantity
        </label>
        <p id="stock-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">
          For things kept in bulk, such as screws or solder, where this one asset stands for many. Leave it off for things
          labeled one by one, such as filament spools: a consumable category counts those.
        </p>
      </div>
      {#if stock.counted}
        <div class="grid gap-4 sm:grid-cols-3">
          <div>
            <label class="label" for="stock-quantity">Quantity</label>
            <input id="stock-quantity" class="input" type="number" min="0" step="1" bind:value={stock.quantity} />
          </div>
          <div>
            <label class="label" for="stock-unit">Unit (optional)</label>
            <input id="stock-unit" class="input" placeholder="pcs, g, m" bind:value={stock.unit} />
          </div>
          <div>
            <label class="label" for="stock-threshold">Low stock threshold (optional)</label>
            <input
              id="stock-threshold"
              class="input"
              type="number"
              min="1"
              step="1"
              aria-describedby="stock-threshold-help"
              bind:value={stock.threshold}
            />
          </div>
        </div>
        <p id="stock-threshold-help" class="text-xs text-zinc-600 dark:text-zinc-400">
          Flag it automatically when the quantity drops below the threshold, and clear the flag when there is enough again.
        </p>
      {/if}
    </fieldset>

    <fieldset class="card space-y-4">
      <legend class="text-lg font-semibold">Fields</legend>
      {#if effective.length === 0}
        <p class="text-sm text-zinc-600 dark:text-zinc-400">The chosen categories record no fields.</p>
      {/if}
      {#each effective as e (e.field.id)}
        {@const target = `field:${e.field.key}`}
        <div>
          <label class="label" for={target}>
            {e.field.label}{#if e.is_required}<span aria-hidden="true" class="text-red-700 dark:text-red-400"> *</span>{/if}
          </label>
          {#if e.field.validation_type === 'select'}
            <select id={target} class="input" required={e.is_required} aria-invalid={errorFor === target} aria-describedby={describedBy(target)} bind:value={values[e.field.key]}>
              <option value="">{e.is_required ? 'Choose…' : 'None'}</option>
              {#each e.field.options?.choices ?? [] as c (c.value)}
                <option value={c.value}>{c.label}</option>
              {/each}
            </select>
          {:else}
            <input
              id={target}
              class="input"
              type={inputType(e.field.validation_type)}
              required={e.is_required}
              aria-invalid={errorFor === target}
              aria-describedby={describedBy(target)}
              bind:value={values[e.field.key]}
            />
          {/if}
          {#if errorFor === target}<p id="{target}-error" class="error-text mt-1" role="alert">{error}</p>{/if}
        </div>
      {/each}
      {#if kept.length}
        <p class="text-sm text-zinc-600 dark:text-zinc-400">
          Kept from former categories, unchanged: {kept.map(([k]) => k).join(', ')}.
        </p>
      {/if}
      <p class="text-xs text-zinc-600 dark:text-zinc-400"><span aria-hidden="true">*</span> Required</p>
    </fieldset>

    <fieldset class="card space-y-4">
      <legend class="text-lg font-semibold">Codes</legend>
      <p class="text-sm text-zinc-600 dark:text-zinc-400">Short codes printed as barcodes. Leave blank for none; each code belongs to one asset at a time.</p>
      {#each generators as g (g.type)}
        {@const target = `code:${g.type}`}
        <div>
          <label class="label" for={target}>{g.name}</label>
          <input
            id={target}
            class="input font-mono"
            aria-invalid={errorFor === target}
            aria-describedby={describedBy(target, `${target}-help`)}
            bind:value={codes[g.type]}
          />
          <p id="{target}-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">{g.code_rules}</p>
          {#if errorFor === target}<p id="{target}-error" class="error-text mt-1" role="alert">{error}</p>{/if}
        </div>
      {/each}
    </fieldset>

    {#if canSpool}
      <label class="flex items-center gap-2 text-sm"><input type="checkbox" bind:checked={queueLabel} /> Add a label to the spool</label>
    {/if}
    <div class="flex gap-2">
      <button type="submit" class="btn-primary" disabled={busy}>{isNew ? 'Create asset' : 'Save'}</button>
      <a class="btn-secondary" href={isNew ? '/assets' : `/assets/${encodeURIComponent(id)}`}>Cancel</a>
    </div>
  </form>
{/if}

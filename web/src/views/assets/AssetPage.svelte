<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { request } from '../../lib/api';
  import { isWebURL, keptValues } from '../../lib/assets';
  import { messageOf } from '../../lib/errors';
  import type { AssetWithDetails, GeneratorInfo } from '../../lib/models';
  import { navigate } from '../../lib/router.svelte';
  import { session } from '../../lib/session.svelte';
  import CategoryBadge from '../../components/CategoryBadge.svelte';
  import ConfirmButton from '../../components/ConfirmButton.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';
  import LabelActions from '../../components/LabelActions.svelte';

  let { params }: { params: Record<string, string> } = $props();
  const id = untrack(() => params.id ?? '');

  let asset = $state<AssetWithDetails | null>(null);
  let generators = $state<GeneratorInfo[]>([]);
  let error = $state('');
  let loading = $state(true);

  onMount(async () => {
    try {
      [asset, generators] = await Promise.all([
        request<{ asset: AssetWithDetails }>('GET', `/assets/${encodeURIComponent(id)}`).then((r) => r.asset),
        request<{ generators: GeneratorInfo[] }>('GET', '/generators').then((r) => r.generators ?? []),
      ]);
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
  });

  const generatorName = (type: string) => generators.find((g) => g.type === type)?.name ?? type;

  async function archive() {
    try {
      await request('DELETE', `/assets/${encodeURIComponent(id)}`);
      navigate('/assets');
    } catch (err) {
      error = messageOf(err);
    }
  }
</script>

<nav aria-label="Breadcrumb" class="mb-2 text-sm"><a class="underline" href="/assets">Assets</a></nav>
<ErrorMessage {error} />

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading…</p>
{:else if asset}
  <div class="mb-4 flex flex-wrap items-center justify-between gap-2">
    <h1 class="text-2xl font-bold">{asset.asset.name}</h1>
    <div class="flex flex-wrap gap-2">
      {#if session.can('assets:write')}<a class="btn-secondary" href="/assets/{encodeURIComponent(id)}/edit">Edit</a>{/if}
      {#if session.can('assets:delete')}
        <ConfirmButton label="Archive" question="Archive this asset? Its codes become free for reuse." onconfirm={archive} />
      {/if}
    </div>
  </div>

  <div class="grid gap-6 lg:grid-cols-2">
    <section class="card" aria-labelledby="details-heading">
      <h2 id="details-heading" class="mb-3 text-lg font-semibold">Details</h2>
      <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
        <dt class="font-medium">Categories</dt>
        <dd class="flex flex-wrap gap-1">
          {#each asset.categories as c (c.id)}<CategoryBadge category={c} href="/categories/{encodeURIComponent(c.id)}" />{/each}
        </dd>
        {#each asset.effective_fields as e (e.field.id)}
          <dt class="font-medium">{e.field.label}</dt>
          <dd>
            {#if asset.field_values[e.field.key]}
              {@const value = asset.field_values[e.field.key]!}
              {#if e.field.validation_type === 'url' && isWebURL(value)}
                <a class="underline" href={value} rel="noopener noreferrer" target="_blank">{value}</a>
              {:else if e.field.validation_type === 'select'}
                {e.field.options?.choices.find((c) => c.value === value)?.label ?? value}
              {:else}
                {value}
              {/if}
            {:else}
              <span class="text-zinc-600 dark:text-zinc-400">{e.is_required ? 'Missing' : '—'}</span>
            {/if}
          </dd>
        {/each}
        {#each asset.generator_keys as k (k.generator_type)}
          <dt class="font-medium">{generatorName(k.generator_type)}</dt>
          <dd class="font-mono">{k.assigned_code}</dd>
        {/each}
      </dl>
      {#if keptValues(asset.field_values, asset.effective_fields).length}
        <details class="mt-4 text-sm">
          <summary class="cursor-pointer">Values kept from former categories</summary>
          <dl class="mt-2 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1">
            {#each keptValues(asset.field_values, asset.effective_fields) as [key, value] (key)}
              <dt class="font-mono">{key}</dt>
              <dd>{value}</dd>
            {/each}
          </dl>
        </details>
      {/if}
    </section>

    <LabelActions assetIds={[id]} />
  </div>
{/if}

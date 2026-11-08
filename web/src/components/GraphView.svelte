<!--
  A graph of followed links (docs/data-model.md#following-links), drawn and
  listed: an asset's, with what needs it to the left and what it needs to
  the right, by hops, or the map of a category or a search. Each box opens
  its asset's graph, redrawn around it. The nested list below shows the same
  assets, for the keyboard and screen readers. The drawing downloads as SVG
  and PDF, which the server draws from the same layout.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../lib/api';
  import { messageOf } from '../lib/errors';
  import { BOX_H, BOX_W, assetNote, linkPath, nested, place, summary, type ListItem } from '../lib/graph';
  import type { AssetGraph, GraphAsset, RelationType } from '../lib/models';
  import AssetIcon from './AssetIcon.svelte';
  import ErrorMessage from './ErrorMessage.svelte';

  let {
    assetId = '',
    query = {},
  }: {
    /** The asset whose graph it is; none for a map. */
    assetId?: string;
    /** For a map, the asset list's filters: category_id and search. */
    query?: Record<string, string>;
  } = $props();

  let followed = $state<RelationType[]>([]);
  let chosen = $state<string[]>([]);
  let direction = $state<'both' | 'out' | 'in'>('both');
  let depth = $state(5);
  let graph = $state<AssetGraph | null>(null);
  let loading = $state(true);
  let error = $state('');

  const base = $derived(assetId ? `/assets/${encodeURIComponent(assetId)}/graph` : '/assets/graph');
  const params = $derived.by(() => {
    const q = new URLSearchParams(Object.entries(query).filter(([, v]) => v));
    if (chosen.length && chosen.length < followed.length) q.set('types', chosen.join(','));
    if (assetId) {
      if (direction !== 'both') q.set('direction', direction);
      if (depth !== 5) q.set('depth', String(depth));
    }
    return q.toString();
  });
  const address = (suffix = '') => `${base}${suffix}${params ? `?${params}` : ''}`;

  async function load() {
    error = '';
    loading = true;
    try {
      graph = (await request<{ graph: AssetGraph }>('GET', address())).graph;
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
  }

  onMount(async () => {
    try {
      const types = (await request<{ relation_types: RelationType[] }>('GET', '/relation-types')).relation_types ?? [];
      followed = types.filter((t) => t.is_followed);
      chosen = followed.map((t) => t.id);
    } catch (err) {
      error = messageOf(err);
    }
    await load();
  });

  function redraw(event: SubmitEvent) {
    event.preventDefault();
    void load();
  }

  const placed = $derived(graph ? place(graph) : null);
  const byId = $derived(new Map((graph?.assets ?? []).map((a) => [a.id, a])));
  const symmetric = $derived(new Set((graph?.types ?? []).filter((t) => t.is_symmetric).map((t) => t.id)));
  const typeName = $derived(new Map((graph?.types ?? []).map((t) => [t.id, t.name])));
  const parts = $derived(graph ? nested(graph) : []);
  const notes = $derived(graph ? summary(graph) : []);
  const root = $derived(graph?.root ? byId.get(graph.root) : undefined);

  /** Where a box leads: its asset's page, on its Graph tab, redrawn around it. */
  const graphHref = (a: GraphAsset) => `/assets/${encodeURIComponent(a.id)}?tab=graph`;
  const hops = (a: GraphAsset) => (a.side === 'root' || !graph?.root ? '' : a.hops === 1 ? '1 hop' : `${a.hops} hops`);
  const describe = (a: GraphAsset) => [a.name, assetNote(a), hops(a)].filter(Boolean).join(', ');
</script>

<div class="space-y-4">
  <form class="flex flex-wrap items-end gap-3" onsubmit={redraw} aria-label="What the graph follows">
    {#if assetId}
      <div>
        <label class="label" for="graph-direction">Which way</label>
        <select id="graph-direction" class="input" bind:value={direction}>
          <option value="both">Both ways</option>
          <option value="out">What it needs</option>
          <option value="in">What needs it</option>
        </select>
      </div>
      <div>
        <label class="label" for="graph-depth">Hops</label>
        <select id="graph-depth" class="input" bind:value={depth}>
          {#each [1, 2, 3, 4, 5, 6, 7, 8, 9, 10] as n (n)}<option value={n}>{n}</option>{/each}
        </select>
      </div>
    {/if}
    {#if followed.length > 1}
      <fieldset>
        <legend class="label">Link types</legend>
        <div class="flex flex-wrap gap-x-4">
          {#each followed as t (t.id)}
            <label class="inline-flex min-h-11 items-center gap-2 text-sm"><input type="checkbox" value={t.id} bind:group={chosen} />{t.name}</label>
          {/each}
        </div>
      </fieldset>
    {/if}
    <button type="submit" class="btn-secondary" disabled={loading}>Draw</button>
    <a class="btn-secondary" href="/api/v1{address('.svg')}" download>Drawing as SVG</a>
    <a class="btn-secondary" href="/api/v1{address('.pdf')}" download>Drawing as PDF</a>
  </form>

  <ErrorMessage {error} />
  {#if loading && !graph}
    <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Following links…</p>
  {:else if graph && placed}
    {#if !followed.length}
      <p class="text-sm">No link type is followed yet. Mark one as followed under <a class="underline" href="/relations">Link types</a>, as <em>depends on</em> is.</p>
    {/if}
    <div class="space-y-1 text-sm text-zinc-700 dark:text-zinc-300" role="status">
      {#if graph.root}<p>What needs it is to the left, what it needs to the right. Arrows point to what is needed.</p>
      {:else}<p>Each asset is to the left of what it needs. Arrows point to what is needed.</p>{/if}
      {#each notes as n, i (i)}<p>{n}</p>{/each}
    </div>

    <div class="overflow-x-auto rounded border border-zinc-300 bg-white dark:border-zinc-700 dark:bg-zinc-900" role="region" aria-label="Drawing" data-graph-drawing>
      <div class="relative" style:width="{placed.width}px" style:height="{placed.height}px">
        <svg class="absolute inset-0 text-zinc-500 dark:text-zinc-400" width={placed.width} height={placed.height} aria-hidden="true">
          <defs>
            <marker id="graph-arrow" viewBox="0 0 10 10" refX="10" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
              <path d="M 0 0 L 10 5 L 0 10 z" fill="currentColor" />
            </marker>
          </defs>
          {#each graph.links as l (l.id)}
            {@const from = placed.at.get(l.from)}
            {@const to = placed.at.get(l.to)}
            {#if from && to}
              <path
                d={linkPath(from, to)}
                fill="none"
                stroke="currentColor"
                stroke-width="1.5"
                stroke-dasharray={symmetric.has(l.type_id) ? '5 4' : undefined}
                marker-end={symmetric.has(l.type_id) ? undefined : 'url(#graph-arrow)'}
              ><title>{byId.get(l.from)?.name} {typeName.get(l.type_id)} {byId.get(l.to)?.name}</title></path>
            {/if}
          {/each}
        </svg>
        {#each graph.assets as a (a.id)}
          {@const p = placed.at.get(a.id)!}
          <a
            href={graphHref(a)}
            aria-label={describe(a)}
            title="Draw the graph around {a.name}"
            data-graph-asset={a.id}
            class="absolute flex items-center gap-2 rounded border bg-white px-2 text-sm hover:bg-zinc-50 dark:bg-zinc-900 dark:hover:bg-zinc-800
              {a.id === graph.root ? 'border-2 border-zinc-900 font-semibold dark:border-zinc-100' : 'border-zinc-400 dark:border-zinc-500'}
              {a.archived ? 'border-dashed text-zinc-600 dark:text-zinc-400' : ''}"
            style:left="{p.x}px"
            style:top="{p.y}px"
            style:width="{BOX_W}px"
            style:height="{BOX_H}px"
          >
            {#if a.condition === 'out_of_service'}<span class="h-3 w-3 shrink-0 rounded-full bg-red-600" aria-hidden="true"></span>
            {:else if a.condition === 'needs_maintenance'}<span class="h-3 w-3 shrink-0 rounded-full bg-amber-500" aria-hidden="true"></span>{/if}
            <AssetIcon icon={a.icon} height={28} />
            <span class="min-w-0">
              <span class="block truncate">{a.name}</span>
              {#if assetNote(a)}<span class="block truncate text-xs font-normal text-zinc-600 dark:text-zinc-400">{assetNote(a)}</span>{/if}
            </span>
          </a>
        {/each}
      </div>
    </div>

    <section aria-labelledby="graph-list-heading">
      <h2 id="graph-list-heading" class="section-title mb-2">As a list</h2>
      {#if root}
        <p class="text-sm">
          <a class="inline-flex min-h-11 items-center font-semibold underline" href="/assets/{encodeURIComponent(root.id)}">{root.name}</a>{#if assetNote(root)}, {assetNote(root).toLowerCase()}{/if}
        </p>
      {/if}
      {#each parts as part (part.heading)}
        <h3 class="mt-3 font-semibold">{part.heading}</h3>
        {@render list(part.items)}
      {/each}
      {#if !parts.length && !root}<p class="text-sm text-zinc-600 dark:text-zinc-400">No assets.</p>{/if}
    </section>
  {/if}
</div>

{#snippet list(items: ListItem[])}
  <ul class="list-disc space-y-1 pl-5 text-sm" data-graph-list>
    {#each items as it (it.asset.id)}
      <li>
        <a class="inline-flex min-h-11 items-center underline" href="/assets/{encodeURIComponent(it.asset.id)}" data-graph-listed={it.asset.id}>{it.asset.name}</a
        >{#if assetNote(it.asset)}, {assetNote(it.asset).toLowerCase()}{/if}{#if hops(it.asset)}, {hops(it.asset)}{/if}{#if it.also.length}; also linked to {it.also.join(', ')}{/if}.
        <a class="ml-1 inline-flex min-h-11 items-center text-xs underline" href={graphHref(it.asset)}>Draw around it<span class="sr-only">: {it.asset.name}</span></a>
        {#if it.children.length}{@render list(it.children)}{/if}
      </li>
    {/each}
  </ul>
{/snippet}

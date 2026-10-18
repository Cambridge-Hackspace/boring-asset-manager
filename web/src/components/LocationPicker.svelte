<script lang="ts">
  import { request } from '../lib/api';
  import { messageOf } from '../lib/errors';
  import type { AssetWithDetails } from '../lib/models';

  // Choosing where an asset is: the asset it is in, found by name or code,
  // or nowhere (README §5.3).
  let {
    value,
    exclude,
    id = 'where',
    onchange,
  }: {
    value: { id: string; name: string } | null;
    /** The asset itself, which cannot be in itself. */
    exclude?: string;
    id?: string;
    onchange: (place: { id: string; name: string } | null) => void;
  } = $props();

  let search = $state('');
  let found = $state<{ id: string; name: string; where: string }[] | null>(null);
  let error = $state('');

  async function find() {
    error = '';
    const q = search.trim();
    if (!q) return;
    try {
      const list = (await request<{ assets: AssetWithDetails[] }>('GET', `/assets?search=${encodeURIComponent(q)}`)).assets ?? [];
      found = list
        .filter((a) => a.asset.id !== exclude && !a.location.some((p) => p.id === exclude))
        .slice(0, 20)
        .map((a) => ({ id: a.asset.id, name: a.asset.name, where: a.location.map((p) => p.name).join(' › ') }));
    } catch (err) {
      error = messageOf(err);
    }
  }

  function pick(place: { id: string; name: string } | null) {
    onchange(place);
    found = null;
    search = '';
  }
</script>

<div class="space-y-2">
  <p class="text-sm">
    {#if value}In <strong>{value.name}</strong>.
      <button type="button" class="underline" onclick={() => pick(null)}>Not in anything</button>
    {:else}Not set.{/if}
  </p>
  <div class="flex flex-wrap items-end gap-2">
    <div class="grow">
      <label class="label" for="{id}-search">{value ? 'Move it to' : 'Put it in'}</label>
      <input
        id="{id}-search"
        class="input"
        type="search"
        placeholder="Shelf, box or room, by name or code"
        bind:value={search}
        onkeydown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault();
            void find();
          }
        }}
      />
    </div>
    <button type="button" class="btn-secondary" onclick={find} disabled={!search.trim()}>Find</button>
  </div>
  {#if error}<p class="error-text">{error}</p>{/if}
  {#if found}
    {#if found.length === 0}
      <p class="text-sm text-zinc-600 dark:text-zinc-400">Nothing matches.</p>
    {:else}
      <ul class="space-y-1 text-sm" aria-label="Places found">
        {#each found as f (f.id)}
          <li>
            <button type="button" class="underline" onclick={() => pick({ id: f.id, name: f.name })}>{f.name}</button>
            {#if f.where}<span class="text-zinc-600 dark:text-zinc-400"> in {f.where}</span>{/if}
          </li>
        {/each}
      </ul>
    {/if}
  {/if}
</div>

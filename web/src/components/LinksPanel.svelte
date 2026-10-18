<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../lib/api';
  import { linkChoices } from '../lib/assets';
  import { messageOf } from '../lib/errors';
  import type { AssetWithDetails, RelationType } from '../lib/models';
  import { session } from '../lib/session.svelte';
  import ErrorMessage from './ErrorMessage.svelte';

  // An asset's links to others, and adding and removing them.
  let { asset, onchange }: { asset: AssetWithDetails; onchange: (a: AssetWithDetails, done: string) => void } = $props();

  let types = $state<RelationType[]>([]);
  let choice = $state('');
  let search = $state('');
  let found = $state<{ id: string; name: string }[] | null>(null);
  let otherId = $state('');
  let busy = $state(false);
  let error = $state('');

  const canEdit = $derived(session.canOn(asset.scopes, 'assets.write') && !asset.asset.archived_at);
  const choices = $derived(linkChoices(types, asset.suggested_relation_type_ids));
  const path = $derived(`/assets/${encodeURIComponent(asset.asset.id)}/links`);

  onMount(async () => {
    // The types are for adding links; those who cannot have no need of them.
    if (!canEdit) return;
    try {
      types = (await request<{ relation_types: RelationType[] }>('GET', '/relation-types')).relation_types ?? [];
      choice = choices[0]?.key ?? '';
    } catch (err) {
      error = messageOf(err);
    }
  });

  async function find(event: SubmitEvent) {
    event.preventDefault();
    error = '';
    try {
      const list = (await request<{ assets: AssetWithDetails[] }>('GET', `/assets?search=${encodeURIComponent(search.trim())}`)).assets ?? [];
      found = list.filter((a) => a.asset.id !== asset.asset.id).slice(0, 50).map((a) => ({ id: a.asset.id, name: a.asset.name }));
      otherId = found[0]?.id ?? '';
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function act(action: () => Promise<{ asset: AssetWithDetails }>, done: string) {
    error = '';
    busy = true;
    try {
      onchange((await action()).asset, done);
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }

  function add() {
    const c = choices.find((c) => c.key === choice);
    const other = found?.find((a) => a.id === otherId);
    if (!c || !other) return;
    void act(() => request('POST', path, { type_id: c.typeId, asset_id: other.id, outgoing: c.outgoing }), `Now ${c.label} ${other.name}.`);
    found = null;
    search = '';
  }

  function remove(linkId: string, label: string, name: string) {
    void act(() => request('DELETE', `${path}/${encodeURIComponent(linkId)}`), `No longer ${label} ${name}.`);
  }
</script>

<section class="card" aria-labelledby="links-heading">
  <h2 id="links-heading" class="mb-2 text-lg font-semibold">Links</h2>
  <ErrorMessage {error} />
  {#if asset.links.length === 0}
    <p class="text-sm text-zinc-600 dark:text-zinc-400">Not linked to anything.</p>
  {:else}
    <ul class="space-y-1 text-sm">
      {#each asset.links as l (l.id)}
        <li class="flex flex-wrap items-center gap-2">
          <span>{l.label.charAt(0).toUpperCase() + l.label.slice(1)}</span>
          <a class="underline" href="/assets/{encodeURIComponent(l.asset.id)}">{l.asset.name}</a>
          {#if l.asset.archived}<span class="text-xs text-zinc-600 dark:text-zinc-400">(archived)</span>{/if}
          {#if canEdit}
            <button type="button" class="btn-secondary min-h-8 px-2 py-1 text-xs" disabled={busy} aria-label="Unlink {l.asset.name}" onclick={() => remove(l.id, l.label, l.asset.name)}>
              Unlink
            </button>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}

  {#if canEdit && choices.length}
    <form class="mt-4 flex flex-wrap items-end gap-2" onsubmit={find}>
      <div>
        <label class="label" for="link-type">This asset</label>
        <select id="link-type" class="input" bind:value={choice}>
          {#each choices as c (c.key)}<option value={c.key}>{c.label}{c.suggested ? ' (suggested)' : ''}</option>{/each}
        </select>
      </div>
      <div>
        <label class="label" for="link-search">Find an asset</label>
        <input id="link-search" class="input" type="search" placeholder="Name or code" bind:value={search} />
      </div>
      <button type="submit" class="btn-secondary">Find</button>
    </form>
    {#if found}
      {#if found.length === 0}
        <p class="mt-2 text-sm text-zinc-600 dark:text-zinc-400">No other asset matches.</p>
      {:else}
        <div class="mt-2 flex flex-wrap items-end gap-2">
          <div>
            <label class="label" for="link-other">Asset</label>
            <select id="link-other" class="input" bind:value={otherId}>
              {#each found as a (a.id)}<option value={a.id}>{a.name}</option>{/each}
            </select>
          </div>
          <button type="button" class="btn-primary" disabled={busy || !otherId} onclick={add}>Add link</button>
        </div>
      {/if}
    {/if}
  {/if}
</section>

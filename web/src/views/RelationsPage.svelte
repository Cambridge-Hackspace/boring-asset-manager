<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../lib/api';
  import { messageOf } from '../lib/errors';
  import type { RelationType } from '../lib/models';
  import ConfirmButton from '../components/ConfirmButton.svelte';
  import ErrorMessage from '../components/ErrorMessage.svelte';

  let { params: _params }: { params: Record<string, string> } = $props();

  let types = $state<RelationType[]>([]);
  let loading = $state(true);
  let error = $state('');
  let status = $state('');
  // The type being edited: null for none, '' for a new one.
  let editing = $state<string | null>(null);
  let draft = $state({ name: '', inverse_name: '', is_symmetric: false, is_single: false, is_location: false });

  const inUse = $derived(editing ? (types.find((t) => t.id === editing)?.link_count ?? 0) > 0 : false);

  async function load() {
    try {
      types = (await request<{ relation_types: RelationType[] }>('GET', '/relation-types')).relation_types ?? [];
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
  }
  onMount(load);

  function start(t?: RelationType) {
    error = status = '';
    editing = t?.id ?? '';
    draft = t
      ? { name: t.name, inverse_name: t.inverse_name, is_symmetric: t.is_symmetric, is_single: t.is_single, is_location: t.is_location }
      : { name: '', inverse_name: '', is_symmetric: false, is_single: false, is_location: false };
  }

  // A symmetric relation has no direction, so it is neither single nor a
  // location; only a single relation can be a location.
  function symmetricChanged() {
    if (draft.is_symmetric) draft.is_single = draft.is_location = false;
  }
  function singleChanged() {
    if (!draft.is_single) draft.is_location = false;
  }

  async function save(event: SubmitEvent) {
    event.preventDefault();
    error = status = '';
    const body = { ...draft, inverse_name: draft.is_symmetric ? draft.name : draft.inverse_name };
    try {
      if (editing) await request('PUT', `/relation-types/${encodeURIComponent(editing)}`, body);
      else await request('POST', '/relation-types', body);
      status = `Saved ${draft.name.trim()}.`;
      editing = null;
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function remove(t: RelationType) {
    error = status = '';
    try {
      await request('DELETE', `/relation-types/${encodeURIComponent(t.id)}`);
      status = `Deleted ${t.name}.`;
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }

  function describe(t: RelationType): string {
    if (t.is_symmetric) return `A ${t.name} B, and B ${t.name} A.`;
    const parts = [`A ${t.name} B; B is ${t.inverse_name} A.`];
    if (t.is_single) parts.push(`B can be ${t.inverse_name} one asset only.`);
    if (t.is_location) parts.push('It gives where things are.');
    return parts.join(' ');
  }
</script>

<div class="mb-4 flex flex-wrap items-center justify-between gap-2">
  <h1 class="text-2xl font-bold">Link types</h1>
  {#if editing === null}<button type="button" class="btn-primary" onclick={() => start()}>New relation</button>{/if}
</div>
<p class="mb-4 max-w-2xl text-sm text-zinc-600 dark:text-zinc-400">
  The ways assets can be linked, such as a bin that contains screws. Each link reads correctly from both of its assets.
</p>
<ErrorMessage {error} />
{#if status}<p class="mb-2 text-sm text-green-700 dark:text-green-400" role="status">{status}</p>{/if}

{#if editing !== null}
  <form class="card mb-6 max-w-2xl space-y-4" onsubmit={save}>
    <h2 class="text-lg font-semibold">{editing ? 'Edit relation' : 'New relation'}</h2>
    <div class="grid gap-4 sm:grid-cols-2">
      <div>
        <label class="label" for="rel-name">Name, from the first asset</label>
        <input id="rel-name" class="input" required maxlength="64" placeholder="contains" bind:value={draft.name} />
      </div>
      <div>
        <label class="label" for="rel-inverse" class:opacity-50={draft.is_symmetric}>Name, from the second</label>
        <input id="rel-inverse" class="input" maxlength="64" placeholder="contained in" required={!draft.is_symmetric} disabled={draft.is_symmetric} bind:value={draft.inverse_name} />
      </div>
    </div>
    <fieldset class="space-y-2" aria-describedby={inUse ? 'rel-in-use' : undefined}>
      <legend class="label">How it behaves</legend>
      <label class="flex items-center gap-2 text-sm">
        <input type="checkbox" bind:checked={draft.is_symmetric} disabled={inUse} onchange={symmetricChanged} />
        Reads the same both ways, like "used with"
      </label>
      <label class="flex items-center gap-2 text-sm">
        <input type="checkbox" bind:checked={draft.is_single} disabled={inUse || draft.is_symmetric} onchange={singleChanged} />
        The second asset can have one only, so linking it again moves it
      </label>
      <label class="flex items-center gap-2 text-sm">
        <input type="checkbox" bind:checked={draft.is_location} disabled={inUse || !draft.is_single} />
        Gives where things are, like "contains"
      </label>
      {#if inUse}<p id="rel-in-use" class="text-xs text-zinc-600 dark:text-zinc-400">Links use it, so only its names can change.</p>{/if}
    </fieldset>
    <div class="flex gap-2">
      <button type="submit" class="btn-primary">{editing ? 'Save relation' : 'Create relation'}</button>
      <button type="button" class="btn-secondary" onclick={() => (editing = null)}>Cancel</button>
    </div>
  </form>
{/if}

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading relations…</p>
{:else}
  <ul class="space-y-3">
    {#each types as t (t.id)}
      <li class="card flex flex-wrap items-start justify-between gap-2">
        <div>
          <h2 class="font-semibold">{t.is_symmetric ? t.name : `${t.name} / ${t.inverse_name}`}</h2>
          <p class="text-sm text-zinc-600 dark:text-zinc-400">{describe(t)}</p>
          <p class="text-sm">{t.link_count === 1 ? '1 link' : `${t.link_count} links`}.</p>
        </div>
        <span class="inline-flex flex-wrap gap-2">
          <button type="button" class="btn-secondary" aria-label="Edit {t.name}" onclick={() => start(t)}>Edit</button>
          {#if t.link_count === 0}
            <ConfirmButton label="Delete" question="Delete {t.name}?" onconfirm={() => remove(t)} />
          {/if}
        </span>
      </li>
    {/each}
  </ul>
{/if}

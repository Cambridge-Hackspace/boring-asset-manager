<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../lib/api';
  import { messageOf } from '../lib/errors';
  import type { AssetHook } from '../lib/models';
  import ConfirmButton from '../components/ConfirmButton.svelte';
  import DerivedKey from '../components/DerivedKey.svelte';
  import ErrorMessage from '../components/ErrorMessage.svelte';

  // /hooks: the buttons categories can offer on their assets, such as
  // Identify (docs/hooks.md). Which categories offer them is set on each
  // category's page.
  let { params: _params }: { params: Record<string, string> } = $props();

  let hooks = $state<AssetHook[]>([]);
  let loading = $state(true);
  let error = $state('');
  let status = $state('');
  // The hook being edited: its ID, 'new', or none.
  let editing = $state<string | null>(null);
  let key = $state('');
  let label = $state('');
  let confirm = $state('');
  // The key the hook had when opened: what devices listen for now.
  let had = $state('');

  async function load() {
    try {
      hooks = (await request<{ hooks: AssetHook[] }>('GET', '/hooks')).hooks ?? [];
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
  }
  onMount(load);

  function edit(a?: AssetHook) {
    error = status = '';
    editing = a?.id ?? 'new';
    key = had = a?.key ?? '';
    label = a?.label ?? '';
    confirm = a?.confirm ?? '';
  }

  async function save(event: SubmitEvent) {
    event.preventDefault();
    error = '';
    const body = { key: key.trim(), label, confirm };
    try {
      if (editing === 'new') await request('POST', '/hooks', body);
      else await request('PUT', `/hooks/${encodeURIComponent(editing!)}`, body);
      status = `Saved ${label.trim()}.`;
      editing = null;
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function remove(a: AssetHook) {
    error = status = '';
    try {
      await request('DELETE', `/hooks/${encodeURIComponent(a.id)}`);
      status = `Deleted ${a.label}.`;
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }
</script>

<div class="mb-2 flex flex-wrap items-center justify-between gap-2">
  <h1 class="text-2xl font-bold">Hooks</h1>
  {#if editing === null}<button type="button" class="btn-primary" onclick={() => edit()}>New hook</button>{/if}
</div>
<p class="mb-4 max-w-prose text-sm text-zinc-600 dark:text-zinc-400">
  Buttons on assets, such as <strong>Identify</strong>. Pressing one tells the devices and notification channels listening
  for it, such as a cabinet that lights the drawer an asset is in. Choose which categories offer each one on the
  category's page.
</p>
<ErrorMessage {error} />
{#if status}<p class="mb-2 text-sm text-green-700 dark:text-green-400" role="status">{status}</p>{/if}

{#if editing !== null}
  <form class="card mb-4 max-w-xl space-y-3" onsubmit={save} aria-label={editing === 'new' ? 'New hook' : `Edit ${label}`}>
    <div>
      <label class="label" for="hook-label">Label</label>
      <input id="hook-label" class="input" required maxlength="255" bind:value={label} />
      <!-- Made from the label while new; devices listen for it once saved. -->
      {#key editing}
        <DerivedKey
          id="hook-key"
          bind:value={key}
          from={label}
          follows={editing === 'new'}
          what="key devices listen for"
          lead="Devices listen for"
          warning={editing === 'new' ? '' : `Devices, and flows that trigger it, know this hook as ${had}: change them too, or they stop hearing it.`}
          pattern="[a-z][a-z0-9_\-]*"
        />
      {/key}
    </div>
    <div>
      <label class="label" for="hook-confirm">Ask first (optional)</label>
      <input id="hook-confirm" class="input" maxlength="255" placeholder="Light up its drawer?" bind:value={confirm} />
    </div>
    <div class="flex gap-2">
      <button type="submit" class="btn-primary">Save hook</button>
      <button type="button" class="btn-secondary" onclick={() => (editing = null)}>Cancel</button>
    </div>
  </form>
{/if}

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading hooks…</p>
{:else if hooks.length === 0}
  <p class="text-sm text-zinc-600 dark:text-zinc-400">No hooks yet.</p>
{:else}
  <ul class="space-y-2">
    {#each hooks as a (a.id)}
      <li class="card flex flex-wrap items-center justify-between gap-2">
        <div>
          <span class="font-semibold">{a.label}</span> <span class="font-mono text-sm text-zinc-600 dark:text-zinc-400">{a.key}</span>
          {#if a.confirm}<span class="block text-sm text-zinc-600 dark:text-zinc-400">Asks: {a.confirm}</span>{/if}
        </div>
        <div class="flex gap-2">
          <button type="button" class="btn-secondary" aria-label="Edit {a.label}" onclick={() => edit(a)}>Edit</button>
          <ConfirmButton label="Delete" confirmLabel="Delete {a.label}" question="Delete this hook? No category will offer it." onconfirm={() => remove(a)} />
        </div>
      </li>
    {/each}
  </ul>
{/if}

<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { Field } from '../../lib/models';
  import { session } from '../../lib/session.svelte';
  import ConfirmButton from '../../components/ConfirmButton.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';
  import FieldForm from './FieldForm.svelte';

  let { params: _params }: { params: Record<string, string> } = $props();

  let fields = $state<Field[]>([]);
  let loading = $state(true);
  let error = $state('');
  let editing = $state<string | null>(null); // a field ID, 'new', or null

  const canManage = $derived(session.can('categories.manage'));

  async function load() {
    try {
      fields = (await request<{ fields: Field[] }>('GET', '/fields')).fields ?? [];
      fields.sort((a, b) => a.label.localeCompare(b.label));
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
  }
  onMount(load);

  async function remove(f: Field) {
    error = '';
    try {
      await request('DELETE', `/fields/${encodeURIComponent(f.id)}`);
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }

  function saved() {
    editing = null;
    void load();
  }

  function describe(f: Field): string {
    if (f.validation_type !== 'select') return '';
    const n = f.options?.choices.length ?? 0;
    return `${n} choice${n === 1 ? '' : 's'}`;
  }
</script>

<div class="mb-4 flex flex-wrap items-center justify-between gap-2">
  <div>
    <h1 class="text-2xl font-bold">Fields</h1>
    <p class="text-sm text-zinc-600 dark:text-zinc-400">The details assets can record. Categories choose which apply.</p>
  </div>
  {#if canManage && editing !== 'new'}
    <button type="button" class="btn-primary" onclick={() => (editing = 'new')}>New field</button>
  {/if}
</div>

<ErrorMessage {error} />

{#if editing === 'new'}
  <div class="mb-4"><FieldForm onsaved={saved} oncancel={() => (editing = null)} /></div>
{/if}

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading fields…</p>
{:else if fields.length === 0}
  <p class="text-sm text-zinc-600 dark:text-zinc-400">No fields yet.</p>
{:else}
  <div class="overflow-x-auto">
    <table class="w-full text-left text-sm">
      <thead class="border-b border-zinc-300 dark:border-zinc-700">
        <tr>
          <th scope="col" class="py-2 pr-4">Label</th>
          <th scope="col" class="py-2 pr-4">Key</th>
          <th scope="col" class="py-2 pr-4">Type</th>
          <th scope="col" class="py-2"><span class="sr-only">Actions</span></th>
        </tr>
      </thead>
      <tbody>
        {#each fields as f (f.id)}
          {#if editing === f.id}
            <tr><td colspan="4" class="py-2"><FieldForm field={f} onsaved={saved} oncancel={() => (editing = null)} /></td></tr>
          {:else}
            <tr class="border-b border-zinc-200 dark:border-zinc-800">
              <td class="py-2 pr-4 font-medium">{f.label}</td>
              <td class="py-2 pr-4 font-mono">{f.key}</td>
              <td class="py-2 pr-4">{f.validation_type} <span class="text-zinc-600 dark:text-zinc-400">{describe(f)}</span></td>
              <td class="py-2 text-right">
                {#if canManage}
                  <span class="inline-flex flex-wrap justify-end gap-2">
                    <button type="button" class="btn-secondary" aria-label="Edit {f.label}" onclick={() => (editing = f.id)}>Edit</button>
                    <ConfirmButton label="Delete" question="Delete “{f.label}” and every value recorded for it?" onconfirm={() => remove(f)} />
                  </span>
                {/if}
              </td>
            </tr>
          {/if}
        {/each}
      </tbody>
    </table>
  </div>
{/if}

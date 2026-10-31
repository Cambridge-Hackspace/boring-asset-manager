<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { TemplateSummary } from '../../lib/models';
  import ConfirmButton from '../../components/ConfirmButton.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  let { params: _params }: { params: Record<string, string> } = $props();

  let templates = $state<TemplateSummary[]>([]);
  // The printers each template suits as they are loaded now; absent for
  // those who may not see printers.
  let suits = $state<Record<string, { id: string; name: string }[]> | undefined>();
  let loading = $state(true);
  let error = $state('');

  async function load() {
    try {
      const r = await request<{ templates: TemplateSummary[]; suits?: Record<string, { id: string; name: string }[]> }>('GET', '/templates');
      templates = r.templates ?? [];
      suits = r.suits;
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
  }
  onMount(load);

  async function remove(t: TemplateSummary) {
    error = '';
    try {
      await request('DELETE', `/templates/${encodeURIComponent(t.id)}`);
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }
</script>

<div class="mb-4 flex flex-wrap items-center justify-between gap-2">
  <h1 class="text-2xl font-bold">Label templates</h1>
  <a class="btn-primary" href="/templates/new">New template</a>
</div>
<ErrorMessage {error} />

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading templates…</p>
{:else if templates.length === 0}
  <p class="text-sm text-zinc-600 dark:text-zinc-400">No templates yet.</p>
{:else}
  <table class="w-full text-left text-sm">
    <thead class="border-b border-zinc-300 dark:border-zinc-700">
      <tr>
        <th scope="col" class="py-2 pr-4">Name</th>
        <th scope="col" class="py-2 pr-4">Size</th>
        {#if suits}<th scope="col" class="py-2 pr-4">Prints on</th>{/if}
        <th scope="col" class="py-2"><span class="sr-only">Actions</span></th>
      </tr>
    </thead>
    <tbody>
      {#each templates as t (t.id)}
        <tr class="border-b border-zinc-200 dark:border-zinc-800">
          <td class="py-2 pr-4 font-medium"><a class="inline-flex min-h-11 items-center hover:underline" href="/templates/{encodeURIComponent(t.id)}/edit">{t.name}</a></td>
          <td class="py-2 pr-4">{t.width} × {t.height} {t.units}, {t.dpi} DPI</td>
          {#if suits}
            <td class="py-2 pr-4">
              {#if suits[t.id]?.length}
                {suits[t.id]!.map((p) => p.name).join(', ')}
              {:else}
                <span class="text-zinc-600 dark:text-zinc-400">No printer, as loaded now</span>
              {/if}
            </td>
          {/if}
          <td class="py-2 text-right">
            <ConfirmButton label="Delete" question="Delete the “{t.name}” template?" onconfirm={() => remove(t)} />
          </td>
        </tr>
      {/each}
    </tbody>
  </table>
{/if}

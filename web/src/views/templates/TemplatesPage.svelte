<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { TemplateSummary } from '../../lib/models';
  import ConfirmButton from '../../components/ConfirmButton.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  let { params: _params }: { params: Record<string, string> } = $props();

  let templates = $state<TemplateSummary[]>([]);
  let loading = $state(true);
  let error = $state('');

  async function load() {
    try {
      templates = (await request<{ templates: TemplateSummary[] }>('GET', '/templates')).templates ?? [];
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
  <p role="status" class="text-sm text-zinc-500">Loading templates…</p>
{:else if templates.length === 0}
  <p class="text-sm text-zinc-500">No templates yet.</p>
{:else}
  <table class="w-full text-left text-sm">
    <thead class="border-b border-zinc-300 dark:border-zinc-700">
      <tr>
        <th scope="col" class="py-2 pr-4">Name</th>
        <th scope="col" class="py-2 pr-4">ID</th>
        <th scope="col" class="py-2 pr-4">Size</th>
        <th scope="col" class="py-2"><span class="sr-only">Actions</span></th>
      </tr>
    </thead>
    <tbody>
      {#each templates as t (t.id)}
        <tr class="border-b border-zinc-200 dark:border-zinc-800">
          <td class="py-2 pr-4 font-medium"><a class="hover:underline" href="/templates/{encodeURIComponent(t.id)}/edit">{t.name}</a></td>
          <td class="py-2 pr-4 font-mono">{t.id}</td>
          <td class="py-2 pr-4">{t.width} × {t.height} {t.units}, {t.dpi} DPI</td>
          <td class="py-2 text-right">
            <ConfirmButton label="Delete" question="Delete the “{t.name}” template?" onconfirm={() => remove(t)} />
          </td>
        </tr>
      {/each}
    </tbody>
  </table>
{/if}

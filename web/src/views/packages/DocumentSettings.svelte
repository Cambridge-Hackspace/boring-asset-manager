<!--
  The instance's name, at the top of every page of a PDF, and the page size
  PDFs are made in unless another is chosen (docs/reports.md#pdf).
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  let name = $state('');
  let pageSize = $state('letter');
  let error = $state('');
  let status = $state('');
  let busy = $state(false);

  onMount(async () => {
    try {
      const s = await request<{ instance_name: string; page_size: string }>('GET', '/settings/documents');
      [name, pageSize] = [s.instance_name, s.page_size];
    } catch (err) {
      error = messageOf(err);
    }
  });

  async function save(event: SubmitEvent) {
    event.preventDefault();
    error = status = '';
    busy = true;
    try {
      await request('PUT', '/settings/documents', { instance_name: name, page_size: pageSize });
      status = 'Saved.';
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }
</script>

<section class="card mb-6 max-w-3xl space-y-3" aria-labelledby="documents-heading">
  <h2 id="documents-heading" class="text-lg font-semibold">PDF documents</h2>
  <p class="text-sm text-zinc-600 dark:text-zinc-400">Reports and asset lists download as PDF. Every page is headed with this name.</p>
  <ErrorMessage {error} />
  <form class="flex flex-wrap items-end gap-3" onsubmit={save}>
    <div>
      <label class="label" for="instance-name">Heading on every page</label>
      <input id="instance-name" class="input" required maxlength="100" bind:value={name} />
    </div>
    <div>
      <label class="label" for="page-size">Page size</label>
      <select id="page-size" class="input" bind:value={pageSize}>
        <option value="letter">Letter</option>
        <option value="a4">A4</option>
      </select>
    </div>
    <button type="submit" class="btn-secondary" disabled={busy}>Save</button>
    {#if status}<p role="status" class="text-sm text-green-700 dark:text-green-400">{status}</p>{/if}
  </form>
</section>

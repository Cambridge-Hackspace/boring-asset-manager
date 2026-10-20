<script lang="ts">
  import { request } from '../../../lib/api';
  import { messageOf } from '../../../lib/errors';
  import type { AssetWithDetails } from '../../../lib/models';
  import ErrorMessage from '../../../components/ErrorMessage.svelte';
  import LabelActions from '../../../components/LabelActions.svelte';
  import AdaptivePreview from '../AdaptivePreview.svelte';
  import { useEditor } from './editor.svelte';

  // What the label is drawn with: placeholders, or an asset's own values;
  // and, for a saved template, that asset's label to download or print.
  let {
    asset = $bindable(),
    isNew,
    dirty,
  }: { asset: { id: string; name: string } | null; isNew: boolean; dirty: boolean } = $props();

  const editor = useEditor();
  let query = $state('');
  let results = $state<AssetWithDetails[]>([]);
  let error = $state('');

  async function search(event: SubmitEvent) {
    event.preventDefault();
    error = '';
    try {
      const q = new URLSearchParams({ search: query });
      results = ((await request<{ assets: AssetWithDetails[] }>('GET', `/assets?${q}`)).assets ?? []).slice(0, 8);
    } catch (err) {
      error = messageOf(err);
    }
  }
</script>

<div class="space-y-2">
  {#if asset}
    <p class="text-xs">
      Showing <strong>{asset.name}</strong>.
      <button type="button" class="underline" onclick={() => (asset = null)}>Use placeholders</button>
    </p>
  {:else}
    <p class="text-xs text-zinc-600 dark:text-zinc-400">Showing placeholders. Find an asset to see its values.</p>
  {/if}
  <form class="flex gap-1" role="search" onsubmit={search}>
    <label class="sr-only" for="preview-search">Find an asset</label>
    <input id="preview-search" class="input-sm" type="search" placeholder="Name or code" bind:value={query} />
    <button type="submit" class="btn-sm">Find</button>
  </form>
  <ErrorMessage {error} />
  {#if results.length}
    <ul class="text-xs">
      {#each results as a (a.asset.id)}
        <li>
          <button
            type="button"
            class="w-full truncate py-0.5 text-left hover:underline"
            onclick={() => {
              asset = { id: a.asset.id, name: a.asset.name };
              results = [];
            }}>{a.asset.name}</button
          >
        </li>
      {/each}
    </ul>
  {/if}
  {#if asset && !isNew && !dirty}
    {#key asset.id}
      <LabelActions assetIds={[asset.id]} templateId={editor.doc.id} />
    {/key}
  {:else if asset}
    <p class="text-xs text-zinc-600 dark:text-zinc-400">Save the template to export or print this asset's label; Test print works as it is.</p>
  {/if}
  {#if editor.doc.sizing === 'adaptive'}
    <AdaptivePreview doc={editor.doc} />
  {/if}
</div>

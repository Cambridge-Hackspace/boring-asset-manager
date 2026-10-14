<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { ImageInfo } from '../../lib/models';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  let { value, onpick }: { value: string; onpick: (id: string) => void } = $props();

  let images = $state<ImageInfo[]>([]);
  let error = $state('');
  let uploading = $state(false);

  async function load() {
    try {
      images = (await request<{ images: ImageInfo[] }>('GET', '/images')).images ?? [];
    } catch (err) {
      error = messageOf(err);
    }
  }
  onMount(load);

  async function upload(event: Event) {
    const input = event.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    if (!file) return;
    error = '';
    uploading = true;
    const form = new FormData();
    form.append('file', file);
    try {
      const res = await request<{ image: ImageInfo }>('POST', '/images', form);
      await load();
      onpick(res.image.id);
    } catch (err) {
      error = messageOf(err);
    } finally {
      uploading = false;
      input.value = '';
    }
  }
</script>

<fieldset class="space-y-2">
  <legend class="label">Image</legend>
  {#if images.length}
    <div class="grid grid-cols-3 gap-2" role="radiogroup" aria-label="Uploaded images">
      {#each images as img (img.id)}
        <button
          type="button"
          role="radio"
          aria-checked={img.id === value}
          class="rounded border p-1 {img.id === value ? 'border-blue-600 ring-2 ring-blue-600' : 'border-zinc-300 dark:border-zinc-700'}"
          onclick={() => onpick(img.id)}
        >
          <img src="/api/v1/images/{encodeURIComponent(img.id)}/content" alt={img.filename} class="mx-auto h-12 object-contain" />
          <span class="block truncate text-xs">{img.filename}</span>
        </button>
      {/each}
    </div>
  {:else}
    <p class="text-sm text-zinc-500">No images uploaded yet.</p>
  {/if}
  <label class="btn-secondary cursor-pointer">
    {uploading ? 'Uploading…' : 'Upload PNG or JPEG'}
    <input type="file" accept="image/png,image/jpeg" class="sr-only" onchange={upload} disabled={uploading} />
  </label>
  <ErrorMessage {error} />
</fieldset>

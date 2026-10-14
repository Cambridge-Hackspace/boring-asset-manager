<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { ImageInfo } from '../../lib/models';
  import type { TemplateDoc } from '../../lib/template';
  import ConfirmButton from '../../components/ConfirmButton.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  let { params: _params }: { params: Record<string, string> } = $props();

  let images = $state<ImageInfo[]>([]);
  let templates = $state<TemplateDoc[]>([]);
  let loading = $state(true);
  let uploading = $state(false);
  let error = $state('');

  async function load() {
    try {
      [images, templates] = await Promise.all([
        request<{ images: ImageInfo[] }>('GET', '/images').then((r) => r.images ?? []),
        request<{ templates: TemplateDoc[] }>('GET', '/templates').then((r) => r.templates ?? []),
      ]);
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
  }
  onMount(load);

  const usedBy = (id: string) => templates.filter((t) => (t.elements ?? []).some((e) => e.image_id === id));

  async function upload(event: Event) {
    const input = event.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    if (!file) return;
    error = '';
    uploading = true;
    const form = new FormData();
    form.append('file', file);
    try {
      await request('POST', '/images', form);
      await load();
    } catch (err) {
      error = messageOf(err);
    } finally {
      uploading = false;
      input.value = '';
    }
  }

  async function remove(img: ImageInfo) {
    error = '';
    try {
      await request('DELETE', `/images/${encodeURIComponent(img.id)}`);
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }

  const kb = (n: number) => `${Math.max(1, Math.round(n / 1024))} KB`;
</script>

<div class="mb-4 flex flex-wrap items-center justify-between gap-2">
  <div>
    <h1 class="text-2xl font-bold">Images</h1>
    <p class="text-sm text-zinc-600 dark:text-zinc-400">PNG or JPEG, up to 2 MiB and 4096 × 4096 pixels, for use in label templates.</p>
  </div>
  <label class="btn-primary cursor-pointer">
    {uploading ? 'Uploading…' : 'Upload image'}
    <input type="file" accept="image/png,image/jpeg" class="sr-only" onchange={upload} disabled={uploading} />
  </label>
</div>
<ErrorMessage {error} />

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading images…</p>
{:else if images.length === 0}
  <p class="text-sm text-zinc-600 dark:text-zinc-400">No images yet.</p>
{:else}
  <ul class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
    {#each images as img (img.id)}
      {@const users = usedBy(img.id)}
      <li class="card space-y-2">
        <img src="/api/v1/images/{encodeURIComponent(img.id)}/content" alt={img.filename} class="mx-auto h-24 object-contain" />
        <p class="truncate font-medium" title={img.filename}>{img.filename}</p>
        <p class="text-xs text-zinc-600 dark:text-zinc-400">{img.width_px} × {img.height_px} px, {kb(img.byte_size)}</p>
        <p class="text-xs">
          {#if users.length}
            Used by {#each users as t, i (t.id)}{i ? ', ' : ''}<a class="underline" href="/templates/{encodeURIComponent(t.id)}/edit">{t.name}</a>{/each}
          {:else}
            Not used by any template.
          {/if}
        </p>
        <ConfirmButton label="Delete" question="Delete {img.filename}?" disabled={users.length > 0} onconfirm={() => remove(img)} />
      </li>
    {/each}
  </ul>
{/if}

<script lang="ts">
  import { request } from '../lib/api';
  import { messageOf } from '../lib/errors';
  import { photoUrl, type AssetWithDetails } from '../lib/models';
  import { session } from '../lib/session.svelte';
  import ConfirmButton from './ConfirmButton.svelte';
  import ErrorMessage from './ErrorMessage.svelte';

  // An asset's photos: shown, added, removed and put in order.
  let { asset, onchange }: { asset: AssetWithDetails; onchange: (a: AssetWithDetails, done: string) => void } = $props();

  const max = 10;
  let busy = $state(false);
  let error = $state('');

  const id = $derived(asset.asset.id);
  const path = $derived(`/assets/${encodeURIComponent(id)}/photos`);
  const canEdit = $derived(session.canOn(asset.scopes, 'assets.write') && !asset.asset.archived_at);

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

  async function upload(event: Event) {
    const input = event.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    if (!file) return;
    const form = new FormData();
    form.append('file', file);
    await act(() => request('POST', path, form), 'Photo added.');
    input.value = '';
  }

  function move(i: number, by: number) {
    const ids = asset.photos.map((p) => p.id);
    const [moved] = ids.splice(i, 1);
    ids.splice(i + by, 0, moved!);
    void act(() => request('PUT', path, { photo_ids: ids }), by < 0 ? 'Photo moved earlier.' : 'Photo moved later.');
  }

  function remove(photoId: string) {
    void act(() => request('DELETE', `${path}/${encodeURIComponent(photoId)}`), 'Photo removed.');
  }
</script>

<section aria-labelledby="photos-heading">
  <div class="mb-3 flex flex-wrap items-center justify-between gap-2">
    <h2 id="photos-heading" class="section-title">Photos</h2>
    {#if canEdit && asset.photos.length < max}
      <label class="btn-secondary cursor-pointer">
        {busy ? 'Working…' : 'Add a photo'}
        <input type="file" accept="image/jpeg,image/png" class="sr-only" onchange={upload} disabled={busy} />
      </label>
    {/if}
  </div>
  <ErrorMessage {error} />
  {#if asset.photos.length === 0}
    <p class="text-sm text-zinc-600 dark:text-zinc-400">No photos.</p>
  {:else}
    <ul class="grid grid-cols-2 gap-3 sm:grid-cols-3">
      {#each asset.photos as p, i (p.id)}
        <li class="space-y-1">
          <a href={photoUrl(id, p.id)} target="_blank" rel="noopener">
            <img
              src={photoUrl(id, p.id, true)}
              alt="Photo {i + 1} of {asset.asset.name}{i === 0 ? ', shown in lists' : ''}"
              class="aspect-square w-full rounded object-cover"
              loading="lazy"
            />
          </a>
          {#if canEdit}
            <div class="flex flex-wrap gap-1">
              <button type="button" class="btn-secondary px-2 py-0.5 text-xs" disabled={busy || i === 0} aria-label="Move photo {i + 1} earlier" onclick={() => move(i, -1)}>←</button>
              <button type="button" class="btn-secondary px-2 py-0.5 text-xs" disabled={busy || i === asset.photos.length - 1} aria-label="Move photo {i + 1} later" onclick={() => move(i, 1)}>→</button>
              <ConfirmButton label="Remove" question="Remove photo {i + 1}?" onconfirm={() => remove(p.id)} />
            </div>
          {/if}
        </li>
      {/each}
    </ul>
    {#if canEdit && asset.photos.length >= max}
      <p class="mt-2 text-xs text-zinc-600 dark:text-zinc-400">An asset can have {max} photos; remove one to add another.</p>
    {/if}
  {/if}
</section>

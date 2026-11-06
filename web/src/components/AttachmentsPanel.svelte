<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../lib/api';
  import { messageOf } from '../lib/errors';
  import { attachmentUrl, type Attachment, type AttachmentLimits, type AttachmentOwner } from '../lib/models';
  import { formatBytes } from '../lib/upload';
  import ConfirmButton from './ConfirmButton.svelte';
  import ErrorMessage from './ErrorMessage.svelte';

  // The files attached to an asset or a category (docs/data-model.md#attachments):
  // shown, added, renamed and described, put in order, and removed. Images
  // show as photos always have; documents and other files download.
  let {
    owner,
    ownerId,
    ownerName,
    canEdit,
    onchange,
  }: {
    owner: AttachmentOwner;
    ownerId: string;
    ownerName: string;
    canEdit: boolean;
    onchange: (done: string) => void;
  } = $props();

  let attachments = $state<Attachment[]>([]);
  let limits = $state<AttachmentLimits | null>(null);
  let loaded = $state(false);
  let busy = $state(false);
  let error = $state('');
  // The attachment being renamed or described, and what it is to be.
  let editing = $state('');
  let editName = $state('');
  let editDescription = $state('');

  const path = $derived(`/${owner === 'asset' ? 'assets' : 'categories'}/${encodeURIComponent(ownerId)}/attachments`);
  const max = $derived(limits?.max_per_object ?? Infinity);
  // Images are numbered among themselves, as photos were: an asset's first is the one lists show.
  const imageNumbers = $derived.by(() => {
    const out: Record<string, number> = {};
    let n = 0;
    for (const a of attachments) if (a.kind === 'image') out[a.id] = ++n;
    return out;
  });
  const accept = $derived(
    ['image/jpeg', 'image/png', 'application/pdf', 'text/plain', 'text/markdown', 'text/csv', '.pdf', '.txt', '.md', '.markdown', '.csv', ...(limits?.allowed_types ?? [])].join(','),
  );

  onMount(async () => {
    try {
      const r = await request<{ attachments: Attachment[]; limits: AttachmentLimits }>('GET', path);
      attachments = r.attachments ?? [];
      limits = r.limits;
    } catch (err) {
      error = messageOf(err);
    } finally {
      loaded = true;
    }
  });

  async function act(action: () => Promise<{ attachments?: Attachment[]; attachment?: Attachment }>, done: string) {
    error = '';
    busy = true;
    try {
      const r = await action();
      if (r.attachments) attachments = r.attachments;
      else if (r.attachment) attachments = attachments.map((a) => (a.id === r.attachment!.id ? r.attachment! : a));
      onchange(done);
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
    await act(() => request('POST', path, form), 'Attachment added.');
    input.value = '';
  }

  function move(i: number, by: number) {
    const ids = attachments.map((a) => a.id);
    const [moved] = ids.splice(i, 1);
    ids.splice(i + by, 0, moved!);
    void act(() => request('PUT', path, { attachment_ids: ids }), by < 0 ? 'Attachment moved earlier.' : 'Attachment moved later.');
  }

  function edit(a: Attachment) {
    editing = a.id;
    editName = a.name;
    editDescription = a.description;
  }

  async function save(event: SubmitEvent) {
    event.preventDefault();
    const id = editing;
    await act(() => request('PUT', `${path}/${encodeURIComponent(id)}`, { name: editName, description: editDescription }), 'Attachment saved.');
    if (!error) editing = '';
  }

  function remove(id: string) {
    void act(() => request('DELETE', `${path}/${encodeURIComponent(id)}`), 'Attachment removed.');
  }

  // A short label for a file's type, on its tile.
  function typeLabel(a: Attachment): string {
    const ext = a.filename.includes('.') ? a.filename.split('.').pop()!.toUpperCase() : '';
    if (a.content_type === 'application/pdf') return 'PDF';
    return ext.length > 0 && ext.length <= 5 ? ext : 'FILE';
  }

  function alt(a: Attachment): string {
    const n = imageNumbers[a.id];
    return `Photo ${n} of ${ownerName}${owner === 'asset' && n === 1 ? ', shown in lists' : ''}`;
  }

  function about(a: Attachment): string {
    const parts = [formatBytes(a.byte_size)];
    if (a.added_by_name) parts.push(`added by ${a.added_by_name}`);
    parts.push(new Date(a.created_at).toLocaleDateString());
    return parts.join(' · ');
  }
</script>

<section aria-labelledby="attachments-heading-{owner}">
  <div class="mb-3 flex flex-wrap items-center justify-between gap-2">
    <h2 id="attachments-heading-{owner}" class="section-title">Attachments</h2>
    {#if canEdit && loaded && attachments.length < max}
      <label class="btn-secondary cursor-pointer">
        {busy ? 'Working…' : 'Add a file'}
        <input type="file" {accept} class="sr-only" onchange={upload} disabled={busy} />
      </label>
    {/if}
  </div>
  <ErrorMessage {error} />
  {#if !loaded}
    <p class="text-sm text-zinc-600 dark:text-zinc-400">Loading…</p>
  {:else if attachments.length === 0}
    <p class="text-sm text-zinc-600 dark:text-zinc-400">No attachments.</p>
  {:else}
    <ul class="grid grid-cols-2 gap-3 sm:grid-cols-3">
      {#each attachments as a, i (a.id)}
        <li class="min-w-0 space-y-1" data-kind={a.kind}>
          {#if a.kind === 'image'}
            <a href={attachmentUrl(owner, ownerId, a.id)} target="_blank" rel="noopener">
              <img src={attachmentUrl(owner, ownerId, a.id, true)} alt={alt(a)} class="aspect-square w-full rounded object-cover" loading="lazy" />
            </a>
          {:else}
            <a
              href={attachmentUrl(owner, ownerId, a.id)}
              download={a.filename}
              class="flex aspect-square w-full flex-col items-center justify-center gap-1 rounded border border-zinc-300 bg-zinc-50 text-zinc-700 hover:bg-zinc-100 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-300 dark:hover:bg-zinc-800"
              aria-label="Download {a.name}"
            >
              <svg aria-hidden="true" viewBox="0 0 24 24" class="h-10 w-10" fill="none" stroke="currentColor" stroke-width="1.5">
                <path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z" />
                <path d="M14 3v5h5" />
              </svg>
              <span class="text-xs font-semibold">{typeLabel(a)}</span>
            </a>
          {/if}
          {#if editing === a.id}
            <form class="space-y-1" onsubmit={save}>
              <label class="block text-xs">
                Name
                <input class="input w-full" bind:value={editName} required maxlength="255" />
              </label>
              <label class="block text-xs">
                Description
                <textarea class="input w-full" rows="2" bind:value={editDescription} maxlength="4000"></textarea>
              </label>
              <div class="flex flex-wrap gap-1">
                <button type="submit" class="btn-primary px-2 py-0.5 text-xs" disabled={busy}>Save</button>
                <button type="button" class="btn-secondary px-2 py-0.5 text-xs" onclick={() => (editing = '')}>Cancel</button>
              </div>
            </form>
          {:else}
            <p class="truncate text-sm font-medium" title={a.name}>{a.name}</p>
            {#if a.description}<p class="text-xs whitespace-pre-line text-zinc-700 dark:text-zinc-300">{a.description}</p>{/if}
            <p class="text-xs text-zinc-600 dark:text-zinc-400">{about(a)}</p>
          {/if}
          {#if canEdit && editing !== a.id}
            <div class="flex flex-wrap gap-1">
              <button type="button" class="btn-secondary px-2 py-0.5 text-xs" disabled={busy || i === 0} aria-label="Move {a.name} earlier" onclick={() => move(i, -1)}>←</button>
              <button type="button" class="btn-secondary px-2 py-0.5 text-xs" disabled={busy || i === attachments.length - 1} aria-label="Move {a.name} later" onclick={() => move(i, 1)}>→</button>
              <button type="button" class="btn-secondary px-2 py-0.5 text-xs" disabled={busy} aria-label="Rename or describe {a.name}" onclick={() => edit(a)}>Edit</button>
              <ConfirmButton label="Remove" question="Remove {a.name}?" onconfirm={() => remove(a.id)} />
            </div>
          {/if}
        </li>
      {/each}
    </ul>
    {#if canEdit && attachments.length >= max}
      <p class="mt-2 text-xs text-zinc-600 dark:text-zinc-400">This can have {max} attachments; remove one to add another.</p>
    {/if}
  {/if}
  {#if canEdit && limits}
    <p class="mt-2 text-xs text-zinc-600 dark:text-zinc-400">
      JPEG and PNG images up to {formatBytes(limits.max_image_bytes)}, and PDFs, text, Markdown and CSV files{limits.allowed_types.length ? `, and ${limits.allowed_types.join(', ')}` : ''} up to
      {formatBytes(limits.max_file_bytes)}.
    </p>
  {/if}
</section>

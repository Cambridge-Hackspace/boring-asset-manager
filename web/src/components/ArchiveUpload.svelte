<script lang="ts">
  import { onDestroy } from 'svelte';
  import { messageOf } from '../lib/errors';
  import {
    formatBytes,
    forgetUpload,
    resumeKey,
    unfinishedUploads,
    uploadFile,
    type UploadProgress,
    type UploadState,
  } from '../lib/upload';
  import ErrorMessage from './ErrorMessage.svelte';

  // Uploads an archive of any size in parts (README §2.3): its progress,
  // pause and resume, and an unfinished upload offered again when its file
  // is chosen. onuploaded is given the complete upload, to use by its ID.
  let {
    label = 'Choose an archive',
    onuploaded,
    fetch: fetchFn,
  }: { label?: string; onuploaded: (up: UploadState, file: File) => void; fetch?: typeof fetch } = $props();

  let file = $state<File | null>(null);
  let progress = $state<UploadProgress | null>(null);
  let running = $state(false);
  let error = $state('');
  let unfinished = $state(unfinishedUploads());
  let controller: AbortController | null = null;

  const percent = $derived(progress && progress.total > 0 ? Math.floor((progress.done / progress.total) * 100) : 0);
  const resuming = $derived(file !== null && unfinished.some((u) => u.name === file!.name && u.size === file!.size));

  function warnBeforeLeaving(event: BeforeUnloadEvent) {
    if (!running) return;
    event.preventDefault();
    // Older browsers show nothing without this.
    event.returnValue = '';
  }
  $effect(() => {
    window.addEventListener('beforeunload', warnBeforeLeaving);
    return () => window.removeEventListener('beforeunload', warnBeforeLeaving);
  });
  onDestroy(() => controller?.abort());

  async function run() {
    if (!file) return;
    error = '';
    running = true;
    controller = new AbortController();
    try {
      const up = await uploadFile(file, { signal: controller.signal, onProgress: (p) => (progress = p), fetch: fetchFn });
      onuploaded(up, file);
    } catch (err) {
      if (!controller.signal.aborted) error = messageOf(err);
    } finally {
      running = false;
      unfinished = unfinishedUploads();
    }
  }

  function choose(event: Event) {
    const input = event.currentTarget as HTMLInputElement;
    const chosen = input.files?.[0];
    // Cleared, so choosing the same file again, to resume it, is a change.
    input.value = '';
    if (!chosen) return;
    controller?.abort();
    file = chosen;
    progress = null;
    void run();
  }

  function pause() {
    controller?.abort();
  }

  async function cancel() {
    controller?.abort();
    if (file) await forgetUpload(file, { fetch: fetchFn });
    file = null;
    progress = null;
    unfinished = unfinishedUploads();
  }

  function status(p: UploadProgress | null): string {
    if (!p) return '';
    if (p.phase === 'hashing') return `Checking the file: ${percent}%`;
    if (p.phase === 'done') return `Uploaded ${formatBytes(p.total)}.`;
    return `${running ? 'Uploading' : 'Paused at'} ${formatBytes(p.done)} of ${formatBytes(p.total)} (${percent}%)`;
  }
</script>

<div class="space-y-3">
  {#if unfinished.length > 0 && !running}
    <div class="rounded border border-amber-300 bg-amber-50 p-3 text-sm dark:border-amber-700 dark:bg-amber-950">
      <p class="font-medium">Unfinished uploads</p>
      <ul class="mt-1 list-disc pl-5">
        {#each unfinished as u (u.id)}
          <li>{u.name}: {formatBytes(u.received)} of {formatBytes(u.size)}. Choose this file again to carry on where it stopped.</li>
        {/each}
      </ul>
    </div>
  {/if}

  <label class="btn-secondary inline-block cursor-pointer" class:opacity-50={running}>
    {label}
    <input
      type="file"
      accept=".gz,.tgz,application/gzip"
      class="sr-only"
      onchange={choose}
      disabled={running}
      data-testid="archive-file"
    />
  </label>

  {#if file}
    <div class="space-y-2" aria-live="polite">
      <p class="text-sm">
        <span class="font-medium">{file.name}</span>
        ({formatBytes(file.size)}){#if resuming && !progress}, resuming{/if}
      </p>
      {#if progress && progress.phase !== 'done'}
        <progress class="w-full" max="100" value={percent} aria-label="Upload progress">{percent}%</progress>
      {/if}
      <p class="text-sm text-zinc-600 dark:text-zinc-400" data-testid="upload-status">{status(progress)}</p>
      {#if progress?.phase !== 'done'}
        <div class="flex gap-2">
          {#if running}
            <button type="button" class="btn-secondary" onclick={pause}>Pause</button>
          {:else}
            <button type="button" class="btn-primary" onclick={run}>Resume</button>
          {/if}
          <button type="button" class="btn-secondary" onclick={cancel}>Cancel</button>
        </div>
      {/if}
    </div>
  {/if}

  <ErrorMessage {error} />
</div>

<script lang="ts">
  import { request } from '../lib/api';
  import { messageOf } from '../lib/errors';
  import ErrorMessage from './ErrorMessage.svelte';

  // Writing a comment in Markdown (docs/comments.md), with Write and
  // Preview tabs as other sites' comment boxes have, the preview rendered
  // by the server as the comment will be. A file attached while writing
  // goes on what the comment is on, and a link to it goes in the text where
  // the cursor is. @mentions (#224) will attach to the text area here,
  // through insert() and the textarea's input events.
  let {
    id,
    label,
    value = '',
    uploadPath,
    saveLabel,
    onsave,
    oncancel,
    onattached,
  }: {
    /** Unique on the page, for the labels. */
    id: string;
    label: string;
    value?: string;
    /** Where files are attached while writing, if they may be. */
    uploadPath?: string;
    saveLabel: string;
    onsave: (body: string) => Promise<void>;
    oncancel?: () => void;
    /** After a file is attached to what the comment is on. */
    onattached?: () => void;
  } = $props();

  // svelte-ignore state_referenced_locally
  let body = $state(value);
  let tab = $state<'write' | 'preview'>('write');
  let preview = $state('');
  let busy = $state(false);
  let error = $state('');
  let textarea: HTMLTextAreaElement | undefined = $state();

  /** Puts text in at the cursor, or at the end, on a line of its own. */
  export function insert(text: string) {
    const at = textarea?.selectionStart ?? body.length;
    const before = body.slice(0, at);
    const after = body.slice(at);
    const lead = before && !before.endsWith('\n') ? '\n' : '';
    body = `${before}${lead}${text}\n${after}`;
  }

  async function showPreview() {
    tab = 'preview';
    error = '';
    try {
      preview = (await request<{ html: string }>('POST', '/markdown', { body })).html;
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function attach(event: Event) {
    const input = event.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    if (!file || !uploadPath) return;
    error = '';
    busy = true;
    try {
      const form = new FormData();
      form.append('file', file);
      const r = await request<{ markdown: string }>('POST', uploadPath, form);
      insert(r.markdown);
      onattached?.();
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
      input.value = '';
    }
  }

  async function save(event: SubmitEvent) {
    event.preventDefault();
    if (!body.trim()) {
      error = 'Write something first.';
      return;
    }
    error = '';
    busy = true;
    try {
      await onsave(body);
      body = '';
      tab = 'write';
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }
</script>

<form class="space-y-2" onsubmit={save}>
  <div class="flex flex-wrap items-end justify-between gap-2">
    <label class="label mb-0" for="{id}-body">{label}</label>
    <div class="flex gap-1" role="group" aria-label="Write or preview">
      <button type="button" class="inline-flex min-h-11 items-center rounded px-3 text-sm aria-pressed:bg-zinc-200 aria-pressed:font-semibold dark:aria-pressed:bg-zinc-700" aria-pressed={tab === 'write'} onclick={() => (tab = 'write')}>Write</button>
      <button type="button" class="inline-flex min-h-11 items-center rounded px-3 text-sm aria-pressed:bg-zinc-200 aria-pressed:font-semibold dark:aria-pressed:bg-zinc-700" aria-pressed={tab === 'preview'} onclick={showPreview}>Preview</button>
    </div>
  </div>
  <textarea
    id="{id}-body"
    class="input min-h-28 w-full font-mono text-sm"
    rows="5"
    bind:this={textarea}
    bind:value={body}
    hidden={tab !== 'write'}
    aria-describedby="{id}-hint"
  ></textarea>
  {#if tab === 'preview'}
    <div class="comment-body min-h-28 rounded border border-zinc-300 p-3 dark:border-zinc-700" aria-label="Preview">
      {#if preview}
        <!-- Rendered and sanitized on the server (internal/markdown). -->
        <!-- eslint-disable-next-line svelte/no-at-html-tags -->
        {@html preview}
      {:else}
        <p class="text-sm text-zinc-600 dark:text-zinc-400">Nothing to preview.</p>
      {/if}
    </div>
  {/if}
  <p id="{id}-hint" class="text-xs text-zinc-600 dark:text-zinc-400">
    Markdown: **bold**, lists, tables, `code`, [links](https://example.com) and - [ ] task lists. Images from other sites show as links.
  </p>
  <ErrorMessage {error} />
  <div class="flex flex-wrap items-center gap-2">
    <button type="submit" class="btn-secondary" disabled={busy}>{saveLabel}</button>
    {#if oncancel}<button type="button" class="btn-secondary" disabled={busy} onclick={oncancel}>Cancel</button>{/if}
    {#if uploadPath}
      <label class="btn-secondary cursor-pointer" for="{id}-file">Attach a file</label>
      <input id="{id}-file" class="sr-only" type="file" disabled={busy} onchange={attach} />
    {/if}
  </div>
</form>

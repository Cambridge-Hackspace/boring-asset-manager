<!--
  The icon library (README §3.7): isolex source no one category owns,
  such as bases, fragments and axes icons share. Each file is written in
  the source editor, and published or kept as a draft as a category's
  icon is.
-->
<script lang="ts">
  import { ApiError, request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { Category, CategoryIcon, IconDiag, IconLibraryFile, IconRevision, IconSave } from '../../lib/models';
  import type { Vocabulary } from '../../lib/isolex/complete';
  import { splitDiags } from '../../lib/icons';
  import type { Problem } from '../../lib/isolex/syntax';
  import IconSourceEditor from '../../components/IconSourceEditor.svelte';
  import ConfirmButton from '../../components/ConfirmButton.svelte';
  import IconDiagList from '../../components/IconDiagList.svelte';

  interface Props {
    files: IconLibraryFile[];
    canManage: boolean;
    vocabulary: Vocabulary;
    categories: Category[];
    icons: CategoryIcon[];
    onsaved: () => void | Promise<void>;
  }
  let { files, canManage, vocabulary, categories, icons, onsaved }: Props = $props();

  // the file open: its ID, 'new', or none
  let open = $state<string | null>(null);
  let path = $state('');
  let text = $state('');
  let renameTo = $state('');
  let problems = $state<Problem[]>([]);
  let elsewhere = $state<IconDiag[]>([]);
  let error = $state('');
  let refusal = $state<IconDiag[]>([]);
  let status = $state('');
  let saving = $state(false);
  let revisions = $state<IconRevision[] | null>(null);

  const file = $derived(files.find((f) => f.id === open));
  const libFile = (p: string) => `library/${p}`;

  function show(f: IconLibraryFile | null) {
    error = status = '';
    refusal = [];
    revisions = null;
    open = f ? f.id : 'new';
    path = renameTo = f?.path ?? 'shared/';
    text = f ? (f.draft ?? f.source ?? '') : '';
    const split = splitDiags(libFile(path), f?.draft ? f.draft_errors : [], []);
    problems = split.here;
    elsewhere = split.elsewhere;
  }

  function refused(err: unknown) {
    error = messageOf(err);
    const details = err instanceof ApiError ? (err.body as { details?: unknown })?.details : undefined;
    refusal = Array.isArray(details) ? (details as IconDiag[]) : [];
  }

  function said(save: IconSave, p: string) {
    const split = splitDiags(libFile(p), save.errors, save.warnings);
    problems = split.here;
    elsewhere = split.elsewhere;
    status = save.published ? 'Published.' : `Kept as a draft: ${save.errors.length} problem${save.errors.length === 1 ? '' : 's'} to fix before it is published.`;
  }

  async function save() {
    error = status = '';
    refusal = [];
    saving = true;
    try {
      if (open === 'new') {
        const out = await request<{ file: IconLibraryFile; save: IconSave }>('POST', '/icon-library', { path, source: text });
        await onsaved();
        open = out.file.id;
        renameTo = out.file.path;
        said(out.save, out.file.path);
      } else if (file) {
        const out = await request<{ file: IconLibraryFile; save: IconSave }>('PUT', `/icon-library/${encodeURIComponent(file.id)}`, { source: text });
        await onsaved();
        said(out.save, out.file.path);
      }
    } catch (err) {
      refused(err);
    } finally {
      saving = false;
    }
  }

  async function act(method: 'POST' | 'DELETE', suffix: string, body: unknown, done: string, close: boolean) {
    if (!file) return;
    error = status = '';
    refusal = [];
    try {
      await request(method, `/icon-library/${encodeURIComponent(file.id)}${suffix}`, body);
      await onsaved();
      if (close) open = null;
      status = done;
    } catch (err) {
      refused(err);
    }
  }

  async function history(event: Event) {
    if (!(event.currentTarget as HTMLDetailsElement).open || revisions || !file) return;
    try {
      revisions = (await request<{ revisions: IconRevision[] }>('GET', `/icon-library/${encodeURIComponent(file.id)}/revisions`)).revisions ?? [];
    } catch (err) {
      error = messageOf(err);
    }
  }
</script>

<section class="card" aria-labelledby="library-heading">
  <h2 id="library-heading" class="text-lg font-semibold">Library</h2>
  <p class="mb-3 text-sm text-zinc-600 dark:text-zinc-400">What icons share and no one category owns: bases to extend, fragments to insert, axes of variants.</p>
  {#if files.length}
    <ul class="mb-3 divide-y divide-zinc-200 text-sm dark:divide-zinc-800">
      {#each files as f (f.id)}
        <li class="flex flex-wrap items-center gap-2 py-1.5">
          <span class="font-mono">{f.path}</span>
          {#if f.draft}<span class="text-amber-900 dark:text-amber-200">draft, {f.draft_errors.length} problem{f.draft_errors.length === 1 ? '' : 's'}</span>{/if}
          {#if f.source === null}<span class="text-zinc-600 dark:text-zinc-400">never published</span>{/if}
          <button type="button" class="btn-sm ml-auto" aria-label="Open {f.path}" onclick={() => show(f)}>Open</button>
        </li>
      {/each}
    </ul>
  {:else}
    <p class="mb-3 text-sm">The library is empty.</p>
  {/if}
  {#if canManage && open === null}<button type="button" class="btn-secondary" onclick={() => show(null)}>New file</button>{/if}
  {#if status && open === null}<p role="status" class="mt-2 text-sm text-green-700 dark:text-green-400">{status}</p>{/if}

  {#if open !== null}
    <div class="mt-4 border-t border-zinc-200 pt-4 dark:border-zinc-800">
      {#if open === 'new'}
        <div class="mb-3 max-w-sm">
          <label class="label" for="library-path">Its path</label>
          <input id="library-path" class="input font-mono" bind:value={path} aria-describedby="library-path-help" />
          <p id="library-path-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">Such as shared/racks.ilx: names of letters, digits, -, _ and ., separated by /, ending .ilx.</p>
        </div>
      {:else}
        <h3 class="mb-2 font-mono text-sm">{file?.path}</h3>
      {/if}
      <IconSourceEditor bind:value={text} {problems} {vocabulary} label="{open === 'new' ? 'New library file' : file?.path} source" readonly={!canManage} rows={14} />
      {#if elsewhere.length}
        <div class="mt-2 text-sm">
          <h4 class="font-medium">Problems in other icons, caused by this</h4>
          <IconDiagList diags={elsewhere} {categories} {icons} />
        </div>
      {/if}
      {#if error}
        <div role="alert" class="mt-2 text-sm text-red-700 dark:text-red-400">
          <p>{error}</p>
          {#if refusal.length}<IconDiagList diags={refusal} {categories} {icons} />{/if}
        </div>
      {/if}
      {#if status}<p role="status" class="mt-2 text-sm text-green-700 dark:text-green-400">{status}</p>{/if}
      <div class="mt-3 flex flex-wrap items-center gap-2">
        {#if canManage}<button type="button" class="btn-primary" disabled={saving} onclick={save}>{saving ? 'Saving…' : 'Save'}</button>{/if}
        <button type="button" class="btn-secondary" onclick={() => (open = null)}>Close</button>
        {#if canManage && file?.draft}
          <ConfirmButton label="Discard the draft" question={file.source ? 'Go back to what is published?' : 'Discard it? The file was never published.'} onconfirm={() => act('DELETE', '/draft', undefined, 'The draft is discarded.', !file.source)} />
        {/if}
      </div>
      {#if file?.source}
        <div class="mt-4 grid max-w-2xl gap-4">
          <details ontoggle={history}>
            <summary class="cursor-pointer text-sm font-medium">History</summary>
            {#if revisions === null}
              <p class="mt-1 text-sm" role="status">Loading…</p>
            {:else}
              <ul class="mt-2 space-y-1 text-sm">
                {#each revisions as r (r.id)}
                  <li class="flex flex-wrap items-center gap-2">
                    <time datetime={r.created_at}>{new Date(r.created_at).toLocaleString()}</time>
                    <button type="button" class="btn-sm" onclick={() => (text = r.source)}>Load into the editor</button>
                  </li>
                {/each}
              </ul>
            {/if}
          </details>
          {#if canManage}
            <form
              class="flex flex-wrap items-end gap-2"
              onsubmit={(e) => {
                e.preventDefault();
                void act('POST', '/rename', { path: renameTo }, `Moved to ${renameTo}.`, false);
              }}
            >
              <div>
                <label class="label" for="library-rename">Move it</label>
                <input id="library-rename" class="input font-mono" bind:value={renameTo} />
              </div>
              <button class="btn-secondary" disabled={renameTo === file.path}>Move</button>
            </form>
            <div>
              <ConfirmButton label="Delete the file" question="Delete this file and its history?" onconfirm={() => act('DELETE', '', undefined, 'The file is deleted.', true)} />
            </div>
          {/if}
        </div>
      {/if}
    </div>
  {/if}
</section>

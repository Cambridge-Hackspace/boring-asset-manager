<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { request } from '../lib/api';
  import { messageOf } from '../lib/errors';
  import type { Comment } from '../lib/models';
  import CommentEditor from './CommentEditor.svelte';
  import ConfirmButton from './ConfirmButton.svelte';
  import ErrorMessage from './ErrorMessage.svelte';

  // The comments on an asset, a category, a loan or a flow's filing
  // (docs/comments.md), oldest first, then those on its categories and
  // their ancestors, nearest first, each marked with its category and
  // linking to it there. They can be narrowed to its own. Authors edit and
  // delete their own; moderators delete any.
  let {
    id,
    path,
    uploadPath,
    canWrite,
    own,
    heading = 'Comments',
    compact = false,
    onchange,
    onattached,
  }: {
    /** Unique on the page, for headings and labels. */
    id: string;
    /** The comments' route, such as /assets/{id}/comments. */
    path: string;
    /** Where files are attached while writing, if they may be. */
    uploadPath?: string;
    canWrite: boolean;
    /** What its own are called, in the filter: "Only this asset's". */
    own: string;
    heading?: string;
    /** Within a card, with a smaller heading. */
    compact?: boolean;
    onchange?: (done: string) => void;
    /** After a file is attached, while writing, to what the comments are on. */
    onattached?: () => void;
  } = $props();

  let comments = $state<Comment[]>([]);
  let loaded = $state(false);
  let error = $state('');
  let onlyOwn = $state(false);
  let editing = $state('');

  const inherited = $derived(comments.some((c) => c.category));
  const shown = $derived(onlyOwn ? comments.filter((c) => !c.category) : comments);

  async function load() {
    try {
      comments = (await request<{ comments: Comment[] }>('GET', path)).comments ?? [];
    } catch (err) {
      error = messageOf(err);
    } finally {
      loaded = true;
    }
  }

  onMount(async () => {
    await load();
    // A link to a comment, such as from a notification, shows it.
    const target = location.hash.startsWith('#comment-') ? document.getElementById(location.hash.slice(1)) : null;
    if (target) {
      await tick();
      target.scrollIntoView?.({ block: 'center' });
    }
  });

  function replace(c: Comment) {
    comments = comments.map((x) => (x.id === c.id ? c : x));
  }

  async function add(body: string) {
    const r = await request<{ comment: Comment }>('POST', path, { body });
    const ownCount = comments.filter((c) => !c.category).length;
    comments = [...comments.slice(0, ownCount), r.comment, ...comments.slice(ownCount)];
    onchange?.('Comment added.');
  }

  async function save(c: Comment, body: string) {
    replace((await request<{ comment: Comment }>('PUT', `/comments/${encodeURIComponent(c.id)}`, { body })).comment);
    editing = '';
    onchange?.('Comment edited.');
  }

  async function remove(c: Comment) {
    error = '';
    try {
      replace((await request<{ comment: Comment }>('DELETE', `/comments/${encodeURIComponent(c.id)}`)).comment);
      onchange?.('Comment deleted.');
    } catch (err) {
      error = messageOf(err);
    }
  }

  const when = (iso: string) => new Date(iso).toLocaleString();
  const who = (c: Comment) => (c.mine ? 'You' : (c.author_name ?? 'Someone'));
</script>

<section aria-labelledby="{id}-heading">
  <div class="mb-3 flex flex-wrap items-center justify-between gap-2">
    <h2 id="{id}-heading" class={compact ? 'text-base font-semibold' : 'section-title'}>{heading}</h2>
    {#if inherited}
      <label class="inline-flex min-h-11 items-center gap-2 text-sm">
        <input type="checkbox" bind:checked={onlyOwn} />
        Only {own}
      </label>
    {/if}
  </div>
  <ErrorMessage {error} />
  {#if loaded && !shown.length}
    <p class="mb-3 text-sm text-zinc-600 dark:text-zinc-400">No comments yet.</p>
  {/if}
  <ol class="space-y-4">
    {#each shown as c (c.id)}
      <li>
        <article id="comment-{c.id}" class="rounded border border-zinc-200 p-3 dark:border-zinc-700" aria-label="Comment by {who(c)}">
          <header class="mb-2 flex flex-wrap items-center gap-x-2 text-xs text-zinc-600 dark:text-zinc-400">
            <span class="font-semibold text-zinc-900 dark:text-zinc-100">{who(c)}</span>
            <time datetime={c.created_at}>{when(c.created_at)}</time>
            {#if c.edited_at && !c.deleted_at}<span title="Edited {when(c.edited_at)}">(edited)</span>{/if}
            {#if c.category}
              <span>on <a class="inline-flex min-h-11 items-center underline" href="/categories/{encodeURIComponent(c.category.id)}?tab=comments#comment-{c.id}">{c.category.name}</a></span>
            {/if}
          </header>
          {#if c.deleted_at}
            <p class="text-sm italic text-zinc-600 dark:text-zinc-400">
              Deleted by {c.deleted_by_name ?? 'someone'}, {when(c.deleted_at)}.
            </p>
          {:else if editing === c.id}
            <CommentEditor id="edit-{c.id}" label="Edit the comment" value={c.body} {uploadPath} saveLabel="Save" onsave={(body) => save(c, body)} oncancel={() => (editing = '')} {onattached} />
          {:else}
            <div class="comment-body">
              <!-- Rendered and sanitized on the server (internal/markdown). -->
              <!-- eslint-disable-next-line svelte/no-at-html-tags -->
              {@html c.html}
            </div>
            {#if c.can_edit || c.can_delete}
              <div class="mt-2 flex flex-wrap gap-2">
                {#if c.can_edit}<button type="button" class="btn-secondary" onclick={() => (editing = c.id)}>Edit</button>{/if}
                {#if c.can_delete}<ConfirmButton label="Delete" question="Delete this comment? Its text stays in history." onconfirm={() => remove(c)} />{/if}
              </div>
            {/if}
          {/if}
        </article>
      </li>
    {/each}
  </ol>
  {#if canWrite}
    <div class="mt-4">
      <CommentEditor id="{id}-new" label="Add a comment" {uploadPath} saveLabel="Comment" onsave={add} {onattached} />
    </div>
  {/if}
</section>

<style>
  /* Global, so that the editor's preview reads as the comment will. */
  :global(.comment-body :is(h1, h2, h3, h4, h5, h6)) {
    margin: 0.75rem 0 0.5rem;
    font-weight: 600;
  }
  :global(.comment-body h1) {
    font-size: 1.25rem;
  }
  :global(.comment-body h2) {
    font-size: 1.125rem;
  }
  :global(.comment-body :is(p, ul, ol, pre, blockquote, table)) {
    margin-bottom: 0.5rem;
    font-size: 0.875rem;
    line-height: 1.5;
  }
  :global(.comment-body ul) {
    list-style: disc;
    padding-left: 1.25rem;
  }
  :global(.comment-body ol) {
    list-style: decimal;
    padding-left: 1.25rem;
  }
  :global(.comment-body li:has(> .task-mark)) {
    list-style: none;
    margin-left: -1.25rem;
  }
  :global(.comment-body code) {
    font-family: ui-monospace, monospace;
    font-size: 0.8125rem;
  }
  :global(.comment-body pre) {
    overflow-x: auto;
    border-radius: 0.25rem;
    background: rgb(127 127 127 / 0.12);
    padding: 0.5rem;
  }
  :global(.comment-body blockquote) {
    border-left: 3px solid rgb(127 127 127 / 0.5);
    padding-left: 0.75rem;
  }
  :global(.comment-body table) {
    display: block;
    overflow-x: auto;
    border-collapse: collapse;
  }
  :global(.comment-body :is(th, td)) {
    border: 1px solid rgb(127 127 127 / 0.4);
    padding: 0.25rem 0.5rem;
  }
  :global(.comment-body a) {
    text-decoration: underline;
  }
  :global(.comment-body img) {
    max-width: 100%;
    max-height: 24rem;
  }
</style>

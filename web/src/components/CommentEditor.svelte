<script lang="ts">
  import { tick } from 'svelte';
  import { request } from '../lib/api';
  import { messageOf } from '../lib/errors';
  import { matching, mentionQuery, remember, toDisplay, toStored, type MentionPerson } from '../lib/mentions';
  import ErrorMessage from './ErrorMessage.svelte';

  // Writing a comment in Markdown (docs/comments.md), with Write and
  // Preview tabs as other sites' comment boxes have, the preview rendered
  // by the server as the comment will be. A file attached while writing
  // goes on what the comment is on, and a link to it goes in the text where
  // the cursor is. Typing @ offers the people who may read what it is on
  // (#224), as other sites' comment boxes do: the text area shows each as
  // @ and their name, and the comment stores them as <@id>.
  let {
    id,
    label,
    value = '',
    mentions = [],
    uploadPath,
    peoplePath,
    saveLabel,
    onsave,
    oncancel,
    onattached,
  }: {
    /** Unique on the page, for the labels. */
    id: string;
    label: string;
    value?: string;
    /** The people value mentions, by name, to show them by. */
    mentions?: MentionPerson[];
    /** Where files are attached while writing, if they may be. */
    uploadPath?: string;
    /** Where the people to mention are listed, if they may be mentioned. */
    peoplePath?: string;
    saveLabel: string;
    onsave: (body: string) => Promise<void>;
    oncancel?: () => void;
    /** After a file is attached to what the comment is on. */
    onattached?: () => void;
  } = $props();

  // The names shown for the people mentioned, by name, with their IDs.
  const known = new Map<string, string>();
  // svelte-ignore state_referenced_locally
  let body = $state(toDisplay(value, mentions, known));
  let tab = $state<'write' | 'preview'>('write');
  let preview = $state('');
  let busy = $state(false);
  let error = $state('');
  let textarea: HTMLTextAreaElement | undefined = $state();

  // Suggestions: the people, read once, the first time @ is typed; null
  // until then, and empty when there are none or they may not be read.
  let people = $state<MentionPerson[] | null>(null);
  let typing = $state<{ start: number; query: string } | null>(null);
  let active = $state(0);
  const suggestions = $derived(typing && people ? matching(people, typing.query) : []);
  const listID = $derived(`${id}-people`);

  /** Puts text in at the cursor, or at the end, on a line of its own. */
  export function insert(text: string) {
    const at = textarea?.selectionStart ?? body.length;
    const before = body.slice(0, at);
    const after = body.slice(at);
    const lead = before && !before.endsWith('\n') ? '\n' : '';
    body = `${before}${lead}${text}\n${after}`;
  }

  async function loadPeople() {
    if (people !== null || !peoplePath) return;
    people = [];
    try {
      people = (await request<{ people: MentionPerson[] }>('GET', peoplePath)).people ?? [];
    } catch {
      // No one to offer: mentions can still be written by hand.
    }
  }

  function track() {
    if (!peoplePath || !textarea) {
      typing = null;
      return;
    }
    const was = typing?.query;
    typing = mentionQuery(body, textarea.selectionStart ?? body.length);
    if (typing) {
      void loadPeople();
      if (typing.query !== was) active = 0;
    }
  }

  async function choose(person: MentionPerson) {
    if (!typing || !textarea) return;
    const { start, query } = typing;
    const shown = remember(known, person.name, person.id) ? `@${person.name}` : `<@${person.id}>`;
    const end = start + 1 + query.length;
    body = `${body.slice(0, start)}${shown} ${body.slice(end)}`;
    typing = null;
    const caret = start + shown.length + 1;
    await tick();
    textarea.focus();
    textarea.setSelectionRange(caret, caret);
  }

  function keydown(event: KeyboardEvent) {
    if (!suggestions.length) return;
    switch (event.key) {
      case 'ArrowDown':
        active = (active + 1) % suggestions.length;
        break;
      case 'ArrowUp':
        active = (active - 1 + suggestions.length) % suggestions.length;
        break;
      case 'Enter':
      case 'Tab': {
        const person = suggestions[active];
        if (person) void choose(person);
        break;
      }
      case 'Escape':
        typing = null;
        break;
      default:
        return;
    }
    event.preventDefault();
  }

  async function showPreview() {
    tab = 'preview';
    typing = null;
    error = '';
    try {
      preview = (await request<{ html: string }>('POST', '/markdown', { body: toStored(body, known) })).html;
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
      await onsave(toStored(body, known));
      body = '';
      tab = 'write';
      typing = null;
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
  <div class="relative" hidden={tab !== 'write'}>
    <textarea
      id="{id}-body"
      class="input min-h-28 w-full font-mono text-sm"
      rows="5"
      bind:this={textarea}
      bind:value={body}
      aria-describedby="{id}-hint"
      aria-autocomplete={peoplePath ? 'list' : undefined}
      aria-controls={suggestions.length ? listID : undefined}
      aria-activedescendant={suggestions.length ? `${listID}-${active}` : undefined}
      oninput={track}
      onclick={track}
      onkeyup={(event) => {
        if (event.key === 'ArrowLeft' || event.key === 'ArrowRight' || event.key === 'Home' || event.key === 'End') track();
      }}
      onkeydown={keydown}
      onblur={() => (typing = null)}
    ></textarea>
    {#if suggestions.length}
      <ul id={listID} role="listbox" aria-label="People to mention" class="absolute left-0 z-20 mt-1 max-h-64 w-full max-w-sm overflow-y-auto rounded border border-zinc-300 bg-white py-1 shadow-lg dark:border-zinc-700 dark:bg-zinc-900">
        {#each suggestions as person, i (person.id)}
          <li
            id="{listID}-{i}"
            role="option"
            tabindex="-1"
            aria-selected={i === active}
            class="flex min-h-11 cursor-pointer items-center px-3 text-sm aria-selected:bg-zinc-200 dark:aria-selected:bg-zinc-700"
            onmousedown={(event) => event.preventDefault()}
            onclick={() => choose(person)}
            onkeydown={keydown}
          >
            {person.name}
          </li>
        {/each}
      </ul>
    {/if}
  </div>
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
    Markdown: **bold**, lists, tables, `code`, [links](https://example.com) and - [ ] task lists. Images from other sites show as links.{#if peoplePath}
      Type @ to mention someone who can see this.{/if}
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

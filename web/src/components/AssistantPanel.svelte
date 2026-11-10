<!--
  The assistant beside the page (docs/assistant.md): a conversation, with the
  page it is sent from, each turn shown as it happens, a change waiting for
  the person to confirm it, and a guide's questions. Beside the page on a
  wide screen; over it on a phone, with a way back.
-->
<script lang="ts">
  import { tick } from 'svelte';
  import { router } from '../lib/router.svelte';
  import { messageOf } from '../lib/errors';
  import {
    LimitError,
    assistantApi,
    panel,
    resetTime,
    streamTurn,
    type Block,
    type Conversation,
    type Guide,
    type MessageView,
    type PendingView,
    type TurnLine,
  } from '../lib/assistant.svelte';
  import ErrorMessage from './ErrorMessage.svelte';

  let conversations = $state<Conversation[]>([]);
  let guides = $state<Guide[]>([]);
  let conversation = $state<Conversation | null>(null);
  let messages = $state<MessageView[]>([]);
  let pending = $state<PendingView | null>(null);
  let pendingNote = $state('');
  let guide = $state<Guide | null>(null);
  let text = $state('');
  let busy = $state(false);
  let error = $state('');
  let limit = $state<{ error: string; resets_at: string } | null>(null);
  // The page's context, unless the person took it away from this page.
  let dropped = $state('');
  const context = $derived(panel.context);
  const sendContext = $derived(context && dropped !== (context.path ?? '') ? context : null);
  let log: HTMLElement | undefined = $state();
  let box: HTMLTextAreaElement | undefined = $state();

  const name = $derived(panel.status?.name || 'Assistant');

  async function loadLists() {
    try {
      [conversations, guides] = await Promise.all([assistantApi.conversations(), assistantApi.guides().catch(() => [])]);
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function open(id: string | null) {
    error = '';
    limit = null;
    pending = null;
    guide = null;
    if (!id) {
      conversation = null;
      messages = [];
      return;
    }
    try {
      const d = await assistantApi.conversation(id);
      conversation = d.conversation;
      messages = d.messages ?? [];
      pending = d.pending ?? null;
      guide = d.guide ?? null;
      await scrollDown();
    } catch (err) {
      // Deleted, or past its time: start afresh.
      panel.choose(null);
      conversation = null;
      messages = [];
      error = messageOf(err);
    }
  }

  $effect(() => {
    void loadLists();
  });
  $effect(() => {
    const id = panel.conversation;
    if (id !== (conversation?.id ?? null)) void open(id);
  });

  async function scrollDown() {
    await tick();
    log?.scrollTo?.({ top: log.scrollHeight });
  }

  function online(line: TurnLine) {
    switch (line.type) {
      case 'message': {
        const at = messages.findIndex((m) => m.id === line.message.id);
        if (at >= 0) messages[at] = line.message;
        else messages = [...messages, line.message];
        void scrollDown();
        break;
      }
      case 'changed':
        panel.changed();
        break;
      case 'confirm':
        pending = line.pending;
        pendingNote = line.description ?? '';
        break;
      case 'progress':
        if (conversation) conversation = { ...conversation, progress: line.progress };
        break;
      case 'limit':
        limit = { error: line.error, resets_at: line.resets_at };
        break;
      case 'error':
        error = line.error;
        break;
      case 'done':
        conversation = line.conversation;
        pending = line.pending ?? null;
        break;
    }
  }

  async function turn(path: string, body: unknown) {
    busy = true;
    error = '';
    limit = null;
    try {
      await streamTurn(path, body, online);
    } catch (err) {
      if (err instanceof LimitError) limit = { error: err.message, resets_at: err.resetsAt };
      else error = messageOf(err);
    } finally {
      busy = false;
      void loadLists();
      void panel.load();
    }
  }

  async function start(guideSlug?: string) {
    error = '';
    try {
      const c = await assistantApi.start(guideSlug);
      conversation = c;
      messages = [];
      pending = null;
      guide = guideSlug ? (guides.find((g) => g.slug === guideSlug) ?? null) : null;
      panel.choose(c.id);
      await loadLists();
      return c;
    } catch (err) {
      error = messageOf(err);
      return null;
    }
  }

  async function send(event: SubmitEvent) {
    event.preventDefault();
    const said = text.trim();
    if (!said || busy) return;
    const c = conversation ?? (await start());
    if (!c) return;
    text = '';
    await turn(`/assistant/conversations/${encodeURIComponent(c.id)}/messages`, { text: said, context: sendContext ? [sendContext] : [] });
    box?.focus();
  }

  function keydown(e: KeyboardEvent) {
    // Enter sends, as chat does; Shift+Enter starts a new line.
    if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
      e.preventDefault();
      (e.currentTarget as HTMLTextAreaElement).form?.requestSubmit();
    }
  }

  async function answer(approve: boolean) {
    if (!conversation || !pending) return;
    const call = pending.id;
    pending = null;
    await turn(`/assistant/conversations/${encodeURIComponent(conversation.id)}/confirm`, { call, approve });
  }

  function textOf(b: Block): string {
    return b.text ?? '';
  }

  function argumentsOf(input: unknown): string {
    try {
      return JSON.stringify(input, null, 2);
    } catch {
      return String(input);
    }
  }

  const answered = (i: number) => conversation?.progress?.[String(i + 1)];
  const when = (s: string) => new Date(s).toLocaleString();
</script>

<aside
  id="assistant-panel"
  aria-label={name}
  class="fixed inset-0 z-40 flex flex-col overflow-hidden bg-zinc-50 print:hidden md:sticky md:inset-auto md:top-0 md:z-auto md:h-dvh md:w-[26rem] md:shrink-0 md:border-l-2 md:border-zinc-900 dark:bg-zinc-950 md:dark:border-zinc-600"
>
  <div class="flex flex-wrap items-center gap-2 border-b-2 border-zinc-900 px-3 py-2 dark:border-zinc-600">
    <button type="button" class="btn-secondary md:hidden" onclick={() => panel.hide()}>Back to the page</button>
    <h2 class="mr-auto text-lg font-bold">{name}</h2>
    <button type="button" class="btn-secondary" onclick={() => open(null).then(() => panel.choose(null))}>New</button>
    <button type="button" class="btn-secondary hidden md:inline-flex" aria-label="Close the assistant" onclick={() => panel.hide()}>Close</button>
  </div>

  {#if conversations.length}
    <div class="border-b border-zinc-300 px-3 py-2 dark:border-zinc-700">
      <label class="sr-only" for="assistant-conversation">Conversation</label>
      <select
        id="assistant-conversation"
        class="input"
        value={conversation?.id ?? ''}
        onchange={(e) => panel.choose((e.currentTarget as HTMLSelectElement).value || null)}
      >
        <option value="">A new conversation</option>
        {#each conversations as c (c.id)}<option value={c.id}>{c.title}</option>{/each}
      </select>
    </div>
  {/if}

  {#if guide && guide.questions.length}
    <section class="border-b border-zinc-300 px-3 py-2 text-sm dark:border-zinc-700" aria-labelledby="guide-progress-heading">
      <h3 id="guide-progress-heading" class="font-semibold">{guide.name}</h3>
      <p class="text-zinc-600 dark:text-zinc-400">
        {guide.questions.filter((_, i) => answered(i)).length} of {guide.questions.length} answered
      </p>
      <ol class="mt-1 space-y-1">
        {#each guide.questions as q, i (i)}
          <li class="flex gap-2" data-answered={answered(i) ? 'true' : 'false'}>
            <span aria-hidden="true">{answered(i) ? '✓' : '○'}</span>
            <span>
              <span class={answered(i) ? 'text-zinc-600 dark:text-zinc-400' : 'font-medium'}>{q}</span>
              <span class="sr-only">{answered(i) ? ', answered' : ', still open'}</span>
              {#if answered(i)}<span class="block text-xs text-zinc-600 dark:text-zinc-400">{answered(i)}</span>{/if}
            </span>
          </li>
        {/each}
      </ol>
    </section>
  {/if}

  <div bind:this={log} class="flex-1 space-y-3 overflow-y-auto px-3 py-3" aria-live="polite">
    {#if !conversation}
      <p class="text-sm">Ask in your own words, or tell it what you have. It uses BAM!'s tools as you, with your permissions.</p>
      {#if guides.length}
        <div class="space-y-2">
          <p class="text-sm font-semibold">Or start from a guide:</p>
          {#each guides as g (g.id)}
            <button type="button" class="btn-secondary w-full justify-start text-left" onclick={() => start(g.slug)}>{g.name}</button>
          {/each}
        </div>
      {/if}
    {/if}
    {#each messages as m (m.id)}
      {#if m.role === 'user'}
        <div class="ml-6 rounded border-2 border-zinc-900 bg-white p-2 text-sm dark:border-zinc-500 dark:bg-zinc-900" data-role="user">
          {#if m.context?.length}<p class="mb-1 text-xs text-zinc-600 dark:text-zinc-400">From {m.context.map((c) => c.label).join(', ')}</p>{/if}
          {#each m.blocks as b, i (i)}{#if b.type === 'text'}<p class="whitespace-pre-wrap">{textOf(b)}</p>{/if}{/each}
        </div>
      {:else if m.role === 'assistant'}
        <div class="mr-6 space-y-1 text-sm" data-role="assistant">
          {#each m.blocks as b, i (i)}
            {#if b.type === 'text' && b.text}
              <p class="whitespace-pre-wrap">{b.text}</p>
            {:else if b.type === 'tool_call'}
              <p class="text-xs text-zinc-600 dark:text-zinc-400">Used {b.title || b.name}</p>
            {/if}
          {/each}
        </div>
      {:else}
        {#each m.blocks as b, i (i)}
          {#if b.type === 'tool_result' && b.is_error}
            <p class="text-xs text-zinc-600 dark:text-zinc-400" data-role="tool">{b.title || b.name}: {b.content}</p>
          {/if}
        {/each}
      {/if}
    {/each}
    {#if busy}<p class="text-sm text-zinc-600 dark:text-zinc-400" role="status">Working…</p>{/if}
  </div>

  {#if pending}
    <section class="notice-strong mx-3 mb-2 block space-y-2" aria-labelledby="pending-heading" data-testid="assistant-confirm">
      <h3 id="pending-heading" class="font-semibold">Confirm: {pending.title}</h3>
      {#if pendingNote}<p class="text-sm">{pendingNote}</p>{/if}
      <details class="text-sm">
        <summary class="inline-flex min-h-11 cursor-pointer items-center">What it will send</summary>
        <pre class="max-h-48 overflow-auto rounded bg-zinc-100 p-2 text-xs whitespace-pre-wrap dark:bg-zinc-800">{argumentsOf(pending.input)}</pre>
      </details>
      <div class="flex flex-wrap gap-2">
        <button type="button" class="btn-secondary" disabled={busy} onclick={() => answer(true)}>Confirm</button>
        <button type="button" class="btn-secondary" disabled={busy} onclick={() => answer(false)}>Decline</button>
      </div>
    </section>
  {/if}

  <div class="space-y-2 border-t-2 border-zinc-900 px-3 py-2 dark:border-zinc-600">
    <ErrorMessage {error} />
    {#if limit}
      <p class="rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-sm dark:border-amber-700 dark:bg-amber-950" role="alert">
        {limit.error.replace(/ It resets at .*$/, '')} It resets {resetTime(limit.resets_at)}.
      </p>
    {/if}
    {#if sendContext}
      <p class="flex flex-wrap items-center gap-2 text-xs">
        <span class="chip-muted">On {sendContext.kind}: {sendContext.label}</span>
        <button type="button" class="btn-secondary" aria-label="Leave out the page {sendContext.label}" onclick={() => (dropped = sendContext?.path ?? router.path)}>
          Leave out
        </button>
      </p>
    {/if}
    <form class="flex items-end gap-2" onsubmit={send}>
      <label class="sr-only" for="assistant-message">Message to {name}</label>
      <textarea
        id="assistant-message"
        bind:this={box}
        bind:value={text}
        class="input min-h-11 flex-1 resize-y"
        rows="2"
        maxlength="20000"
        placeholder="Ask, or describe what you have"
        onkeydown={keydown}
      ></textarea>
      <button type="submit" class="btn-secondary" disabled={busy || !text.trim()}>Send</button>
    </form>
    {#if conversation}<p class="text-xs text-zinc-600 dark:text-zinc-400">Started {when(conversation.created_at)}. Only you can read it.</p>{/if}
  </div>
</aside>

<script lang="ts">
  // A person's conversations with the assistant beside the page
  // (docs/assistant.md#conversations): theirs alone, kept for a time the
  // instance sets, opened in the panel or deleted here.
  import { onMount } from 'svelte';
  import { messageOf } from '../lib/errors';
  import { assistantApi, panel, type Conversation } from '../lib/assistant.svelte';
  import ConfirmButton from '../components/ConfirmButton.svelte';
  import ErrorMessage from '../components/ErrorMessage.svelte';

  let conversations = $state<Conversation[]>([]);
  let loaded = $state(false);
  let error = $state('');
  let status = $state('');
  const when = (iso: string) => new Date(iso).toLocaleString();

  async function load() {
    try {
      conversations = await assistantApi.conversations();
    } catch {
      // Without the assistant there are none to show.
      conversations = [];
    } finally {
      loaded = true;
    }
  }
  onMount(load);

  async function remove(c: Conversation) {
    error = status = '';
    try {
      await assistantApi.remove(c.id);
      if (panel.conversation === c.id) panel.choose(null);
      status = `Deleted ${c.title}.`;
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }
</script>

{#if loaded && (conversations.length || panel.available)}
  <section class="card space-y-3" aria-labelledby="conversations-heading">
    <h2 id="conversations-heading" class="text-lg font-semibold">Conversations with the assistant</h2>
    <p class="text-sm text-zinc-600 dark:text-zinc-400">
      No one else can read them, admins included. Each is deleted a while after its last message, as this instance keeps them.
    </p>
    <ErrorMessage {error} />
    {#if status}<p role="status" class="text-sm">{status}</p>{/if}
    {#if conversations.length}
      <ul class="divide-y divide-zinc-200 dark:divide-zinc-800">
        {#each conversations as c (c.id)}
          <li class="flex flex-wrap items-center justify-between gap-2 py-2">
            <div class="min-w-0">
              <p class="font-medium break-words">{c.title}</p>
              <p class="text-xs text-zinc-600 dark:text-zinc-400">Last {when(c.updated_at)}</p>
            </div>
            <div class="flex flex-wrap gap-2">
              {#if panel.available}<button type="button" class="btn-secondary" onclick={() => panel.show(c.id)}>Open</button>{/if}
              <ConfirmButton label="Delete" question="Delete this conversation?" onconfirm={() => remove(c)} />
            </div>
          </li>
        {/each}
      </ul>
    {:else}
      <p class="text-sm">None yet: open the assistant from the header to start one.</p>
    {/if}
  </section>
{/if}

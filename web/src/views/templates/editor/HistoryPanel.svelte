<script lang="ts">
  import { useEditor } from './editor.svelte';

  // Every change, by name, as Krita's undo history: click one to go back to
  // just after it, or on again to one undone.
  const editor = useEditor();
  const done = $derived(editor.changes.done);
  const undone = $derived(editor.changes.undone);
</script>

<ol class="-mx-1 max-h-56 space-y-px overflow-y-auto" aria-label="History">
  <li>
    <button
      type="button"
      class="w-full rounded px-1 py-0.5 min-h-8 text-left text-xs hover:bg-zinc-100 aria-[current=step]:bg-blue-100 dark:hover:bg-zinc-800 dark:aria-[current=step]:bg-blue-950"
      aria-current={done.length === 0 ? 'step' : undefined}
      onclick={() => editor.jumpTo(0)}>As opened</button
    >
  </li>
  {#each done as label, k (k)}
    <li>
      <button
        type="button"
        class="w-full truncate rounded px-1 py-0.5 min-h-8 text-left text-xs hover:bg-zinc-100 aria-[current=step]:bg-blue-100 dark:hover:bg-zinc-800 dark:aria-[current=step]:bg-blue-950"
        aria-current={k === done.length - 1 ? 'step' : undefined}
        onclick={() => editor.jumpTo(k + 1)}>{label}</button
      >
    </li>
  {/each}
  {#each undone as label, k (k)}
    <li>
      <button
        type="button"
        class="w-full truncate rounded px-1 py-0.5 min-h-8 text-left text-xs text-zinc-600 italic hover:bg-zinc-100 dark:text-zinc-400 dark:hover:bg-zinc-800"
        title="Undone: click to do it again"
        onclick={() => editor.jumpTo(done.length + k + 1)}>{label}</button
      >
    </li>
  {/each}
</ol>

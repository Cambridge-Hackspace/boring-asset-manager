<script lang="ts">
  import type { LiveState } from '../../../lib/live/client';
  import { useEditor } from './editor.svelte';

  // Along the bottom, as in Krita: where the pointer is on the label, what
  // can be done now (the modifiers, mostly), and how the label is drawn.
  let { liveState }: { liveState: LiveState } = $props();
  const editor = useEditor();
  const fmt = (v: number) => (editor.doc.units === 'in' ? v.toFixed(3) : v.toFixed(1));
</script>

<div class="flex h-7 items-center gap-4 border-t border-zinc-200 bg-zinc-100 px-3 text-xs text-zinc-700 dark:border-zinc-800 dark:bg-zinc-900 dark:text-zinc-300">
  <span class="w-36 shrink-0 tabular-nums">
    {#if editor.pointer}{fmt(editor.pointer.x)}, {fmt(editor.pointer.y)} {editor.doc.units}{/if}
  </span>
  <span class="min-w-0 flex-1 truncate" title={editor.hint}>{editor.hint}</span>
  <span role="status" class="flex shrink-0 items-center gap-1.5">
    {#if liveState === 'loading'}
      <span aria-hidden="true" class="h-3 w-3 animate-spin rounded-full border-2 border-zinc-400 border-t-transparent"></span>
      Loading the live preview
    {:else if liveState === 'failed'}
      <span title="This browser cannot run the live preview: the server draws the label shortly after each change">Live preview unavailable</span>
    {/if}
  </span>
</div>

<script lang="ts">
  import { layerName } from '../../../lib/template';
  import { useEditor } from './editor.svelte';
  import Icon from './Icon.svelte';

  // The elements in drawing order, the top first. A row selects its
  // element (Shift adds it); dragging a row moves it over or under the
  // others.
  let { problemElements }: { problemElements: Set<number> } = $props();

  const editor = useEditor();
  const rows = $derived(editor.doc.elements.map((e, i) => ({ e, i })).reverse());
  let dragging = $state<number | null>(null);
  let over = $state<number | null>(null);

  function choose(event: MouseEvent, i: number) {
    const sel = editor.selected;
    editor.selected = event.shiftKey ? (sel.includes(i) ? sel.filter((s) => s !== i) : [...sel, i]) : [i];
  }

  function drop(event: DragEvent, i: number) {
    event.preventDefault();
    if (dragging !== null && dragging !== i) editor.moveLayer(dragging, i);
    dragging = over = null;
  }
</script>

{#if rows.length}
  <ol class="-mx-1 space-y-px" aria-label="Layers">
    {#each rows as { e, i } (i)}
      {@const selected = editor.selected.includes(i)}
      <li
        draggable="true"
        class="flex items-center gap-1 rounded px-1 {selected ? 'bg-blue-100 dark:bg-blue-950' : 'hover:bg-zinc-100 dark:hover:bg-zinc-800'} {over === i ? 'ring-1 ring-blue-600' : ''}"
        ondragstart={(ev) => {
          dragging = i;
          ev.dataTransfer?.setData('text/plain', String(i));
        }}
        ondragover={(ev) => {
          ev.preventDefault();
          over = i;
        }}
        ondragleave={() => (over = over === i ? null : over)}
        ondrop={(ev) => drop(ev, i)}
        ondragend={() => (dragging = over = null)}
      >
        <button
          type="button"
          class="min-w-0 flex-1 truncate py-1 text-left text-xs {problemElements.has(i) ? 'text-red-700 dark:text-red-400' : ''}"
          aria-pressed={selected}
          onclick={(ev) => choose(ev, i)}
        >
          {layerName(e)}{#if e.group}<span class="text-zinc-600 dark:text-zinc-400"> · {e.group}</span>{/if}
        </button>
        <button
          type="button"
          class="icon-btn h-6 w-6 {e.locked ? '' : 'opacity-40 hover:opacity-100'}"
          aria-pressed={e.locked ?? false}
          aria-label="Lock {layerName(e)}"
          title={e.locked ? 'Locked: it cannot be taken on the label' : 'Lock it, so it cannot be taken on the label by accident'}
          onclick={() => editor.toggleLock(i)}
        >
          <Icon name={e.locked ? 'lock' : 'unlock'} size={14} />
        </button>
      </li>
    {/each}
  </ol>
{:else}
  <p class="text-xs text-zinc-600 dark:text-zinc-400">The label is empty.</p>
{/if}

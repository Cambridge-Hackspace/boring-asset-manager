<script lang="ts">
  import { layerName, type Restack, type TemplateElement } from '../../lib/template';

  // The elements in drawing order, the top first. A row selects its
  // element; dragging a row, or Page Up, Page Down, Home and End on it,
  // moves it over or under the others.
  let {
    elements,
    selected,
    problemElements,
    onselect,
    onrestack,
    onmove,
    onlock,
  }: {
    elements: TemplateElement[];
    selected: number[];
    problemElements: Set<number>;
    onselect: (indices: number[]) => void;
    onrestack: (how: Restack) => void;
    /** Moves the element at from to to, both indices in the drawing order. */
    onmove: (from: number, to: number) => void;
    /** Locks an element, or unlocks it. */
    onlock: (index: number) => void;
  } = $props();

  // Rows run top first: the last element listed is drawn on top.
  const rows = $derived(elements.map((e, i) => ({ e, i })).reverse());
  let dragging = $state<number | null>(null);
  let over = $state<number | null>(null);

  function choose(event: MouseEvent, i: number) {
    if (event.shiftKey) onselect(selected.includes(i) ? selected.filter((s) => s !== i) : [...selected, i]);
    else onselect([i]);
  }

  const keys: Record<string, Restack> = { PageUp: 'forward', PageDown: 'backward', Home: 'front', End: 'back' };

  function keydown(event: KeyboardEvent, i: number) {
    const how = keys[event.key];
    if (!how) return;
    event.preventDefault();
    if (!selected.includes(i)) onselect([i]);
    onrestack(how);
  }

  function drop(event: DragEvent, i: number) {
    event.preventDefault();
    if (dragging !== null && dragging !== i) onmove(dragging, i);
    dragging = over = null;
  }
</script>

<section class="card space-y-2" aria-labelledby="layers-heading">
  <h2 id="layers-heading" class="text-lg font-semibold">Layers</h2>
  <p class="text-xs text-zinc-600 dark:text-zinc-400">
    The top is drawn over everything below it. Drag a layer to move it; or, on a layer, Page Up and Page Down move it one step, Home and End to the top and bottom.
  </p>
  <ol class="space-y-1 text-sm" aria-label="Layers">
    {#each rows as { e, i } (i)}
      <li
        draggable="true"
        class="flex items-center gap-2 rounded-md border px-2 py-1 {over === i ? 'border-blue-600' : 'border-transparent'} {selected.includes(i) ? 'bg-blue-50 dark:bg-blue-950' : ''}"
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
        <span aria-hidden="true" class="cursor-grab select-none text-zinc-500">⠿</span>
        <button
          type="button"
          class="min-w-0 flex-1 truncate text-left hover:underline {selected.includes(i) ? 'font-semibold' : ''} {problemElements.has(i) ? 'text-red-700 dark:text-red-400' : ''}"
          aria-pressed={selected.includes(i)}
          onclick={(ev) => choose(ev, i)}
          onkeydown={(ev) => keydown(ev, i)}
        >
          {layerName(e)}{#if e.group}<span class="text-xs text-zinc-600 dark:text-zinc-400"> · {e.group}</span>{/if}
        </button>
        <button
          type="button"
          class="rounded px-1 text-xs {e.locked ? 'bg-zinc-200 dark:bg-zinc-700' : 'text-zinc-600 dark:text-zinc-400'}"
          aria-pressed={e.locked ?? false}
          aria-label="Lock {layerName(e)}"
          title={e.locked ? 'Locked: not selected on the label by accident' : 'Lock it, so it is not selected on the label by accident'}
          onclick={() => onlock(i)}>{e.locked ? 'Locked' : 'Lock'}</button
        >
      </li>
    {/each}
  </ol>
</section>

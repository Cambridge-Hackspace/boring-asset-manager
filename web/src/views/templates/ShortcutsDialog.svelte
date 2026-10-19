<script lang="ts">
  // Every keyboard shortcut of the template editor, opened with ?.
  let { open, onclose }: { open: boolean; onclose: () => void } = $props();

  let dialog: HTMLDialogElement | undefined = $state();
  $effect(() => {
    if (open && dialog && !dialog.open) dialog.showModal();
    else if (!open && dialog?.open) dialog.close();
  });

  const groups: [string, [string, string][]][] = [
    [
      'Selecting',
      [
        ['Click', 'Select an element, and the rest of its group'],
        ['Shift+click', 'Add to the selection, or take from it'],
        ['Alt+click', 'Select what is beneath the selected element there'],
        ['Drag on the label', 'Select what the box touches'],
        ['Ctrl+A', 'Select everything not locked'],
        ['Escape', 'Select nothing; stop drawing'],
      ],
    ],
    [
      'Changing',
      [
        ['Arrow keys', 'Move a grid step; with Shift, ten'],
        ['[ and ]', 'Turn 15°; with Shift, 1°'],
        ['Page Up, Page Down', 'Bring forward, send backward'],
        ['Home, End', 'Bring to the front, send to the back'],
        ['Delete', 'Remove'],
        ['Ctrl+G, Ctrl+Shift+G', 'Group, ungroup'],
        ['Ctrl+C, Ctrl+X, Ctrl+V', 'Copy, cut, paste, also between templates; paste SVG to import it'],
        ['Ctrl+D', 'Duplicate'],
        ['Ctrl+Z, Ctrl+Shift+Z', 'Undo, redo'],
        ['Alt while dragging', 'Leave the grid and the guides; when turning, do not snap'],
      ],
    ],
    [
      'Viewing',
      [
        ['Ctrl+wheel, pinch, + and -', 'Zoom in and out'],
        ['0', 'Zoom to fit'],
        ['1', 'Actual size'],
        ['Space+drag, middle button, two fingers', 'Pan'],
        ['Drag from a ruler', 'Place a guide; drag it off the label to remove it'],
        ['?', 'These shortcuts'],
      ],
    ],
  ];
</script>

<dialog
  bind:this={dialog}
  aria-labelledby="shortcuts-heading"
  class="max-h-[85vh] w-[min(40rem,calc(100vw-2rem))] overflow-auto rounded-lg bg-white p-6 text-zinc-900 shadow-xl backdrop:bg-black/40 dark:bg-zinc-900 dark:text-zinc-100"
  onclose={onclose}
>
  <div class="mb-4 flex items-center justify-between gap-4">
    <h2 id="shortcuts-heading" class="text-lg font-semibold">Keyboard shortcuts</h2>
    <button type="button" class="btn-secondary" onclick={() => dialog?.close()}>Close</button>
  </div>
  {#each groups as [title, rows] (title)}
    <h3 class="mt-3 mb-1 font-semibold">{title}</h3>
    <dl class="grid grid-cols-[minmax(0,2fr)_minmax(0,3fr)] gap-x-4 gap-y-1 text-sm">
      {#each rows as [keys, what] (keys)}
        <dt class="font-mono">{keys}</dt>
        <dd>{what}</dd>
      {/each}
    </dl>
  {/each}
</dialog>

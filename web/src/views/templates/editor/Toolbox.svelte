<script lang="ts">
  import { useEditor, type Tool } from './editor.svelte';
  import Icon, { type IconName } from './Icon.svelte';

  // The toolbox, down the left as in Krita and Inkscape: one tool in hand
  // at a time. Chosen from the keyboard, a tool that adds something adds it
  // in the middle of the label at once, as there is no pointer to draw with.
  let { onadd, onimport }: { onadd: (tool: Exclude<Tool, 'select'>) => void; onimport: (file: File) => void } = $props();

  const editor = useEditor();

  const tools: { tool: Tool; icon: IconName; name: string }[] = [
    { tool: 'select', icon: 'select', name: 'Select and move' },
    { tool: 'text', icon: 'text', name: 'Text' },
    { tool: 'code', icon: 'code', name: 'Barcode or QR code' },
    { tool: 'image', icon: 'image', name: 'Image' },
    { tool: 'rectangle', icon: 'rectangle', name: 'Rectangle' },
    { tool: 'ellipse', icon: 'ellipse', name: 'Ellipse' },
    { tool: 'line', icon: 'line', name: 'Line' },
    { tool: 'polygon', icon: 'polygon', name: 'Polygon' },
  ];

  const button =
    'grid h-9 w-9 place-items-center rounded-md text-zinc-700 hover:bg-zinc-200 aria-pressed:bg-zinc-800 aria-pressed:text-white dark:text-zinc-300 dark:hover:bg-zinc-800 dark:aria-pressed:bg-zinc-200 dark:aria-pressed:text-zinc-900';

  function choose(event: MouseEvent, tool: Tool) {
    if (event.detail === 0 && tool !== 'select') {
      editor.tool = 'select';
      onadd(tool);
      return;
    }
    editor.tool = tool;
  }
</script>

<div role="toolbar" aria-label="Tools" class="flex flex-wrap items-center gap-0.5 p-1 md:flex-col md:flex-nowrap">
  {#each tools as t, i (t.tool)}
    {#if i === 1 || i === 4}<hr class="hidden md:my-1 md:block md:w-6 md:border-zinc-300 dark:md:border-zinc-700" />{/if}
    <button
      type="button"
      class={button}
      aria-label={t.name}
      aria-pressed={editor.tool === t.tool}
      title={t.tool === 'select' ? t.name : `${t.name}: drag on the label to place it`}
      onclick={(ev) => choose(ev, t.tool)}
    >
      <Icon name={t.icon} />
    </button>
  {/each}
  <hr class="hidden md:my-1 md:block md:w-6 md:border-zinc-300 dark:md:border-zinc-700" />
  <label class="{button} cursor-pointer focus-within:outline-2 focus-within:outline-accent-600" title="Import SVG artwork as shapes">
    <Icon name="import" />
    <span class="sr-only">Import SVG</span>
    <input
      type="file"
      accept=".svg,image/svg+xml"
      class="sr-only"
      onchange={(ev) => {
        const file = ev.currentTarget.files?.[0];
        ev.currentTarget.value = '';
        if (file) onimport(file);
      }}
    />
  </label>
</div>

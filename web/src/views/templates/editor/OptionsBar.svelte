<script lang="ts">
  import type { GeneratorInfo } from '../../../lib/models';
  import { boundsOf, normalAngle, withProp } from '../../../lib/template';
  import { useEditor } from './editor.svelte';
  import Icon from './Icon.svelte';
  import NumberField from './NumberField.svelte';
  import { anchorAt, anchors, setAtAnchor, type Anchor } from './transform';

  // The tool's options, across the top as in Krita: for the select tool,
  // the selection's place and size in numbers, given at the anchor chosen
  // on the 3 × 3 grid (which Ctrl+click on a handle also sets), with its
  // aspect locked or not; for the others, what they will add.
  let { generators }: { generators: GeneratorInfo[] } = $props();

  const editor = useEditor();
  const doc = $derived(editor.doc);
  const index = $derived(editor.selected.length === 1 ? editor.selected[0]! : -1);
  const e = $derived(index >= 0 ? doc.elements[index] : undefined);
  const box = $derived(editor.selected.length > 1 ? boundsOf(editor.selected.map((i) => doc.elements[i]!).filter(Boolean)) : null);
  const id = (name: string) => `el-${index}-${name}`;
  const at = $derived(e ? anchorAt(e, editor.anchor) : null);
  const round = (v: number) => Math.round(v * 1e4) / 1e4;

  function set(change: { x?: number; y?: number; width?: number; height?: number }) {
    if (e) editor.update(index, setAtAnchor(doc, e, editor.anchor, change, editor.aspectLocked));
  }
  function setRotation(degrees: number) {
    if (e && Number.isFinite(degrees)) editor.update(index, withProp(e, 'rotation', normalAngle(degrees) || undefined));
  }
  const flippable = $derived(e?.type === 'static_image' || (e?.type === 'shape' && e.shape !== 'rectangle' && e.shape !== 'ellipse'));
  const anchorNames: Record<Anchor, string> = {
    nw: 'Top left', n: 'Top', ne: 'Top right', w: 'Left', c: 'Center', e: 'Right', sw: 'Bottom left', s: 'Bottom', se: 'Bottom right',
  };
  // The Snap menu, as Krita's "Snap to" popup.
  let snapOpen = $state(false);
  const snapItems = [
    ['grid', 'The grid'],
    ['guides', 'Guides'],
    ['elements', 'Other elements: edges and centers'],
    ['label', 'The label: edges and center'],
  ] as const;

  const names: Record<string, string> = { text: 'Text', code: 'Code', image: 'Image', rectangle: 'Rectangle', ellipse: 'Ellipse', line: 'Line', polygon: 'Polygon' };
</script>

<svelte:window onpointerdown={(e) => snapOpen && !(e.target as Element).closest('[data-snap-menu]') && (snapOpen = false)} />

<div class="flex min-h-9 flex-wrap items-center gap-x-3 gap-y-1 border-b border-zinc-200 bg-zinc-50 px-2 py-1 text-xs dark:border-zinc-800 dark:bg-zinc-900" role="group" aria-label="Tool options">
  {#if editor.tool === 'select'}
    {#if e && at}
      <div class="grid grid-cols-3 gap-px" role="radiogroup" aria-label="Numbers given at">
        {#each anchors as a (a)}
          <button
            type="button"
            role="radio"
            aria-checked={editor.anchor === a}
            aria-label={anchorNames[a]}
            title="Numbers given at the {anchorNames[a].toLowerCase()}"
            class="h-2 w-2 rounded-[1px] border border-zinc-400 aria-checked:border-blue-600 aria-checked:bg-blue-600 dark:border-zinc-500"
            onclick={() => (editor.anchor = a)}
          ></button>
        {/each}
      </div>
      <fieldset class="flex items-center gap-2">
        <legend class="sr-only">Position and size ({doc.units}){#if e.rotation}, before it is turned{/if}</legend>
        <div class="w-24"><NumberField id={id('x')} label="X" value={round(at[0])} length={doc.units} onchange={(n) => set({ x: n })} /></div>
        <div class="w-24"><NumberField id={id('y')} label="Y" value={round(at[1])} length={doc.units} onchange={(n) => set({ y: n })} /></div>
        <div class="w-24"><NumberField id={id('width')} label="W" title="Width" value={e.width} min={0} length={doc.units} onchange={(n) => set({ width: n })} /></div>
        <button
          type="button"
          class="icon-btn h-6 w-6"
          aria-pressed={editor.aspectLocked}
          aria-label="Keep proportions"
          title="Width and height changed together, in proportion"
          onclick={() => (editor.aspectLocked = !editor.aspectLocked)}><Icon name={editor.aspectLocked ? 'lock' : 'unlock'} size={14} /></button
        >
        <div class="w-24"><NumberField id={id('height')} label="H" title="Height" value={e.height} min={0} length={doc.units} onchange={(n) => set({ height: n })} /></div>
        <span class="text-zinc-600 dark:text-zinc-400">{doc.units}</span>
      </fieldset>
      <div class="flex items-center gap-0.5">
        <div class="w-24"><NumberField id={id('rotation')} label="∠" title="Turned, in degrees clockwise" value={e.rotation ?? 0} step={15} unit="°" onchange={setRotation} /></div>
        <button type="button" class="icon-btn" aria-label="Turn 90° left" title="Turn 90° left" onclick={() => setRotation((e.rotation ?? 0) - 90)}><Icon name="turnLeft" size={16} /></button>
        <button type="button" class="icon-btn" aria-label="Turn 90° right" title="Turn 90° right" onclick={() => setRotation((e.rotation ?? 0) + 90)}><Icon name="turnRight" size={16} /></button>
        {#if flippable}
          <button type="button" class="icon-btn" aria-label="Flip across" title="Flip across" onclick={() => editor.flip(true)}><Icon name="flipAcross" size={16} /></button>
          <button type="button" class="icon-btn" aria-label="Flip down" title="Flip down" onclick={() => editor.flip(false)}><Icon name="flipDown" size={16} /></button>
        {/if}
      </div>
    {:else if box}
      <span>{editor.selected.length} selected, {box.width.toFixed(2)} × {box.height.toFixed(2)} {doc.units}</span>
    {:else}
      <span class="text-zinc-600 dark:text-zinc-400">Select and move</span>
    {/if}
    <div class="relative ml-auto" data-snap-menu>
      <button
        type="button"
        class="btn-sm"
        aria-haspopup="menu"
        aria-expanded={snapOpen}
        title="What dragging snaps to; Shift turns it off while held"
        onclick={() => (snapOpen = !snapOpen)}>Snap to ▾</button
      >
      {#if snapOpen}
        <div
          role="menu"
          aria-label="Snap to"
          tabindex="-1"
          class="absolute top-full right-0 z-40 mt-1 min-w-56 rounded-md border border-zinc-300 bg-white py-1 shadow-lg dark:border-zinc-700 dark:bg-zinc-900"
          onkeydown={(e) => e.key === 'Escape' && (snapOpen = false)}
        >
          {#each snapItems as [key, label] (key)}
            <button
              type="button"
              role="menuitemcheckbox"
              aria-checked={editor.snapTo[key]}
              class="flex w-full items-center gap-2 px-3 py-1 text-left hover:bg-zinc-100 dark:hover:bg-zinc-800"
              onclick={() => editor.setSnap(key, !editor.snapTo[key])}
            >
              <span class="w-3" aria-hidden="true">{editor.snapTo[key] ? '✓' : ''}</span>{label}
            </button>
          {/each}
          <div role="separator" class="my-1 border-t border-zinc-200 dark:border-zinc-700"></div>
          <button
            type="button"
            role="menuitemcheckbox"
            aria-checked={editor.snapAngles}
            class="flex w-full items-center gap-2 px-3 py-1 text-left hover:bg-zinc-100 dark:hover:bg-zinc-800"
            onclick={() => editor.setSnapAngles(!editor.snapAngles)}
          >
            <span class="w-3" aria-hidden="true">{editor.snapAngles ? '✓' : ''}</span>Turns, to 45°
          </button>
        </div>
      {/if}
    </div>
  {:else}
    <span class="font-medium">{names[editor.tool]}</span>
    {#if editor.tool === 'code'}
      <label class="flex items-center gap-1" for="tool-code-type">
        <span class="text-zinc-600 dark:text-zinc-400">Type</span>
        <select id="tool-code-type" class="input-sm w-auto" bind:value={editor.codeType}>
          {#each generators as g (g.type)}<option value={g.type}>{g.name}</option>{/each}
        </select>
      </label>
    {/if}
  {/if}
</div>

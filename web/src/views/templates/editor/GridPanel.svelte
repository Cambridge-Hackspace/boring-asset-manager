<script lang="ts">
  import { gridStep, type Grid } from '../../../lib/template';
  import { useEditor } from './editor.svelte';
  import Icon from './Icon.svelte';
  import NumberField from './NumberField.svelte';
  import { gridOf } from './snapping';

  // The grid and the guides, as Krita's grid docker: the grid's spacing
  // across and down, kept equal with the lock, its offset, its
  // subdivisions, and whether it is shown, all kept with the template; the
  // guides shown or not, locked, or all taken away.
  const editor = useEditor();
  const doc = $derived(editor.doc);
  const grid = $derived(gridOf(doc));
  let equal = $state(true);

  function set(change: Partial<Grid>) {
    let next: Grid = { ...grid, ...change };
    if (equal && change.x !== undefined) next = { ...next, y: change.x };
    if (equal && change.y !== undefined) next = { ...next, x: change.y };
    // Offsets stay within a step.
    if ((next.offset_x ?? 0) >= next.x) next = { ...next, offset_x: undefined };
    if ((next.offset_y ?? 0) >= next.y) next = { ...next, offset_y: undefined };
    const clean = Object.fromEntries(Object.entries(next).filter(([, v]) => v !== undefined)) as unknown as Grid;
    editor.apply({ ...$state.snapshot(editor.doc), grid: clean }, 'Change the grid');
  }
  const row = 'grid grid-cols-2 gap-x-2 gap-y-1';
</script>

<div class="space-y-3">
  <fieldset class="space-y-1">
    <legend class="flex w-full items-center justify-between text-xs text-zinc-600 dark:text-zinc-400">
      Grid
      <label class="flex min-h-8 items-center gap-1"><input type="checkbox" checked={doc.grid?.visible ?? false} onchange={(e) => set({ visible: e.currentTarget.checked || undefined })} /> Show it</label>
    </legend>
    <div class="flex items-center gap-1">
      <div class="flex-1"><NumberField id="grid-x" label="Step across" value={grid.x} min={0.001} step={gridStep(doc.units)} length={doc.units} onchange={(n) => n > 0 && set({ x: n })} /></div>
      <button type="button" class="icon-btn" aria-pressed={equal} aria-label="Keep the spacing equal" title="The same spacing across and down" onclick={() => (equal = !equal)}>
        <Icon name={equal ? 'lock' : 'unlock'} size={14} />
      </button>
      <div class="flex-1"><NumberField id="grid-y" label="Step down" value={grid.y} min={0.001} step={gridStep(doc.units)} length={doc.units} onchange={(n) => n > 0 && set({ y: n })} /></div>
    </div>
    <div class={row}>
      <NumberField id="grid-offset-x" label="Offset" title="Where the lines across start" value={grid.offset_x ?? 0} min={0} length={doc.units} onchange={(n) => set({ offset_x: n || undefined })} />
      <NumberField id="grid-offset-y" label="Offset" title="Where the lines down start" value={grid.offset_y ?? 0} min={0} length={doc.units} onchange={(n) => set({ offset_y: n || undefined })} />
      <NumberField
        id="grid-subdivisions"
        label="Divided in"
        title="Fainter lines between"
        value={grid.subdivisions ?? 1}
        min={1}
        max={10}
        onchange={(n) => set({ subdivisions: Math.round(n) > 1 ? Math.round(n) : undefined })}
      />
    </div>
  </fieldset>
  <fieldset class="space-y-1">
    <legend class="text-xs text-zinc-600 dark:text-zinc-400">Guides</legend>
    <p class="text-xs text-zinc-600 dark:text-zinc-400">Drag one out of a ruler; drag it off the label to take it away.</p>
    <div class="flex flex-wrap items-center gap-3 text-xs">
      <label class="flex min-h-8 items-center gap-1"><input type="checkbox" checked={editor.guidesShown} onchange={(e) => editor.setGuidesShown(e.currentTarget.checked)} /> Show them</label>
      <label class="flex min-h-8 items-center gap-1"><input type="checkbox" checked={editor.guidesLocked} onchange={(e) => editor.setGuidesLocked(e.currentTarget.checked)} /> Lock them</label>
      <button type="button" class="btn-sm" disabled={!doc.guides} onclick={() => editor.setGuides({}, 'Delete all guides')}>Delete all</button>
    </div>
  </fieldset>
</div>

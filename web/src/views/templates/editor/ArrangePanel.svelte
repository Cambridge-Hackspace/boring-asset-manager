<script lang="ts">
  import type { Alignment, Restack } from '../../../lib/template';
  import { useEditor } from './editor.svelte';
  import Icon, { type IconName } from './Icon.svelte';

  // Stacking, lining up and spreading out what is selected.
  const editor = useEditor();
  const none = $derived(!editor.selected.length);
  const n = $derived(editor.selected.length);
  // Aligning to the label, rather than to each other.
  let toLabel = $state(false);

  const stacking: [Restack, IconName, string][] = [
    ['front', 'front', 'Bring to front'],
    ['forward', 'forward', 'Bring forward'],
    ['backward', 'backward', 'Send backward'],
    ['back', 'back', 'Send to back'],
  ];
  const alignments: [Alignment, string][] = [
    ['left', 'Left edges'],
    ['center', 'Centers'],
    ['right', 'Right edges'],
    ['top', 'Tops'],
    ['middle', 'Middles'],
    ['bottom', 'Bottoms'],
  ];
</script>

<div class="space-y-2">
  <div class="flex gap-0.5" role="group" aria-label="Stacking">
    {#each stacking as [how, icon, label] (how)}
      <button type="button" class="icon-btn" aria-label={label} title={label} disabled={none} onclick={() => editor.restack(how)}><Icon name={icon} size={16} /></button>
    {/each}
  </div>
  <div class="space-y-1" role="group" aria-label="Line up">
    <div class="grid grid-cols-3 gap-1">
      {#each alignments as [how, label] (how)}
        <button type="button" class="btn-sm" disabled={none} onclick={() => editor.arrange({ align: how }, toLabel)}>{label}</button>
      {/each}
    </div>
    <label class="flex items-center gap-1 text-xs"><input type="checkbox" bind:checked={toLabel} /> To the label, not each other</label>
  </div>
  <div class="grid grid-cols-2 gap-1" role="group" aria-label="Spread out evenly">
    <button type="button" class="btn-sm" disabled={n < 3} onclick={() => editor.arrange({ distribute: 'across' }, false)}>Spread across</button>
    <button type="button" class="btn-sm" disabled={n < 3} onclick={() => editor.arrange({ distribute: 'down' }, false)}>Spread down</button>
  </div>
</div>

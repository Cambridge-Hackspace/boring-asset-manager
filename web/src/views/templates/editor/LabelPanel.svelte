<script lang="ts">
  import { convertUnits, makeAdaptive, makeFixed, orientation, turnTemplate, type Adapt, type TemplateDoc, type Units } from '../../../lib/template';
  import { useEditor } from './editor.svelte';
  import Icon from './Icon.svelte';
  import NumberField from './NumberField.svelte';

  // The template's own settings: its name, units, size and resolution, which
  // way up it is, and whether it is laid out again for other sizes.
  let { isNew, problems }: { isNew: boolean; problems: Record<string, string> } = $props();

  const editor = useEditor();
  const doc = $derived(editor.doc);

  function change(next: TemplateDoc) {
    editor.apply(next);
  }
  function update(patch: Partial<TemplateDoc>) {
    change({ ...$state.snapshot(editor.doc), ...patch } as TemplateDoc);
  }
  function setSize(key: 'width' | 'height' | 'dpi', n: number) {
    if (n > 0) update({ [key]: key === 'dpi' ? Math.round(n) : n });
  }
  function setRange(key: 'min_width' | 'max_width' | 'min_height' | 'max_height', n: number) {
    if (doc.adapt && n > 0) update({ adapt: { ...doc.adapt, [key]: n } });
  }
  function setOrientation(to: 'portrait' | 'landscape') {
    if (orientation(doc) !== to) change(turnTemplate(doc, to === 'portrait'));
  }
  const field = 'mb-0.5 block text-xs text-zinc-600 dark:text-zinc-400';
</script>

<div class="space-y-3">
  <div>
    <label class={field} for="tpl-name">Name</label>
    <!-- The name is not an undo step: typing it is not editing the label. -->
    <input id="tpl-name" class="input-sm" required value={doc.name} oninput={(e) => (editor.doc.name = e.currentTarget.value)} aria-invalid={Boolean(problems.name)} />
    {#if problems.name}<p class="error-text text-xs">{problems.name}</p>{/if}
    {#if !isNew}<p class="mt-0.5 truncate font-mono text-[0.7rem] text-zinc-600 dark:text-zinc-400" title="Assigned when first saved; the API names the template by it">{doc.id}</p>{/if}
  </div>

  <div class="grid grid-cols-2 gap-x-2 gap-y-1">
    <NumberField id="tpl-width" label="Width" value={doc.width} min={0} unit={doc.units} onchange={(n) => setSize('width', n)} />
    <NumberField id="tpl-height" label="Height" value={doc.height} min={0} unit={doc.units} onchange={(n) => setSize('height', n)} />
    <NumberField id="tpl-dpi" label="DPI" title="Dots per inch: the printer's resolution" value={doc.dpi} min={150} max={600} step={1} onchange={(n) => setSize('dpi', n)} />
    <label class="flex items-center gap-1 text-xs" for="tpl-units">
      <span class="text-zinc-600 dark:text-zinc-400">Units</span>
      <select id="tpl-units" class="input-sm" value={doc.units} onchange={(e) => change(convertUnits(doc, e.currentTarget.value as Units))}>
        <option value="mm">Millimeters</option>
        <option value="in">Inches</option>
      </select>
    </label>
  </div>
  {#each ['width', 'height', 'dpi', 'units'] as key (key)}
    {#if problems[key]}<p class="error-text text-xs">{problems[key]}</p>{/if}
  {/each}

  <div class="flex items-center gap-1">
    <div class="flex rounded border border-zinc-300 dark:border-zinc-700" role="radiogroup" aria-label="Orientation">
      {#each [['landscape', 'Landscape'], ['portrait', 'Portrait']] as const as [value, label] (value)}
        <button
          type="button"
          role="radio"
          aria-checked={orientation(doc) === value}
          class="px-2 py-0.5 text-xs aria-checked:bg-zinc-800 aria-checked:text-white dark:aria-checked:bg-zinc-200 dark:aria-checked:text-zinc-900"
          onclick={() => setOrientation(value)}>{label}</button
        >
      {/each}
    </div>
    <button type="button" class="icon-btn" aria-label="Turn the label left" title="Turn the label left, with everything on it" onclick={() => change(turnTemplate(doc, false))}><Icon name="turnLeft" size={16} /></button>
    <button type="button" class="icon-btn" aria-label="Turn the label right" title="Turn the label right, with everything on it" onclick={() => change(turnTemplate(doc, true))}><Icon name="turnRight" size={16} /></button>
  </div>

  <fieldset class="space-y-1">
    <legend class={field}>Sizing</legend>
    <label class="flex items-center gap-1 text-xs">
      <input type="radio" name="tpl-sizing" checked={doc.sizing !== 'adaptive'} onchange={() => change(makeFixed(doc))} />
      One label size
    </label>
    <label class="flex items-center gap-1 text-xs">
      <input type="radio" name="tpl-sizing" checked={doc.sizing === 'adaptive'} onchange={() => change(makeAdaptive(doc))} />
      Fits several sizes, laid out again for each
    </label>
    {#if doc.sizing === 'adaptive' && doc.adapt}
      <div class="grid grid-cols-2 gap-x-2 gap-y-1 pt-1">
        {#each [['min_width', 'Narrowest'], ['max_width', 'Widest'], ['min_height', 'Shortest'], ['max_height', 'Tallest']] as const as [key, label] (key)}
          <NumberField id="tpl-{key}" {label} value={doc.adapt[key]} min={0} unit={doc.units} onchange={(n) => setRange(key, n)} />
        {/each}
      </div>
      <label class="flex items-center gap-1 text-xs">
        <input type="checkbox" checked={doc.turnable ?? false} onchange={(e) => update({ turnable: e.currentTarget.checked || undefined })} />
        Also takes labels turned a quarter turn
      </label>
      <label class="flex items-center gap-1 text-xs" for="tpl-length">
        <span class="shrink-0 text-zinc-600 dark:text-zinc-400">On a roll</span>
        <select id="tpl-length" class="input-sm" value={doc.adapt.continuous_length} onchange={(e) => update({ adapt: { ...doc.adapt!, continuous_length: e.currentTarget.value as Adapt['continuous_length'] } })}>
          <option value="proportional">Length in proportion to its width</option>
          <option value="fixed">Length as designed</option>
        </select>
      </label>
      {#each Object.entries(problems).filter(([k]) => k.startsWith('adapt') || k === 'sizing') as [k, message] (k)}
        <p class="error-text text-xs">{message}</p>
      {/each}
    {/if}
  </fieldset>
</div>

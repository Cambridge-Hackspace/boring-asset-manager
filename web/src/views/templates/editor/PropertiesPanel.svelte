<script lang="ts">
  import type { GeneratorInfo } from '../../../lib/models';
  import {
    defaultStroke,
    describeElement,
    hairline,
    marginOf,
    opensAPage,
    SCAN_URL,
    sides,
    withMargin,
    withProp,
    type CodeLayout,
    type Paint,
    type Side,
    type TemplateElement,
  } from '../../../lib/template';
  import AdaptiveElementPanel from '../AdaptiveElementPanel.svelte';
  import ImagePicker from '../ImagePicker.svelte';
  import { useEditor } from './editor.svelte';
  import Icon from './Icon.svelte';
  import NumberField from './NumberField.svelte';

  // What is selected, and its settings: with one element, all of them, each
  // change one undoable step; with several, what is done to them together.
  let {
    generators,
    placeholderList,
    problemsFor,
    codeLayouts,
  }: {
    generators: GeneratorInfo[];
    placeholderList: string[];
    problemsFor: (index: number) => string[];
    codeLayouts: Record<number, CodeLayout>;
  } = $props();

  const editor = useEditor();
  const doc = $derived(editor.doc);
  const index = $derived(editor.selected.length === 1 ? editor.selected[0]! : -1);
  const e = $derived(index >= 0 ? doc.elements[index] : undefined);
  const id = (name: string) => `el-${index}-${name}`;

  function update(next: TemplateElement) {
    editor.update(index, next);
  }
  function set<K extends keyof TemplateElement>(key: K, value: TemplateElement[K]) {
    if (e) update({ ...e, [key]: value });
  }

  function setGeneratorType(type: string) {
    if (!e) return;
    const levels = generators.find((g) => g.type === type)?.error_correction_levels ?? [];
    const next: TemplateElement = { ...e, generator_type: type };
    if (!levels.includes(next.error_correction ?? '')) delete next.error_correction;
    update(next);
  }

  const paints: [Paint, string][] = [
    ['black', 'Black'],
    ['white', 'White'],
    ['none', 'None'],
  ];
  function setStroke(paint: Paint) {
    if (!e) return;
    let next = withProp(e, 'stroke', paint);
    if (paint === 'none') next = withProp(next, 'stroke_width', undefined);
    else if (!next.stroke_width) next = withProp(next, 'stroke_width', defaultStroke(doc.units));
    update(next);
  }

  function setMargin(on: Side[], raw: string) {
    const n = Number(raw);
    if (!e || (raw !== '' && !(Number.isFinite(n) && n >= 0))) return;
    update(withMargin(e, on, raw === '' ? undefined : n));
  }
  function setMarginUnit(to: 'modules' | 'length') {
    if (!e) return;
    // A margin in one unit means nothing in the other: start again from the standard.
    const { margin: _m, quiet_zone: _q, ...rest } = e;
    update(withProp(rest, 'margin_unit', to === 'modules' ? undefined : to));
  }

  const generator = $derived(generators.find((g) => g.type === e?.generator_type));
  const grouped = $derived(
    editor.selected.length > 1 && editor.selected.every((i) => doc.elements[i]?.group && doc.elements[i]?.group === doc.elements[editor.selected[0]!]?.group),
  );
  const layout = $derived(index >= 0 ? codeLayouts[index] : undefined);
  const sideNames: Record<Side, string> = { top: 'Top', right: 'Right', bottom: 'Bottom', left: 'Left' };
  const row = 'grid grid-cols-2 gap-x-2 gap-y-1';
</script>

{#if e}
  <div class="space-y-3">
    <div class="flex items-center justify-between gap-2">
      <p class="font-medium">{describeElement(e, index)}</p>
      <button type="button" class="icon-btn" aria-label="Remove it" title="Remove it" onclick={() => editor.remove()}><Icon name="trash" size={16} /></button>
    </div>
    {#if problemsFor(index).length}
      <ul class="space-y-1" role="alert">
        {#each problemsFor(index) as p (p)}<li class="error-text text-xs">{p}</li>{/each}
      </ul>
    {/if}


    {#if e.type === 'text' || e.type === 'generator'}
      <div>
        <label class="mb-0.5 block text-xs text-zinc-600 dark:text-zinc-400" for={id('binding')}>Content</label>
        <input
          id={id('binding')}
          class="input-sm font-mono"
          list={id('placeholders')}
          value={e.content_binding ?? ''}
          placeholder={e.type === 'generator' ? `{{asset.code.${e.generator_type}}}` : ''}
          title="Text with placeholders such as {'{{asset.name}}'}"
          onchange={(ev) => set('content_binding', ev.currentTarget.value)}
        />
        <datalist id={id('placeholders')}>
          {#each placeholderList as p (p)}<option value={p}></option>{/each}
        </datalist>
        {#if !opensAPage(e)}
          <p class="mt-1 text-xs text-amber-900 dark:text-amber-200">
            A phone that scans this sees {e.content_binding ? 'its text' : 'the code'}, not a page.
            <button type="button" class="underline" onclick={() => set('content_binding', SCAN_URL)}>Use the scan link</button>
          </p>
        {/if}
      </div>
    {/if}

    {#if e.type === 'text'}
      <div class={row}>
        <NumberField id={id('size')} label="Size" value={e.font_size} min={1} step={0.5} unit="pt" onchange={(n) => set('font_size', n)} />
        <NumberField id={id('min')} label="Least" title="The smallest it shrinks to, to fit" value={e.min_font_size} min={1} step={0.5} unit="pt" onchange={(n) => set('min_font_size', n)} />
        <label class="flex items-center gap-1 text-xs" for={id('family')}>
          <span class="text-zinc-600 dark:text-zinc-400">Font</span>
          <select id={id('family')} class="input-sm" value={e.font_family ?? 'sans'} onchange={(ev) => set('font_family', ev.currentTarget.value as 'sans' | 'mono')}>
            <option value="sans">Sans</option>
            <option value="mono">Monospace</option>
          </select>
        </label>
        <label class="flex items-center gap-1 text-xs" for={id('align')}>
          <span class="text-zinc-600 dark:text-zinc-400">Align</span>
          <select id={id('align')} class="input-sm" value={e.align ?? 'left'} onchange={(ev) => set('align', ev.currentTarget.value as 'left' | 'center' | 'right')}>
            <option value="left">Left</option>
            <option value="center">Center</option>
            <option value="right">Right</option>
          </select>
        </label>
        <label class="flex items-center gap-1 text-xs" for={id('color')}>
          <span class="text-zinc-600 dark:text-zinc-400">Color</span>
          <select id={id('color')} class="input-sm" value={e.fill ?? 'black'} onchange={(ev) => update(withProp(e, 'fill', ev.currentTarget.value === 'white' ? 'white' : undefined))}>
            <option value="black">Black</option>
            <option value="white">White</option>
          </select>
        </label>
        <div class="flex items-center gap-3 text-xs">
          <label class="flex items-center gap-1"><input type="checkbox" checked={e.font_weight === 'bold'} onchange={(ev) => set('font_weight', ev.currentTarget.checked ? 'bold' : 'normal')} /> Bold</label>
          <label class="flex items-center gap-1"><input type="checkbox" checked={e.wrap ?? false} onchange={(ev) => set('wrap', ev.currentTarget.checked)} /> Wrap</label>
        </div>
      </div>
    {:else if e.type === 'generator'}
      <div class="space-y-2">
        <label class="flex items-center gap-1 text-xs" for={id('gen')}>
          <span class="text-zinc-600 dark:text-zinc-400">Code type</span>
          <select id={id('gen')} class="input-sm" value={e.generator_type} onchange={(ev) => setGeneratorType(ev.currentTarget.value)}>
            {#each generators as g (g.type)}<option value={g.type}>{g.name}</option>{/each}
          </select>
        </label>
        <fieldset class="space-y-1">
          <legend class="flex w-full items-center justify-between text-xs text-zinc-600 dark:text-zinc-400">
            Margin
            <select class="input-sm w-auto" aria-label="Margin counted in" value={e.margin_unit ?? 'modules'} onchange={(ev) => setMarginUnit(ev.currentTarget.value as 'modules' | 'length')}>
              <option value="modules">Modules</option>
              <option value="length">{doc.units}</option>
            </select>
          </legend>
          <div class="grid grid-cols-4 gap-1">
            {#each sides as side (side)}
              <label class="text-[0.7rem] text-zinc-600 dark:text-zinc-400" for={id(`margin-${side}`)}>
                {sideNames[side]}
                <input
                  id={id(`margin-${side}`)}
                  class="input-sm"
                  type="number"
                  min="0"
                  step="any"
                  placeholder="Std"
                  title="Empty is the code's standard quiet zone"
                  value={marginOf(e, side) ?? ''}
                  onchange={(ev) => setMargin([side], ev.currentTarget.value)}
                />
              </label>
            {/each}
          </div>
          {#if layout?.narrow?.length}
            <p class="text-xs text-amber-900 dark:text-amber-200" role="status">Less than the code's standard quiet zone on its {layout.narrow.join(', ')}: some scanners need it.</p>
          {/if}
        </fieldset>
        <div class={row}>
          <label class="flex items-center gap-1 text-xs" for={id('code-align')}>
            <span class="text-zinc-600 dark:text-zinc-400">Across</span>
            <select id={id('code-align')} class="input-sm" value={e.align ?? 'center'} onchange={(ev) => update(withProp(e, 'align', ev.currentTarget.value === 'center' ? undefined : (ev.currentTarget.value as 'left' | 'right')))}>
              <option value="left">Left</option>
              <option value="center">Center</option>
              <option value="right">Right</option>
            </select>
          </label>
          <label class="flex items-center gap-1 text-xs" for={id('code-valign')}>
            <span class="text-zinc-600 dark:text-zinc-400">Down</span>
            <select
              id={id('code-valign')}
              class="input-sm"
              value={e.vertical_align ?? 'middle'}
              onchange={(ev) => update(withProp(e, 'vertical_align', ev.currentTarget.value === 'middle' ? undefined : (ev.currentTarget.value as 'top' | 'bottom')))}
            >
              <option value="top">Top</option>
              <option value="middle">Middle</option>
              <option value="bottom">Bottom</option>
            </select>
          </label>
          {#if generator?.error_correction_levels.length}
            <label class="flex items-center gap-1 text-xs" for={id('ecc')}>
              <span class="text-zinc-600 dark:text-zinc-400">Error correction</span>
              <select id={id('ecc')} class="input-sm" value={e.error_correction ?? ''} onchange={(ev) => set('error_correction', ev.currentTarget.value || undefined)}>
                <option value="">Default ({generator.error_correction_levels[0]})</option>
                {#each generator.error_correction_levels as l (l)}<option value={l}>{l}</option>{/each}
              </select>
            </label>
          {/if}
          <NumberField
            id={id('module')}
            label="Least module"
            title="The smallest module, in millimeters, for the scanners in use; 0.3 mm suits most phones"
            value={e.min_module_mm}
            min={0}
            max={5}
            step={0.05}
            unit="mm"
            onchange={(n) => update(withProp(e, 'min_module_mm', n > 0 ? n : undefined))}
          />
        </div>
      </div>
    {:else if e.type === 'shape'}
      {@const fillable = e.shape !== 'line' && !(e.shape === 'polygon' && e.closed === false)}
      <div class={row}>
        <label class="flex items-center gap-1 text-xs" for={id('stroke')}>
          <span class="text-zinc-600 dark:text-zinc-400">Stroke</span>
          <select id={id('stroke')} class="input-sm" value={e.stroke ?? 'black'} onchange={(ev) => setStroke(ev.currentTarget.value as Paint)}>
            {#each paints as [value, label] (value)}<option {value}>{label}</option>{/each}
          </select>
        </label>
        {#if e.stroke !== 'none'}
          <NumberField id={id('stroke-width')} label="Width" title="Stroke width" value={e.stroke_width} min={0} unit={doc.units} onchange={(n) => update(withProp(e, 'stroke_width', n || undefined))} />
        {/if}
        {#if fillable}
          <label class="flex items-center gap-1 text-xs" for={id('fill')}>
            <span class="text-zinc-600 dark:text-zinc-400">Fill</span>
            <select id={id('fill')} class="input-sm" value={e.fill ?? 'none'} onchange={(ev) => update(withProp(e, 'fill', ev.currentTarget.value as Paint))}>
              {#each paints as [value, label] (value)}<option {value}>{label}</option>{/each}
            </select>
          </label>
        {/if}
        {#if e.shape === 'rectangle'}
          <NumberField id={id('radius')} label="Corners" title="Corner radius" value={e.corner_radius ?? 0} min={0} unit={doc.units} onchange={(n) => update(withProp(e, 'corner_radius', n || undefined))} />
        {:else if e.shape === 'polygon'}
          <label class="flex items-center gap-1 text-xs">
            <input
              type="checkbox"
              checked={e.closed !== false}
              onchange={(ev) => {
                // An open polygon has no inside to fill.
                update(ev.currentTarget.checked ? withProp(e, 'closed', undefined) : { ...withProp(e, 'closed', false), fill: 'none' });
              }}
            /> Closed
          </label>
        {/if}
      </div>
      {#if e.stroke === 'none' && (e.fill ?? 'none') === 'none'}
        <p class="error-text text-xs" role="alert">With no stroke and no fill, it draws nothing.</p>
      {:else if hairline(e, doc)}
        <p class="text-xs text-amber-900 dark:text-amber-200" role="status">Its stroke is thinner than one printer dot at {doc.dpi} DPI, so it prints one dot wide.</p>
      {/if}
    {:else}
      <ImagePicker value={e.image_id ?? ''} onpick={(imageId) => set('image_id', imageId)} />
    {/if}

    {#if doc.sizing === 'adaptive'}
      <AdaptiveElementPanel {doc} {e} {index} {generators} onupdate={update} />
    {/if}
  </div>
{:else if editor.selected.length > 1}
  <div class="space-y-2">
    <p class="font-medium">{grouped ? `Group ${doc.elements[editor.selected[0]!]!.group}` : `${editor.selected.length} elements`}</p>
    <div class="flex flex-wrap gap-1">
      {#if grouped}
        <button type="button" class="btn-sm" onclick={() => editor.ungroup()}>Ungroup</button>
      {:else}
        <button type="button" class="btn-sm" onclick={() => editor.group()}>Group</button>
      {/if}
      <button type="button" class="icon-btn" aria-label="Turn them 90° left" title="Turn them 90° left" onclick={() => editor.turn(-90)}><Icon name="turnLeft" size={16} /></button>
      <button type="button" class="icon-btn" aria-label="Turn them 90° right" title="Turn them 90° right" onclick={() => editor.turn(90)}><Icon name="turnRight" size={16} /></button>
      <button type="button" class="icon-btn" aria-label="Flip them across" title="Flip them across" onclick={() => editor.flip(true)}><Icon name="flipAcross" size={16} /></button>
      <button type="button" class="icon-btn" aria-label="Flip them down" title="Flip them down" onclick={() => editor.flip(false)}><Icon name="flipDown" size={16} /></button>
      <button type="button" class="icon-btn" aria-label="Remove them" title="Remove them" onclick={() => editor.remove()}><Icon name="trash" size={16} /></button>
    </div>
  </div>
{:else}
  <p class="text-xs text-zinc-600 dark:text-zinc-400">Nothing selected. Choose a tool on the left to add to the label, or click what is on it.</p>
{/if}

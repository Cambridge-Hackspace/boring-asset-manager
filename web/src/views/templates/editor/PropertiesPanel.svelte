<script lang="ts">
  import type { GeneratorInfo } from '../../../lib/models';
  import {
    defaultStroke,
    describeElement,
    hairline,
    paddingOf,
    opensAPage,
    SCAN_URL,
    sides,
    withPadding,
    withProp,
    type CodeLayout,
    type Paint,
    type Side,
    type TemplateElement,
    mirrorOf,
    mirrorSides,
  } from '../../../lib/template';
  import AdaptiveElementPanel from '../AdaptiveElementPanel.svelte';
  import ImagePicker from '../ImagePicker.svelte';
  import { useEditor } from './editor.svelte';
  import Icon from './Icon.svelte';
  import NumberField from './NumberField.svelte';
  import { parseLength } from './transform';

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

  /** Mirrored in its box across, or down, or not. */
  function setMirror(across: boolean, down: boolean) {
    if (e) update(withProp(e, 'mirror', mirrorOf(across, down)));
  }
  const mirrored = $derived(mirrorSides(e?.mirror));

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

  /** Padding typed, with its unit or in the template's; empty is the standard. */
  function setPadding(on: Side[], raw: string) {
    if (!e) return;
    if (raw.trim() === '') {
      update(withPadding(e, on, undefined, layout));
      return;
    }
    const n = parseLength(raw, doc.units);
    if (n === null || n < 0) return;
    update(withPadding(e, on, Math.round(n * 1e4) / 1e4, layout));
  }
  // Each side on its own once they differ, or when asked for.
  let separate = $state(false);
  const eachSide = $derived.by(() => {
    if (separate || !e) return separate;
    const values = sides.filter((s) => e.generator_type !== 'code128' || s === 'left' || s === 'right').map((s) => paddingOf(e, s, layout));
    return values.some((v) => v !== values[0]);
  });
  const fmt = (v: number) => String(Math.round(v * (doc.units === 'in' ? 1000 : 100)) / (doc.units === 'in' ? 1000 : 100));
  const standardText = (side: Side) => (layout?.standard ? `${fmt(layout.standard[side])} standard` : 'Standard');
  /** What was left over beyond the padding, where the box is not a whole number of modules, said in words. */
  const leftover = $derived.by(() => {
    if (!layout?.padding) return '';
    const extra = sides.map((s) => layout.margin[s] - layout.padding![s]);
    const across = extra[1]! + extra[3]!;
    const down = extra[0]! + extra[2]!;
    const tiny = doc.units === 'in' ? 0.0005 : 0.01;
    if (across < tiny && down < tiny) return '';
    const parts = [across >= tiny ? `${fmt(across)} ${doc.units} across` : '', down >= tiny ? `${fmt(down)} ${doc.units} down` : ''].filter(Boolean);
    return `${parts.join(' and ')} left over: the code's squares are whole printer dots, so it does not fill the box exactly. Placed as Across and Down say.`;
  });

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
      <div role="alert">
        <ul class="space-y-1">
          {#each problemsFor(index) as p (p)}<li class="error-text text-xs">{p}</li>{/each}
        </ul>
      </div>
    {/if}

    <!-- Any element mirrors in its box, as Krita and Inkscape flip a selection. -->
    <div class="flex items-center gap-1" role="group" aria-label="Mirrored">
      <span class="mr-auto text-xs text-zinc-600 dark:text-zinc-400">Mirrored</span>
      <button type="button" class="btn-sm" aria-pressed={mirrored.across} title="Mirror it in its box, left for right" onclick={() => setMirror(!mirrored.across, mirrored.down)}>Left for right</button>
      <button type="button" class="btn-sm" aria-pressed={mirrored.down} title="Mirror it in its box, top for bottom" onclick={() => setMirror(mirrored.across, !mirrored.down)}>Top for bottom</button>
    </div>
    {#if e.type === 'generator' && e.mirror}
      <p class="text-xs text-amber-900 dark:text-amber-200" data-mirrored-code>Mirrored, it does not scan from the side the label is read from.</p>
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
        <label class="flex items-center gap-1 text-xs" for={id('fit')}>
          <span class="text-zinc-600 dark:text-zinc-400">Fit</span>
          <select
            id={id('fit')}
            class="input-sm"
            value={e.fit ?? 'shrink'}
            onchange={(ev) => update(ev.currentTarget.value === 'fill' ? withProp(e, 'fit', 'fill') : withProp(withProp(e, 'fit', undefined), 'max_font_size', undefined))}
          >
            <option value="shrink">Shrink to fit</option>
            <option value="fill">Fill the box</option>
          </select>
        </label>
        {#if e.fit === 'fill'}
          <NumberField id={id('max')} label="Most" title="The largest it grows to, filling its box" value={e.max_font_size ?? 200} min={1} max={200} step={0.5} unit="pt" onchange={(n) => update(withProp(e, 'max_font_size', n >= 200 ? undefined : n))} />
        {/if}
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
        <!-- Padding, as Figma's and the CSS box model's: the space kept clear inside the code's box. -->
        <fieldset class="space-y-1">
          <legend class="flex w-full items-center justify-between text-xs text-zinc-600 dark:text-zinc-400">
            Padding
            <span class="flex gap-1">
              <button
                type="button"
                class="btn-sm"
                aria-pressed={eachSide}
                title="Set each side on its own"
                onclick={() => (separate = !eachSide)}>Each side</button
              >
              <button type="button" class="btn-sm" disabled={!e.margin && e.quiet_zone === undefined} title="The space the code's standard asks for, each side" onclick={() => update(withPadding(e, [...sides], undefined))}>Standard</button>
            </span>
          </legend>
          {#if eachSide}
            <div class="grid grid-cols-4 gap-1">
              {#each sides as side (side)}
                <label class="text-[0.7rem] text-zinc-600 dark:text-zinc-400" for={id(`padding-${side}`)}>
                  {sideNames[side]}
                  <input
                    id={id(`padding-${side}`)}
                    class="input-sm"
                    inputmode="decimal"
                    placeholder={standardText(side)}
                    title="In {doc.units}; empty is the code's standard quiet zone"
                    value={paddingOf(e, side, layout) ?? ''}
                    onchange={(ev) => setPadding([side], ev.currentTarget.value)}
                  />
                </label>
              {/each}
            </div>
          {:else}
            <label class="flex items-center gap-1 text-xs" for={id('padding')}>
              <span class="text-zinc-600 dark:text-zinc-400">All sides</span>
              <input
                id={id('padding')}
                class="input-sm"
                inputmode="decimal"
                placeholder={standardText('left')}
                title="In {doc.units}; empty is the code's standard quiet zone"
                value={paddingOf(e, 'left', layout) ?? ''}
                onchange={(ev) => setPadding(e.generator_type === 'code128' && !e.margin ? ['left', 'right'] : [...sides], ev.currentTarget.value)}
              />
              <span class="text-zinc-600 dark:text-zinc-400">{doc.units}</span>
            </label>
          {/if}
          {#if leftover}
            <p class="text-xs text-zinc-600 dark:text-zinc-400" data-leftover>{leftover}</p>
          {/if}
          {#if layout?.narrow?.length}
            {@const n = layout.narrow}
            <p class="text-xs text-amber-900 dark:text-amber-200" role="status">
              Less than the code's standard quiet zone on its {n.length > 1 ? `${n.slice(0, -1).join(', ')} and ${n[n.length - 1]}` : n[0]}: some scanners need it.
            </p>
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

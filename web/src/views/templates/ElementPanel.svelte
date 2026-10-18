<script lang="ts">
  import type { GeneratorInfo } from '../../lib/models';
  import { describeElement, withProp, type TemplateDoc, type TemplateElement } from '../../lib/template';
  import ImagePicker from './ImagePicker.svelte';
  import AdaptiveElementPanel from './AdaptiveElementPanel.svelte';

  // Edits one element. Each change goes through onupdate with the new
  // element, and is one undoable step.
  let {
    doc,
    index,
    generators,
    placeholderList,
    problems,
    onupdate,
    onremove,
    onorder,
  }: {
    doc: TemplateDoc;
    index: number;
    generators: GeneratorInfo[];
    placeholderList: string[];
    problems: string[];
    onupdate: (e: TemplateElement) => void;
    onremove: () => void;
    onorder: (delta: number) => void;
  } = $props();

  const e = $derived(doc.elements[index]!);
  const generator = $derived(generators.find((g) => g.type === e.generator_type));
  const id = (name: string) => `el-${index}-${name}`;

  function set<K extends keyof TemplateElement>(key: K, value: TemplateElement[K]) {
    onupdate({ ...e, [key]: value });
  }

  function setNumber(key: 'x' | 'y' | 'width' | 'height' | 'font_size' | 'min_font_size' | 'quiet_zone', raw: string) {
    const n = Number(raw);
    if (raw === '' && key === 'quiet_zone') {
      const { quiet_zone: _q, ...rest } = e;
      onupdate(rest);
      return;
    }
    if (Number.isFinite(n)) set(key, n);
  }

  function setGeneratorType(type: string) {
    const levels = generators.find((g) => g.type === type)?.error_correction_levels ?? [];
    const next: TemplateElement = { ...e, generator_type: type };
    if (!levels.includes(next.error_correction ?? '')) delete next.error_correction;
    onupdate(next);
  }
</script>

<section class="card space-y-4" aria-labelledby="element-heading">
  <div class="flex flex-wrap items-center justify-between gap-2">
    <h2 id="element-heading" class="text-lg font-semibold">{describeElement(e, index)}</h2>
    <div class="flex flex-wrap gap-1">
      <button type="button" class="btn-secondary" onclick={() => onorder(-1)} disabled={index === 0}>Send backward</button>
      <button type="button" class="btn-secondary" onclick={() => onorder(1)} disabled={index === doc.elements.length - 1}>Bring forward</button>
      <button type="button" class="btn-secondary text-red-700 dark:text-red-400" onclick={onremove}>Remove</button>
    </div>
  </div>

  {#if problems.length}
    <ul class="space-y-1" role="alert">
      {#each problems as p (p)}<li class="error-text">{p}</li>{/each}
    </ul>
  {/if}

  <fieldset class="grid grid-cols-2 gap-3 sm:grid-cols-4">
    <legend class="label">Position and size ({doc.units})</legend>
    {#each [['x', 'Left'], ['y', 'Top'], ['width', 'Width'], ['height', 'Height']] as const as [key, label] (key)}
      <div>
        <label class="text-xs" for={id(key)}>{label}</label>
        <input id={id(key)} class="input" type="number" step="any" min="0" value={e[key]} onchange={(ev) => setNumber(key, ev.currentTarget.value)} />
      </div>
    {/each}
  </fieldset>

  {#if e.type === 'text' || e.type === 'generator'}
    <div>
      <label class="label" for={id('binding')}>Content</label>
      <input
        id={id('binding')}
        class="input font-mono"
        list={id('placeholders')}
        value={e.content_binding ?? ''}
        placeholder={e.type === 'generator' ? `{{asset.code.${e.generator_type}}}` : ''}
        aria-describedby={id('binding-help')}
        onchange={(ev) => set('content_binding', ev.currentTarget.value)}
      />
      <datalist id={id('placeholders')}>
        {#each placeholderList as p (p)}<option value={p}></option>{/each}
      </datalist>
      <p id={id('binding-help')} class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">
        Text with placeholders such as {'{{asset.name}}'} or {'{{field.serial}}'}.{#if e.type === 'generator'}{' '}Empty encodes the asset’s {e.generator_type} code.{/if}
      </p>
    </div>
  {/if}

  {#if e.type === 'text'}
    <div class="grid grid-cols-2 gap-3 sm:grid-cols-3">
      <div>
        <label class="label" for={id('size')}>Size (pt)</label>
        <input id={id('size')} class="input" type="number" min="1" step="0.5" value={e.font_size} onchange={(ev) => setNumber('font_size', ev.currentTarget.value)} />
      </div>
      <div>
        <label class="label" for={id('min')}>Shrink to (pt)</label>
        <input id={id('min')} class="input" type="number" min="1" step="0.5" value={e.min_font_size ?? ''} onchange={(ev) => setNumber('min_font_size', ev.currentTarget.value)} />
      </div>
      <div>
        <label class="label" for={id('weight')}>Weight</label>
        <select id={id('weight')} class="input" value={e.font_weight ?? 'normal'} onchange={(ev) => set('font_weight', ev.currentTarget.value as 'normal' | 'bold')}>
          <option value="normal">Normal</option>
          <option value="bold">Bold</option>
        </select>
      </div>
      <div>
        <label class="label" for={id('family')}>Font</label>
        <select id={id('family')} class="input" value={e.font_family ?? 'sans'} onchange={(ev) => set('font_family', ev.currentTarget.value as 'sans' | 'mono')}>
          <option value="sans">Sans</option>
          <option value="mono">Monospace</option>
        </select>
      </div>
      <div>
        <label class="label" for={id('align')}>Align</label>
        <select id={id('align')} class="input" value={e.align ?? 'left'} onchange={(ev) => set('align', ev.currentTarget.value as 'left' | 'center' | 'right')}>
          <option value="left">Left</option>
          <option value="center">Center</option>
          <option value="right">Right</option>
        </select>
      </div>
      <label class="flex items-center gap-2 self-end pb-2 text-sm">
        <input type="checkbox" checked={e.wrap ?? false} onchange={(ev) => set('wrap', ev.currentTarget.checked)} /> Wrap lines
      </label>
    </div>
  {:else if e.type === 'generator'}
    <div class="grid grid-cols-2 gap-3 sm:grid-cols-3">
      <div>
        <label class="label" for={id('gen')}>Symbology</label>
        <select id={id('gen')} class="input" value={e.generator_type} onchange={(ev) => setGeneratorType(ev.currentTarget.value)}>
          {#each generators as g (g.type)}<option value={g.type}>{g.name}</option>{/each}
        </select>
      </div>
      {#if generator?.error_correction_levels.length}
        <div>
          <label class="label" for={id('ecc')}>Error correction</label>
          <select id={id('ecc')} class="input" value={e.error_correction ?? ''} onchange={(ev) => set('error_correction', ev.currentTarget.value || undefined)}>
            <option value="">Default ({generator.error_correction_levels[0]})</option>
            {#each generator.error_correction_levels as l (l)}<option value={l}>{l}</option>{/each}
          </select>
        </div>
      {/if}
      <div>
        <label class="label" for={id('quiet')}>Quiet zone (modules)</label>
        <input id={id('quiet')} class="input" type="number" min="0" step="1" placeholder="Standard" value={e.quiet_zone ?? ''} onchange={(ev) => setNumber('quiet_zone', ev.currentTarget.value)} />
      </div>
      <div>
        <label class="label" for={id('module')}>Smallest module (mm)</label>
        <input
          id={id('module')}
          class="input"
          type="number"
          min="0"
          max="5"
          step="0.05"
          placeholder="Two dots"
          aria-describedby={id('module-help')}
          value={e.min_module_mm ?? ''}
          onchange={(ev) => {
            const n = Number(ev.currentTarget.value);
            onupdate(withProp(e, 'min_module_mm', ev.currentTarget.value === '' || !(n > 0) ? undefined : n));
          }}
        />
        <p id={id('module-help')} class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">For the scanners in use; 0.3 mm suits most phones.</p>
      </div>
    </div>
  {:else}
    <ImagePicker value={e.image_id ?? ''} onpick={(imageId) => set('image_id', imageId)} />
  {/if}

  {#if doc.sizing === 'adaptive'}
    <AdaptiveElementPanel {doc} {e} {index} {generators} {onupdate} />
  {/if}
</section>

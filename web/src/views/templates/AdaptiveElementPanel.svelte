<script lang="ts">
  import type { GeneratorInfo } from '../../lib/models';
  import { tooSmall, withProp, type Anchor, type TemplateDoc, type TemplateElement } from '../../lib/template';

  // How an element of an adaptive template follows the label, its limits,
  // and what it does where it does not fit (docs/labels.md).
  let {
    doc,
    e,
    index,
    generators,
    onupdate,
  }: { doc: TemplateDoc; e: TemplateElement; index: number; generators: GeneratorInfo[]; onupdate: (e: TemplateElement) => void } = $props();

  const id = (name: string) => `el-${index}-adapt-${name}`;
  // Across, start and end are the left and right; down, the top and bottom.
  const anchorsAcross: [Anchor, string][] = [
    ['scale', 'In proportion'],
    ['start', 'Kept to the left'],
    ['end', 'Kept to the right'],
    ['both', 'Stretched from left to right'],
    ['center', 'Kept in the middle'],
  ];
  const anchorsDown: [Anchor, string][] = [
    ['scale', 'In proportion'],
    ['start', 'Kept to the top'],
    ['end', 'Kept to the bottom'],
    ['both', 'Stretched from top to bottom'],
    ['center', 'Kept in the middle'],
  ];
  const when = $derived(tooSmall(e));

  function setAnchor(key: 'anchor_x' | 'anchor_y', value: string) {
    onupdate(withProp(e, key, value === 'scale' ? undefined : (value as Anchor)));
  }

  function setSize(key: 'min_size' | 'max_size', raw: string) {
    const n = Number(raw);
    onupdate(withProp(e, key, raw === '' || !Number.isFinite(n) || n <= 0 ? undefined : n));
  }

  function setWhen(value: string) {
    let next = withProp(e, 'when_too_small', value as TemplateElement['when_too_small']);
    if (value === 'alternative' && !next.alternative) {
      next = withProp(next, 'alternative', { type: 'text', x: 0, y: 0, width: 0, height: 0, content_binding: '{{asset.name}}', font_size: 8 });
    }
    if (value !== 'alternative') next = withProp(next, 'alternative', undefined);
    onupdate(next);
  }

  function setAlternative(alt: TemplateElement) {
    onupdate(withProp(e, 'alternative', alt));
  }

  function setAlternativeType(type: string) {
    setAlternative(
      type === 'generator'
        ? { type: 'generator', x: 0, y: 0, width: 0, height: 0, generator_type: generators.find((g) => g.type === 'micro_qr')?.type ?? generators[0]?.type ?? 'qr' }
        : { type: 'text', x: 0, y: 0, width: 0, height: 0, content_binding: '{{asset.name}}', font_size: 8 },
    );
  }
</script>

<fieldset class="space-y-3 border-t border-zinc-200 pt-3 dark:border-zinc-800">
  <legend class="label">On other label sizes</legend>
  <div class="grid grid-cols-2 gap-3">
    <div>
      <label class="text-xs" for={id('x')}>Across</label>
      <select id={id('x')} class="input" value={e.anchor_x ?? 'scale'} onchange={(ev) => setAnchor('anchor_x', ev.currentTarget.value)}>
        {#each anchorsAcross as [value, label] (value)}<option {value}>{label}</option>{/each}
      </select>
    </div>
    <div>
      <label class="text-xs" for={id('y')}>Down</label>
      <select id={id('y')} class="input" value={e.anchor_y ?? 'scale'} onchange={(ev) => setAnchor('anchor_y', ev.currentTarget.value)}>
        {#each anchorsDown as [value, label] (value)}<option {value}>{label}</option>{/each}
      </select>
    </div>
    <div>
      <label class="text-xs" for={id('min')}>Smallest ({doc.units})</label>
      <input id={id('min')} class="input" type="number" min="0" step="any" placeholder="Any" value={e.min_size ?? ''} onchange={(ev) => setSize('min_size', ev.currentTarget.value)} />
    </div>
    <div>
      <label class="text-xs" for={id('max')}>Largest ({doc.units})</label>
      <input id={id('max')} class="input" type="number" min="0" step="any" placeholder="Any" value={e.max_size ?? ''} onchange={(ev) => setSize('max_size', ev.currentTarget.value)} />
    </div>
  </div>
  <div>
    <label class="text-xs" for={id('when')}>Where it does not fit</label>
    <select id={id('when')} class="input" value={when} onchange={(ev) => setWhen(ev.currentTarget.value)}>
      <option value="refuse">The label is refused</option>
      <option value="hide">It is left off</option>
      <option value="alternative">Something else takes its place</option>
    </select>
  </div>
  {#if when === 'alternative' && e.alternative}
    {@const alt = e.alternative}
    <div class="grid grid-cols-2 gap-3">
      <div>
        <label class="text-xs" for={id('alt-type')}>In its place</label>
        <select id={id('alt-type')} class="input" value={alt.type} onchange={(ev) => setAlternativeType(ev.currentTarget.value)}>
          <option value="generator">A code</option>
          <option value="text">Text</option>
        </select>
      </div>
      {#if alt.type === 'generator'}
        <div>
          <label class="text-xs" for={id('alt-gen')}>Code type</label>
          <select id={id('alt-gen')} class="input" value={alt.generator_type} onchange={(ev) => setAlternative({ ...alt, generator_type: ev.currentTarget.value })}>
            {#each generators as g (g.type)}<option value={g.type}>{g.name}</option>{/each}
          </select>
        </div>
      {:else}
        <div>
          <label class="text-xs" for={id('alt-size')}>Size (pt)</label>
          <input id={id('alt-size')} class="input" type="number" min="1" step="0.5" value={alt.font_size} onchange={(ev) => setAlternative({ ...alt, font_size: Number(ev.currentTarget.value) })} />
        </div>
      {/if}
      <div class="col-span-2">
        <label class="text-xs" for={id('alt-content')}>Its content</label>
        <input
          id={id('alt-content')}
          class="input font-mono"
          value={alt.content_binding ?? ''}
          placeholder={alt.type === 'generator' ? `{{asset.code.${alt.generator_type}}}` : ''}
          onchange={(ev) => setAlternative(withProp(alt, 'content_binding', ev.currentTarget.value || undefined))}
        />
      </div>
    </div>
  {/if}
</fieldset>

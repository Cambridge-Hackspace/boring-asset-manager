<script lang="ts">
  import { marginOf, sides, withMargin, withProp, type CodeLayout, type Side, type TemplateDoc, type TemplateElement } from '../../lib/template';

  // A code's margin, the space kept clear around it, and where it sits in
  // its box. The margin is one value or one per side, in modules or the
  // template's units; empty is the code's standard quiet zone.
  let {
    doc,
    e,
    index,
    layout,
    onupdate,
  }: {
    doc: TemplateDoc;
    e: TemplateElement;
    index: number;
    /** How the server laid it out, when known. */
    layout?: CodeLayout;
    onupdate: (e: TemplateElement) => void;
  } = $props();

  const id = (name: string) => `el-${index}-${name}`;
  const names: Record<Side, string> = { top: 'Top side', right: 'Right side', bottom: 'Bottom side', left: 'Left side' };
  const values = $derived(sides.map((s) => marginOf(e, s)));
  // Each side on its own once they differ, or when asked for.
  let separate = $state(false);
  const eachSide = $derived(separate || values.some((v) => v !== values[0]));
  const unit = $derived(e.margin_unit === 'length' ? doc.units : 'modules');

  function setMargin(on: Side[], raw: string) {
    const n = Number(raw);
    if (raw !== '' && !(Number.isFinite(n) && n >= 0)) return;
    onupdate(withMargin(e, on, raw === '' ? undefined : n));
  }

  function setUnit(to: 'modules' | 'length') {
    // A margin given in one unit means nothing in the other: start again from the standard.
    const { margin: _m, quiet_zone: _q, ...rest } = e;
    onupdate(withProp(rest, 'margin_unit', to === 'modules' ? undefined : to));
  }

  const across = [
    ['left', 'Left'],
    ['center', 'Center'],
    ['right', 'Right'],
  ] as const;
  const down = [
    ['top', 'Top'],
    ['middle', 'Middle'],
    ['bottom', 'Bottom'],
  ] as const;
  const narrowText = (n: Side[]) => (n.length === 1 ? `its ${n[0]} side` : `its ${n.slice(0, -1).join(', ')} and ${n[n.length - 1]} sides`);
</script>

<fieldset class="space-y-2">
  <legend class="label">Margin</legend>
  <div class="flex flex-wrap items-end gap-3">
    {#if eachSide}
      {#each sides as side, k (side)}
        <div>
          <label class="text-xs" for={id(`margin-${side}`)}>{names[side]}</label>
          <input id={id(`margin-${side}`)} class="input w-24" type="number" min="0" step="any" placeholder="Standard" value={values[k] ?? ''} onchange={(ev) => setMargin([side], ev.currentTarget.value)} />
        </div>
      {/each}
    {:else}
      <div>
        <label class="text-xs" for={id('margin')}>Every side</label>
        <input id={id('margin')} class="input w-24" type="number" min="0" step="any" placeholder="Standard" value={values[0] ?? ''} onchange={(ev) => setMargin(sides, ev.currentTarget.value)} />
      </div>
    {/if}
    <div>
      <label class="text-xs" for={id('margin-unit')}>In</label>
      <select id={id('margin-unit')} class="input" value={e.margin_unit ?? 'modules'} onchange={(ev) => setUnit(ev.currentTarget.value as 'modules' | 'length')}>
        <option value="modules">modules</option>
        <option value="length">{doc.units}</option>
      </select>
    </div>
    <label class="flex items-center gap-2 pb-2 text-sm">
      <input type="checkbox" checked={eachSide} disabled={eachSide && !separate} onchange={(ev) => (separate = ev.currentTarget.checked)} /> Each side on its own
    </label>
  </div>
  <p class="text-xs text-zinc-600 dark:text-zinc-400">
    In {unit}{unit === 'modules' ? ', growing with the code' : ', whatever the code holds'}; empty is the code’s standard quiet zone. Margins are whole printer dots.
  </p>
  {#if layout?.narrow?.length}
    <p class="rounded-md border border-amber-300 bg-amber-50 px-2 py-1 text-sm dark:border-amber-800 dark:bg-amber-950" role="status">
      Less space than the code’s standard quiet zone on {narrowText(layout.narrow)}. Some scanners need it, though a code at the label’s edge often scans.
    </p>
  {/if}
</fieldset>

<div class="flex flex-wrap gap-4">
  <div role="group" aria-label="Where it sits across its box" class="space-y-1">
    <p class="label" aria-hidden="true">Across its box</p>
    <div class="flex gap-1">
      {#each across as [value, label] (value)}
        <button type="button" class="btn-secondary" aria-pressed={(e.align ?? 'center') === value} onclick={() => onupdate(withProp(e, 'align', value === 'center' ? undefined : value))}>{label}</button>
      {/each}
    </div>
  </div>
  <div role="group" aria-label="Where it sits down its box" class="space-y-1">
    <p class="label" aria-hidden="true">Down its box</p>
    <div class="flex gap-1">
      {#each down as [value, label] (value)}
        <button type="button" class="btn-secondary" aria-pressed={(e.vertical_align ?? 'middle') === value} onclick={() => onupdate(withProp(e, 'vertical_align', value === 'middle' ? undefined : value))}>{label}</button>
      {/each}
    </div>
  </div>
</div>

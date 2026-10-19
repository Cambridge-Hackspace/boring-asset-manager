<script lang="ts">
  import { gridStep, snap, type Guides, type TemplateDoc } from '../../lib/template';

  // The template's guides over the label: lines to line elements up
  // against, which never print. A guide drags along, and comes off when
  // dragged off the label; focused, the arrow keys move it a grid step (Shift
  // for ten) and Delete removes it.
  let {
    doc,
    onbegin,
    onchange,
    dragging,
  }: {
    doc: TemplateDoc;
    onbegin: () => void;
    onchange: (guides: Guides) => void;
    /** Starts dragging a guide, by axis and index. */
    dragging: (event: PointerEvent, axis: 'x' | 'y', index: number) => void;
  } = $props();

  const pct = (v: number, total: number) => `${(v / total) * 100}%`;
  const lines = $derived([
    ...(doc.guides?.x ?? []).map((at, i) => ({ axis: 'x' as const, at, i })),
    ...(doc.guides?.y ?? []).map((at, i) => ({ axis: 'y' as const, at, i })),
  ]);

  function keydown(event: KeyboardEvent, axis: 'x' | 'y', i: number) {
    const list = [...(doc.guides?.[axis] ?? [])];
    const step = gridStep(doc.units) * (event.shiftKey ? 10 : 1);
    const by = { ArrowLeft: -step, ArrowUp: -step, ArrowRight: step, ArrowDown: step }[event.key];
    if (by !== undefined) {
      event.preventDefault();
      event.stopPropagation();
      const limit = axis === 'x' ? doc.width : doc.height;
      onbegin();
      list[i] = Math.min(Math.max(0, snap(list[i]! + by, gridStep(doc.units), doc.units)), limit);
      onchange({ ...doc.guides, [axis]: list });
    } else if (event.key === 'Delete' || event.key === 'Backspace') {
      event.preventDefault();
      event.stopPropagation();
      onbegin();
      list.splice(i, 1);
      onchange({ ...doc.guides, [axis]: list });
    }
  }
</script>

{#each lines as g (`${g.axis}${g.i}`)}
  <span
    role="slider"
    tabindex="0"
    aria-orientation={g.axis === 'x' ? 'horizontal' : 'vertical'}
    aria-label="{g.axis === 'x' ? 'Vertical' : 'Horizontal'} guide"
    aria-valuemin={0}
    aria-valuemax={g.axis === 'x' ? doc.width : doc.height}
    aria-valuenow={g.at}
    aria-valuetext="{g.at} {doc.units} from the {g.axis === 'x' ? 'left' : 'top'}"
    class="guide absolute z-[5] {g.axis === 'x' ? 'top-0 bottom-0 cursor-ew-resize' : 'right-0 left-0 cursor-ns-resize'}"
    style={g.axis === 'x' ? `left:calc(${pct(g.at, doc.width)} - 4px)` : `top:calc(${pct(g.at, doc.height)} - 4px)`}
    onpointerdown={(ev) => dragging(ev, g.axis, g.i)}
    onkeydown={(ev) => keydown(ev, g.axis, g.i)}
  ></span>
{/each}

<style>
  /* A thin line, in a band wide enough to take. */
  .guide {
    --w: 9px;
    background: linear-gradient(rgb(14 165 233), rgb(14 165 233)) center / 1px 100% no-repeat;
  }
  .guide[aria-orientation='horizontal'] {
    width: var(--w);
  }
  .guide[aria-orientation='vertical'] {
    height: var(--w);
    background-size: 100% 1px;
  }
  .guide:focus-visible {
    outline: 2px solid rgb(14 165 233);
  }
</style>

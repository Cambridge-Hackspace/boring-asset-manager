<script lang="ts">
  import { turned } from './transform';

  // An angle set by turning a dial, as Krita's rotation dials: dragged, it
  // snaps as turning an element does, to 15° steps with Ctrl and to 45°
  // near one unless Shift is held or angle snapping is off; the arrow keys
  // turn it a degree, 15° with Shift. A drag is begun, then turns it as it
  // goes, one change in all; a key is a change.
  let {
    label,
    value,
    snapAngles,
    onbegin,
    oninput,
    onchange,
  }: {
    label: string;
    value: number;
    snapAngles: boolean;
    onbegin: () => void;
    oninput: (degrees: number) => void;
    onchange: (degrees: number) => void;
  } = $props();

  let dial = $state<HTMLDivElement | null>(null);

  function angleAt(event: PointerEvent): number {
    const r = dial!.getBoundingClientRect();
    // Clockwise from up, as the dial's pointer is drawn at 0.
    const degrees = (Math.atan2(event.clientY - (r.top + r.height / 2), event.clientX - (r.left + r.width / 2)) * 180) / Math.PI + 90;
    return turned(degrees, { steps: event.ctrlKey || event.metaKey, snap45: snapAngles && !event.shiftKey });
  }

  function keydown(event: KeyboardEvent) {
    const by = event.shiftKey ? 15 : 1;
    const step = { ArrowRight: by, ArrowUp: by, ArrowLeft: -by, ArrowDown: -by }[event.key];
    if (step === undefined) return;
    event.preventDefault();
    onchange(turned(value + step, {}));
  }
</script>

<div
  bind:this={dial}
  class="relative size-8 shrink-0 cursor-grab touch-none rounded-full border border-zinc-400 bg-white focus-visible:outline-2 focus-visible:outline-blue-600 dark:border-zinc-600 dark:bg-zinc-900"
  role="slider"
  tabindex="0"
  aria-label={label}
  aria-valuemin={0}
  aria-valuemax={359}
  aria-valuenow={value}
  aria-valuetext="{value}°"
  title="{label}: drag to turn; Ctrl for 15° steps"
  onpointerdown={(e) => {
    e.currentTarget.setPointerCapture(e.pointerId);
    onbegin();
    oninput(angleAt(e));
  }}
  onpointermove={(e) => {
    if (e.currentTarget.hasPointerCapture(e.pointerId)) oninput(angleAt(e));
  }}
  onkeydown={keydown}
>
  <span class="absolute top-0.5 left-1/2 h-3.5 w-0.5 origin-[50%_100%] -translate-x-1/2 bg-blue-600" style="transform: rotate({value}deg)"></span>
</div>

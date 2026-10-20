<script lang="ts">
  import type { Frame } from '../../../lib/live/client';
  import { useEditor } from './editor.svelte';

  // The whole label in small, as Krita's overview: the part in view is
  // outlined; drag in it to move the view there, and scroll on it to zoom.
  let { frame, previewURL }: { frame: Frame | null; previewURL: string } = $props();

  const editor = useEditor();
  const doc = $derived(editor.doc);
  let box = $state<HTMLDivElement | null>(null);
  let thumb = $state<HTMLCanvasElement | null>(null);
  let dragging = false;

  $effect(() => {
    const f = frame;
    if (!f || !thumb) return;
    thumb.getContext('2d')?.putImageData(new ImageData(new Uint8ClampedArray(f.pixels), f.width, f.height), 0, 0);
  });

  /** The part of the label in view, as fractions of it, kept on it. */
  const seen = $derived.by(() => {
    const { scale, x, y } = editor.view;
    const { w, h } = editor.viewport;
    const clip = (v: number) => Math.min(Math.max(v, 0), 1);
    const left = clip(-x / scale / doc.width);
    const top = clip(-y / scale / doc.height);
    const right = clip((w - x) / scale / doc.width);
    const bottom = clip((h - y) / scale / doc.height);
    return { left, top, width: right - left, height: bottom - top };
  });

  /** Centers the view on the point of the label under the pointer. */
  function moveTo(event: PointerEvent) {
    if (!box) return;
    const r = box.getBoundingClientRect();
    const ux = ((event.clientX - r.left) / r.width) * doc.width;
    const uy = ((event.clientY - r.top) / r.height) * doc.height;
    const { scale, x, y } = editor.view;
    editor.panBy(editor.viewport.w / 2 - ux * scale - x, editor.viewport.h / 2 - uy * scale - y);
  }

  $effect(() => {
    const b = box;
    if (!b) return;
    const wheel = (event: WheelEvent) => {
      event.preventDefault();
      editor.zoomBy(Math.exp(-event.deltaY * 0.002), editor.viewport.w / 2, editor.viewport.h / 2);
    };
    b.addEventListener('wheel', wheel, { passive: false });
    return () => b.removeEventListener('wheel', wheel);
  });
</script>

<div
  bind:this={box}
  role="presentation"
  class="relative w-full cursor-pointer touch-none overflow-hidden border border-zinc-300 bg-white dark:border-zinc-700"
  style="aspect-ratio: {doc.width} / {doc.height}"
  onpointerdown={(e) => {
    dragging = true;
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    moveTo(e);
  }}
  onpointermove={(e) => dragging && moveTo(e)}
  onpointerup={() => (dragging = false)}
>
  {#if frame}
    <canvas bind:this={thumb} class="pointer-events-none h-full w-full" width={frame.width} height={frame.height}></canvas>
  {:else if previewURL}
    <img src={previewURL} alt="" class="pointer-events-none h-full w-full" draggable="false" />
  {/if}
  <div
    class="pointer-events-none absolute border-2 border-blue-600 bg-blue-600/10"
    data-overview-view
    style="left:{seen.left * 100}%;top:{seen.top * 100}%;width:{seen.width * 100}%;height:{seen.height * 100}%"
  ></div>
</div>
<p class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">Drag to move the view; scroll to zoom.</p>

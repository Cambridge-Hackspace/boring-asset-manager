<!--
  A label icon as it prints (README §3.7): in black and white, at the size
  of an icon on 12 mm tape at 203 dots an inch, shaded and as an outline,
  drawn by the labels' own renderer in the browser (lib/live), from the
  icons as the editor checks them.
-->
<script lang="ts">
  import { onDestroy, onMount, untrack } from 'svelte';
  import { startLiveRenderer, type LiveRenderer, type LiveState } from '../../lib/live/client';
  import type { IrDocument } from '../../lib/isolex/ir';
  import type { IconColor } from '../../lib/isolex/palette';
  import { iconset } from '../../lib/iconset.svelte';
  import type { TemplateDoc } from '../../lib/template';

  interface Props {
    /** The icons to draw from: the editor's check, or the build. */
    doc: IrDocument;
    /** The icon to draw. */
    name: string;
    palette: IconColor[];
    /** What the category token prints as. */
    categoryColor: string;
  }
  let { doc, name, palette, categoryColor }: Props = $props();

  // 12 mm, 96 dots: about what an icon has on 12 mm tape
  const size = 12;
  const template = (icon: string): TemplateDoc => ({
    id: '',
    name: 'Label icon',
    units: 'mm',
    width: size * 2 + 2,
    height: size,
    dpi: 203,
    layering: 'list',
    elements: [
      { type: 'icon', x: 0, y: 0, width: size, height: size, icon } as TemplateDoc['elements'][number],
      { type: 'icon', x: size + 2, y: 0, width: size, height: size, icon, shading: 'outline' } as TemplateDoc['elements'][number],
    ],
  });

  let live: LiveRenderer | null = null;
  let liveState = $state<LiveState>('loading');
  let canvas = $state<HTMLCanvasElement | undefined>();
  let drawn = $state(false);

  onMount(() => {
    live = startLiveRenderer(() => Promise.reject(new Error('no images')));
    live.onstate = (s) => (liveState = s);
    liveState = live.state;
  });
  onDestroy(() => live?.close());

  $effect(() => {
    const json = JSON.stringify({
      icons: doc.icons,
      unknown: iconset.unknown,
      colors: Object.fromEntries(palette.map((c) => [c.token, c.light])),
    });
    const [icon, color] = [name, categoryColor];
    if (liveState !== 'ready' || !live) return;
    untrack(() => {
      live!.setIcons(json);
      void live!.render(template(icon), { data: { icon_color: color }, placeholders: false }).then((f) => {
        if (!f || !canvas) return;
        canvas.width = f.width;
        canvas.height = f.height;
        canvas.getContext('2d')?.putImageData(new ImageData(new Uint8ClampedArray(f.pixels), f.width, f.height), 0, 0);
        drawn = true;
      });
    });
  });
</script>

<div>
  <h3 class="mb-2 text-sm font-medium">As it prints</h3>
  {#if liveState === 'failed'}
    <p class="text-sm text-zinc-600 dark:text-zinc-400">The label preview cannot be drawn in this browser.</p>
  {:else}
    <canvas
      bind:this={canvas}
      class="rounded border border-zinc-300 bg-white [image-rendering:pixelated]"
      style="width: {(size * 2 + 2) * 8}px; height: {size * 8}px"
      role="img"
      aria-label="{name} as it prints in black and white on a 12 mm label: shaded, and as an outline"
    ></canvas>
    <p class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">
      {drawn ? 'Shaded, and as an outline, at 203 dots an inch, enlarged.' : 'Drawing…'}
    </p>
  {/if}
</div>

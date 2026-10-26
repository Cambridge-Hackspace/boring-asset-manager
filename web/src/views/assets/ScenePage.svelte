<!--
  /assets/:id/scene (README §3.7): what is in a location, each drawn with
  its icon where it stands on the floor, as the server lays them out.
  Wheel or the buttons zoom; dragging pans. Choosing one says what it is,
  with ways to open it and to look inside; a double click does the
  likelier. ?show= outlines one and mutes the rest: where it is.
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import { conditionLabel } from '../../lib/assets';
  import { formatNumber as n } from '../../lib/isolex/ir';
  import { iconset } from '../../lib/iconset.svelte';
  import { navigate, router } from '../../lib/router.svelte';
  import { describeItem, gridLines, hull, panBy, zoomAt, type Scene, type SceneItem, type ViewBox } from '../../lib/scene';
  import IsoShapes from '../../components/IsoShapes.svelte';
  import IconPaletteStyle from '../../components/IconPaletteStyle.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  let { params }: { params: Record<string, string> } = $props();
  const id = $derived(params.id ?? '');

  let scene = $state<Scene | null>(null);
  let error = $state('');
  let loading = $state(true);
  let view = $state<ViewBox>([0, 0, 1, 1]);
  let selected = $state<string | null>(null);
  const shown = $derived(router.query.get('show'));
  let svg = $state<SVGSVGElement | undefined>();

  $effect(() => {
    void iconset.load();
  });

  $effect(() => {
    const at = id;
    untrack(() => void load(at));
  });

  async function load(at: string) {
    loading = true;
    error = '';
    try {
      scene = await request<Scene>('GET', `/assets/${encodeURIComponent(at)}/scene`);
      view = [...scene.viewbox];
      selected = shown && scene.items.some((i) => i.id === shown) ? shown : null;
    } catch (err) {
      scene = null;
      error = messageOf(err);
    } finally {
      loading = false;
    }
  }

  const chosen = $derived(scene?.items.find((i) => i.id === selected) ?? null);
  const points = (pts: [number, number][]) => pts.map(([x, y]) => `${n(x)},${n(y)}`).join(' ');
  const floorOutline = $derived(scene ? hull({ x: 0, y: 0, z: 0, size: [scene.floor[0], scene.floor[1], 0] }) : []);

  function drawn(item: SceneItem) {
    if (item.shapes) return item.shapes;
    const icon = item.icon ? iconset.named(item.icon.name) : undefined;
    return icon?.shapes ?? [];
  }

  function open(item: SceneItem) {
    navigate(item.holds ? `/assets/${encodeURIComponent(item.id)}/scene` : `/assets/${encodeURIComponent(item.id)}`);
  }

  function zoom(factor: number, at?: [number, number]) {
    if (!scene) return;
    const [px, py] = at ?? [view[0] + view[2] / 2, view[1] + view[3] / 2];
    view = zoomAt(view, scene.viewbox, px, py, factor);
  }

  /** A point on the screen in the scene's coordinates. */
  function toScene(clientX: number, clientY: number): [number, number] {
    const m = svg?.getScreenCTM();
    if (!svg || !m) return [view[0], view[1]];
    const p = new DOMPoint(clientX, clientY).matrixTransform(m.inverse());
    return [p.x, p.y];
  }

  function onwheel(e: WheelEvent) {
    e.preventDefault();
    zoom(Math.exp(-e.deltaY * 0.0015), toScene(e.clientX, e.clientY));
  }

  // a drag pans; a press that does not move chooses what it is on
  let drag: { x: number; y: number; moved: boolean } | null = null;
  function onpointerdown(e: PointerEvent) {
    if (e.button !== 0) return;
    drag = { x: e.clientX, y: e.clientY, moved: false };
  }
  function onpointermove(e: PointerEvent) {
    if (!drag) return;
    if (!drag.moved) {
      if (Math.hypot(e.clientX - drag.x, e.clientY - drag.y) < 4) return;
      // panning from here on, wherever the pointer goes
      (e.currentTarget as Element).setPointerCapture(e.pointerId);
    }
    const [ax, ay] = toScene(drag.x, drag.y);
    const [bx, by] = toScene(e.clientX, e.clientY);
    view = panBy(view, ax - bx, ay - by);
    drag = { x: e.clientX, y: e.clientY, moved: true };
  }
  function onpointerup(e: PointerEvent) {
    const was = drag;
    drag = null;
    if (!was || was.moved) return;
    const hit = (e.target as Element).closest('[data-item]');
    selected = hit?.getAttribute('data-item') ?? null;
  }
  function ondblclick(e: MouseEvent) {
    const hit = (e.target as Element).closest('[data-item]');
    const item = scene?.items.find((i) => i.id === hit?.getAttribute('data-item'));
    if (item) open(item);
  }
  function onkey(e: KeyboardEvent, item: SceneItem) {
    if (e.key === 'Enter') {
      e.preventDefault();
      open(item);
    } else if (e.key === ' ') {
      e.preventDefault();
      selected = item.id;
    }
  }
</script>

<IconPaletteStyle palette={iconset.palette} />

<nav aria-label="Breadcrumb" class="mb-2 text-sm">
  <a class="underline" href="/assets">Assets</a>
  {#each scene?.path ?? [] as place (place.id)}
    {' › '}{#if place.id === scene?.location.id}<span aria-current="page">{place.name}</span>{:else}<a class="underline" href="/assets/{encodeURIComponent(place.id)}/scene">{place.name}</a>{/if}
  {/each}
</nav>
<ErrorMessage {error} />

{#if loading && !scene}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading…</p>
{:else if scene}
  <div class="mb-3 flex flex-wrap items-center justify-between gap-2">
    <h1 class="text-2xl font-bold">Inside {scene.location.name}</h1>
    <div class="flex flex-wrap items-center gap-2">
      <a class="btn-secondary" href="/assets/{encodeURIComponent(scene.location.id)}">Open {scene.location.name}</a>
      <div class="flex" role="group" aria-label="Zoom">
        <button type="button" class="btn-secondary rounded-r-none" aria-label="Zoom out" onclick={() => zoom(1 / 1.25)}>−</button>
        <button type="button" class="btn-secondary rounded-none border-x-0" onclick={() => scene && (view = [...scene.viewbox])}>Fit</button>
        <button type="button" class="btn-secondary rounded-l-none" aria-label="Zoom in" onclick={() => zoom(1.25)}>+</button>
      </div>
    </div>
  </div>

  {#if !scene.items.length}
    <p class="text-sm">Nothing is in {scene.location.name}.</p>
  {:else}
    <div class="grid gap-4 lg:grid-cols-[minmax(0,1fr)_18rem]">
      <!-- the drawing: each item is also a focusable button, for the keyboard -->
      <svg
        bind:this={svg}
        xmlns="http://www.w3.org/2000/svg"
        viewBox="{n(view[0])} {n(view[1])} {n(view[2])} {n(view[3])}"
        class="h-[60vh] w-full cursor-grab touch-none rounded border border-zinc-200 bg-white select-none active:cursor-grabbing dark:border-zinc-700 dark:bg-zinc-950"
        role="group"
        aria-label="{scene.location.name}, its contents drawn where they stand"
        {onwheel}
        {onpointerdown}
        {onpointermove}
        {onpointerup}
        {ondblclick}
      >
        <polygon points={points(floorOutline)} class="fill-zinc-100 dark:fill-zinc-900" />
        {#each gridLines(scene.floor[0], scene.floor[1]) as [x1, y1, x2, y2], k (k)}
          <line x1={n(x1)} y1={n(y1)} x2={n(x2)} y2={n(y2)} class="stroke-zinc-300 dark:stroke-zinc-700" stroke-width="0.6" />
        {/each}
        {#each scene.items as item (item.id)}
          {@const muted = !!shown && item.id !== shown}
          <g
            data-item={item.id}
            role="button"
            tabindex="0"
            aria-label={describeItem(item)}
            aria-pressed={selected === item.id}
            class="outline-none {muted ? 'opacity-30' : ''}"
            style={item.icon?.color ? `--iso-category:${item.icon.color}` : undefined}
            onkeydown={(e) => onkey(e, item)}
            onfocus={() => (selected = item.id)}
          >
            <title>{describeItem(item)}</title>
            <g transform="translate({n(item.tx)} {n(item.ty)})"><IsoShapes shapes={drawn(item)} /></g>
            <polygon points={points(hull(item))} fill="transparent" />
          </g>
        {/each}
        {#each scene.items.filter((i) => i.id === shown || i.id === selected) as item (item.id)}
          <polygon
            points={points(hull(item))}
            fill="none"
            stroke-width={item.id === shown ? 3 : 2}
            stroke-dasharray={item.id === shown ? undefined : '4 3'}
            class={item.id === shown ? 'stroke-amber-500' : 'stroke-zinc-500 dark:stroke-zinc-300'}
            pointer-events="none"
            data-outline={item.id}
          />
        {/each}
      </svg>

      <aside aria-label="Chosen" class="card text-sm">
        {#if chosen}
          <h2 class="text-base font-semibold">{chosen.name}</h2>
          <dl class="mt-2 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1">
            {#if chosen.code}<dt class="text-zinc-600 dark:text-zinc-400">Code</dt><dd class="font-mono">{chosen.code}</dd>{/if}
            <dt class="text-zinc-600 dark:text-zinc-400">Condition</dt><dd>{conditionLabel(chosen.condition)}</dd>
            {#if chosen.holds}<dt class="text-zinc-600 dark:text-zinc-400">Holds</dt><dd>{chosen.holds}</dd>{/if}
          </dl>
          <div class="mt-3 flex flex-wrap gap-2">
            <a class="btn-secondary" href="/assets/{encodeURIComponent(chosen.id)}">Open</a>
            {#if chosen.holds}<a class="btn-secondary" href="/assets/{encodeURIComponent(chosen.id)}/scene">Look inside</a>{/if}
          </div>
        {:else}
          <p class="text-zinc-600 dark:text-zinc-400">Choose something to see what it is. A double click opens it, or goes inside.</p>
        {/if}
      </aside>
    </div>
  {/if}
{/if}

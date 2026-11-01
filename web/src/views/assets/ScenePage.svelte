<!--
  /assets/:id/scene (docs/icons.md): what is in a location, each drawn with
  its icon where it stands on the floor, as the server lays them out.
  Wheel or the buttons zoom; dragging pans. Choosing one says what it is,
  with ways to open it and to look inside; a double click does the
  likelier. ?show= outlines one and mutes the rest: where it is.
  Arranging, for those who may change what is in it: dragging a thing
  moves it a tile at a time, the chosen one's handle lifts it, and the
  arrow keys and Page Up and Page Down do the same; each move is saved.
  What overlaps is outlined, and named when chosen. Save as SVG or PNG
  downloads it drawn by the server, as the page draws it.
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import { conditionLabel } from '../../lib/assets';
  import { formatNumber as n } from '../../lib/isolex/ir';
  import { iconset } from '../../lib/iconset.svelte';
  import { navigate, router } from '../../lib/router.svelte';
  import { describeItem, floorDelta, gridLines, hull, liftTo, LIFT_STEP, moveTo, overlapNote, panBy, position, project, S, sceneImage, zoomAt, type Scene, type SceneItem, type ViewBox } from '../../lib/scene';
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

  // arranging: where the thing being moved is, until the move is saved
  let arranging = $state(false);
  let moving = $state<{ id: string; x: number; y: number; z: number } | null>(null);
  const canArrange = $derived(!!scene?.items.some((i) => i.movable));
  const items = $derived(
    (scene?.items ?? []).map((i) => {
      if (moving?.id !== i.id) return i;
      const [tx, ty] = project(moving.x, moving.y, moving.z);
      return { ...i, x: moving.x, y: moving.y, z: moving.z, tx, ty };
    }),
  );
  const chosen = $derived(items.find((i) => i.id === selected) ?? null);
  // the chosen one's handle, to lift it: above the middle of its top
  const lift = $derived.by(() => {
    if (!arranging || !chosen?.movable) return null;
    const [x, y] = project(chosen.x + chosen.size[0] / 2, chosen.y + chosen.size[1] / 2, chosen.z + chosen.size[2]);
    return { x, y, top: y - 28 };
  });
  const points = (pts: [number, number][]) => pts.map(([x, y]) => `${n(x)},${n(y)}`).join(' ');
  const floorOutline = $derived(scene ? hull({ x: 0, y: 0, z: 0, size: [scene.floor[0], scene.floor[1], 0] }) : []);

  function drawn(item: SceneItem) {
    if (item.shapes) return item.shapes;
    const icon = iconset.forAsset(item.icon);
    return icon?.shapes ?? [];
  }

  /** Places an item, or with at null puts it back with the rest, and shows the scene as it is then. */
  async function place(id: string, at: { x: number; y: number; z: number } | null) {
    if (!scene) return;
    const path = `/assets/${encodeURIComponent(id)}/placement`;
    const location = scene.location.id;
    try {
      scene = at
        ? await request<Scene>('PUT', path, { in: location, ...at })
        : await request<Scene>('DELETE', `${path}?in=${encodeURIComponent(location)}`);
      error = '';
    } catch (err) {
      error = messageOf(err);
    }
    if (moving?.id === id && (!at || (moving.x === at.x && moving.y === at.y && moving.z === at.z))) moving = null;
  }

  // the keyboard's moves are saved once they stop
  let pending: ReturnType<typeof setTimeout> | undefined;
  function nudge(item: SceneItem, key: string): boolean {
    if (!scene) return false;
    const at = moving?.id === item.id ? moving : { id: item.id, x: item.x, y: item.y, z: item.z };
    const step: Record<string, [number, number]> = { ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1] };
    const by = step[key];
    let next = at;
    if (by) {
      const [x, y] = moveTo(item, [at.x, at.y], ...by, scene.floor);
      next = { ...at, x, y };
    } else if (key === 'PageUp' || key === 'PageDown') {
      next = { ...at, z: Math.max(0, at.z + (key === 'PageUp' ? LIFT_STEP : -LIFT_STEP)) };
    } else {
      return false;
    }
    moving = next;
    clearTimeout(pending);
    pending = setTimeout(() => void place(next.id, { x: next.x, y: next.y, z: next.z }), 400);
    return true;
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

  // a drag pans, or arranging, moves the thing it starts on or lifts by
  // the handle; a press that does not move chooses what it is on
  let drag: { x: number; y: number; moved: boolean; kind: 'pan' | 'move' | 'lift'; item?: SceneItem } | null = null;
  function onpointerdown(e: PointerEvent) {
    if (e.button !== 0) return;
    drag = { x: e.clientX, y: e.clientY, moved: false, kind: 'pan' };
    if (!arranging || !scene) return;
    const target = e.target as Element;
    const lifting = !!target.closest('[data-lift]');
    const id = lifting ? selected : target.closest('[data-item]')?.getAttribute('data-item');
    const item = scene.items.find((i) => i.id === id);
    if (item?.movable) drag = { ...drag, kind: lifting ? 'lift' : 'move', item };
  }
  function onpointermove(e: PointerEvent) {
    if (!drag || !scene) return;
    if (!drag.moved) {
      if (Math.hypot(e.clientX - drag.x, e.clientY - drag.y) < 4) return;
      // dragging from here on, wherever the pointer goes
      (e.currentTarget as Element).setPointerCapture(e.pointerId);
    }
    const [ax, ay] = toScene(drag.x, drag.y);
    const [bx, by] = toScene(e.clientX, e.clientY);
    const item = drag.item;
    if (drag.kind === 'pan' || !item) {
      view = panBy(view, ax - bx, ay - by);
      drag = { ...drag, x: e.clientX, y: e.clientY, moved: true };
      return;
    }
    // from where the drag started: the view stays put while it lasts
    if (drag.kind === 'move') {
      const [x, y] = moveTo(item, [item.x, item.y], ...floorDelta(bx - ax, by - ay), scene.floor);
      moving = { id: item.id, x, y, z: item.z };
    } else {
      moving = { id: item.id, x: item.x, y: item.y, z: liftTo(scene.items, item, item.z - (by - ay) / S) };
    }
    selected = item.id;
    drag = { ...drag, moved: true };
  }
  function onpointerup(e: PointerEvent) {
    const was = drag;
    drag = null;
    if (!was) return;
    if (was.moved) {
      const at = moving;
      if (was.kind !== 'pan' && at && was.item && (at.x !== was.item.x || at.y !== was.item.y || at.z !== was.item.z)) {
        void place(at.id, { x: at.x, y: at.y, z: at.z });
      } else if (was.kind !== 'pan') {
        moving = null;
      }
      return;
    }
    if (was.kind === 'lift') return;
    const hit = (e.target as Element).closest('[data-item]');
    selected = hit?.getAttribute('data-item') ?? null;
  }
  function ondblclick(e: MouseEvent) {
    const hit = (e.target as Element).closest('[data-item]');
    const item = scene?.items.find((i) => i.id === hit?.getAttribute('data-item'));
    if (item) open(item);
  }
  function onkey(e: KeyboardEvent, item: SceneItem) {
    if (arranging && item.movable && nudge(item, e.key)) {
      e.preventDefault();
    } else if (e.key === 'Enter') {
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
      <a class="btn-secondary" href={sceneImage(scene.location.id, 'svg', shown)} download="{scene.location.name}.svg">Save as SVG</a>
      <a class="btn-secondary" href={sceneImage(scene.location.id, 'png', shown)} download="{scene.location.name}.png">Save as PNG</a>
      {#if canArrange}
        <button type="button" class="btn-secondary" aria-pressed={arranging} onclick={() => (arranging = !arranging)}>Arrange</button>
      {/if}
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
        {#each items as item (item.id)}
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
        {#each items.filter((i) => i.overlaps?.length) as item (item.id)}
          <polygon
            points={points(hull(item))}
            fill="none"
            stroke-width="1.5"
            stroke-dasharray="2 2"
            class="stroke-red-600 dark:stroke-red-400"
            pointer-events="none"
            data-overlap={item.id}
          />
        {/each}
        {#each items.filter((i) => i.id === shown || i.id === selected) as item (item.id)}
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
        {#if lift}
          <g data-lift class="cursor-ns-resize">
            <title>Lift</title>
            <line x1={n(lift.x)} y1={n(lift.y)} x2={n(lift.x)} y2={n(lift.top)} class="stroke-zinc-500 dark:stroke-zinc-300" stroke-width="1.5" pointer-events="none" />
            <circle cx={n(lift.x)} cy={n(lift.top)} r="7" class="fill-white stroke-zinc-600 dark:fill-zinc-900 dark:stroke-zinc-300" stroke-width="1.5" />
            <path d="M{n(lift.x - 3)} {n(lift.top - 1)}l3 -3l3 3M{n(lift.x - 3)} {n(lift.top + 1)}l3 3l3 -3" fill="none" class="stroke-zinc-600 dark:stroke-zinc-300" stroke-width="1.2" pointer-events="none" />
          </g>
        {/if}
      </svg>

      <aside aria-label="Chosen" class="card text-sm">
        {#if chosen}
          <h2 class="text-base font-semibold">{chosen.name}</h2>
          <dl class="mt-2 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1">
            {#if chosen.code}<dt class="text-zinc-600 dark:text-zinc-400">Code</dt><dd class="font-mono">{chosen.code}</dd>{/if}
            <dt class="text-zinc-600 dark:text-zinc-400">Condition</dt><dd>{conditionLabel(chosen.condition)}</dd>
            {#if chosen.holds}<dt class="text-zinc-600 dark:text-zinc-400">Holds</dt><dd>{chosen.holds}</dd>{/if}
            <dt class="text-zinc-600 dark:text-zinc-400">Stands at</dt>
            <dd data-at>{position(chosen)}{chosen.arranged ? '' : ', packed with the rest'}</dd>
          </dl>
          {#if overlapNote(chosen, items)}<p class="mt-2 text-red-700 dark:text-red-400">{overlapNote(chosen, items)}</p>{/if}
          <div class="mt-3 flex flex-wrap gap-2">
            <a class="btn-secondary" href="/assets/{encodeURIComponent(chosen.id)}">Open</a>
            {#if chosen.holds}<a class="btn-secondary" href="/assets/{encodeURIComponent(chosen.id)}/scene">Look inside</a>{/if}
            {#if arranging && chosen.movable && chosen.arranged}
              {@const id = chosen.id}
              <button type="button" class="btn-secondary" onclick={() => place(id, null)}>Put back</button>
            {/if}
          </div>
        {:else}
          <p class="text-zinc-600 dark:text-zinc-400">Choose something to see what it is. A double click opens it, or goes inside.</p>
        {/if}
        {#if arranging}
          <p class="mt-3 text-zinc-600 dark:text-zinc-400">
            Drag things to move them a tile at a time, and lift the chosen one by its handle. The arrow keys move it too, and Page Up and Page Down lift and lower it.
            <strong>Put back</strong> packs it with the rest again.
          </p>
        {/if}
      </aside>
    </div>
  {/if}
{/if}

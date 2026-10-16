<script lang="ts">
  import type { ChartSeries } from '../lib/models';

  // A report's chart: bars, a donut or lines, with its data as a table for
  // anyone who cannot see it.
  let { kind, series, title }: { kind: 'bar' | 'donut' | 'line'; series: ChartSeries[]; title: string } = $props();

  // Fills and strokes, in order; listed whole so Tailwind keeps them.
  const fills = ['fill-sky-600', 'fill-amber-500', 'fill-emerald-600', 'fill-rose-600', 'fill-violet-600', 'fill-lime-600', 'fill-orange-600', 'fill-cyan-600'];
  const strokes = ['stroke-sky-600', 'stroke-amber-500', 'stroke-emerald-600', 'stroke-rose-600', 'stroke-violet-600', 'stroke-lime-600', 'stroke-orange-600', 'stroke-cyan-600'];
  const backgrounds = ['bg-sky-600', 'bg-amber-500', 'bg-emerald-600', 'bg-rose-600', 'bg-violet-600', 'bg-lime-600', 'bg-orange-600', 'bg-cyan-600'];

  const points = $derived(series[0]?.points ?? []);
  const max = $derived(Math.max(1, ...series.flatMap((s) => s.points.map((p) => p.value))));
  const total = $derived(points.reduce((sum, p) => sum + p.value, 0));
  const summary = $derived(
    kind === 'line'
      ? series.map((s) => `${s.label}: ${s.points.map((p) => `${p.label} ${p.value}`).join(', ')}`).join('; ')
      : points.map((p) => `${p.label} ${p.value}`).join(', ') || 'nothing to show',
  );

  // The donut's slices, as arcs of a circle of radius 40 around (50, 50).
  const slices = $derived.by(() => {
    let angle = -Math.PI / 2;
    return points
      .filter((p) => p.value > 0)
      .map((p, i) => {
        const sweep = (p.value / Math.max(total, 1)) * Math.PI * 2;
        const start = angle;
        angle += sweep;
        const at = (a: number) => `${50 + 40 * Math.cos(a)} ${50 + 40 * Math.sin(a)}`;
        // A single slice is the whole ring: two halves, since an arc cannot
        // end where it starts.
        const d =
          sweep >= Math.PI * 2 - 1e-9
            ? `M ${at(start)} A 40 40 0 1 1 ${at(start + Math.PI)} A 40 40 0 1 1 ${at(start)}`
            : `M ${at(start)} A 40 40 0 ${sweep > Math.PI ? 1 : 0} 1 ${at(angle)}`;
        return { d, label: p.label, value: p.value, colour: i % strokes.length };
      });
  });

  // The line chart: 300 wide, 120 high, with room for labels below.
  const width = 300;
  const height = 120;
  const x = (i: number) => (points.length < 2 ? width / 2 : (i / (points.length - 1)) * (width - 20) + 10);
  const y = (v: number) => height - 10 - (v / max) * (height - 20);
</script>

<figure class="max-w-2xl">
  {#if kind === 'bar'}
    <div role="img" aria-label="Bar chart of {title || series[0]?.label}: {summary}" class="space-y-1">
      {#each points as p, i (p.label)}
        <div class="grid grid-cols-[minmax(6rem,35%)_1fr_auto] items-center gap-2 text-sm" aria-hidden="true">
          <span class="truncate" title={p.label}>{p.label}</span>
          <span class="h-4 rounded-sm bg-zinc-100 dark:bg-zinc-800">
            <span class="block h-4 rounded-sm {backgrounds[i % backgrounds.length]}" style="width: {(p.value / max) * 100}%"></span>
          </span>
          <span class="tabular-nums">{p.value}</span>
        </div>
      {:else}
        <p class="text-sm text-zinc-600 dark:text-zinc-400">Nothing to show.</p>
      {/each}
    </div>
  {:else if kind === 'donut'}
    <div class="flex flex-wrap items-center gap-6">
      <svg viewBox="0 0 100 100" class="h-40 w-40" role="img" aria-label="Donut chart of {title || series[0]?.label}: {summary}">
        <circle cx="50" cy="50" r="40" class="fill-none stroke-zinc-200 dark:stroke-zinc-700" stroke-width="18" />
        {#each slices as s (s.label)}
          <path d={s.d} class="fill-none {strokes[s.colour]}" stroke-width="18" />
        {/each}
        <text x="50" y="54" text-anchor="middle" class="fill-current text-[12px] font-semibold">{total}</text>
      </svg>
      <ul class="space-y-1 text-sm" aria-hidden="true">
        {#each slices as s (s.label)}
          <li class="flex items-center gap-2">
            <span class="inline-block h-3 w-3 rounded-sm {backgrounds[s.colour]}"></span>{s.label}: <span class="tabular-nums">{s.value}</span>
          </li>
        {/each}
      </ul>
    </div>
  {:else}
    <svg viewBox="0 0 {width} {height + 18}" class="w-full" role="img" aria-label="Line chart of {title || 'history'}: {summary}">
      <line x1="10" y1={height - 10} x2={width - 10} y2={height - 10} class="stroke-zinc-300 dark:stroke-zinc-600" stroke-width="0.5" />
      {#each series as s, si (s.label)}
        <polyline
          points={s.points.map((p, i) => `${x(i)},${y(p.value)}`).join(' ')}
          class="fill-none {strokes[si % strokes.length]}"
          stroke-width="1.5"
        />
        {#each s.points as p, i (i)}
          <circle cx={x(i)} cy={y(p.value)} r="1.8" class={fills[si % fills.length]} />
        {/each}
      {/each}
      {#each points as p, i (i)}
        {#if (points.length - 1 - i) % 2 === 0 || points.length < 7}
          <text x={x(i)} y={height + 6} text-anchor="middle" class="fill-current text-[7px]">{p.label}</text>
        {/if}
      {/each}
      <text x="10" y="8" class="fill-current text-[7px]">{max}</text>
    </svg>
    {#if series.length > 1}
      <ul class="mt-1 flex flex-wrap gap-4 text-sm" aria-hidden="true">
        {#each series as s, si (s.label)}
          <li class="flex items-center gap-2"><span class="inline-block h-3 w-3 rounded-sm {backgrounds[si % backgrounds.length]}"></span>{s.label}</li>
        {/each}
      </ul>
    {/if}
  {/if}

  <details class="mt-2 text-sm print:hidden">
    <summary class="cursor-pointer">Data</summary>
    <table class="mt-1 text-left">
      <thead>
        <tr>
          <th scope="col" class="pr-4">{kind === 'line' ? 'Period' : 'Group'}</th>
          {#each series as s (s.label)}<th scope="col" class="pr-4">{s.label}</th>{/each}
        </tr>
      </thead>
      <tbody>
        {#each points as p, i (i)}
          <tr>
            <th scope="row" class="pr-4 font-normal">{p.label}</th>
            {#each series as s (s.label)}<td class="pr-4 tabular-nums">{s.points[i]?.value ?? 0}</td>{/each}
          </tr>
        {/each}
      </tbody>
    </table>
  </details>
</figure>

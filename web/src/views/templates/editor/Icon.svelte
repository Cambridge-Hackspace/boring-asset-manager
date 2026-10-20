<script lang="ts" module>
  // Line icons on a 20-unit grid, drawn in the current color.
  const paths = {
    select: 'M5 3l10 7-4.5 1 2.5 5-2 1-2.5-5L5 15z',
    text: 'M4 5V3.5h12V5M10 3.5v13M7.5 16.5h5',
    code: 'M3 3h5v5H3zM12 3h5v5h-5zM3 12h5v5H3zM12 12h2v2h-2zM15 15h2v2h-2zM12 16h1M16 12h1M5 5h1v1H5zM14 5h1v1h-1zM5 14h1v1H5z',
    image: 'M3 4h14v12H3zM3 13l4-4 3 3 2-2 5 5M12.5 7.5a1 1 0 1 0 0 .01',
    rectangle: 'M3.5 5h13v10h-13z',
    ellipse: 'M10 4.5c3.9 0 7 2.5 7 5.5s-3.1 5.5-7 5.5S3 13 3 10s3.1-5.5 7-5.5z',
    line: 'M4 16L16 4',
    polygon: 'M10 3l7 5-2.7 8.5H5.7L3 8z',
    import: 'M10 13V3M6 7l4-4 4 4M4 13v4h12v-4',
    undo: 'M7 5L3 9l4 4M3 9h9a5 5 0 0 1 0 10h-2',
    redo: 'M13 5l4 4-4 4M17 9H8a5 5 0 0 0 0 10h2',
    chevron: 'M6 8l4 4 4-4',
    up: 'M6 12l4-4 4 4',
    down: 'M6 8l4 4 4-4',
    lock: 'M5 9h10v8H5zM7 9V6.5a3 3 0 0 1 6 0V9',
    unlock: 'M5 9h10v8H5zM7 9V6.5a3 3 0 0 1 5.7-1.3',
    expand: 'M3 7V3h4M13 3h4v4M17 13v4h-4M7 17H3v-4',
    front: 'M7 7h10v10H7zM3 3h10v2M3 3v10h2',
    back: 'M3 3h10v10H3zM15 7h2v10H7v-2',
    forward: 'M10 16V4M5 9l5-5 5 5',
    backward: 'M10 4v12M5 11l5 5 5-5',
    trash: 'M4 6h12M8 6V4h4v2M6 6l1 11h6l1-11',
    turnLeft: 'M8 4L4 8l4 4M4 8h7a5 5 0 0 1 5 5v3',
    turnRight: 'M12 4l4 4-4 4M16 8H9a5 5 0 0 0-5 5v3',
    flipAcross: 'M10 2v16M8 5L3 15h5zM12 5l5 10h-5z',
    flipDown: 'M2 10h16M5 8L15 3v5zM5 12l10 5v-5z',
    group: 'M3 3h6v6H3zM11 11h6v6h-6zM9 6h4v5M11 14H6V9',
  } as const;
  export type IconName = keyof typeof paths;
</script>

<script lang="ts">
  let { name, size = 18 }: { name: IconName; size?: number } = $props();
</script>

<svg
  aria-hidden="true"
  width={size}
  height={size}
  viewBox="0 0 20 20"
  fill="none"
  stroke="currentColor"
  stroke-width="1.5"
  stroke-linecap="round"
  stroke-linejoin="round"><path d={paths[name]} /></svg
>

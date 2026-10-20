<script lang="ts" generics="T extends string">
  // A color chosen from swatches, as Figma's and Inkscape's fills are: one
  // square of each color the printer has, the one chosen ringed, and none a
  // white square struck through in red. Radio buttons underneath, so the
  // arrow keys move through them as through any choice of one.
  let { label, name, value, options, onchange }: { label: string; name: string; value: T; options: [T, string][]; onchange: (value: T) => void } = $props();
</script>

<div class="flex items-center gap-1 text-xs" role="radiogroup" aria-label={label}>
  <span class="text-zinc-600 dark:text-zinc-400" aria-hidden="true">{label}</span>
  {#each options as [v, title] (v)}
    <label class="relative cursor-pointer" {title}>
      <input type="radio" class="peer sr-only" {name} value={v} checked={value === v} onchange={() => onchange(v)} aria-label={title} />
      <span
        class="swatch block size-5 rounded border border-zinc-400 peer-checked:ring-2 peer-checked:ring-blue-600 peer-checked:ring-offset-1 peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-blue-600 dark:border-zinc-500 dark:peer-checked:ring-offset-zinc-950"
        class:bg-black={v === 'black'}
        class:bg-white={v !== 'black'}
        class:none={v === 'none'}
      ></span>
    </label>
  {/each}
</div>

<style>
  .none {
    background-image: linear-gradient(to top right, transparent calc(50% - 1px), rgb(220 38 38) calc(50% - 1px), rgb(220 38 38) calc(50% + 1px), transparent calc(50% + 1px));
  }
</style>

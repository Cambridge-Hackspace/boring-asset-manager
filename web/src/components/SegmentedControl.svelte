<script lang="ts" generics="T extends string">
  // Switches between views of one list (#159), such as the print queue's
  // Waiting, Jobs and Printed: one row of pressed-or-not buttons, never the
  // page's primary action.
  let {
    label,
    options,
    value,
    onchange,
  }: { label: string; options: { value: T; label: string }[]; value: T; onchange: (value: T) => void } = $props();
</script>

<div role="group" aria-label={label} class="inline-flex max-w-full flex-wrap overflow-hidden rounded border-2 border-zinc-900 dark:border-zinc-300">
  {#each options as o (o.value)}
    <button
      type="button"
      aria-pressed={o.value === value}
      class="min-h-11 cursor-pointer border-r-2 border-zinc-900 bg-white px-4 text-sm font-medium last:border-r-0 hover:bg-zinc-100 aria-pressed:bg-zinc-900 aria-pressed:font-semibold aria-pressed:text-zinc-50 dark:border-zinc-300 dark:bg-zinc-900 dark:hover:bg-zinc-800 dark:aria-pressed:bg-zinc-100 dark:aria-pressed:text-zinc-900"
      onclick={() => onchange(o.value)}
    >
      {o.label}
    </button>
  {/each}
</div>

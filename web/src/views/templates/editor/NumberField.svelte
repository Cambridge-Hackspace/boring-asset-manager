<script lang="ts">
  // A number in a panel or the options bar, its name beside it, changed
  // when it is entered (Enter, or leaving the field).
  let {
    id,
    label,
    value,
    unit = '',
    min,
    max,
    step = 'any',
    title,
    onchange,
  }: {
    id: string;
    label: string;
    value: number | undefined;
    unit?: string;
    min?: number;
    max?: number;
    step?: number | 'any';
    title?: string;
    onchange: (value: number) => void;
  } = $props();
</script>

<label class="flex items-center gap-1 text-xs" for={id} {title}>
  <span class="shrink-0 text-zinc-600 dark:text-zinc-400">{label}</span>
  <input
    {id}
    class="input-sm w-full"
    type="number"
    {min}
    {max}
    {step}
    value={value ?? ''}
    onchange={(ev) => {
      const n = Number(ev.currentTarget.value);
      if (ev.currentTarget.value !== '' && Number.isFinite(n)) onchange(n);
    }}
  />
  {#if unit}<span class="shrink-0 text-zinc-600 dark:text-zinc-400">{unit}</span>{/if}
</label>

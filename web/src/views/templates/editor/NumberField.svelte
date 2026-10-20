<script lang="ts">
  import type { Units } from '../../../lib/template';
  import { parseLength } from './transform';

  // A number in a panel or the options bar, its name beside it, changed
  // when it is entered (Enter, or leaving the field). A length takes its
  // unit typed with it, "5mm" or "0.2in", converted to the template's, as
  // Krita's unit fields do. The arrow keys step it, ten steps with Shift.
  let {
    id,
    label,
    value,
    unit = '',
    length,
    min,
    max,
    step = 1,
    title,
    onchange,
  }: {
    id: string;
    label: string;
    value: number | undefined;
    unit?: string;
    /** The template's units, when this is a length that takes units typed. */
    length?: Units;
    min?: number;
    max?: number;
    step?: number;
    title?: string;
    onchange: (value: number) => void;
  } = $props();

  function read(text: string): number | null {
    if (text.trim() === '') return null;
    const n = length ? parseLength(text, length) : Number(text);
    return n !== null && Number.isFinite(n) ? n : null;
  }

  function within(n: number): number {
    return Math.min(max ?? Infinity, Math.max(min ?? -Infinity, n));
  }

  function commit(input: HTMLInputElement) {
    const n = read(input.value);
    if (n === null) {
      input.value = value === undefined ? '' : String(value);
      return;
    }
    const v = within(Math.round(n * 1e4) / 1e4);
    input.value = String(v);
    if (v !== value) onchange(v);
  }

  function keydown(event: KeyboardEvent & { currentTarget: HTMLInputElement }) {
    if (event.key === 'Enter') {
      commit(event.currentTarget);
      return;
    }
    if (event.key !== 'ArrowUp' && event.key !== 'ArrowDown') return;
    event.preventDefault();
    const from = read(event.currentTarget.value) ?? value ?? 0;
    const by = (event.key === 'ArrowUp' ? 1 : -1) * step * (event.shiftKey ? 10 : 1);
    const v = within(Math.round((from + by) * 1e4) / 1e4);
    event.currentTarget.value = String(v);
    onchange(v);
  }
</script>

<label class="flex items-center gap-1 text-xs" for={id} {title}>
  <span class="shrink-0 text-zinc-600 dark:text-zinc-400">{label}</span>
  <input
    {id}
    class="input-sm w-full"
    type="text"
    inputmode="decimal"
    autocomplete="off"
    spellcheck="false"
    value={value ?? ''}
    onchange={(ev) => commit(ev.currentTarget)}
    onkeydown={keydown}
  />
  {#if unit}<span class="shrink-0 text-zinc-600 dark:text-zinc-400">{unit}</span>{/if}
</label>

<!--
  An editor for isolex source (README §3.7), as a code editor works: a
  plain textarea, so typing, undo, selection and screen readers are the
  browser's own, over a colored copy of the text; line numbers with a
  mark on each line that has a problem, the problem's token underlined;
  completion of node names, properties and the lexicon's names; Tab
  indents (Esc, then Tab, leaves), Enter keeps the indent, Ctrl+/
  comments lines.
-->
<script lang="ts">
  import { tick } from 'svelte';
  import { tokenize, offsetOf, rangeAt, type Problem, type TokenKind } from '../lib/isolex/syntax';
  import { complete, type Completion, type Vocabulary } from '../lib/isolex/complete';
  import { indent, outdent, newline, closeBrace, toggleComment, type Change } from '../lib/isolex/edit';

  interface Props {
    value: string;
    /** What the compiler said of this source. */
    problems?: Problem[];
    vocabulary?: Vocabulary;
    /** The editor's accessible name. */
    label: string;
    id?: string;
    rows?: number;
    readonly?: boolean;
  }

  let {
    value = $bindable(''),
    problems = [],
    vocabulary = { tokens: [], definitions: [], icons: [] },
    label,
    id = 'isolex-' + Math.random().toString(36).slice(2),
    rows = 18,
    readonly = false,
  }: Props = $props();

  let area: HTMLTextAreaElement;
  let overlay: HTMLPreElement;
  let gutter: HTMLDivElement;
  let measure: HTMLSpanElement;

  // Esc, then Tab, moves on rather than indenting, so the keyboard is
  // never kept in here.
  let leaving = false;
  let completion = $state<Completion | null>(null);
  let active = $state(0);
  let popup = $state({ left: 0, top: 0 });

  const LINE = 20; // px, leading-5
  const PAD = 8; // px, p-2

  const colors: Record<TokenKind, string> = {
    comment: 'italic text-zinc-600 dark:text-zinc-400',
    string: 'text-emerald-700 dark:text-emerald-400',
    number: 'text-sky-700 dark:text-sky-400',
    keyword: 'text-violet-700 dark:text-violet-400',
    node: 'text-zinc-950 dark:text-white',
    property: 'text-amber-800 dark:text-amber-400',
    punct: 'text-zinc-600 dark:text-zinc-400',
    ident: 'text-zinc-800 dark:text-zinc-200',
    space: '',
  };

  const lines = $derived(value.split('\n').length);

  /** Each problem's range in the text. */
  const ranges = $derived(
    problems.map((p) => {
      const [from, to] = rangeAt(value, offsetOf(value, p.line, p.col));
      return { from, to, severity: p.severity };
    }),
  );

  const marks = $derived.by(() => {
    const byLine = new Map<number, 'error' | 'warning'>();
    for (const p of problems) {
      if (byLine.get(p.line) !== 'error') byLine.set(p.line, p.severity);
    }
    return byLine;
  });

  /** The text as colored pieces, split where a problem's range starts or ends. */
  const segments = $derived.by(() => {
    const cuts = new Set<number>();
    for (const r of ranges) cuts.add(r.from).add(r.to);
    const out: { text: string; cls: string }[] = [];
    for (const t of tokenize(value)) {
      const points = [t.start, ...[...cuts].filter((c) => c > t.start && c < t.end).sort((a, b) => a - b), t.end];
      for (let k = 0; k + 1 < points.length; k++) {
        const from = points[k]!;
        const to = points[k + 1]!;
        const hit = ranges.find((r) => r.from <= from && to <= r.to);
        const squiggle = hit
          ? hit.severity === 'error'
            ? ' underline decoration-wavy decoration-red-600 dark:decoration-red-400'
            : ' underline decoration-wavy decoration-amber-500'
          : '';
        out.push({ text: value.slice(from, to), cls: colors[t.kind] + squiggle });
      }
    }
    return out;
  });

  function follow() {
    overlay.scrollTop = area.scrollTop;
    overlay.scrollLeft = area.scrollLeft;
    gutter.scrollTop = area.scrollTop;
  }

  /** Applies a change as one step of the browser's own undo. */
  function apply(c: Change) {
    area.focus();
    area.setSelectionRange(c.from, c.to);
    let done = false;
    if (typeof document.execCommand === 'function') {
      done = c.insert === '' ? c.from === c.to || document.execCommand('delete') : document.execCommand('insertText', false, c.insert);
    }
    if (!done) {
      area.setRangeText(c.insert, c.from, c.to, 'end');
      area.dispatchEvent(new Event('input', { bubbles: true }));
    }
    value = area.value;
    area.setSelectionRange(c.selStart, c.selEnd);
  }

  function suggest() {
    const caret = area.selectionStart;
    completion = area.selectionStart === area.selectionEnd ? complete(value, caret, vocabulary) : null;
    active = 0;
    if (!completion) return;
    // under the start of what is being completed, as a code editor puts it
    const before = value.slice(0, completion.from);
    const line = before.split('\n').length - 1;
    const col = before.length - (before.lastIndexOf('\n') + 1);
    const ch = measure?.getBoundingClientRect().width / 10 || 8;
    popup = { left: PAD + col * ch - area.scrollLeft, top: PAD + (line + 1) * LINE - area.scrollTop };
  }

  function accept() {
    const c = completion;
    if (!c) return;
    const s = c.items[active]!;
    const at = c.from;
    completion = null;
    apply({
      from: c.from,
      to: c.to,
      insert: s.insert,
      selStart: s.select ? at + s.select[0] : at + s.insert.length,
      selEnd: s.select ? at + s.select[1] : at + s.insert.length,
    });
  }

  function onkeydown(e: KeyboardEvent) {
    if (readonly) return;
    const s = area.selectionStart;
    const end = area.selectionEnd;
    if (completion) {
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        e.preventDefault();
        const n = completion.items.length;
        active = (active + (e.key === 'ArrowDown' ? 1 : n - 1)) % n;
        return;
      }
      if (e.key === 'Enter' || e.key === 'Tab') {
        e.preventDefault();
        accept();
        return;
      }
      if (e.key === 'Escape') {
        e.preventDefault();
        completion = null;
        return;
      }
    }
    if (e.key === ' ' && e.ctrlKey) {
      e.preventDefault();
      suggest();
      return;
    }
    if (e.key === 'Escape') {
      leaving = true;
      return;
    }
    if (e.key === 'Tab') {
      if (leaving || e.ctrlKey || e.altKey || e.metaKey) {
        leaving = false;
        return; // the browser moves focus on
      }
      e.preventDefault();
      apply(e.shiftKey ? outdent(value, s, end) : indent(value, s, end));
      return;
    }
    leaving = false;
    if (e.key === 'Enter' && !e.shiftKey && !e.ctrlKey && !e.altKey && !e.metaKey && !e.isComposing) {
      e.preventDefault();
      apply(newline(value, s, end));
      return;
    }
    if (e.key === '}') {
      const c = closeBrace(value, s, end);
      if (c) {
        e.preventDefault();
        apply(c);
      }
      return;
    }
    if (e.key === '/' && (e.ctrlKey || e.metaKey)) {
      e.preventDefault();
      apply(toggleComment(value, s, end));
    }
  }

  function oninput(e: Event) {
    const typed = (e as InputEvent).data;
    if ((e as InputEvent).inputType === 'insertText' && typed && /[A-Za-z0-9_."=-]$/u.test(typed)) {
      suggest();
    } else {
      completion = null;
    }
  }

  /** Puts the caret on a problem, and it in view. */
  export async function reveal(p: Problem) {
    const [from, to] = rangeAt(value, offsetOf(value, p.line, p.col));
    area.focus();
    area.setSelectionRange(from, to);
    area.scrollTop = Math.max(0, (p.line - 1) * LINE - area.clientHeight / 2);
    await tick();
    follow();
  }

  const listId = $derived(id + '-suggestions');
  const helpId = $derived(id + '-help');
  const problemsId = $derived(id + '-problems');
  const errors = $derived(problems.filter((p) => p.severity === 'error').length);
</script>

<div class="min-w-0">
  <div class="relative flex rounded-md border border-zinc-300 bg-white font-mono text-[13px] leading-5 dark:border-zinc-700 dark:bg-zinc-950">
    <div
      bind:this={gutter}
      aria-hidden="true"
      class="shrink-0 overflow-hidden rounded-l-md border-r border-zinc-200 bg-zinc-50 py-2 text-right text-zinc-500 select-none dark:border-zinc-800 dark:bg-zinc-900 dark:text-zinc-400"
      style="height: {rows * LINE + 2 * PAD}px"
    >
      {#each { length: lines } as _, i (i)}
        {@const mark = marks.get(i + 1)}
        <div class="flex h-5 items-center justify-end gap-1 pr-2 pl-1">
          {#if mark}<span data-mark={mark} class={mark === 'error' ? 'text-red-700 dark:text-red-400' : 'text-amber-600'}>{mark === 'error' ? '●' : '▲'}</span>{/if}
          <span class="min-w-[2ch] tabular-nums">{i + 1}</span>
        </div>
      {/each}
    </div>
    <div class="relative min-w-0 flex-1">
      <pre
        bind:this={overlay}
        aria-hidden="true"
        data-overlay
        class="pointer-events-none absolute inset-0 m-0 overflow-hidden p-2 whitespace-pre [tab-size:2]">{#each segments as seg, k (k)}<span class={seg.cls}>{seg.text}</span>{/each}{'\n'}</pre>
      <textarea
        bind:this={area}
        bind:value
        {id}
        {readonly}
        aria-label={label}
        aria-describedby="{helpId}{problems.length ? ' ' + problemsId : ''}"
        aria-invalid={errors > 0}
        aria-autocomplete="list"
        aria-controls={completion ? listId : undefined}
        aria-activedescendant={completion ? `${listId}-${active}` : undefined}
        spellcheck="false"
        autocapitalize="off"
        autocomplete="off"
        wrap="off"
        class="relative block w-full resize-none overflow-auto bg-transparent p-2 whitespace-pre text-transparent caret-zinc-900 outline-none [tab-size:2] selection:bg-sky-200/70 focus-visible:outline-none dark:caret-zinc-100 dark:selection:bg-sky-800/70"
        style="height: {rows * LINE + 2 * PAD}px"
        onscroll={follow}
        {onkeydown}
        {oninput}
        onblur={() => (completion = null)}
      ></textarea>
      <span bind:this={measure} aria-hidden="true" class="invisible absolute whitespace-pre">0000000000</span>
      {#if completion}
        <ul
          id={listId}
          role="listbox"
          aria-label="Suggestions"
          class="absolute z-10 max-h-56 min-w-56 overflow-auto rounded-md border border-zinc-300 bg-white py-1 font-sans text-sm shadow-lg dark:border-zinc-700 dark:bg-zinc-900"
          style="left: {popup.left}px; top: {popup.top}px"
        >
          {#each completion.items as s, k (s.label)}
            <li
              id="{listId}-{k}"
              role="option"
              aria-selected={k === active}
              class="flex cursor-pointer items-baseline gap-3 px-2 py-0.5 aria-selected:bg-zinc-200 dark:aria-selected:bg-zinc-700"
              onmousedown={(e) => {
                e.preventDefault();
                active = k;
                accept();
              }}
            >
              <span class="font-mono">{s.label}</span>
              <span class="truncate text-xs text-zinc-600 dark:text-zinc-400">{s.detail}</span>
            </li>
          {/each}
        </ul>
      {/if}
    </div>
  </div>
  <p id={helpId} class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">
    Tab indents; Esc, then Tab, leaves the editor. Ctrl+Space suggests what fits; Ctrl+/ comments lines out.
  </p>
  <div role="status" class="sr-only">{completion ? `${completion.items.length} suggestions` : ''}</div>
  {#if problems.length}
    <div id={problemsId} class="mt-2">
      <h3 class="text-sm font-medium">Problems</h3>
      <ul class="mt-1 space-y-0.5 text-sm">
        {#each problems as p, k (k)}
          <li>
            <button
              type="button"
              class="flex w-full items-baseline gap-2 rounded px-1 text-left hover:bg-zinc-100 dark:hover:bg-zinc-800"
              onclick={() => reveal(p)}
            >
              <span class={p.severity === 'error' ? 'text-red-700 dark:text-red-400' : 'text-amber-700 dark:text-amber-400'} aria-hidden="true"
                >{p.severity === 'error' ? '●' : '▲'}</span
              >
              <span class="shrink-0 font-mono text-xs text-zinc-600 tabular-nums dark:text-zinc-400" aria-hidden="true">{p.line}:{p.col}</span>
              <span class="sr-only">{p.severity === 'error' ? 'Error' : 'Warning'}, line {p.line}, column {p.col}:</span>
              <span>{p.message}</span>
            </button>
          </li>
        {/each}
      </ul>
    </div>
  {/if}
</div>

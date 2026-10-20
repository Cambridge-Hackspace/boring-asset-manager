<script lang="ts" module>
  import type { Snippet } from 'svelte';

  export interface DockPanel {
    id: string;
    title: string;
    content: Snippet;
  }

  const storageKey = 'bam.editor.panels';

  interface Layout {
    order: string[];
    collapsed: string[];
  }

  /** The panels' order and which are collapsed, as last left in this browser. */
  export function readLayout(): Layout {
    try {
      const raw = JSON.parse(localStorage.getItem(storageKey) ?? '{}') as Partial<Layout>;
      return { order: Array.isArray(raw.order) ? raw.order : [], collapsed: Array.isArray(raw.collapsed) ? raw.collapsed : [] };
    } catch {
      return { order: [], collapsed: [] };
    }
  }

  function writeLayout(layout: Layout) {
    try {
      localStorage.setItem(storageKey, JSON.stringify(layout));
    } catch {
      // Storage unavailable: the layout is not remembered.
    }
  }

  /** Panels in the order kept, any not in it after, as given. */
  export function ordered<T extends { id: string }>(panels: T[], order: string[]): T[] {
    const rank = (id: string) => {
      const i = order.indexOf(id);
      return i < 0 ? order.length + panels.findIndex((p) => p.id === id) : i;
    };
    return [...panels].sort((a, b) => rank(a.id) - rank(b.id));
  }
</script>

<script lang="ts">
  import Icon from './Icon.svelte';

  // The panels down the right, as Krita's dockers: each collapses to its
  // title, and moves up or down; the layout is remembered in this browser.
  let { panels }: { panels: DockPanel[] } = $props();

  const initial = readLayout();
  let order = $state<string[]>(initial.order);
  let collapsed = $state<string[]>(initial.collapsed);
  const shown = $derived(ordered(panels, order));

  function save() {
    writeLayout({ order, collapsed });
  }

  function toggle(id: string) {
    collapsed = collapsed.includes(id) ? collapsed.filter((c) => c !== id) : [...collapsed, id];
    save();
  }

  function move(id: string, by: number) {
    const ids = shown.map((p) => p.id);
    const from = ids.indexOf(id);
    const to = from + by;
    if (to < 0 || to >= ids.length) return;
    [ids[from], ids[to]] = [ids[to]!, ids[from]!];
    order = ids;
    save();
  }
</script>

<div class="flex flex-col">
  {#each shown as p, i (p.id)}
    {@const open = !collapsed.includes(p.id)}
    <section class="group border-b border-zinc-200 dark:border-zinc-800" aria-labelledby="panel-{p.id}">
      <h2 class="flex items-center bg-zinc-100 text-xs font-semibold dark:bg-zinc-900">
        <button
          type="button"
          id="panel-{p.id}"
          class="flex flex-1 items-center gap-1 px-2 py-1.5 text-left"
          aria-expanded={open}
          onclick={() => toggle(p.id)}
        >
          <span class={open ? '' : '-rotate-90'}><Icon name="chevron" size={14} /></span>
          {p.title}
        </button>
        <span class="flex opacity-0 group-focus-within:opacity-100 group-hover:opacity-100">
          <button type="button" class="p-1 disabled:invisible" aria-label="Move {p.title} up" disabled={i === 0} onclick={() => move(p.id, -1)}>
            <Icon name="up" size={14} />
          </button>
          <button type="button" class="p-1 disabled:invisible" aria-label="Move {p.title} down" disabled={i === shown.length - 1} onclick={() => move(p.id, 1)}>
            <Icon name="down" size={14} />
          </button>
        </span>
      </h2>
      <!-- Hidden, not removed, so what is in it is kept while it is collapsed. -->
      <div class="p-2 text-sm" hidden={!open}>{@render p.content()}</div>
    </section>
  {/each}
</div>

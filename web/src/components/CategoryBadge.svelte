<script lang="ts">
  import { textOn } from '../lib/color';
  import { iconset } from '../lib/iconset.svelte';
  import type { Category } from '../lib/models';
  import IsoIcon from './IsoIcon.svelte';

  let { category, href }: { category: Pick<Category, 'id' | 'name' | 'color_hex'>; href?: string } = $props();

  const style = $derived(`background-color: ${category.color_hex}; color: ${textOn(category.color_hex)}`);
  // Its icon (README §3.7), on the page's own background: drawn on the
  // category's color, what the icon paints in that color would vanish.
  $effect(() => {
    void iconset.load();
  });
  const icon = $derived(iconset.forCategory(category.id));
</script>

{#snippet content()}
  {#if icon}<span class="-ml-1.5 inline-flex rounded-full bg-white p-px dark:bg-zinc-950"><IsoIcon {icon} height={14} categoryColor={category.color_hex} /></span>{/if}{category.name}
{/snippet}

<!-- The name always accompanies the color, so color is never the only cue. -->
{#if href}
  <a {href} class="inline-flex items-center gap-1 rounded-full px-2.5 py-0.5 text-xs font-semibold hover:underline" {style}>{@render content()}</a>
{:else}
  <span class="inline-flex items-center gap-1 rounded-full px-2.5 py-0.5 text-xs font-semibold" {style}>{@render content()}</span>
{/if}

<script lang="ts">
  import { textOn } from '../lib/color';
  import type { Category } from '../lib/models';

  let { category, href }: { category: Pick<Category, 'id' | 'name' | 'color_hex'>; href?: string } = $props();

  const style = $derived(`background-color: ${category.color_hex}; color: ${textOn(category.color_hex)}`);
</script>

<!-- The name always accompanies the color, so color is never the only cue. -->
{#if href}
  <a {href} class="inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-semibold hover:underline" {style}>{category.name}</a>
{:else}
  <span class="inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-semibold" {style}>{category.name}</span>
{/if}

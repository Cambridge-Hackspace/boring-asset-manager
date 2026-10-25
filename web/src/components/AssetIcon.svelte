<!--
  The icon an asset is drawn with (README §3.7), as the server resolved
  it: its own or its categories', in its category's color, or the unknown
  icon for a name the build no longer draws. Nothing when it has none, or
  when icons cannot be read here.
-->
<script lang="ts">
  import { iconset } from '../lib/iconset.svelte';
  import type { ResolvedIcon } from '../lib/models';
  import IsoIcon from './IsoIcon.svelte';

  interface Props {
    icon: ResolvedIcon | null | undefined;
    /** Its height, in CSS pixels. */
    height?: number;
    /** What it shows, for those who cannot see it; empty for decoration. */
    label?: string;
  }

  let { icon, height = 32, label = '' }: Props = $props();

  $effect(() => {
    void iconset.load();
  });
  const drawn = $derived(iconset.forAsset(icon));
  const said = $derived(label && icon?.missing ? `${label}: no icon is drawn by the name ${icon.name}` : label);
</script>

{#if drawn}<IsoIcon icon={drawn} {height} label={said} categoryColor={icon?.color ?? ''} />{/if}

<!--
  An asset as a list shows it beside its name: its first photo, with its
  icon (README §3.7) as a chip in the corner, or, with no photo, its icon.
  Nothing when it has neither.
-->
<script lang="ts">
  import { iconset } from '../lib/iconset.svelte';
  import { photoUrl, type AssetWithDetails } from '../lib/models';
  import IsoIcon from './IsoIcon.svelte';

  interface Props {
    asset: Pick<AssetWithDetails, 'asset' | 'photos' | 'resolved_icon'>;
    /** Its size, in CSS pixels. */
    size?: number;
  }

  let { asset, size = 32 }: Props = $props();

  $effect(() => {
    void iconset.load();
  });
  const drawn = $derived(iconset.forAsset(asset.resolved_icon));
  const color = $derived(asset.resolved_icon?.color ?? '');
  const photo = $derived(asset.photos[0]);
</script>

{#if photo}
  <span class="relative inline-block shrink-0 align-middle" style="width: {size}px; height: {size}px">
    <img src={photoUrl(asset.asset.id, photo.id, true)} alt="" class="h-full w-full rounded object-cover" loading="lazy" />
    {#if drawn}
      <span class="absolute -right-1.5 -bottom-1.5 inline-flex rounded-full bg-white p-px ring-1 ring-zinc-300 dark:bg-zinc-950 dark:ring-zinc-600" data-icon-chip>
        <IsoIcon icon={drawn} height={Math.round(size * 0.5)} categoryColor={color} />
      </span>
    {/if}
  </span>
{:else if drawn}
  <span class="inline-grid shrink-0 place-items-center align-middle" style="width: {size}px; height: {size}px">
    <IsoIcon icon={drawn} height={size} categoryColor={color} />
  </span>
{/if}

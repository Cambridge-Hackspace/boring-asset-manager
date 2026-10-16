<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../lib/api';
  import { describeBuild, type BuildInfo } from '../lib/version';

  // Which BAM is running, on every page. Nothing shows if it cannot be read.
  let build = $state<{ text: string; href?: string } | null>(null);

  onMount(async () => {
    try {
      build = describeBuild(await request<BuildInfo>('GET', '/info'));
    } catch {
      build = null;
    }
  });
</script>

{#if build}
  <footer class="mx-auto max-w-7xl px-4 pt-2 pb-6 text-xs text-zinc-600 print:hidden dark:text-zinc-400">
    {#if build.href}<a class="hover:underline" href={build.href} rel="noopener" target="_blank">{build.text}</a>{:else}{build.text}{/if}
  </footer>
{/if}

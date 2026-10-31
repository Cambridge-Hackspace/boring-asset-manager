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
  <footer class="mt-12 bg-zinc-900 text-zinc-100 print:hidden dark:border-t-2 dark:border-zinc-700 dark:bg-zinc-950">
    <div class="mx-auto flex min-h-14 max-w-7xl items-center px-4 font-mono text-xs">
      {#if build.href}<a class="inline-flex min-h-11 items-center text-highlight underline underline-offset-4" href={build.href} rel="noopener" target="_blank">{build.text}</a>{:else}{build.text}{/if}
    </div>
  </footer>
{/if}

<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../lib/api';
  import { session } from '../lib/session.svelte';
  import { appearance, type Theme } from '../lib/theme.svelte';
  import { describeBuild, type BuildInfo } from '../lib/version';

  // Which BAM is running, on every page, if it can be read; and light or
  // dark, or the system's (#168).
  let build = $state<{ text: string; href?: string } | null>(null);
  let failed = $state('');

  const themes: { value: Theme; label: string }[] = [
    { value: 'light', label: 'Light' },
    { value: 'system', label: 'System' },
    { value: 'dark', label: 'Dark' },
  ];

  onMount(async () => {
    try {
      build = describeBuild(await request<BuildInfo>('GET', '/info'));
    } catch {
      build = null;
    }
  });

  async function choose(theme: Theme) {
    failed = '';
    try {
      await appearance.choose(theme, !!session.user);
    } catch {
      failed = 'Not saved to your profile; it holds in this browser.';
    }
  }
</script>

<footer class="mt-12 bg-zinc-900 text-zinc-100 print:hidden dark:border-t-2 dark:border-zinc-700 dark:bg-zinc-950">
  <div class="mx-auto flex min-h-14 max-w-7xl flex-wrap items-center justify-between gap-x-6 gap-y-2 px-4 py-1.5 font-mono text-xs">
    {#if build?.href}<a class="inline-flex min-h-11 items-center text-highlight underline underline-offset-4" href={build.href} rel="noopener" target="_blank">{build.text}</a>{:else if build}<span>{build.text}</span>{:else}<span></span>{/if}
    <div class="flex flex-wrap items-center gap-x-3 gap-y-1">
      {#if failed}<span role="status">{failed}</span>{/if}
      <div role="group" aria-label="Appearance" class="inline-flex overflow-hidden rounded border-2 border-zinc-100 dark:border-zinc-300">
        {#each themes as t (t.value)}
          <button
            type="button"
            aria-pressed={appearance.theme === t.value}
            class="min-h-11 cursor-pointer border-r-2 border-zinc-100 px-3 last:border-r-0 hover:bg-zinc-800 aria-pressed:bg-zinc-100 aria-pressed:font-semibold aria-pressed:text-zinc-900 dark:border-zinc-300 dark:aria-pressed:bg-zinc-300"
            onclick={() => choose(t.value)}
          >
            {t.label}
          </button>
        {/each}
      </div>
    </div>
  </div>
</footer>

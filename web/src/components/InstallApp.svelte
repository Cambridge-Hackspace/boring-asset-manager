<script lang="ts">
  // Installing BAM! as an app of its own (docs/installing.md#on-a-phone):
  // a button where the browser offers to, as Chrome on Android does, and on
  // an iPhone or iPad, where none does, how Share › Add to Home Screen does
  // it. Nothing once it is installed.
  import { installedApp } from '../lib/install/app.svelte';

  let declined = $state(false);

  async function install() {
    declined = !(await installedApp.install());
  }
</script>

{#if !installedApp.standalone && (installedApp.installable || installedApp.ios)}
  <section class="card max-w-2xl space-y-2" aria-labelledby="install-heading">
    <h2 id="install-heading" class="text-lg font-bold">Install BAM!</h2>
    {#if installedApp.installable}
      <p class="text-sm">Put BAM! on this device's home screen, to open it in a window of its own, at once, even with no signal.</p>
      <button type="button" class="btn-secondary" onclick={install}>Install</button>
    {:else}
      <p class="text-sm">
        To put BAM! on the home screen, open it in Safari, tap <strong>Share</strong>, then
        <strong>Add to Home Screen</strong>. It then opens in a window of its own, at once, even with no signal.
      </p>
    {/if}
    {#if declined}<p class="text-sm text-zinc-600 dark:text-zinc-400" role="status">Not installed. The browser's menu offers it again.</p>{/if}
    <p class="text-sm">
      <a class="inline-flex min-h-11 items-center underline" href="/help/install">More about installing</a>
    </p>
  </section>
{/if}

<script lang="ts">
  import { onMount } from 'svelte';
  import { navigate } from '../lib/router.svelte';
  import { scanTarget } from '../lib/scan';
  import CodeScanner from '../components/CodeScanner.svelte';

  // /scan: looking a code up, by the camera where the browser can read
  // codes, or typed, pasted or from a USB scanner (README §6.4).
  let { params: _params }: { params: Record<string, string> } = $props();

  let code = $state('');
  let input: HTMLInputElement | undefined = $state();

  onMount(() => input?.focus());

  function go(text: string) {
    const to = scanTarget(text);
    if (to) navigate(to);
  }

  function lookUp(event: SubmitEvent) {
    event.preventDefault();
    go(code);
  }
</script>

<h1 class="mb-4 text-2xl font-bold">Scan</h1>
<div class="max-w-md space-y-4">
  <form class="card space-y-3" onsubmit={lookUp}>
    <label class="label" for="scan-code">Code</label>
    <div class="flex gap-2">
      <input id="scan-code" bind:this={input} class="input" autocomplete="off" placeholder="HS-100, or a scan link" bind:value={code} />
      <button type="submit" class="btn-primary" disabled={!code.trim()}>Look up</button>
    </div>
    <p class="text-xs text-zinc-600 dark:text-zinc-400">Type or paste the code on a label. A USB scanner types it here too.</p>
  </form>

  <section class="card space-y-3" aria-labelledby="camera-heading">
    <h2 id="camera-heading" class="text-lg font-semibold">Camera</h2>
    <CodeScanner
      onfound={go}
      unsupported="This browser cannot read codes with the camera. A phone’s own camera app opens labels whose QR codes link to their scan page."
    />
  </section>
</div>

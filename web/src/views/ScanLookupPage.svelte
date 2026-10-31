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

<h1 class="mb-6 text-3xl font-bold">Scan a label</h1>
<!-- The camera first, as on a phone; typing, or a USB scanner, beneath. -->
<div class="max-w-md space-y-6">
  <section aria-label="Camera">
    <CodeScanner
      onfound={go}
      unsupported="This browser cannot read codes with the camera. A phone’s own camera app opens labels whose QR codes link to their scan page."
    />
  </section>

  <form class="space-y-2" onsubmit={lookUp}>
    <label class="label" for="scan-code">Or type the code</label>
    <div class="flex gap-2">
      <input id="scan-code" bind:this={input} class="input font-mono" autocomplete="off" placeholder="HS-100, or a scan link" bind:value={code} />
      <button type="submit" class="btn-primary shrink-0 whitespace-nowrap" disabled={!code.trim()}>Look up</button>
    </div>
    <p class="text-xs text-zinc-600 dark:text-zinc-400">Type or paste the code on a label. A USB scanner types it here too.</p>
  </form>
</div>

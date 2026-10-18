<script lang="ts">
  import { onDestroy, onMount, tick } from 'svelte';
  import { navigate } from '../lib/router.svelte';
  import { scanTarget } from '../lib/scan';

  // /scan: looking a code up, by the camera where the browser can read
  // codes, or typed, pasted or from a USB scanner (README §6.4).
  let { params: _params }: { params: Record<string, string> } = $props();

  interface Detected {
    rawValue: string;
  }
  interface Detector {
    detect(source: HTMLVideoElement): Promise<Detected[]>;
  }
  interface DetectorClass {
    new (options: { formats: string[] }): Detector;
    getSupportedFormats(): Promise<string[]>;
  }

  const Detector = (globalThis as { BarcodeDetector?: DetectorClass }).BarcodeDetector;
  const canScan = Boolean(Detector && navigator.mediaDevices?.getUserMedia);

  let code = $state('');
  let input: HTMLInputElement | undefined = $state();
  let video: HTMLVideoElement | undefined = $state();
  let stream: MediaStream | null = null;
  let scanning = $state(false);
  let cameraError = $state('');
  let frame = 0;

  onMount(() => input?.focus());
  onDestroy(stop);

  function go(text: string) {
    const to = scanTarget(text);
    if (!to) return;
    stop();
    navigate(to);
  }

  function lookUp(event: SubmitEvent) {
    event.preventDefault();
    go(code);
  }

  async function start() {
    if (!Detector) return;
    cameraError = '';
    try {
      const supported = await Detector.getSupportedFormats();
      const formats = ['qr_code', 'micro_qr_code', 'code_128'].filter((f) => supported.includes(f));
      const detector = new Detector({ formats: formats.length ? formats : supported });
      stream = await navigator.mediaDevices.getUserMedia({ video: { facingMode: 'environment' } });
      scanning = true;
      await tick();
      if (!video) return;
      video.srcObject = stream;
      await video.play();
      const look = async () => {
        if (!scanning || !video) return;
        try {
          const found = await detector.detect(video);
          if (found[0]?.rawValue) {
            go(found[0].rawValue);
            return;
          }
        } catch {
          // A frame it could not read: try the next.
        }
        frame = requestAnimationFrame(look);
      };
      frame = requestAnimationFrame(look);
    } catch {
      stop();
      cameraError = 'The camera could not be opened. Allow this site to use it, or type the code instead.';
    }
  }

  function stop() {
    scanning = false;
    cancelAnimationFrame(frame);
    stream?.getTracks().forEach((t) => t.stop());
    stream = null;
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

  {#if canScan}
    <section class="card space-y-3" aria-labelledby="camera-heading">
      <h2 id="camera-heading" class="text-lg font-semibold">Camera</h2>
      {#if scanning}
        <!-- svelte-ignore a11y_media_has_caption -->
        <video bind:this={video} class="w-full rounded bg-black" playsinline muted></video>
        <button type="button" class="btn-secondary" onclick={stop}>Stop</button>
      {:else}
        <button type="button" class="btn-secondary" onclick={start}>Scan with the camera</button>
      {/if}
      {#if cameraError}<p class="error-text">{cameraError}</p>{/if}
    </section>
  {:else}
    <p class="text-sm text-zinc-600 dark:text-zinc-400">
      This browser cannot read codes with the camera. A phone’s own camera app opens labels whose QR codes link to their scan page.
    </p>
  {/if}
</div>
